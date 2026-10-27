// Package seriesediting owns complete manual series metadata saves.
// Source rules remain in seriesmeta; this package owns atomic storage and the saved view.
package seriesediting

import (
	"context"
	"errors"
	"strings"

	"manga-manager/internal/database"
	"manga-manager/internal/seriesmeta"
)

// Transaction exposes only the metadata save and its required derived statistics.
type Transaction interface {
	seriesmeta.ManualQueries
	RefreshSeriesStats(context.Context, int64) error
}

// Store keeps the complete save inside one write transaction.
type Store interface {
	Edit(context.Context, func(Transaction) error) error
}

// Result contains the committed view or the current version of a rejected edit.
type Result struct {
	Series         database.Series
	CurrentVersion string
}

// ErrInvalidVersion rejects an explicitly supplied empty concurrency token.
var ErrInvalidVersion = errors.New("expected_version must not be empty; omit the field to opt out of concurrency checks")

// Service owns manual editing's transaction, source rules, statistics and response read.
type Service struct{ store Store }

// New creates a manual editor with a transaction capability scoped to metadata.
func New(store Store) *Service { return &Service{store: store} }

// Save preserves scalar replacement and absent-versus-empty collection semantics.
// A failed save commits nothing. The successful result needs no post-commit reread.
func (s *Service) Save(ctx context.Context, seriesID int64, input seriesmeta.ManualInput) (Result, error) {
	var result Result
	if input.ExpectedVersion != nil && strings.TrimSpace(*input.ExpectedVersion) == "" {
		return result, ErrInvalidVersion
	}
	err := s.store.Edit(ctx, func(q Transaction) error {
		current, err := q.GetSeries(ctx, seriesID)
		if err != nil {
			return err
		}
		result.CurrentVersion, err = seriesmeta.SaveManual(ctx, q, current, input)
		if err != nil {
			return err
		}
		if err := q.RefreshSeriesStats(ctx, seriesID); err != nil {
			return err
		}
		result.Series, err = q.GetSeries(ctx, seriesID)
		return err
	})
	if err != nil {
		result.Series = database.Series{}
	}
	return result, err
}
