// Package diskjobs owns external-library and ComicInfo background use cases.
// Session plans remain in external, metadata projection in seriesmeta, and archive writes in parser.
package diskjobs

import (
	"context"
	"fmt"
	"strconv"

	"manga-manager/internal/application"
	"manga-manager/internal/database"
	"manga-manager/internal/external"
	"manga-manager/internal/runhandle"
	"manga-manager/internal/task"
)

// Store supplies the current library identity for launch declarations.
type Store interface {
	application.LibraryRoots
	GetLibrary(context.Context, int64) (database.Library, error)
	GetSeries(context.Context, int64) (database.Series, error)
	ListBooksBySeries(context.Context, int64) ([]database.Book, error)
	GetTagsForSeries(context.Context, int64) ([]database.Tag, error)
	GetAuthorsForSeries(context.Context, int64) ([]database.Author, error)
}

// Effects supplies semantic publication at the application completion boundary.
type Effects struct{ Publish func(string) }

// Service launches disk use cases through the process's shared run entry.
type Service struct {
	runs     *application.Runs
	store    Store
	external *external.Manager
	effects  Effects
}

// New binds domain capabilities before any disk work can be admitted.
func New(runs *application.Runs, store Store, manager *external.Manager, effects Effects) *Service {
	return &Service{runs: runs, store: store, external: manager, effects: effects}
}

// SessionLaunch contains the compatible session projection and admitted scan run.
type SessionLaunch struct {
	Session external.SessionSnapshot
	Run     task.Launched
}

// CreateExternalSession owns session creation, scan launch and failed-launch cleanup.
func (c *Service) CreateExternalSession(ctx context.Context, libraryID int64, path string, ignoreExtension bool) (SessionLaunch, error) {
	session, err := c.external.CreateSession(ctx, libraryID, path, ignoreExtension)
	if err != nil {
		return SessionLaunch{}, err
	}
	run, err := c.ScanExternal(ctx, libraryID, session.SessionID)
	if err != nil {
		c.external.ClearSession(libraryID, session.SessionID)
		return SessionLaunch{}, &LaunchError{err}
	}
	return SessionLaunch{Session: session, Run: run}, nil
}

// ExternalScanKey is the diagnostic task key shared with transport responses.
func ExternalScanKey(libraryID int64, sessionID string) string {
	return fmt.Sprintf("scan_external_library_%s_%d", sessionID, libraryID)
}

// ScanExternal launches an existing, nonpersistent session without reconstructing it.
func (c *Service) ScanExternal(ctx context.Context, libraryID int64, sessionID string) (task.Launched, error) {
	spec := task.RunSpec{
		Identity: task.Identity{Type: "scan_external_library", Scope: task.ScopeLibrary, ScopeID: libraryID},
		Trigger:  task.TriggerManual, LibraryIDs: []int64{libraryID},
		Key:       ExternalScanKey(libraryID, sessionID),
		StartCode: "task.msg.scan_external_library.start",
		CanCancel: true, CanPause: true,
		Args:         map[string]string{"session_id": sessionID},
		CompleteCode: "task.msg.scan_external_library.complete",
		CancelCode:   "task.msg.scan_external_library.cancelled",
		FailCode:     "task.msg.scan_external_library.failed",
	}
	if c.store != nil {
		if lib, err := c.store.GetLibrary(ctx, libraryID); err == nil {
			spec.ScopeName = lib.Name
		}
	}
	session, err := c.external.GetSession(libraryID, sessionID)
	if err != nil {
		return task.Launched{}, err
	}
	if err = c.cover(ctx, &spec, libraryID, []string{session.ExternalPath}); err != nil {
		return task.Launched{}, err
	}
	validate := spec.ValidateAccess
	spec.ValidateAccess = func() error {
		if validate != nil {
			if err := validate(); err != nil {
				return err
			}
		}
		_, err := c.external.GetSession(libraryID, sessionID)
		return err
	}
	return c.runs.Start(ctx, spec, func(ctx context.Context, handle *runhandle.Handle) (task.Result, error) {
		snapshot, err := c.external.ScanSession(ctx, sessionID, externalScanHandle{handle})
		if err != nil {
			return task.Result{}, err
		}
		c.publish()
		if snapshot.ScannedFiles == 0 {
			return task.Result{Code: "task.msg.scan_external_library.complete_empty"}, nil
		}
		return task.Result{}, nil
	})
}

func (c *Service) publish() {
	if c.effects.Publish != nil {
		c.effects.Publish("refresh")
	}
}

type externalScanHandle struct{ *runhandle.Handle }

func (h externalScanHandle) Advance(current, total int) {
	h.Report(runhandle.Frame{Current: &current, Total: &total, Phase: "discovering", Code: "task.msg.scan_external_library.progress", Params: map[string]string{"count": strconv.Itoa(current)}, Metrics: map[string]int64{"scanned_files": int64(current)}})
}

func (c *Service) cover(ctx context.Context, spec *task.RunSpec, owner int64, paths []string) error {
	if c.store == nil {
		return nil
	}
	ids, validate, err := application.CoverPaths(ctx, c.store, owner, paths)
	if err != nil {
		return err
	}
	previous := spec.ValidateAccess
	spec.LibraryIDs = ids
	spec.ValidateAccess = func() error {
		if err := validate(); err != nil {
			return err
		}
		if previous != nil {
			return previous()
		}
		return nil
	}
	return nil
}
