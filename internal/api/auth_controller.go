// 本文件是站点多用户鉴权的 HTTP 层，实现「强制登录 + 首次建管理员 + 角色」这条主线。
// 采用服务端会话（Cookie session）+ 同步器式 CSRF 令牌：cookie 存不可读的随机会话令牌，
// DB 存其 SHA-256；改写类请求需在 X-CSRF-Token 头回传会话绑定的 CSRF 令牌。角色分 admin（全权）
// 与 regular（只读浏览 + 记录本人进度/书签/短评）。两个入口：authGate 守 /api 组，requireBasicAuth
// 守阅读协议；强制改密（must_change_password）两边同口径全拒。密码只经 bcrypt，Claude 不代填。

package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
)

const (
	sessionCookieName = "mm_session"
	// 会话有效期 30 天，滑动续期：距上次活跃超过 sessionTouchAfter 才续，服务端 expires_at 与
	// 浏览器 Cookie 同一节奏一起推后（见 authGate），二者分叉就等于续期对客户端是哑的。
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

// currentUserID 返回当前登录用户 id；无用户时（首启尚无账户 / 直接调用处理器的单元测试 / 未接入会话的
// 阅读协议）返回 0。0 表示走旧的全局进度路径（books.last_read_page + series_stats），>0 走每用户进度。
// authGate 保证：一旦站点存在账户，未登录请求会被 401 拦下，故已登录处理器里 currentUserID 恒 > 0。
func (c *Controller) currentUserID(r *http.Request) int64 {
	if u, ok := userFromContext(r.Context()); ok {
		return u.ID
	}
	return 0
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

// sessionCookieSecure 决定会话 Cookie 是否带 Secure 标志。
//
// 判定顺序是「显式配置 → 可信代理的声明 → 直连 TLS」，三者都有明确理由：
//
//   - server.cookie_secure 是管理员的显式意图，永远优先。
//   - 其次才看转发头，且只在直连对端落在 server.trusted_proxies 内时采信。
//     无条件采信 X-Forwarded-Proto 意味着任何客户端都能自己决定 Secure 标志，
//     这与已按 trusted_proxies 收紧的 clientIP 是两套口径。
//   - **可信代理说了 http 就以它为准，不再看 r.TLS**：proxy→backend 之间常另有一段内部 TLS，
//     若让 r.TLS 覆盖代理的声明，明文对外的部署会被误判成 https 而下发 Secure，
//     浏览器随即丢弃 Cookie，登录直接失效。r.TLS 只在没有可信声明时兜底。
func (c *Controller) sessionCookieSecure(r *http.Request) bool {
	if r == nil {
		return false
	}
	switch c.currentConfig().Server.CookieSecure {
	case config.CookieSecureAlways:
		return true
	case config.CookieSecureNever:
		return false
	}

	forwardedProto := forwardedRequestProto(r)
	if c.trustsForwardedHeaders(r) {
		if forwardedProto != "" {
			return strings.EqualFold(forwardedProto, "https")
		}
	} else if strings.EqualFold(forwardedProto, "https") {
		// 声明了 https 却来自未登记的对端：忽略它，但要让管理员知道为什么 Secure 没生效，
		// 否则表现就是「明明是 HTTPS 部署，Cookie 却没有 Secure」的无声降级。
		c.warnUntrustedForwardedProto()
	}
	if r.TLS != nil {
		return true
	}
	return false
}

// forwardedRequestProto 取转发协议，与 requestBaseURL 同口径（X-Forwarded-Proto 优先，
// 其次 X-Forwarded-Scheme）——只统一「信不信」而头名不统一，口径就仍然是两套。
func forwardedRequestProto(r *http.Request) string {
	if proto := firstHeaderValue(r, "X-Forwarded-Proto"); proto != "" {
		return proto
	}
	return firstHeaderValue(r, "X-Forwarded-Scheme")
}

// warnUntrustedForwardedProto 每进程只告警一次：该头由客户端可控，
// 未配 trusted_proxies 的直连部署可能被任意请求触发，逐次打日志等于给了一个刷日志的口子。
func (c *Controller) warnUntrustedForwardedProto() {
	if c == nil {
		return
	}
	c.untrustedProtoWarnOnce.Do(func() {
		slog.Warn("Ignoring X-Forwarded-Proto: https from an untrusted peer; session cookie will not be marked Secure",
			"hint", "configure server.trusted_proxies with the proxy network, or set server.cookie_secure: always")
	})
}

func (c *Controller) setSessionCookie(w http.ResponseWriter, r *http.Request, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   c.sessionCookieSecure(r),
		SameSite: http.SameSiteLaxMode,
		Expires:  expires,
		MaxAge:   int(time.Until(expires).Seconds()),
	})
}

