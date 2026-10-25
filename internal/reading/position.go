// Package reading owns reading position rules, persistence coordination and activity follow-up.
// Inputs identify their reader explicitly; transport parsing stays with adapters.
package reading

import (
	"context"
	"errors"
	"time"

	"manga-manager/internal/database"
)

// Position identifies one reading fact. OccurredAt and FactID survive transport retries.
type Position struct {
	UserID int64
	BookID int64
	Page   int64
	// OccurredAt is client or historical fact time and survives replay.
	OccurredAt *time.Time
	// ReceivedAt is trusted server receipt time for a fresh report, never HTTP input.
	// It is mutually exclusive with OccurredAt; RecordPosition rejects both together.
	ReceivedAt *time.Time
	FactID     string
}

// ErrConflictingPositionTimes rejects reports with ambiguous time provenance.
var ErrConflictingPositionTimes = errors.New("position occurrence and receipt times are mutually exclusive")

// Result distinguishes a committed position from best-effort activity warnings.
type Result struct {
	Applied  bool
	Stale    bool
	Page     int64
	Warnings []error
}

// Store exposes the atomic position operation and the independent activity projections.
type Store interface {
	ResolveReadingOperationTime(context.Context, int64, string, time.Time) (time.Time, error)
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
	now        func() time.Time
}

// New connects durable state and the affected read-cache invalidation.
func New(store Store, invalidate func(), options ...Option) *Service {
	service := &Service{store: store, invalidate: invalidate, now: time.Now}
	for _, option := range options {
		option(service)
	}
	return service
}

// Option configures a service dependency.
type Option func(*Service)

// WithClock supplies the occurrence clock for reports without a client timestamp.
func WithClock(now func() time.Time) Option {
	return func(service *Service) {
		if now != nil {
			service.now = now
		}
	}
}

// RecordPosition saves a newer position and attempts each compatibility activity projection independently.
func (s *Service) RecordPosition(ctx context.Context, p Position) (Result, error) {
	if p.OccurredAt != nil && p.ReceivedAt != nil {
		return Result{}, ErrConflictingPositionTimes
	}
	stale := false
	saved, err := s.store.MutateReadingPosition(ctx, p.UserID, p.BookID, p.FactID, func(state database.ReadingPositionState) database.ReadingPositionMutation {
		at := s.now()
		if p.OccurredAt != nil {
			at = *p.OccurredAt
		} else if p.ReceivedAt != nil {
			at = *p.ReceivedAt
		}
		page := p.Page
		if page < 1 {
			page = 1
		}
		if state.PageCount > 0 && page > state.PageCount {
			page = state.PageCount
		}
		retained := database.ReadingPositionMutation{Page: state.Page}
		freshReceive := p.OccurredAt == nil && (p.ReceivedAt != nil || p.FactID == "")
		if state.Duplicate {
			return retained
		}
		if staleOccurrence(at, state.ChangedAtNS, freshReceive) {
			stale = !state.LastReadAt.Valid || !at.Equal(state.LastReadAt.Time) || page != state.Page
			return retained
		}
		if state.LastReadAt.Valid && at.Before(state.LastReadAt.Time) {
			stale = true
			return retained
		}
		// Legacy clients cannot identify retries. Bound their repeated same-page writes,
		// while timestamped reading facts always retain the newer occurrence.
		if p.OccurredAt == nil && p.ReceivedAt == nil && p.FactID == "" && state.LastReadAt.Valid && page == state.Page && at.Sub(state.LastReadAt.Time) < 2*time.Second {
			return retained
		}
		if p.ReceivedAt == nil && state.LastReadAt.Valid && page == state.Page && at.Equal(state.LastReadAt.Time) {
			return retained
		}
		return database.ReadingPositionMutation{Apply: true, Page: page, OccurredAt: at}
	})
	result := Result{Applied: saved.Apply, Stale: stale, Page: saved.Page}
	if err != nil || !saved.Apply {
		return result, err
	}
	if s.invalidate != nil {
		defer s.invalidate()
	}
	result.Warnings = s.recordActivity(ctx, p.UserID, p.BookID, saved.Page, saved.OccurredAt)
	return result, nil
}

// Equal receive-clock ticks order fresh server-received inputs by their transaction;
// facts carrying occurrence evidence remain subject to strict replay rejection.
func staleOccurrence(at time.Time, changedAtNS int64, freshLegacy bool) bool {
	return at.UnixNano() < changedAtNS || (at.UnixNano() == changedAtNS && !freshLegacy)
}
