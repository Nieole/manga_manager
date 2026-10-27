package database

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// seedLegacyBookProgress seeds the global persisted projection and its cached aggregate.
func seedLegacyBookProgress(ctx context.Context, store Store, arg UpdateBookProgressParams) error {
	if arg.LastReadAt.Valid {
		arg.LastReadAt.Time = LocalWallClock(arg.LastReadAt.Time)
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

// seedLegacyUserBookProgress seeds a pre-receipt reader position and rebuilds its aggregate.
func seedLegacyUserBookProgress(ctx context.Context, store Store, uid, bid, page int64, at time.Time) error {
	_, err := store.DB().ExecContext(ctx, `INSERT INTO user_book_progress(user_id,book_id,last_read_page,last_read_at,updated_at) VALUES(?,?,?,?,CURRENT_TIMESTAMP) ON CONFLICT(user_id,book_id) DO UPDATE SET last_read_page=excluded.last_read_page,last_read_at=excluded.last_read_at,is_read=FALSE,updated_at=CURRENT_TIMESTAMP`, uid, bid, page, LocalWallClock(at))
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
