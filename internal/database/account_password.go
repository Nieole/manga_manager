package database

import "context"

// AccountPasswordWrite keeps the site-password change and revocation indivisible.
type AccountPasswordWrite interface {
	GetUser(context.Context, int64) (User, error)
	ReplacePassword(context.Context, int64, string, bool) error
	RevokeSessions(context.Context, int64) error
}
type accountPasswordWrite struct{ db DBTX }

func (s *SqlStore) WithAccountPassword(ctx context.Context, apply func(AccountPasswordWrite) error) error {
	return s.ExecTx(ctx, func(q *Queries) error { return apply(accountPasswordWrite{db: q.db}) })
}
func (w accountPasswordWrite) GetUser(ctx context.Context, id int64) (User, error) {
	return (accountManagementWrite{db: w.db}).GetUser(ctx, id)
}
func (w accountPasswordWrite) ReplacePassword(ctx context.Context, id int64, hash string, mustChange bool) error {
	_, err := w.db.ExecContext(ctx, `UPDATE users SET password_hash = ?, must_change_password = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, hash, mustChange, id)
	return err
}
func (w accountPasswordWrite) RevokeSessions(ctx context.Context, id int64) error {
	_, err := w.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, id)
	return err
}
