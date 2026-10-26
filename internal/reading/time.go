package reading

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"

	"manga-manager/internal/database"
)

// TimeReport is a best-effort increment of active seconds, without historical date evidence.
type TimeReport struct{ UserID, BookID, Seconds int64 }

// RecordTime attributes an increment to its reader and receipt day. Time and daily
// activity commit together; failure never rolls back an independently saved position.
// Callers must not blindly retry: a lost response may follow a successful commit.
func (s *Service) RecordTime(ctx context.Context, report TimeReport) error {
	if report.UserID == 0 || report.Seconds <= 0 {
		return nil
	}
	if report.Seconds > 3600 {
		report.Seconds = 3600
	}
	day := database.ActivityDayKey(database.LocalWallClock(s.now()))
	err := s.store.ExecTx(ctx, func(q *database.Queries) error {
		if _, err := q.GetBook(ctx, report.BookID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		return q.AccumulateReadingTime(ctx, report.UserID, report.BookID, report.Seconds, day)
	})
	if err != nil {
		slog.WarnContext(ctx, "Failed to record reading time", "user_id", report.UserID, "book_id", report.BookID, "error", err)
	}
	return err
}
