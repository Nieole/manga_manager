package reading

import (
	"context"
	"errors"
	"time"

	"manga-manager/internal/database"
)

const (
	// MaxMarkBooks bounds the expanded selection before any writes.
	MaxMarkBooks = 20000
	// MaxMarkSeries bounds the input series selection.
	MaxMarkSeries = 2000
	markBatchSize = 500
)

// ErrMarkLimit identifies selections rejected before applying changes.
var ErrMarkLimit = errors.New("too many books or series in one request")

// MarkBooksInput identifies one bulk read-state operation, stable across retries.
type MarkBooksInput struct {
	UserID      int64
	BookIDs     []int64
	IsRead      bool
	OccurredAt  *time.Time
	OperationID string
}

// MarkSeriesInput marks the current books of selected series.
type MarkSeriesInput struct {
	UserID      int64
	SeriesIDs   []int64
	IsRead      bool
	OccurredAt  *time.Time
	OperationID string
}

// MarkResult retains committed book identities even when a later batch fails.
type MarkResult struct {
	CommittedBookIDs []int64
	Warnings         []error
}

// MarkBooks applies a read state in bounded atomic batches. Clears retain durable rejection evidence.
func (s *Service) MarkBooks(ctx context.Context, input MarkBooksInput) (MarkResult, error) {
	result := MarkResult{CommittedBookIDs: []int64{}}
	if len(input.BookIDs) > MaxMarkBooks {
		return result, ErrMarkLimit
	}
	ids := uniqueIDs(input.BookIDs)
	if len(ids) == 0 {
		return result, nil
	}
	at := s.now()
	if input.OccurredAt != nil {
		at = *input.OccurredAt
	}
	factID := ""
	if input.OperationID != "" {
		factID = "mark:" + input.OperationID
		var err error
		at, err = s.store.ResolveReadingOperationTime(ctx, input.UserID, factID, at)
		if err != nil {
			return result, err
		}
	}
	defer func() {
		if len(result.CommittedBookIDs) > 0 && s.invalidate != nil {
			s.invalidate()
		}
	}()
	for start := 0; start < len(ids); start += markBatchSize {
		end := start + markBatchSize
		if end > len(ids) {
			end = len(ids)
		}
		saved, err := s.store.MutateReadingPositions(ctx, input.UserID, ids[start:end], factID, func(_ int64, state database.ReadingPositionState) database.ReadingPositionMutation {
			freshLegacy := input.OccurredAt == nil && input.OperationID == ""
			// Without client evidence, equal receive-clock ticks use transaction order.
			// Identified or timestamped operations retain strict replay rejection.
			stale := staleOccurrence(at, state.ChangedAtNS, freshLegacy)
			if state.Duplicate || stale || (state.LastReadAt.Valid && at.Before(state.LastReadAt.Time)) {
				return database.ReadingPositionMutation{Page: state.Page}
			}
			page := state.PageCount
			if page <= 0 {
				page = 0
			}
			return database.ReadingPositionMutation{IsRead: input.IsRead && state.PageCount <= 0, Apply: true, Clear: !input.IsRead, Page: page, OccurredAt: at}
		})
		if err != nil {
			return result, err
		}
		for _, id := range ids[start:end] {
			if saved[id].Apply {
				result.CommittedBookIDs = append(result.CommittedBookIDs, id)
				if input.IsRead {
					result.Warnings = append(result.Warnings, s.recordActivity(ctx, input.UserID, id, saved[id].Page, saved[id].OccurredAt)...)
				}
			}
		}
	}
	return result, nil
}

// MarkSeries expands the selection before writing, sharing the same limit, rules and commits as MarkBooks.
func (s *Service) MarkSeries(ctx context.Context, input MarkSeriesInput) (MarkResult, error) {
	empty := MarkResult{CommittedBookIDs: []int64{}}
	if len(input.SeriesIDs) > MaxMarkSeries {
		return empty, ErrMarkLimit
	}
	var ids []int64
	for _, id := range uniqueIDs(input.SeriesIDs) {
		books, err := s.store.ListBooksBySeries(ctx, id)
		if err != nil {
			return empty, err
		}
		for _, book := range books {
			ids = append(ids, book.ID)
		}
		if len(ids) > MaxMarkBooks {
			return empty, ErrMarkLimit
		}
	}
	return s.MarkBooks(ctx, MarkBooksInput{UserID: input.UserID, BookIDs: ids, IsRead: input.IsRead, OccurredAt: input.OccurredAt, OperationID: input.OperationID})
}

func uniqueIDs(ids []int64) []int64 {
	out := make([]int64, 0, len(ids))
	seen := make(map[int64]bool)
	for _, id := range ids {
		if id > 0 && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}
