// 业务说明：本文件是站点多用户鉴权的 HTTP 层，实现「强制登录 + 首次建管理员 + 角色」这条主线。
// 采用服务端会话（Cookie session）+ 同步器式 CSRF 令牌：cookie 存不可读的随机会话令牌，
// DB 存其 SHA-256；改写类请求需在 X-CSRF-Token 头回传会话绑定的 CSRF 令牌。角色分 admin（全权）
// 与 regular（只读浏览 + 记录本人进度/书签/短评）。authGate 是全 /api 组统一的鉴权中间件。
// 维护要点：密码只经 bcrypt；Claude 不代填密码——初始密码由管理员设置、用户首登改密（must_change_password）。

package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"manga-manager/internal/database"
)

const (
	sessionCookieName = "mm_session"
	// 会话有效期 30 天，滑动续期；为避免每请求写库，仅在距上次活跃超过 sessionTouchAfter 时续期。
	sessionTTL        = 30 * 24 * time.Hour
	sessionTouchAfter = time.Hour
	minPasswordLen    = 8
)

// authCtxKey 是鉴权中间件写入请求上下文的键类型（独立类型避免与其他包的 context key 冲突）。
type authCtxKey int

const (
	userCtxKey authCtxKey = iota
	sessionCtxKey
)

// userFromContext 取出 authGate 解析出的当前登录用户。
func userFromContext(ctx context.Context) (database.User, bool) {
	u, ok := ctx.Value(userCtxKey).(database.User)
	return u, ok
}

// sessionFromContext 取出当前会话。
func sessionFromContext(ctx context.Context) (database.Session, bool) {
	s, ok := ctx.Value(sessionCtxKey).(database.Session)
	return s, ok
}

// ---- 令牌 / 口令 / Cookie 基础工具 ----

