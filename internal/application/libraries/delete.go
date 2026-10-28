// Package libraries owns complete library management operations and their access handoff.
package libraries

import (
	"context"
	"errors"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/task"
)

// Store supplies current library facts and atomic deletion with its FK cascades.
type Store interface {
	GetLibrary(context.Context, int64) (database.Library, error)
	DeleteLibrary(context.Context, int64) error
	LibraryRelocationProtection(context.Context, int64) (*database.LibraryRelocationProtection, error)
}

// Watcher is the process-owned filesystem subscription used by library management.
type Watcher interface {
	UnwatchLibrary(string)
	WatchLibrary(int64, string, string) error
}

type Effects struct {
	Watcher     Watcher
	AfterDelete func(int64)
}

// DeleteResult keeps occupied, uncommitted and committed outcomes distinguishable.
type DeleteResult struct {
	Deleted   bool                   `json:"deleted"`
	LibraryID int64                  `json:"library_id"`
	Conflicts []task.LibraryConflict `json:"conflicts,omitempty"`
	Warnings  []string               `json:"warnings,omitempty"`
}

type Service struct {
	store   Store
	engine  *task.Engine
	effects Effects
}

func New(store Store, engine *task.Engine, effects Effects) *Service {
	return &Service{store: store, engine: engine, effects: effects}
}

// Delete closes admission before checking occupiers. Busy libraries remain unchanged.
// Cancellation and bounded draining of eligible runs are separate from this immediate operation.
func (s *Service) Delete(ctx context.Context, id int64) (DeleteResult, error) {
	result := DeleteResult{LibraryID: id}
	// Only existence is checked here; path facts are loaded again after admission closes.
	if _, err := s.store.GetLibrary(ctx, id); err != nil {
		return result, err
	}
	reservation, err := s.engine.ReserveLibrary(ctx, id)
	if err != nil {
		return deleteConflict(result, err)
	}
	defer reservation.Release()
	library, err := s.store.GetLibrary(ctx, id)
	if err != nil {
		return result, err
	}
	if err := reservation.Upgrade(ctx); err != nil {
		return deleteConflict(result, err)
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if s.effects.Watcher != nil {
		s.effects.Watcher.UnwatchLibrary(library.Path)
	}
	if err := s.store.DeleteLibrary(ctx, id); err != nil {
		if restoreErr := s.restoreWatcher(ctx, id); restoreErr != nil {
			result.Warnings = append(result.Warnings, "watcher_restore_failed")
		}
		return result, err
	}
	result.Deleted = true
	if err := reservation.Retire(); err != nil {
		result.Warnings = append(result.Warnings, "access_retirement_failed")
	}
	if s.effects.AfterDelete != nil {
		s.effects.AfterDelete(id)
	}
	return result, nil
}

func deleteConflict(result DeleteResult, err error) (DeleteResult, error) {
	var busy *task.LibraryBusyError
	if errors.As(err, &busy) {
		result.Conflicts = busy.Conflicts
	}
	return result, err
}

func (s *Service) restoreWatcher(ctx context.Context, id int64) error {
	if s.effects.Watcher == nil {
		return nil
	}
	restoreCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	current, err := s.store.GetLibrary(restoreCtx, id)
	if err != nil {
		return err
	}
	if current.ScanMode != "watch" {
		return nil
	}
	protection, err := s.store.LibraryRelocationProtection(restoreCtx, id)
	if err != nil {
		return err
	}
	if protection != nil {
		return nil
	}
	return s.effects.Watcher.WatchLibrary(current.ID, current.Path, current.ScanFormats)
}
