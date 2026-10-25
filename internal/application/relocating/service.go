// Package relocating coordinates relocation runs, access topology and derived application state.
// Deterministic mappings, policy compensation and durable recovery stay in relocation.
package relocating

import (
	"context"
	"errors"
	"manga-manager/internal/application"
	"manga-manager/internal/database"
	"manga-manager/internal/relocation"
	"manga-manager/internal/task"
)

// Store supplies current libraries and their persisted automatic-processing protection.
type Store interface {
	GetLibrary(context.Context, int64) (database.Library, error)
	ListLibraries(context.Context) ([]database.Library, error)
	LibraryRelocationProtection(context.Context, int64) (*database.LibraryRelocationProtection, error)
}

// RunReader is the durable observation boundary, independent of the engine writer lock.
type RunReader interface {
	LoadRun(context.Context, int64) (task.Run, error)
}

// Watcher is the existing filesystem watcher resource, owned by the composition root.
type Watcher interface {
	UnwatchLibrary(string)
	WatchLibrary(int64, string, string) error
}

// Effects are leaf application capabilities; the use case owns their composition and order.
type Effects struct {
	PurgeReadingPaths  func()
	InvalidateStats    func(string)
	InvalidateExternal func(int64)
	Watcher            func() (Watcher, error)
}

// Service owns relocation collaboration while the domain owns durable decisions.
type Service struct {
	domain    *relocation.Service
	runs      *application.Runs
	engine    *task.Engine
	runReader RunReader
	store     Store
	effects   Effects
	topology  relocationTopology
}

// New wires the domain's access and completion hooks to one application owner.
func New(domain *relocation.Service, runs *application.Runs, engine *task.Engine, reader RunReader, store Store, effects Effects) *Service {
	c := &Service{domain: domain, runs: runs, engine: engine, runReader: reader, store: store, effects: effects}
	domain.Acquire = c.acquireExclusive
	domain.AcquireOperation = c.acquireOperation
	domain.Effects = c.afterMove
	domain.RestoreEffects = c.afterRestoration
	return c
}

// Recover restores reservations and configuration consistency before other inputs open.
func (c *Service) Recover(ctx context.Context) error {
	if err := c.domain.InterruptChecks(ctx); err != nil {
		return err
	}
	return c.domain.Recover(ctx)
}

func (c *Service) afterMove(ctx context.Context, op relocation.RelocationOperation) error {
	c.effects.PurgeReadingPaths()
	c.effects.InvalidateStats("library_relocated")
	c.effects.InvalidateExternal(op.LibraryID)
	lib, err := c.store.GetLibrary(ctx, op.LibraryID)
	if err != nil {
		return err
	}
	watcher, watchErr := c.effects.Watcher()
	if watcher != nil {
		watcher.UnwatchLibrary(op.SourceRoot)
	}
	if lib.ScanMode != "watch" {
		return nil
	}
	protection, err := c.store.LibraryRelocationProtection(ctx, lib.ID)
	if err != nil {
		return err
	}
	if protection != nil {
		return nil
	}
	if watchErr != nil {
		return watchErr
	}
	if watcher == nil {
		return errors.New("file watcher unavailable")
	}
	return watcher.WatchLibrary(lib.ID, lib.Path, lib.ScanFormats)
}

func (c *Service) afterRestoration(ctx context.Context, op relocation.RelocationOperation) error {
	lib, err := c.store.GetLibrary(ctx, op.LibraryID)
	if err != nil {
		return err
	}
	c.effects.PurgeReadingPaths()
	c.effects.InvalidateExternal(op.LibraryID)
	watcher, watchErr := c.effects.Watcher()
	if watcher != nil {
		watcher.UnwatchLibrary(lib.Path)
	}
	if op.ScanMode == "watch" {
		if watchErr != nil {
			return watchErr
		}
		if watcher == nil {
			return errors.New("file watcher unavailable")
		}
		if err = watcher.WatchLibrary(lib.ID, lib.Path, op.ScanFormats); err != nil {
			return err
		}
	}
	c.effects.InvalidateStats("library_restored")
	return nil
}