// generateToken 生成 32 字节的加密随机令牌（URL-safe base64，无填充）。
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// hashSessionID 返回会话令牌的 SHA-256 十六进制串，作为 sessions 表主键（DB 不存明文令牌）。
func hashSessionID(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func hashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

func verifyPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

func isTLS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   isTLS(r),
		SameSite: http.SameSiteLaxMode,
		Expires:  expires,
		MaxAge:   int(time.Until(expires).Seconds()),
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   isTLS(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

// ---- 中间件 ----

func isMutating(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

// isPublicAuthPath 是无需会话即可访问的公开鉴权端点：状态探测、首次建管理员、登录。
func isPublicAuthPath(p string) bool {
	switch p {
	case "/api/auth/status", "/api/auth/setup", "/api/auth/login":
		return true
	}
	return false
}

// isRegularWritablePath 判断某改写类请求是否属于「普通用户也可执行」的个人操作：
// 自身账户（登出/改密）与本人阅读状态（进度、书签）。其余改写一律要求管理员。
func isRegularWritablePath(p string) bool {
	switch p {
	case "/api/auth/logout", "/api/auth/change-password":
		return true
	case "/api/books/bulk-progress", "/api/books/bulk-progress/sync", "/api/series/bulk-progress":
		return true
	}
	if strings.HasPrefix(p, "/api/books/") {
		if strings.HasSuffix(p, "/progress") || strings.Contains(p, "/bookmarks") {
			return true
		}
	}
	return false
}

// usersExist 报告站点是否已存在账户；一旦为真即缓存，避免每请求 COUNT。
// 出错时按「已存在」处理（fail-closed）：公开鉴权端点在 authGate 中先于此判断放行，故首启 setup 不受影响。
func (c *Controller) usersExist(ctx context.Context) bool {
	if c.usersPresent.Load() {
		return true
	}
	n, err := c.store.CountUsers(ctx)
	if err != nil {
		return true
	}
	if n > 0 {
		c.usersPresent.Store(true)
		return true
	}
	return false
}

// authorize 依角色与路径判定权限：/system 与 /users 为管理专属（含只读）；读方法对已登录用户开放；
// 改写方法仅管理员放行，普通用户限个人写操作（见 isRegularWritablePath）。
func (c *Controller) authorize(user database.User, r *http.Request) bool {
	p := r.URL.Path
	if strings.HasPrefix(p, "/api/system/") || p == "/api/users" || strings.HasPrefix(p, "/api/users/") {
		return user.IsAdmin()
	}
	if !isMutating(r.Method) {
		return true
	}
	if user.IsAdmin() {
		return true
	}
	return isRegularWritablePath(p)
}

// authGate 是 /api 组统一的鉴权中间件：放行公开端点与 Mihon（自带鉴权，阶段3 接入 Basic）、
// 首启直通；否则解析会话 cookie → 用户，校验 CSRF 与角色，并把用户/会话写入上下文。
func (c *Controller) authGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if strings.HasPrefix(p, "/api/mihon/") {
			next.ServeHTTP(w, r)
			return
		}
		if isPublicAuthPath(p) {
			next.ServeHTTP(w, r)
			return
		}
		// 首启阶段（尚无任何账户）：站点无数据可保护，直通以便完成初始化与既有测试。
		if !c.usersExist(r.Context()) {
			next.ServeHTTP(w, r)
			return
		}
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil || cookie.Value == "" {
			jsonError(w, http.StatusUnauthorized, apiText(requestLocale(r), "auth.login_required"))
			return
		}
		now := time.Now()
		sess, user, err := c.store.GetSessionWithUser(r.Context(), hashSessionID(cookie.Value), now)
		if err != nil {
			clearSessionCookie(w, r)
			jsonError(w, http.StatusUnauthorized, apiText(requestLocale(r), "auth.login_required"))
			return
		}
		if isMutating(r.Method) {
			if !constantTimeTokenMatch(r.Header.Get("X-CSRF-Token"), sess.CSRFToken) {
				jsonError(w, http.StatusForbidden, apiText(requestLocale(r), "auth.csrf_invalid"))
				return
			}
		}
		if !c.authorize(user, r) {
			jsonError(w, http.StatusForbidden, apiText(requestLocale(r), "auth.admin_required"))
			return
		}
		if now.Sub(sess.LastSeenAt) > sessionTouchAfter {
			_ = c.store.TouchSession(r.Context(), sess.ID, now, now.Add(sessionTTL))
		}
		ctx := context.WithValue(r.Context(), userCtxKey, user)
		ctx = context.WithValue(ctx, sessionCtxKey, sess)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// startSession 新建会话、下发 cookie，返回该会话的 CSRF 令牌供响应体带回前端。
func (c *Controller) startSession(ctx context.Context, w http.ResponseWriter, r *http.Request, userID int64) (string, error) {
	raw, err := generateToken()
	if err != nil {
		return "", err
	}
	csrf, err := generateToken()
	if err != nil {
		return "", err
	}
	expires := time.Now().Add(sessionTTL)
	if err := c.store.CreateSession(ctx, database.Session{
		ID:        hashSessionID(raw),
		UserID:    userID,
		CSRFToken: csrf,
		UserAgent: r.UserAgent(),
		ExpiresAt: expires,
	}); err != nil {
		return "", err
	}
	setSessionCookie(w, r, raw, expires)
	return csrf, nil
}

// authSessionResponse 是登录 / setup / me 的统一返回体。
type authSessionResponse struct {
	User      database.User `json:"user"`
	CSRFToken string        `json:"csrf_token"`
}

// ---- 公开端点 ----

// authStatus 报告站点初始化状态与当前登录态，供前端启动时决定进入 setup / 登录 / 应用。
func (c *Controller) authStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	count, err := c.store.CountUsers(ctx)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "failed to read users")
		return
	}
	resp := map[string]any{
		"setup_required": count == 0,
		"authenticated":  false,
	}
	if cookie, e := r.Cookie(sessionCookieName); e == nil && cookie.Value != "" {
		if sess, user, err := c.store.GetSessionWithUser(ctx, hashSessionID(cookie.Value), time.Now()); err == nil {
			resp["authenticated"] = true
			resp["user"] = user
			resp["csrf_token"] = sess.CSRFToken
		}
	}
	jsonResponse(w, http.StatusOK, resp)
}