func (c *Controller) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		// 清除也要带上与写入时一致的 Secure：属性不匹配时浏览器可能不认为是同一个 Cookie，
		// 于是「登出」留下一个仍然有效的会话 Cookie。
		Secure:   c.sessionCookieSecure(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

// ---- 阅读协议 HTTP Basic 鉴权（OPDS / Mihon）----

// resolveBasicAuthUser 校验 Basic 凭据并返回站点用户 id；
// 命中缓存即免去一次 bcrypt（bcrypt 故意很慢，协议客户端每个请求都带凭据）。
func (c *Controller) resolveBasicAuthUser(ctx context.Context, username, password string) (int64, bool) {
	if username == "" || password == "" {
		return 0, false
	}
	now := time.Now()
	if uid, ok := c.auth.lookupBasicAuth(username, password, now); ok {
		return uid, true
	}
	user, err := c.store.GetUserByUsername(ctx, username)
	if err != nil || !verifyPassword(user.PasswordHash, password) {
		return 0, false
	}
	c.auth.rememberBasicAuth(username, password, user.ID, now)
	return user.ID, true
}

// basicAuthChallenge 是协议侧的默认 WWW-Authenticate 值。
const basicAuthChallenge = `Basic realm="manga-manager"`

// basicAuthChallengePasswordChange 把「先去网页端改密」这条原因写进 realm。
// 多数 Basic 客户端不显示响应体、只显示 realm，realm 因此是这条拒绝唯一的可见解释；
// 头字段按 ASCII 写，避免阅读器把 UTF-8 realm 渲染成乱码。
const basicAuthChallengePasswordChange = `Basic realm="manga-manager: change your initial password in the web UI first"`

// respondPasswordChangeRequired 应答「口令正确、但账号尚未完成首登改密」。
// 用 401 而非 403：客户端会重新索要凭据，用户在网页端改密后把新口令填进去就恢复了；
// 403 在阅读器上表现为一条死路，且不带可显示的解释。
func respondPasswordChangeRequired(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("WWW-Authenticate", basicAuthChallengePasswordChange)
	jsonError(w, http.StatusUnauthorized, apiText(requestLocale(r), "auth.password_change_required"))
}

// requireBasicAuth 是阅读协议（OPDS/Mihon）的 HTTP Basic 鉴权中间件：校验站点用户名+密码，
// 成功则把用户写入请求上下文（供 currentUserID 与每用户进度取用），失败返回 401 + WWW-Authenticate。
// 强制改密与 authGate 同口径：未完成首登改密的账号在这里读写一并拒绝。
// 站点尚无账户时（首启）直通，避免锁死初始化前的协议访问。
func (c *Controller) requireBasicAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !c.usersExist(r.Context()) {
			next.ServeHTTP(w, r)
			return
		}
		// 按 IP 的失败限流：锁定期内直接 429，避免攻击者用错误凭据反复触发昂贵的 bcrypt（CPU-DoS）。
		ipKey := "basic:" + c.clientIP(r)
		if d, locked := c.auth.basicAuthLimiter.retryAfter(ipKey); locked {
			respondTooManyAttempts(w, r, d)
			return
		}
		if username, password, ok := r.BasicAuth(); ok {
			if uid, valid := c.resolveBasicAuthUser(r.Context(), username, password); valid {
				if user, err := c.store.GetUserByID(r.Context(), uid); err == nil {
					// 口令是对的，先清掉该 IP 的失败计数：否则一台按分钟轮询的阅读器会把自己
					// 关进锁定期，用户改完密码回来照样吃 429。
					c.auth.basicAuthLimiter.recordSuccess(ipKey)
					if user.MustChangePassword {
						respondPasswordChangeRequired(w, r)
						return
					}
					next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userCtxKey, user)))
					return
				}
			}
		}
		c.auth.basicAuthLimiter.recordFailure(ipKey)
		w.Header().Set("WWW-Authenticate", basicAuthChallenge)
		jsonError(w, http.StatusUnauthorized, apiText(requestLocale(r), "auth.login_required"))
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
// 自身账户（登出/改密）与本人阅读状态（进度、书签、阅读时长、系列短评）。其余改写一律要求管理员。
func isRegularWritablePath(p string) bool {
	switch p {
	case "/api/auth/logout", "/api/auth/change-password":
		return true
	case "/api/books/bulk-progress", "/api/books/bulk-progress/sync", "/api/series/bulk-progress":
		return true
	}
	if strings.HasPrefix(p, "/api/books/") {
		if strings.HasSuffix(p, "/progress") || strings.HasSuffix(p, "/reading-time") || strings.Contains(p, "/bookmarks") {
			return true
		}
	}
	if strings.HasPrefix(p, "/api/series/") && strings.HasSuffix(p, "/review") {
		return true
	}
	return false
}

