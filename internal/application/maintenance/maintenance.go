// Package maintenance orchestrates system maintenance and KOReader calibration runs.
package maintenance

import (
	"context"
	"errors"
	"manga-manager/internal/application"
	"manga-manager/internal/application/scanning"
	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/koreader"
	"manga-manager/internal/runhandle"
	"manga-manager/internal/task"
)

// Store supplies maintenance candidates and derived-index writes.
type Store interface {
	ListLibraries(context.Context) ([]database.Library, error)
	RebuildSeriesSearchIndex(context.Context) error
	RebuildBookSearchIndex(context.Context) error
	ClearAllBookCoverPaths(context.Context) error
	ClearAllSeriesStatsCoverPaths(context.Context) error
	CountBooksMissingQuickHash(context.Context) (int64, error)
	ListBooksMissingQuickHashBatch(context.Context, int64, int) ([]database.BookIdentityCandidate, error)
	UpdateBookIdentity(context.Context, database.UpdateBookIdentityParams) error
	CountBooksMissingIdentity(context.Context, string) (int64, error)
}

// Effects publishes completed maintenance changes.
type Effects struct {
	Publish   func(string)
	WarmStats func(string)
}

// Service composes maintenance with scanning and reading capabilities.
type Service struct {
	runs       *application.Runs
	store      Store
	scans      *scanning.Service
	koreader   *koreader.Service
	config     func() config.Config
	effects    Effects
	thumbnails *ThumbnailProgress
}

// New binds complete capabilities before launches and restart replay.
func New(runs *application.Runs, store Store, scans *scanning.Service, reader *koreader.Service, cfg func() config.Config, effects Effects) *Service {
	if effects.Publish == nil {
		effects.Publish = func(string) {}
	}
	if effects.WarmStats == nil {
		effects.WarmStats = func(string) {}
	}
	return &Service{runs: runs, store: store, scans: scans, koreader: reader, config: cfg, effects: effects, thumbnails: NewThumbnailProgress()}
}
func failure(code string, err error) task.Result {
	if errors.Is(err, context.Canceled) {
		return task.Result{}
	}
	return task.Result{Code: code}
}

// RebuildIndex rebuilds both search indexes and waits for scanning in the same admitted run.
func (c *Service) RebuildIndex(trigger task.Trigger) (task.Launched, error) {
	spec := task.RunSpec{
		Identity:     task.Identity{Type: "rebuild_index", Scope: task.ScopeSystem},
		Trigger:      trigger,
		LibraryIDs:   []int64{task.AllLibraries},
		Key:          "rebuild_index",
		StartCode:    "task.msg.rebuild_index.start",
		Total:        1,
		CompleteCode: "task.msg.rebuild_index.complete",
		CancelCode:   "task.msg.rebuild_index.cancelled",
		FailCode:     "task.msg.rebuild_index.failed",
	}
	return c.runs.Start(context.Background(), spec, func(ctx context.Context, handle *runhandle.Handle) (task.Result, error) {
		if err := c.store.RebuildSeriesSearchIndex(ctx); err != nil {
			return failure("task.msg.rebuild_index.series_failed", err), err
		}
		if err := c.store.RebuildBookSearchIndex(ctx); err != nil {
			return failure("task.msg.rebuild_index.book_failed", err), err
		}
		libs, err := c.store.ListLibraries(ctx)
		if err != nil {
			return task.Result{}, err
		}
		return task.Result{}, c.scans.ReindexLibraries(ctx, handle, libs)
	})
}
