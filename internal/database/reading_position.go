package database

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ReadingPositionState is the durable state locked for one reader and book.
type ReadingPositionState struct {
	PageCount   int64
	Page        int64
	LastReadAt  sql.NullTime
	ChangedAtNS int64
	Duplicate   bool
}

// ReadingPositionMutation is the decision applied within the locked transaction.
type ReadingPositionMutation struct {
	Apply      bool
	Page       int64
	OccurredAt time.Time
}

// MutateReadingPosition reads, decides and persists under one SQLite write transaction.
// decide owns reading rules; this storage boundary keeps projections and receipts atomic.
func (s *SqlStore) MutateReadingPosition(ctx context.Context, userID, bookID int64, factID string, decide func(ReadingPositionState) ReadingPositionMutation) (ReadingPositionMutation, error) {
	var result ReadingPositionMutation
	err := s.ExecTx(ctx, func(q *Queries) error {
		book, err := q.GetBook(ctx, bookID)
		if err != nil {
			return err
		}
		state := ReadingPositionState{PageCount: book.PageCount, Page: book.LastReadPage.Int64, LastReadAt: book.LastReadAt}
		if userID > 0 {
			state.Page = 0
			state.LastReadAt = sql.NullTime{}
			err = q.db.QueryRowContext(ctx, `SELECT COALESCE(last_read_page,0),last_read_at FROM user_book_progress WHERE user_id=? AND book_id=?`, userID, bookID).Scan(&state.Page, &state.LastReadAt)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		if factID != "" {
			var exists int
			err = q.db.QueryRowContext(ctx, `SELECT 1 FROM reading_position_receipts WHERE user_id=? AND book_id=? AND fact_id=?`, userID, bookID, factID).Scan(&exists)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			state.Duplicate = err == nil
		}
		err = q.db.QueryRowContext(ctx, `SELECT changed_at_ns FROM reading_position_basis WHERE user_id=? AND book_id=?`, userID, bookID).Scan(&state.ChangedAtNS)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		result = decide(state)
		if !result.Apply {
			return nil
		}
		displayAt := LocalWallClock(result.OccurredAt)
		if userID > 0 {
			_, err = q.db.ExecContext(ctx, `INSERT INTO user_book_progress(user_id,book_id,last_read_page,last_read_at) VALUES(?,?,?,?) ON CONFLICT(user_id,book_id) DO UPDATE SET last_read_page=excluded.last_read_page,last_read_at=excluded.last_read_at,updated_at=CURRENT_TIMESTAMP`, userID, bookID, result.Page, displayAt)
			if err == nil {
				err = refreshUserSeriesProgressTx(ctx, q, userID, book.SeriesID)
			}
		} else {
			err = q.UpdateBookProgress(ctx, UpdateBookProgressParams{ID: bookID, LastReadPage: sql.NullInt64{Int64: result.Page, Valid: true}, LastReadAt: sql.NullTime{Time: displayAt, Valid: true}})
			if err == nil {
				_, err = q.db.ExecContext(ctx, refreshSeriesStatsStatement("s.id = ?"), book.SeriesID)
			}
		}
		if err != nil {
			return err
		}
		_, err = q.db.ExecContext(ctx, `INSERT INTO reading_position_basis(user_id,book_id,changed_at_ns,fact_id) VALUES(?,?,?,?) ON CONFLICT(user_id,book_id) DO UPDATE SET changed_at_ns=excluded.changed_at_ns,fact_id=excluded.fact_id`, userID, bookID, result.OccurredAt.UnixNano(), factID)
		if err == nil && factID != "" {
			_, err = q.db.ExecContext(ctx, `INSERT INTO reading_position_receipts(user_id,book_id,fact_id) VALUES(?,?,?)`, userID, bookID, factID)
		}
		return err
	})
	if err != nil {
		result.Apply = false
	}
	return result, err
}

// LogUserReadingActivityAt projects a proven reading date, retaining daily maximum absolute page semantics.
func (s *SqlStore) LogUserReadingActivityAt(ctx context.Context, userID, bookID, pages int64, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO user_reading_activity(user_id,book_id,date,pages_read) VALUES(?,?,?,?) ON CONFLICT(user_id,book_id,date) DO UPDATE SET pages_read=MAX(user_reading_activity.pages_read,excluded.pages_read)`, userID, bookID, ActivityDayKey(LocalWallClock(at)), pages)
	return err
}
