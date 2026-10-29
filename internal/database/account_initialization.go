package database

import (
	"context"
	"strings"
)

// AccountInitializationWrite only exposes the atomic first-admin decision and creation.
type AccountInitializationWrite interface {
	CountUsers(context.Context) (int64, error)
	CreateAdministrator(context.Context, string, string, string) (User, error)
}
type accountInitializationWrite struct{ db DBTX }

func (s *SqlStore) WithAccountInitialization(ctx context.Context, apply func(AccountInitializationWrite) error) error {
	return s.ExecTx(ctx, func(q *Queries) error { return apply(accountInitializationWrite{db: q.db}) })
}
func (w accountInitializationWrite) CountUsers(ctx context.Context) (int64, error) {
	var n int64
	err := w.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&n)
	return n, err
}
func (w accountInitializationWrite) CreateAdministrator(ctx context.Context, username, hash, display string) (User, error) {
	res, err := w.db.ExecContext(ctx, "INSERT INTO users(username,password_hash,role,display_name,must_change_password) VALUES(?,?,?, ?,0)", username, hash, RoleAdmin, display)
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
	return scanUser(w.db.QueryRowContext(ctx, "SELECT "+userColumns+" FROM users WHERE id=?", id))
}
