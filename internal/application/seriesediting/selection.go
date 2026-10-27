package seriesediting

import (
	"context"
	"log/slog"

	"manga-manager/internal/seriesmeta"
)

// SelectionTransaction owns one atomic incremental edit across the selected series.
type SelectionTransaction interface {
	seriesmeta.SelectionQueries
	ExistingSeriesIDs(context.Context, []int64) ([]int64, error)
}

// SelectionStore separates atomic metadata changes from best-effort statistics refreshes.
type SelectionStore interface {
	EditSelection(context.Context, func(SelectionTransaction) error) error
	RefreshSeriesStats(context.Context, int64) error
}

// SelectionResult records the committed selection and any later statistics failures.
// Updated retains the public requested count, including duplicate and absent IDs.
type SelectionResult struct {
	Updated          int
	Committed        bool
	SeriesIDs        []int64
	RefreshFailedIDs []int64
}

// SelectionService owns incremental editing and its post-commit projection effects.
type SelectionService struct {
	store      SelectionStore
	invalidate func()
}

// NewSelection accepts only selection storage and the metadata cache invalidation effect.
func NewSelection(store SelectionStore, invalidate func()) *SelectionService {
	return &SelectionService{store: store, invalidate: invalidate}
}

// Edit commits the whole selection or nothing; subsequent refresh failures never undo metadata.
func (s *SelectionService) Edit(ctx context.Context, ids []int64, input seriesmeta.SelectionInput) (SelectionResult, error) {
	result := SelectionResult{SeriesIDs: []int64{}, RefreshFailedIDs: []int64{}}
	if len(ids) == 0 {
		return result, nil
	}
	err := s.store.EditSelection(ctx, func(q SelectionTransaction) error {
		if err := seriesmeta.EditSelection(ctx, q, ids, input); err != nil {
			return err
		}
		var err error
		result.SeriesIDs, err = q.ExistingSeriesIDs(ctx, ids)
		return err
	})
	if err != nil {
		result.SeriesIDs = []int64{}
		return result, err
	}
	result.Committed = true
	result.Updated = len(ids)
	for _, id := range result.SeriesIDs {
		if err := s.store.RefreshSeriesStats(ctx, id); err != nil {
			result.RefreshFailedIDs = append(result.RefreshFailedIDs, id)
			slog.WarnContext(ctx, "refresh series stats after bulk edit failed", "series_id", id, "error", err)
		}
	}
	if s.invalidate != nil {
		s.invalidate()
	}
	return result, nil
}
