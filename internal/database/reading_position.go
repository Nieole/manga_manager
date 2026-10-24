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
	Clear      bool
	Page       int64
	OccurredAt time.Time
}

// MutateReadingPosition applies one decision with the same transaction guarantees as a batch.
func (s *SqlStore) MutateReadingPosition(ctx context.Context, userID, bookID int64, factID string, decide func(ReadingPositionState) ReadingPositionMutation) (ReadingPositionMutation, error) {
	results, err := s.MutateReadingPositions(ctx, userID, []int64{bookID}, factID, func(_ int64, state ReadingPositionState) ReadingPositionMutation { return decide(state) })
	if err != nil {
		return ReadingPositionMutation{}, err
	}
	result, found := results[bookID]
	if !found {
		return result, sql.ErrNoRows
	}
	return result, nil
}

// MutateReadingPositions atomically persists a bounded batch, receipts and affected series projections.
// Missing books are omitted. Decisions run while SQLite holds the write lock; a failed batch returns no commits.
func (s *SqlStore) MutateReadingPositions(ctx context.Context, userID int64, bookIDs []int64, factID string, decide func(int64, ReadingPositionState) ReadingPositionMutation) (map[int64]ReadingPositionMutation, error) {
	results := make(map[int64]ReadingPositionMutation)
	err := s.ExecTx(ctx, func(q *Queries) error {
		affected := make(map[int64]struct{})
		for _, bookID := range bookIDs {
			book, err := q.GetBook(ctx, bookID)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			state, err := loadReadingPosition(ctx, q, userID, book, factID)
			if err != nil {
				return err
			}
			mutation := decide(bookID, state)
			results[bookID] = mutation
			if !mutation.Apply {
				continue
			}
			if err := persistReadingPosition(ctx, q, userID, bookID, factID, mutation); err != nil {
				return err
			}
			affected[book.SeriesID] = struct{}{}
		}
		for seriesID := range affected {
			if userID > 0 {
				if err := refreshUserSeriesProgressTx(ctx, q, userID, seriesID); err != nil {
					return err
				}
			} else {
				if _, err := q.db.ExecContext(ctx, refreshSeriesStatsStatement("s.id = ?"), seriesID); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}

func loadReadingPosition(ctx context.Context, q *Queries, userID int64, book Book, factID string) (ReadingPositionState, error) {
	state := ReadingPositionState{PageCount: book.PageCount, Page: book.LastReadPage.Int64, LastReadAt: book.LastReadAt}
	if userID > 0 {
		state.Page = 0
		state.LastReadAt = sql.NullTime{}
		err := q.db.QueryRowContext(ctx, `SELECT COALESCE(last_read_page,0),last_read_at FROM user_book_progress WHERE user_id=? AND book_id=?`, userID, book.ID).Scan(&state.Page, &state.LastReadAt)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return state, err
		}
	}
	if factID != "" {
		var exists int
		err := q.db.QueryRowContext(ctx, `SELECT 1 FROM reading_position_receipts WHERE user_id=? AND book_id=? AND fact_id=?`, userID, book.ID, factID).Scan(&exists)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return state, err
		}
		state.Duplicate = err == nil
	}
	err := q.db.QueryRowContext(ctx, `SELECT changed_at_ns FROM reading_position_basis WHERE user_id=? AND book_id=?`, userID, book.ID).Scan(&state.ChangedAtNS)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return state, err
}

func persistReadingPosition(ctx context.Context, q *Queries, userID, bookID int64, factID string, m ReadingPositionMutation) error {
	if userID > 0 {
		if m.Clear {
			if _, err := q.db.ExecContext(ctx, `DELETE FROM user_book_progress WHERE user_id=? AND book_id=?`, userID, bookID); err != nil {
				return err
			}
		} else {
			if _, err := q.db.ExecContext(ctx, `INSERT INTO user_book_progress(user_id,book_id,last_read_page,last_read_at) VALUES(?,?,?,?) ON CONFLICT(user_id,book_id) DO UPDATE SET last_read_page=excluded.last_read_page,last_read_at=excluded.last_read_at,updated_at=CURRENT_TIMESTAMP`, userID, bookID, m.Page, LocalWallClock(m.OccurredAt)); err != nil {
				return err
			}
		}
	} else {
		page := sql.NullInt64{Int64: m.Page, Valid: !m.Clear}
		at := sql.NullTime{Time: LocalWallClock(m.OccurredAt), Valid: !m.Clear}
		if err := q.UpdateBookProgress(ctx, UpdateBookProgressParams{ID: bookID, LastReadPage: page, LastReadAt: at}); err != nil {
			return err
		}
	}
	if _, err := q.db.ExecContext(ctx, `INSERT INTO reading_position_basis(user_id,book_id,changed_at_ns,fact_id) VALUES(?,?,?,?) ON CONFLICT(user_id,book_id) DO UPDATE SET changed_at_ns=excluded.changed_at_ns,fact_id=excluded.fact_id`, userID, bookID, m.OccurredAt.UnixNano(), factID); err != nil {
		return err
	}
	if factID != "" {
		_, err := q.db.ExecContext(ctx, `INSERT INTO reading_position_receipts(user_id,book_id,fact_id) VALUES(?,?,?)`, userID, bookID, factID)
		return err
	}
	return nil
}

// LogUserReadingActivityAt projects a proven reading date, retaining daily maximum absolute page semantics.
func (s *SqlStore) LogUserReadingActivityAt(ctx context.Context, userID, bookID, pages int64, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO user_reading_activity(user_id,book_id,date,pages_read) VALUES(?,?,?,?) ON CONFLICT(user_id,book_id,date) DO UPDATE SET pages_read=MAX(user_reading_activity.pages_read,excluded.pages_read)`, userID, bookID, ActivityDayKey(LocalWallClock(at)), pages)
	return err
}
