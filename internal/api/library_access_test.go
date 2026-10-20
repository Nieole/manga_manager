package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"sync"
	"testing"

	"manga-manager/internal/database"
	"manga-manager/internal/external"
	"manga-manager/internal/runhandle"
	"manga-manager/internal/task"
)

func TestLibraryExclusiveAccessPausesDownloads(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, _, book := seedBookFixture(t, store, root, "Access", "Series", "1.cbz", 1)
	release, err := c.acquireLibraryExclusive(context.Background(), lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	id := strconv.FormatInt(book.ID, 10)
	rec := httptest.NewRecorder()
	c.serveBookFile(rec, requestWithRouteParam(http.MethodGet, "/api/books/"+id+"/file", nil, "bookId", id))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("download during exclusive: %d %s", rec.Code, rec.Body.String())
	}
	_, err = c.acquireLibraryExclusive(context.Background(), lib.ID)
	if !errors.Is(err, errLibraryUnavailable) {
		t.Fatalf("second exclusive: %v", err)
	}
}

func TestLibraryExclusiveSeesQueuedAndCancellingRuns(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, _, _ := seedBookFixture(t, store, root, "Queued", "Series", "1.cbz", 1)
	var bodies []func()
	c.taskEngine.runBackground = func(fn func()) { bodies = append(bodies, fn) }
	first, err := c.startLibraryScanRun(lib, false, task.TriggerManual)
	if err != nil {
		t.Fatal(err)
	}
	queued, err := c.startLibraryScanRun(lib, false, task.TriggerWatch)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.taskEngine.cancelRun(first.Run.ID); err != nil {
		t.Fatal(err)
	}
	conflicts, err := c.libraryAccessConflicts(context.Background(), lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	statuses := map[int64]task.RunStatus{}
	for _, conflict := range conflicts {
		statuses[conflict.RunID] = conflict.Status
	}
	if statuses[first.Run.ID] != task.StatusCancelling || statuses[queued.Run.ID] != task.StatusQueued {
		t.Fatalf("conflicts: %+v", conflicts)
	}
	if _, err := c.acquireLibraryExclusive(context.Background(), lib.ID); !errors.Is(err, errLibraryUnavailable) {
		t.Fatalf("exclusive before body exit: %v", err)
	}
	if err := c.taskEngine.cancelRun(queued.Run.ID); err != nil {
		t.Fatal(err)
	}
	bodies[0]()
	release, err := c.acquireLibraryExclusive(context.Background(), lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := c.startLibraryScanRun(lib, false, task.TriggerScheduled); !errors.Is(err, errLibraryUnavailable) {
		t.Fatalf("scheduled scan admitted: %v", err)
	}
}

func TestLibraryExclusiveSeesSeriesAndSystemWork(t *testing.T) {
	for _, scope := range []string{"series", "system", "cleanup"} {
		t.Run(scope, func(t *testing.T) {
			c, store, _, root := newTestController(t)
			lib, series, _ := seedBookFixture(t, store, root, "Primary", "Series", "1.cbz", 1)
			other, _, _ := seedBookFixture(t, store, root, "Other", "Series", "1.cbz", 1)
			var body func()
			c.taskEngine.runBackground = func(fn func()) { body = fn }
			identity := seriesTask("scan_series", series.ID, variantSole)
			if scope == "system" {
				identity = systemTask("rebuild_book_hashes", variantHashRebuildForeground)
			}
			if scope == "cleanup" {
				identity = libraryTask("cleanup_library", lib.ID, variantSole)
			}
			run, err := c.taskEngine.start(identity, task.TriggerManual, RunSpec{Key: "access", CanCancel: scope != "cleanup"}, func(context.Context, *runhandle.Handle) (RunResult, error) { return RunResult{}, nil })
			if err != nil {
				t.Fatal(err)
			}
			conflicts, err := c.libraryAccessConflicts(context.Background(), lib.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(conflicts) != 1 || conflicts[0].RunID != run.Run.ID || conflicts[0].CanCancel != (scope != "cleanup") {
				t.Fatalf("conflicts %+v", conflicts)
			}
			if scope != "system" {
				release, err := c.acquireLibraryExclusive(context.Background(), other.ID)
				if err != nil {
					t.Fatal(err)
				}
				release()
			}
			if scope == "cleanup" {
				if err := c.taskEngine.cancelRun(run.Run.ID); err == nil {
					t.Fatal("cleanup unexpectedly cancellable")
				}
			}
			body()
			release, err := c.acquireLibraryExclusive(context.Background(), lib.ID)
			if err != nil {
				t.Fatal(err)
			}
			release()
		})
	}
}

type libraryBlockingWriter struct {
	*httptest.ResponseRecorder
	entered chan struct{}
	resume  chan struct{}
	once    sync.Once
}

func (w *libraryBlockingWriter) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.entered); <-w.resume })
	return w.ResponseRecorder.Write(p)
}

