// Package reading owns reading position rules, persistence coordination and activity follow-up.
// Inputs identify their reader explicitly; transport parsing stays with adapters.
package reading

import (
	"context"
	"log/slog"
	"time"

	"manga-manager/internal/database"
)

// Position identifies one reading fact. OccurredAt and FactID survive transport retries.
type Position struct {
	UserID     int64
	BookID     int64
	Page       int64
	OccurredAt *time.Time
	FactID     string
}

// Result distinguishes a committed position from best-effort activity warnings.
type Result struct {
	Applied  bool
	Page     int64
	Warnings []error
}

// Store exposes the atomic position operation and the independent activity projections.
type Store interface {
	MutateReadingPositions(context.Context, int64, []int64, string, func(int64, database.ReadingPositionState) database.ReadingPositionMutation) (map[int64]database.ReadingPositionMutation, error)
	ListBooksBySeries(context.Context, int64) ([]database.Book, error)
	MutateReadingPosition(context.Context, int64, int64, string, func(database.ReadingPositionState) database.ReadingPositionMutation) (database.ReadingPositionMutation, error)
	LogReadingActivity(context.Context, database.LogReadingActivityParams) error
	LogUserReadingActivityAt(context.Context, int64, int64, int64, time.Time) error
}

// Service owns complete reading use cases.
type Service struct {
	store      Store
	invalidate func()
}

// New connects durable state and the affected read-cache invalidation.
func New(store Store, invalidate func()) *Service {
	return &Service{store: store, invalidate: invalidate}
}

// RecordPosition saves a newer position and attempts each compatibility activity projection independently.
func (s *Service) RecordPosition(ctx context.Context, p Position) (Result, error) {
	saved, err := s.store.MutateReadingPosition(ctx, p.UserID, p.BookID, p.FactID, func(state database.ReadingPositionState) database.ReadingPositionMutation {
		at := time.Now()
		if p.OccurredAt != nil {
			at = *p.OccurredAt
		} else if at.UnixNano() == state.ChangedAtNS {
			at = at.Add(time.Nanosecond)
		}
		retained := database.ReadingPositionMutation{Page: state.Page}
		if state.Duplicate || at.UnixNano() <= state.ChangedAtNS {
			return retained
		}
		if state.LastReadAt.Valid && at.Before(state.LastReadAt.Time) {
			return retained
		}
		page := p.Page
		if page < 1 {
			page = 1
		}
		if state.PageCount > 0 && page > state.PageCount {
			page = state.PageCount
		}
		if state.LastReadAt.Valid && page == state.Page && at.Equal(state.LastReadAt.Time) {
			return retained
		}
		return database.ReadingPositionMutation{Apply: true, Page: page, OccurredAt: at}
	})
	result := Result{Applied: saved.Apply, Page: saved.Page}
	if err != nil || !saved.Apply {
		return result, err
	}
	if s.invalidate != nil {
		defer s.invalidate()
	}
	warn := func(err error) {
		if err != nil {
			result.Warnings = append(result.Warnings, err)
			slog.WarnContext(ctx, "Reading position saved; activity projection failed", "user_id", p.UserID, "book_id", p.BookID, "error", err)
		}
	}
	warn(s.store.LogReadingActivity(ctx, database.LogReadingActivityParams{BookID: p.BookID, PagesRead: saved.Page, Date: database.ActivityDayKey(database.LocalWallClock(saved.OccurredAt))}))
	if p.UserID > 0 {
		warn(s.store.LogUserReadingActivityAt(ctx, p.UserID, p.BookID, saved.Page, saved.OccurredAt))
	}
	return result, nil
}
