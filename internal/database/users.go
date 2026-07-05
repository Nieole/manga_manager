// 业务说明：本文件是站点账户体系（多用户）的数据访问层，封装 users / sessions 两张表的读写。
// 它为鉴权控制层提供账户管理（创建/查询/改密/角色）与服务端会话（Cookie session + CSRF）的存储原语，
// 是「强制登录 + 首次建管理员 + 每用户阅读进度」这条多用户主线的地基。
// 维护要点：password_hash 只存 bcrypt 摘要；sessions.id 存 cookie 令牌的 SHA-256，绝不落明文令牌。

package database

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// 站点账户角色。admin 全权，regular 只读浏览 + 记录本人阅读状态。
const (
	RoleAdmin   = "admin"
	RoleRegular = "regular"
)

var (
	// ErrUserNotFound 表示按 id/username 未找到账户。
	ErrUserNotFound = errors.New("user not found")
	// ErrSessionNotFound 表示会话不存在或已过期。
	ErrSessionNotFound = errors.New("session not found")
	// ErrUsernameTaken 表示用户名已被占用（UNIQUE 冲突）。
	ErrUsernameTaken = errors.New("username already taken")
)

// User 是一个站点登录账户。PasswordHash 带 json:"-"，绝不随接口序列化外泄。
type User struct {
	ID                 int64     `json:"id"`
	Username           string    `json:"username"`
	PasswordHash       string    `json:"-"`
	Role               string    `json:"role"`
	DisplayName        string    `json:"display_name"`
	MustChangePassword bool      `json:"must_change_password"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// IsAdmin 判断账户是否为管理员角色。
func (u User) IsAdmin() bool { return u.Role == RoleAdmin }

// Session 是一条服务端会话记录。ID 是 cookie 令牌的 SHA-256（非明文令牌）。
type Session struct {
	ID         string
	UserID     int64
	CSRFToken  string
	UserAgent  string
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
}

// CreateUserParams 是创建账户的入参。Role 为空时默认 regular。
type CreateUserParams struct {
	Username           string
	PasswordHash       string
	Role               string
	DisplayName        string
	MustChangePassword bool
}

const userColumns = `id, username, password_hash, role, display_name, must_change_password, created_at, updated_at`

// rowScanner 抽象 *sql.Row 与 *sql.Rows 的 Scan，供共用扫描逻辑。
type rowScanner interface {
	Scan(dest ...any) error
}

func scanUser(sc rowScanner) (User, error) {
	var u User
	if err := sc.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.DisplayName, &u.MustChangePassword, &u.CreatedAt, &u.UpdatedAt); err != nil {
		return User{}, err
	}
	return u, nil
}

// CountUsers 返回站点账户总数。0 表示尚未初始化（触发首次建管理员引导）。
func (s *SqlStore) CountUsers(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// CountAdmins 返回管理员数量，用于阻止删除/降级最后一个管理员。
func (s *SqlStore) CountAdmins(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role = ?`, RoleAdmin).Scan(&n)
	return n, err
}

// FirstAdminUserID 返回 id 最小的管理员账户 id。旧的全局阅读进度与 KOReader 账户在迁移时归属该账户。
// 无管理员时返回 ErrUserNotFound。
func (s *SqlStore) FirstAdminUserID(ctx context.Context) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM users WHERE role = ? ORDER BY id ASC LIMIT 1`, RoleAdmin).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrUserNotFound
	}
	return id, err
}

// CreateUser 新建账户。用户名大小写不敏感冲突由调用方先行 GetUserByUsername 预检，
// 这里再以 UNIQUE 约束兜底并翻译成 ErrUsernameTaken。
func (s *SqlStore) CreateUser(ctx context.Context, arg CreateUserParams) (User, error) {
	role := arg.Role
	if role != RoleAdmin {
		role = RoleRegular
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users (username, password_hash, role, display_name, must_change_password) VALUES (?, ?, ?, ?, ?)`,
		arg.Username, arg.PasswordHash, role, arg.DisplayName, arg.MustChangePassword)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return User{}, ErrUsernameTaken
		}
		return User{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return User{}, err
	}
	return s.GetUserByID(ctx, id)
}

