// Package testfixture seeds persisted legacy records for integration tests.
// These helpers intentionally omit reading fact receipts and activity.
package testfixture

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"manga-manager/internal/database"
)

// BookProgress seeds the global persisted projection and its cached aggregate.
func BookProgress(ctx context.Context, store database.Store, arg database.UpdateBookProgressParams) error {
	if arg.LastReadAt.Valid {
		arg.LastReadAt.Time = database.LocalWallClock(arg.LastReadAt.Time)
	}
	if err := store.UpdateBookProgress(ctx, arg); err != nil {
		return err
	}
	sid, err := store.GetSeriesIDByBookID(ctx, arg.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return store.RefreshSeriesStats(ctx, sid)
}

// UserBookProgress seeds a pre-receipt reader position and rebuilds its aggregate.
func UserBookProgress(ctx context.Context, store database.Store, uid, bid, page int64, at time.Time) error {
	_, err := store.DB().ExecContext(ctx, `INSERT INTO user_book_progress(user_id,book_id,last_read_page,last_read_at,updated_at) VALUES(?,?,?,?,CURRENT_TIMESTAMP) ON CONFLICT(user_id,book_id) DO UPDATE SET last_read_page=excluded.last_read_page,last_read_at=excluded.last_read_at,is_read=FALSE,updated_at=CURRENT_TIMESTAMP`, uid, bid, page, database.LocalWallClock(at))
	if err != nil {
		return err
	}
	sid, err := store.GetSeriesIDByBookID(ctx, bid)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO user_series_progress(user_id,series_id) VALUES(?,?) ON CONFLICT(user_id,series_id) DO NOTHING`, uid, sid); err != nil {
		return err
	}
	return store.RefreshUserSeriesProgressForAllUsers(ctx, sid)
}
