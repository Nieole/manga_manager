package reading

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
)

// SyncResult retains one input's outcome without aborting independent later inputs.
type SyncResult struct {
	BookID   int64
	Status   string
	Page     int64
	Message  string
	Warnings []error
}

// SyncPositions applies the same position use case in input order, retaining partial success.
func (s *Service) SyncPositions(ctx context.Context, positions []Position) []SyncResult {
	results := make([]SyncResult, 0, len(positions))
	for _, position := range positions {
		item := SyncResult{BookID: position.BookID}
		if position.BookID <= 0 {
			item.Status, item.Message = "invalid", "book_id is required"
		} else {
			result, err := s.RecordPosition(ctx, position)
			item.Page, item.Warnings = result.Page, result.Warnings
			switch {
			case errors.Is(err, sql.ErrNoRows):
				item.Status = "not_found"
			case err != nil:
				slog.ErrorContext(ctx, "Reading position sync failed", "user_id", position.UserID, "book_id", position.BookID, "error", err)
				item.Status, item.Message = "invalid", "update failed"
			case result.Applied:
				item.Status = "updated"
			case result.Stale:
				item.Status, item.Message = "skipped_stale", "server has newer progress"
			default:
				item.Status = "skipped_unchanged"
			}
		}
		results = append(results, item)
	}
	return results
}