// GetUserByID 按主键查账户，未找到返回 ErrUserNotFound。
func (s *SqlStore) GetUserByID(ctx context.Context, id int64) (User, error) {
	u, err := scanUser(s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	return u, err
}

// GetUserByUsername 按用户名（精确匹配）查账户，未找到返回 ErrUserNotFound。
func (s *SqlStore) GetUserByUsername(ctx context.Context, username string) (User, error) {
	u, err := scanUser(s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE username = ?`, username))
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	return u, err
}

// ListUsers 返回全部账户，按 id 升序（第一个管理员在最前）。
func (s *SqlStore) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+userColumns+` FROM users ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// UpdateUserPassword 改密并同步 must_change_password 标志（用户自助改密后应置 false）。
func (s *SqlStore) UpdateUserPassword(ctx context.Context, id int64, passwordHash string, mustChange bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET password_hash = ?, must_change_password = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		passwordHash, mustChange, id)
	return err
}

// UpdateUserProfile 更新展示名与角色（角色变更的合法性——如禁止降级最后一个管理员——由调用方校验）。
func (s *SqlStore) UpdateUserProfile(ctx context.Context, id int64, displayName, role string) error {
	if role != RoleAdmin {
		role = RoleRegular
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET display_name = ?, role = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		displayName, role, id)
	return err
}

// DeleteUser 删除账户；ON DELETE CASCADE 会一并清掉其会话与（阶段2起）每用户进度。
func (s *SqlStore) DeleteUser(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	return err
}

// CreateSession 落库一条服务端会话。
func (s *SqlStore) CreateSession(ctx context.Context, sess Session) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, user_id, csrf_token, user_agent, expires_at) VALUES (?, ?, ?, ?, ?)`,
		sess.ID, sess.UserID, sess.CSRFToken, sess.UserAgent, sess.ExpiresAt)
	return err
}

// GetSessionWithUser 按会话 id（cookie 令牌的 SHA-256）取回未过期会话及其账户。
// 会话缺失或已过期返回 ErrSessionNotFound。
func (s *SqlStore) GetSessionWithUser(ctx context.Context, id string, now time.Time) (Session, User, error) {
	var (
		sess Session
		u    User
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT s.id, s.user_id, s.csrf_token, s.user_agent, s.created_at, s.last_seen_at, s.expires_at,
		       u.id, u.username, u.password_hash, u.role, u.display_name, u.must_change_password, u.created_at, u.updated_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.id = ? AND s.expires_at > ?`, id, now).Scan(
		&sess.ID, &sess.UserID, &sess.CSRFToken, &sess.UserAgent, &sess.CreatedAt, &sess.LastSeenAt, &sess.ExpiresAt,
		&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.DisplayName, &u.MustChangePassword, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, User{}, ErrSessionNotFound
	}
	if err != nil {
		return Session{}, User{}, err
	}
	return sess, u, nil
}

// TouchSession 滑动续期：更新 last_seen_at 与 expires_at。
func (s *SqlStore) TouchSession(ctx context.Context, id string, lastSeen, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET last_seen_at = ?, expires_at = ? WHERE id = ?`, lastSeen, expiresAt, id)
	return err
}

// DeleteSession 删除单个会话（登出）。
func (s *SqlStore) DeleteSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	return err
}

// DeleteSessionsForUser 清掉某账户的全部会话（改密、禁用、删除账户时用）。
func (s *SqlStore) DeleteSessionsForUser(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
	return err
}

// DeleteExpiredSessions 清理所有过期会话，供定期维护调用。
func (s *SqlStore) DeleteExpiredSessions(ctx context.Context, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, now)
	return err
}
