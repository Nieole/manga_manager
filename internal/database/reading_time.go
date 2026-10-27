package database

import (
	"context"
	"database/sql"
	"errors"
)

// AccumulateReadingTimeForBook commits lifetime and daily time for an existing book.
// A missing book is ignored; callers never receive transaction or query capabilities.
func (s *SqlStore) AccumulateReadingTimeForBook(ctx context.Context, userID, bookID, seconds int64, day string) error {
	return s.ExecTx(ctx, func(q *Queries) error {
		if _, err := q.GetBook(ctx, bookID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		return q.AccumulateReadingTime(ctx, userID, bookID, seconds, day)
	})
}