// isPasswordChangeAllowedPath 是「必须先改密」状态下仍可访问的端点白名单。
func isPasswordChangeAllowedPath(p string) bool {
	switch p {
	case "/api/auth/me", "/api/auth/logout", "/api/auth/change-password", "/api/auth/status":
		return true
	}
	return false
}

// isCsrfExempt 免除个别端点的 CSRF 校验：阅读时长上报经 navigator.sendBeacon 发送，无法附带 X-CSRF-Token 头。
// 该端点仅为本人某书累加阅读秒数（仍要求有效会话 Cookie），被伪造的风险与影响极低。
func isCsrfExempt(p string) bool {
	return strings.HasSuffix(p, "/reading-time")
}

// usersExist 报告站点是否已存在账户；一旦为真即缓存，避免每请求 COUNT。
// 出错时按「已存在」处理（fail-closed）：公开鉴权端点在 authGate 中先于此判断放行，故首启 setup 不受影响。
func (c *Controller) usersExist(ctx context.Context) bool {
	if c.auth.usersExist() {
		return true
	}
	n, err := c.store.CountUsers(ctx)
	if err != nil {
		return true
	}
	if n > 0 {
		c.auth.markUsersExist()
		return true
	}
	return false
}

// isAdminOnlyPath 列出「连读取都必须是管理员」的路径。
//
// 除了 /api/system/ 与 /api/users 这两个前缀，还必须显式包含 /api/browse-dirs：
// 它是 GET，落到 authorize 的「读方法一律放行」分支里，于是任意已登录的普通账号
// （设计上只该浏览漫画与记录本人进度）都能从 ?path=/ 起逐级枚举宿主机的完整目录结构。
func isAdminOnlyPath(p string) bool {
	if strings.HasPrefix(p, "/api/system/") {
		return true
	}
	if p == "/api/users" || strings.HasPrefix(p, "/api/users/") {
		return true
	}
	// 外部库传输本身是纯管理员功能，读侧不得比写侧宽：GET 若落进「读方法一律放行」分支，
	// 任意已登录的普通账号都能拿到会话快照，里面带着服务器上的**绝对路径**
	// （external_path 与 library_path）。
	if strings.HasPrefix(p, "/api/libraries/") && strings.Contains(p, "/external-libraries/") {
		return true
	}
	return p == "/api/browse-dirs"
}

