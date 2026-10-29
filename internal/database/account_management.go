package database

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// AccountManagementWrite exposes only account management invariants and their writes.
type AccountManagementWrite interface {
	GetUser(context.Context, int64) (User, error)
	CountAdmins(context.Context) (int64, error)
	ListUsers(context.Context) ([]User, error)
	CreateUser(context.Context, CreateUserParams) (User, error)
	UpdateProfile(context.Context, int64, string, string) error
	DeleteUser(context.Context, int64) error
}
type accountManagementWrite struct{ db DBTX }

func (s *SqlStore) WithAccountManagement(ctx context.Context, apply func(AccountManagementWrite) error) error {
	return s.ExecTx(ctx, func(q *Queries) error { return apply(accountManagementWrite{db: q.db}) })
}
func (w accountManagementWrite) GetUser(ctx context.Context, id int64) (User, error) {
	u, err := scanUser(w.db.QueryRowContext(ctx, "SELECT "+userColumns+" FROM users WHERE id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrUserNotFound
	}
	return u, err
}
func (w accountManagementWrite) CountAdmins(ctx context.Context) (int64, error) {
	var n int64
	err := w.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE role=?", RoleAdmin).Scan(&n)
	return n, err
}
func (w accountManagementWrite) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := w.db.QueryContext(ctx, "SELECT "+userColumns+" FROM users ORDER BY id ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
func (w accountManagementWrite) CreateUser(ctx context.Context, in CreateUserParams) (User, error) {
	res, err := w.db.ExecContext(ctx, "INSERT INTO users(username,password_hash,role,display_name,must_change_password) VALUES(?,?,?,?,?)", in.Username, in.PasswordHash, in.Role, in.DisplayName, in.MustChangePassword)
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
	return w.GetUser(ctx, id)
}
func (w accountManagementWrite) UpdateProfile(ctx context.Context, id int64, display, role string) error {
	_, err := w.db.ExecContext(ctx, "UPDATE users SET display_name=?,role=?,updated_at=CURRENT_TIMESTAMP WHERE id=?", display, role, id)
	return err
}
func (w accountManagementWrite) DeleteUser(ctx context.Context, id int64) error {
	_, err := w.db.ExecContext(ctx, "DELETE FROM users WHERE id=?", id)
	return err
}