func TestLibraryExclusiveWaitsForDownloadExit(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, _, book := seedBookFixture(t, store, root, "Reading", "Series", "1.cbz", 1)
	if err := os.WriteFile(book.Path, []byte("archive bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatInt(book.ID, 10)
	w := &libraryBlockingWriter{ResponseRecorder: httptest.NewRecorder(), entered: make(chan struct{}), resume: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.serveBookFile(w, requestWithRouteParam(http.MethodGet, "/file", nil, "bookId", id))
	}()
	<-w.entered
	if _, err := c.acquireLibraryExclusive(context.Background(), lib.ID); !errors.Is(err, errLibraryUnavailable) {
		t.Fatalf("in-flight download ignored: %v", err)
	}
	close(w.resume)
	<-done
	release, err := c.acquireLibraryExclusive(context.Background(), lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func TestLibraryExclusiveAndNewRunHaveOneWinner(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, _, _ := seedBookFixture(t, store, root, "Race", "Series", "1.cbz", 1)
	bodies := make(chan func(), 1)
	c.taskEngine.runBackground = func(fn func()) { bodies <- fn }
	for i := 0; i < 20; i++ {
		start := make(chan struct{})
		exclusive := make(chan error, 1)
		launched := make(chan error, 1)
		var release func()
		go func() {
			<-start
			var err error
			release, err = c.acquireLibraryExclusive(context.Background(), lib.ID)
			exclusive <- err
		}()
		go func() { <-start; _, err := c.startLibraryScanRun(lib, false, task.TriggerWatch); launched <- err }()
		close(start)
		exErr, runErr := <-exclusive, <-launched
		if (exErr == nil) == (runErr == nil) {
			t.Fatalf("exclusive=%v run=%v", exErr, runErr)
		}
		if release != nil {
			release()
		} else {
			conflicts, err := c.libraryAccessConflicts(context.Background(), lib.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, conflict := range conflicts {
				if err := c.taskEngine.cancelRun(conflict.RunID); err != nil {
					t.Fatal(err)
				}
			}
			(<-bodies)()
		}
	}
}

func TestLibraryRelocationOwnerCanReserveAndReleaseOnFailure(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, _, _ := seedBookFixture(t, store, root, "Owner", "Series", "1.cbz", 1)
	c.taskEngine.runBackground = runTaskBodySynchronously
	err := c.taskEngine.Run(libraryTask("relocate_library", lib.ID, variantSole), task.TriggerManual, RunSpec{Key: "relocate"}, func(ctx context.Context, _ *runhandle.Handle) (RunResult, error) {
		release, err := c.acquireLibraryExclusive(ctx, lib.ID)
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		conflicts, err := c.libraryAccessConflicts(ctx, lib.ID)
		if err != nil || len(conflicts) != 0 {
			t.Fatalf("self conflict: %+v %v", conflicts, err)
		}
		return RunResult{}, errors.New("commit failed")
	})
	if err != nil {
		t.Fatal(err)
	}
	release, err := c.acquireLibraryExclusive(context.Background(), lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func TestRelocationInvalidatesPreparedTransfers(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, series, _ := seedBookFixture(t, store, root, "Stale", "Series", "1.cbz", 1)
	session, err := c.external.CreateSession(context.Background(), lib.ID, t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.external.ScanSession(context.Background(), session.SessionID, discardScanHandle{}); err != nil {
		t.Fatal(err)
	}
	plan, err := c.external.PrepareTransfer(context.Background(), lib.ID, session.SessionID, []int64{series.ID})
	if err != nil {
		t.Fatal(err)
	}
	if plan.MissingBooks != 1 {
		t.Fatalf("expected a real prepared plan, got %+v", plan)
	}

	release, err := c.acquireLibraryExclusive(context.Background(), lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	c.external.InvalidateLibrary(lib.ID)
	release()
	if err := c.launchExternalLibraryTransferTask(lib.ID, session.SessionID, plan); !errors.Is(err, external.ErrSessionNotFound) {
		t.Fatalf("stale transfer admitted: %v", err)
	}
}

func TestExclusiveBlocksAllLibraryDispatchesAndKeepsOtherLibraryUsable(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, series, _ := seedBookFixture(t, store, root, "Blocked", "Series", "1.cbz", 1)
	other, _, book := seedBookFixture(t, store, root, "Available", "Series", "1.cbz", 1)
	if err := os.WriteFile(book.Path, []byte("another library"), 0600); err != nil {
		t.Fatal(err)
	}
	release, err := c.acquireLibraryExclusive(context.Background(), lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	for _, launch := range []func() error{
		func() error { return c.launchLibraryScanTask(lib, false, task.TriggerManual) },
		func() error { return c.launchLibraryScanTask(lib, false, task.TriggerScheduled) },
		func() error { return c.launchSeriesScanTask(series.ID, false, task.TriggerManual) },
		func() error { return c.runWatchedLibraryScan(context.Background(), lib.ID) },
		func() error { return c.runWatchedLibraryCleanup(context.Background(), lib.ID) },
		func() error { return c.launchCoverRun(lib.ID, task.TriggerChained, true) },
	} {
		if err := launch(); !errors.Is(err, errLibraryUnavailable) {
			t.Fatalf("dispatch was admitted: %v", err)
		}
	}
	id := strconv.FormatInt(book.ID, 10)
	rec := httptest.NewRecorder()
	c.serveBookFile(rec, requestWithRouteParam(http.MethodGet, "/file", nil, "bookId", id))
	if rec.Code != http.StatusOK {
		t.Fatalf("other library download: %d %s", rec.Code, rec.Body.String())
	}
	var body func()
	c.taskEngine.runBackground = func(fn func()) { body = fn }
	if err := c.launchLibraryScanTask(other, false, task.TriggerManual); err != nil {
		t.Fatal(err)
	}
	body()
}

func TestOldScanSnapshotCannotRunAfterRelocation(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, _, _ := seedBookFixture(t, store, root, "Old snapshot", "Series", "1.cbz", 1)
	release, err := c.acquireLibraryExclusive(context.Background(), lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.UpdateLibrary(context.Background(), database.UpdateLibraryParams{ID: lib.ID, Name: lib.Name, Path: t.TempDir(), ScanMode: lib.ScanMode, ScanInterval: lib.ScanInterval, ScanFormats: lib.ScanFormats, KoreaderSyncEnabled: lib.KoreaderSyncEnabled})
	release()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.startLibraryScanRun(lib, false, task.TriggerWatch); !errors.Is(err, errLibraryUnavailable) {
		t.Fatalf("old path closure admitted: %v", err)
	}
}