// authorize 依角色与路径判定权限：/system 与 /users 为管理专属（含只读）；读方法对已登录用户开放；
// 改写方法仅管理员放行，普通用户限个人写操作（见 isRegularWritablePath）。
func (c *Controller) authorize(user database.User, r *http.Request) bool {
	p := r.URL.Path
	if isAdminOnlyPath(p) {
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
		// 首启阶段（尚无任何账户）：仅放行公开鉴权端点（status/setup/login，已在上方 isPublicAuthPath
		// 提前返回）与 Mihon 协议前缀（更靠上处理）。其余端点一律不得在建立首个管理员之前直通——
		// 否则默认 0.0.0.0 监听下，任何网络客户端都能在 setup 窗口内枚举文件系统、读配置、触发扫描/
		// SSRF，甚至抢先 setup 成为永久管理员。这里直接 401，把 setup 前的攻击面收敛到只剩公开端点。
		if !c.usersExist(r.Context()) {
			jsonError(w, http.StatusUnauthorized, apiText(requestLocale(r), "auth.login_required"))
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
			c.clearSessionCookie(w, r)
			jsonError(w, http.StatusUnauthorized, apiText(requestLocale(r), "auth.login_required"))
			return
		}
		if isMutating(r.Method) && !isCsrfExempt(p) {
			if !constantTimeTokenMatch(r.Header.Get("X-CSRF-Token"), sess.CSRFToken) {
				jsonError(w, http.StatusForbidden, apiText(requestLocale(r), "auth.csrf_invalid"))
				return
			}
		}
		if !c.authorize(user, r) {
			jsonError(w, http.StatusForbidden, apiText(requestLocale(r), "auth.admin_required"))
			return
		}
		// must_change_password 必须在服务端校验，不能只靠前端 AuthGate 拦截：否则任何
		// 非浏览器客户端（curl / 阅读协议以外的脚本）都能拿着管理员分配的初始密码无限期
		// 使用。这里强制收敛到「只能看自己、登出、改密」几个端点。
		if user.MustChangePassword && !isPasswordChangeAllowedPath(p) {
			jsonError(w, http.StatusForbidden, apiText(requestLocale(r), "auth.password_change_required"))
			return
		}
		// 滑动续期：推后服务端 expires_at 的同时，必须用同一个令牌、同一套属性把 Cookie 重下发，
		// 否则浏览器那份 Max-Age 永远停在登录那一刻，天天在用的用户仍会在第 30 天被踢下线。
		// 落库失败就不下发：客户端比服务端活得久只会把无声失效换成更迷惑的失效。
		if now.Sub(sess.LastSeenAt) > sessionTouchAfter {
			expires := now.Add(sessionTTL)
			if err := c.store.TouchSession(r.Context(), sess.ID, now, expires); err == nil {
				c.setSessionCookie(w, r, cookie.Value, expires)
			}
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
	c.setSessionCookie(w, r, raw, expires)
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

// setupAdmin 在站点尚无账户时创建首个管理员并立即登录，同时把旧的全局阅读进度/活动与孤儿 KOReader 账户并入该管理员。
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
	c.auth.markUsersExist()
	// 把旧的全局阅读进度迁移到首个管理员名下（幂等）。失败不阻断建号，仅记录。
	if err := c.store.MigrateGlobalProgressToUser(ctx, user.ID); err != nil {
		slog.Warn("migrate global progress to first admin failed", "user_id", user.ID, "error", err)
	}
	// 现有 KOReader 账户并入首个管理员。
	if err := c.store.AssignOrphanKOReaderAccountsToUser(ctx, user.ID); err != nil {
		slog.Warn("assign KOReader accounts to first admin failed", "user_id", user.ID, "error", err)
	}
	// 旧的全局阅读活动（热力图数据）迁入首个管理员。
	if err := c.store.MigrateGlobalActivityToUser(ctx, user.ID); err != nil {
		slog.Warn("migrate global reading activity to first admin failed", "user_id", user.ID, "error", err)
	}
	csrf, err := c.startSession(ctx, w, r, user.ID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "failed to start session")
		return
	}
	jsonResponse(w, http.StatusOK, authSessionResponse{User: user, CSRFToken: csrf})
}

// login 校验用户名口令，成功则建会话下发 cookie。带按 IP + 用户名的失败暴破限流。
func (c *Controller) login(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ipKey := "ip:" + c.clientIP(r)
	if d, locked := c.auth.loginLimiter.retryAfter(ipKey); locked {
		respondTooManyAttempts(w, r, d)
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeAuthJSON(w, r, &req) {
		return
	}
	username := strings.TrimSpace(req.Username)
	userKey := "user:" + strings.ToLower(username)
	if d, locked := c.auth.loginLimiter.retryAfter(userKey); locked {
		respondTooManyAttempts(w, r, d)
		return
	}
	user, err := c.store.GetUserByUsername(ctx, username)
	if err != nil || !verifyPassword(user.PasswordHash, req.Password) {
		// 同时对来源 IP 与目标用户名计失败：前者挡单机横扫多账户，后者挡分布式打单账户。
		c.auth.loginLimiter.recordFailure(ipKey)
		c.auth.loginLimiter.recordFailure(userKey)
		jsonError(w, http.StatusUnauthorized, apiText(requestLocale(r), "auth.invalid_credentials"))
		return
	}
	c.auth.loginLimiter.recordSuccess(ipKey)
	c.auth.loginLimiter.recordSuccess(userKey)
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
	c.clearSessionCookie(w, r)
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
	// 旧口令必须立刻从 Basic 鉴权缓存里踢掉，否则它在 OPDS/Mihon 上最长还能再用 5 分钟。
	c.auth.invalidateBasicAuthForUser(user.ID)
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
	// 管理员重置他人密码同理：被重置的账号的旧口令要立刻失效。
	c.auth.invalidateBasicAuthForUser(id)
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

// startSessionJanitor 周期性清理过期会话。
//
// store.DeleteExpiredSessions 早就实现了、也有 DB 层单测，但生产代码里一个调用点都没有：
// sessions 表只增不减，一个长期运行的实例会无限积累已过期的行。
// 随 Controller 生命周期退出（经 runBackground 登记，Close 会等待）。
func (c *Controller) startSessionJanitor() {
	const interval = time.Hour
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	prune := func() {
		if c.store == nil {
			return
		}
		if err := c.store.DeleteExpiredSessions(context.Background(), time.Now()); err != nil {
			slog.Warn("Failed to prune expired sessions", "error", err)
		}
	}
	// 启动时先清一次，避免实例频繁重启时永远等不到第一个 tick。
	prune()

	for {
		select {
		case <-c.lifecycleDone():
			return
		case <-ticker.C:
			prune()
		}
	}
}