// setupAdmin 在站点尚无账户时创建首个管理员并立即登录。承接旧全局进度与 KOReader 账户的迁移在阶段2/3 挂接。
func (c *Controller) setupAdmin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	count, err := c.store.CountUsers(ctx)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "failed to read users")
		return
	}
	if count > 0 {
		jsonError(w, http.StatusConflict, apiText(requestLocale(r), "auth.setup_done"))
		return
	}
	var req struct {
		Username    string `json:"username"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
	}
	if !decodeAuthJSON(w, r, &req) {
		return
	}
	username := strings.TrimSpace(req.Username)
	if username == "" {
		jsonError(w, http.StatusBadRequest, apiText(requestLocale(r), "auth.username_required"))
		return
	}
	if len(req.Password) < minPasswordLen {
		jsonError(w, http.StatusBadRequest, apiText(requestLocale(r), "auth.password_too_short"))
		return
	}
	hash, err := hashPassword(req.Password)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}
	user, err := c.store.CreateUser(ctx, database.CreateUserParams{
		Username:     username,
		PasswordHash: hash,
		Role:         database.RoleAdmin,
		DisplayName:  strings.TrimSpace(req.DisplayName),
	})
	if err != nil {
		if errors.Is(err, database.ErrUsernameTaken) {
			jsonError(w, http.StatusConflict, apiText(requestLocale(r), "auth.username_taken"))
			return
		}
		jsonError(w, http.StatusInternalServerError, "failed to create admin")
		return
	}
	c.usersPresent.Store(true)
	csrf, err := c.startSession(ctx, w, r, user.ID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "failed to start session")
		return
	}
	jsonResponse(w, http.StatusOK, authSessionResponse{User: user, CSRFToken: csrf})
}

// login 校验用户名口令，成功则建会话下发 cookie。
func (c *Controller) login(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeAuthJSON(w, r, &req) {
		return
	}
	user, err := c.store.GetUserByUsername(ctx, strings.TrimSpace(req.Username))
	if err != nil || !verifyPassword(user.PasswordHash, req.Password) {
		jsonError(w, http.StatusUnauthorized, apiText(requestLocale(r), "auth.invalid_credentials"))
		return
	}
	csrf, err := c.startSession(ctx, w, r, user.ID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "failed to start session")
		return
	}
	jsonResponse(w, http.StatusOK, authSessionResponse{User: user, CSRFToken: csrf})
}

// logout 删除当前会话并清 cookie。
func (c *Controller) logout(w http.ResponseWriter, r *http.Request) {
	if sess, ok := sessionFromContext(r.Context()); ok {
		_ = c.store.DeleteSession(r.Context(), sess.ID)
	}
	clearSessionCookie(w, r)
	jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}

// authMe 返回当前登录用户与其会话的 CSRF 令牌。
func (c *Controller) authMe(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r.Context())
	if !ok {
		jsonError(w, http.StatusUnauthorized, apiText(requestLocale(r), "auth.login_required"))
		return
	}
	sess, _ := sessionFromContext(r.Context())
	jsonResponse(w, http.StatusOK, authSessionResponse{User: user, CSRFToken: sess.CSRFToken})
}

// changePassword 用户自助改密：校验当前密码后更新，并踢掉本人所有会话、重新建立当前会话。
func (c *Controller) changePassword(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, ok := userFromContext(ctx)
	if !ok {
		jsonError(w, http.StatusUnauthorized, apiText(requestLocale(r), "auth.login_required"))
		return
	}
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if !decodeAuthJSON(w, r, &req) {
		return
	}
	if !verifyPassword(user.PasswordHash, req.CurrentPassword) {
		jsonError(w, http.StatusBadRequest, apiText(requestLocale(r), "auth.password_incorrect"))
		return
	}
	if len(req.NewPassword) < minPasswordLen {
		jsonError(w, http.StatusBadRequest, apiText(requestLocale(r), "auth.password_too_short"))
		return
	}
	hash, err := hashPassword(req.NewPassword)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}
	if err := c.store.UpdateUserPassword(ctx, user.ID, hash, false); err != nil {
		jsonError(w, http.StatusInternalServerError, "failed to update password")
		return
	}
	// 改密即失效全部旧会话（含其他设备），再为当前设备建立新会话。
	_ = c.store.DeleteSessionsForUser(ctx, user.ID)
	csrf, err := c.startSession(ctx, w, r, user.ID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "failed to start session")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "ok", "csrf_token": csrf})
}

// ---- 管理员：账户管理 ----

func (c *Controller) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := c.store.ListUsers(r.Context())
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "failed to list users")
		return
	}
	if users == nil {
		users = []database.User{}
	}
	jsonResponse(w, http.StatusOK, users)
}

// createUser 由管理员创建账户并设置初始密码；must_change_password=true 引导用户首登改密。
func (c *Controller) createUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req struct {
		Username    string `json:"username"`
		Password    string `json:"password"`
		Role        string `json:"role"`
		DisplayName string `json:"display_name"`
	}
	if !decodeAuthJSON(w, r, &req) {
		return
	}
	username := strings.TrimSpace(req.Username)
	if username == "" {
		jsonError(w, http.StatusBadRequest, apiText(requestLocale(r), "auth.username_required"))
		return
	}
	if len(req.Password) < minPasswordLen {
		jsonError(w, http.StatusBadRequest, apiText(requestLocale(r), "auth.password_too_short"))
		return
	}
	role := database.RoleRegular
	if req.Role == database.RoleAdmin {
		role = database.RoleAdmin
	} else if req.Role != "" && req.Role != database.RoleRegular {
		jsonError(w, http.StatusBadRequest, apiText(requestLocale(r), "auth.role_invalid"))
		return
	}
	hash, err := hashPassword(req.Password)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}
	user, err := c.store.CreateUser(ctx, database.CreateUserParams{
		Username:           username,
		PasswordHash:       hash,
		Role:               role,
		DisplayName:        strings.TrimSpace(req.DisplayName),
		MustChangePassword: true,
	})
	if err != nil {
		if errors.Is(err, database.ErrUsernameTaken) {
			jsonError(w, http.StatusConflict, apiText(requestLocale(r), "auth.username_taken"))
			return
		}
		jsonError(w, http.StatusInternalServerError, "failed to create user")
		return
	}
	jsonResponse(w, http.StatusOK, user)
}

// updateUser 修改展示名与角色；阻止把最后一个管理员降级。
func (c *Controller) updateUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseID(r, "userId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid user ID")
		return
	}
	target, err := c.store.GetUserByID(ctx, id)
	if err != nil {
		jsonError(w, http.StatusNotFound, apiText(requestLocale(r), "auth.user_not_found"))
		return
	}
	var req struct {
		DisplayName string `json:"display_name"`
		Role        string `json:"role"`
	}
	if !decodeAuthJSON(w, r, &req) {
		return
	}
	role := target.Role
	if req.Role != "" {
		if req.Role != database.RoleAdmin && req.Role != database.RoleRegular {
			jsonError(w, http.StatusBadRequest, apiText(requestLocale(r), "auth.role_invalid"))
			return
		}
		role = req.Role
	}
	if target.IsAdmin() && role != database.RoleAdmin {
		if !c.hasAnotherAdmin(ctx, target.ID) {
			jsonError(w, http.StatusForbidden, apiText(requestLocale(r), "auth.last_admin"))
			return
		}
	}
	if err := c.store.UpdateUserProfile(ctx, id, strings.TrimSpace(req.DisplayName), role); err != nil {
		jsonError(w, http.StatusInternalServerError, "failed to update user")
		return
	}
	updated, _ := c.store.GetUserByID(ctx, id)
	jsonResponse(w, http.StatusOK, updated)
}

// resetUserPassword 由管理员重置某账户密码，并踢掉其全部会话强制重新登录。
func (c *Controller) resetUserPassword(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseID(r, "userId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid user ID")
		return
	}
	if _, err := c.store.GetUserByID(ctx, id); err != nil {
		jsonError(w, http.StatusNotFound, apiText(requestLocale(r), "auth.user_not_found"))
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if !decodeAuthJSON(w, r, &req) {
		return
	}
	if len(req.Password) < minPasswordLen {
		jsonError(w, http.StatusBadRequest, apiText(requestLocale(r), "auth.password_too_short"))
		return
	}
	hash, err := hashPassword(req.Password)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}
	if err := c.store.UpdateUserPassword(ctx, id, hash, true); err != nil {
		jsonError(w, http.StatusInternalServerError, "failed to reset password")
		return
	}
	_ = c.store.DeleteSessionsForUser(ctx, id)
	jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}

// deleteUser 删除账户；禁止删除自身与最后一个管理员。
func (c *Controller) deleteUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseID(r, "userId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid user ID")
		return
	}
	if cur, ok := userFromContext(ctx); ok && cur.ID == id {
		jsonError(w, http.StatusForbidden, apiText(requestLocale(r), "auth.cannot_delete_self"))
		return
	}
	target, err := c.store.GetUserByID(ctx, id)
	if err != nil {
		jsonError(w, http.StatusNotFound, apiText(requestLocale(r), "auth.user_not_found"))
		return
	}
	if target.IsAdmin() && !c.hasAnotherAdmin(ctx, target.ID) {
		jsonError(w, http.StatusForbidden, apiText(requestLocale(r), "auth.last_admin"))
		return
	}
	if err := c.store.DeleteUser(ctx, id); err != nil {
		jsonError(w, http.StatusInternalServerError, "failed to delete user")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}

// hasAnotherAdmin 判断除 excludeID 外是否仍有管理员，用于守卫「最后一个管理员」。
func (c *Controller) hasAnotherAdmin(ctx context.Context, excludeID int64) bool {
	n, err := c.store.CountAdmins(ctx)
	if err != nil {
		return false
	}
	if n > 1 {
		return true
	}
	if n == 1 {
		id, e := c.store.FirstAdminUserID(ctx)
		return e == nil && id != excludeID
	}
	return false
}

// decodeAuthJSON 解码鉴权端点的 JSON 请求体，失败时写 400 并返回 false。
func decodeAuthJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return false
	}
	return true
}
