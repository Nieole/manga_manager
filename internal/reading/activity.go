package reading

import (
	"context"
	"log/slog"
	"time"

	"manga-manager/internal/database"
)

// recordActivity projects the maximum absolute page per book and day, not page-turn increments.
// Compatibility projections are independent and never turn a committed position into a failed save.
func (s *Service) recordActivity(ctx context.Context, userID, bookID, pages int64, at time.Time) []error {
	var warnings []error
	warn := func(err error) {
		if err != nil {
			warnings = append(warnings, err)
			slog.WarnContext(ctx, "Reading state saved; activity projection failed", "user_id", userID, "book_id", bookID, "error", err)
		}
	}
	warn(s.store.LogReadingActivity(ctx, database.LogReadingActivityParams{BookID: bookID, PagesRead: pages, Date: database.ActivityDayKey(database.LocalWallClock(at))}))
	if userID > 0 {
		warn(s.store.LogUserReadingActivityAt(ctx, userID, bookID, pages, at))
	}
	return warnings
}
