// Package libraries owns complete library management operations and their access handoff.
package libraries

import (
	"context"
	"errors"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/task"
)

// Store supplies library facts, persistence, and relocation protection.
type Store interface {
	CreateLibrary(context.Context, database.CreateLibraryParams) (database.Library, error)
	UpdateLibrary(context.Context, database.UpdateLibraryParams) (database.Library, error)
	ListLibraries(context.Context) ([]database.Library, error)
	LibraryHasRecords(context.Context, int64) (bool, error)
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
	Watcher          Watcher
	AfterDelete      func(int64)
	GuardRoot        func(string) (func(), error)
	StartInitialScan func(database.Library) (task.Launched, error)
	AfterSave        func()
}

// DeleteResult keeps occupied, uncommitted and committed outcomes distinguishable.
type DeleteResult struct {
	task.LibraryDrainResult
	Deleted       bool     `json:"deleted"`
	LibraryID     int64    `json:"library_id"`
	Warnings      []string `json:"warnings,omitempty"`
	DrainTimedOut bool     `json:"drain_timed_out,omitempty"`
}

type Service struct {
	store   Store
	engine  *task.Engine
	effects Effects
}

func New(store Store, engine *task.Engine, effects Effects) *Service {
	return &Service{store: store, engine: engine, effects: effects}
}

// Delete closes admission, cancels eligible local work and drains actual accesses
// for at most two seconds before obtaining exclusive deletion access.
func (s *Service) Delete(ctx context.Context, id int64) (result DeleteResult, err error) {
	result = DeleteResult{LibraryID: id}
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
	if s.effects.Watcher != nil {
		s.effects.Watcher.UnwatchLibrary(library.Path)
	}
	defer func() {
		if !result.Deleted {
			if restoreErr := s.restoreWatcher(ctx, id); restoreErr != nil {
				result.Warnings = append(result.Warnings, "watcher_restore_failed")
			}
		}
	}()
	drainCtx, stopDrain := context.WithTimeout(ctx, 2*time.Second)
	result.LibraryDrainResult, err = reservation.Drain(drainCtx)
	stopDrain()
	if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if errors.Is(err, context.DeadlineExceeded) {
			result.DrainTimedOut = true
			return result, &task.LibraryBusyError{Conflicts: result.Conflicts}
		}
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := s.store.DeleteLibrary(ctx, id); err != nil {
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
