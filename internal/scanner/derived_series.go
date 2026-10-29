package scanner

import (
	"context"
	"log/slog"
	"time"
)

type derivedSeriesStore interface {
	RefreshSeriesDerivedData(context.Context, int64) error
}

// derivedSeries retains committed dirty facts until refresh succeeds, including scan cancellation.
// Batch writers never recalculate every large series for each batch.
type derivedSeries struct {
	store  derivedSeriesStore
	dirty  map[int64]bool
	warned map[int64]bool
}

func newDerivedSeries(store derivedSeriesStore) *derivedSeries {
	return &derivedSeries{store: store, dirty: make(map[int64]bool), warned: make(map[int64]bool)}
}
func (d *derivedSeries) touch(ids map[int64]bool) {
	for id := range ids {
		d.dirty[id] = true
	}
}
func (d *derivedSeries) refresh(ctx context.Context) {
	for id := range d.dirty {
		if err := d.store.RefreshSeriesDerivedData(ctx, id); err != nil {
			if !d.warned[id] {
				d.warned[id] = true
				slog.WarnContext(ctx, "Failed to refresh series derived data, keeping it dirty for a later retry", "series_id", id, "err", err)
			}
			continue
		}
		delete(d.dirty, id)
		delete(d.warned, id)
	}
}

func (d *derivedSeries) drain(ctx context.Context, metrics *scanMetrics) {
	if len(d.dirty) == 0 {
		return
	}
	drainCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	for id := range d.dirty {
		one, cancelOne := context.WithTimeout(drainCtx, time.Second)
		err := d.store.RefreshSeriesDerivedData(one, id)
		cancelOne()
		if err == nil {
			delete(d.dirty, id)
		}
	}
	if remaining := len(d.dirty); remaining > 0 {
		metrics.staleSeriesStats.Add(int64(remaining))
		slog.ErrorContext(ctx, "Scan finished with stale series statistics", "stale_series", remaining, "hint", "these series keep outdated book_count/total_pages until they are scanned again")
	}
}
