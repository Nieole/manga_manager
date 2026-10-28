package libraries_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"manga-manager/internal/application/libraries"
	"manga-manager/internal/database"
	"manga-manager/internal/runhandle"
	"manga-manager/internal/task"
	"manga-manager/internal/taskstore"
)

type watchedLibrary struct {
	active bool
	fail   bool
}

func (w *watchedLibrary) UnwatchLibrary(string) { w.active = false }
func (w *watchedLibrary) WatchLibrary(int64, string, string) error {
	if w.fail {
		return errors.New("watch unavailable")
	}
	w.active = true
	return nil
}

func deletionFixture(t *testing.T) (database.Store, *task.Engine, database.Library, *watchedLibrary) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "libraries.db")
	if err := database.Migrate(path); err != nil {
		t.Fatal(err)
	}
	store, err := database.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	lib, err := store.CreateLibrary(context.Background(), database.CreateLibraryParams{Name: "Library", Path: t.TempDir(), ScanMode: "watch", ScanInterval: 60, ScanFormats: "cbz"})
	if err != nil {
		t.Fatal(err)
	}
	engine := task.New(task.Config{Store: taskstore.New(store.DB()), RunBackground: func(fn func()) { fn() }})
	return store, engine, lib, &watchedLibrary{active: true}
}

func TestDeleteBusyLibraryPreservesRecordAndWatcherThenDeletesWhenReadExits(t *testing.T) {
	store, engine, lib, watcher := deletionFixture(t)
	invalidated := false
	service := libraries.New(store, engine, libraries.Effects{Watcher: watcher, AfterDelete: func(int64) { invalidated = true }})
	release, err := engine.AcquireLibraryRead(lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Delete(context.Background(), lib.ID)
	if !errors.Is(err, task.ErrLibraryUnavailable) || result.Deleted || len(result.Conflicts) != 1 || result.Conflicts[0].Reason != "wait_for_reads" {
		t.Fatalf("busy delete=%+v %v", result, err)
	}
	if !watcher.active || invalidated {
		t.Fatal("busy delete changed effects")
	}
	if _, err := store.GetLibrary(context.Background(), lib.ID); err != nil {
		t.Fatal(err)
	}
	release()
	file := filepath.Join(lib.Path, "keep.cbz")
	if err := os.WriteFile(file, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err = service.Delete(context.Background(), lib.ID)
	if err != nil || !result.Deleted || !invalidated || watcher.active {
		t.Fatalf("idle delete=%+v %v", result, err)
	}
	if _, err := store.GetLibrary(context.Background(), lib.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("library still exists: %v", err)
	}
	if data, err := os.ReadFile(file); err != nil || string(data) != "original" {
		t.Fatalf("library files changed: %q %v", data, err)
	}
	if _, err := service.Delete(context.Background(), lib.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("removed library reported as busy: %v", err)
	}
	if _, err := engine.AcquireLibraryRead(lib.ID); !errors.Is(err, task.ErrLibraryUnavailable) {
		t.Fatalf("stale removed ID admitted: %v", err)
	}
	global := task.RunSpec{Identity: task.Identity{Type: "verify_remaining", Scope: task.ScopeSystem}, LibraryIDs: []int64{task.AllLibraries}, Trigger: task.TriggerManual}
	if _, err := engine.Start(context.Background(), global, func(context.Context, *runhandle.Handle) (task.Result, error) { return task.Result{}, nil }); err != nil {
		t.Fatalf("retired identity blocked remaining global work: %v", err)
	}
}

func TestDeleteCancelsLocalRunAndWaitsForItsActualExit(t *testing.T) {
	store, _, lib, watcher := deletionFixture(t)
	engine := task.New(task.Config{Store: taskstore.New(store.DB()), RunBackground: func(fn func()) { go fn() }})
	entered, cancelled, exit := make(chan struct{}), make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-exit:
		default:
			close(exit)
		}
	}()
	launched, err := engine.Start(context.Background(), task.RunSpec{Identity: task.Identity{Type: "scan", Scope: task.ScopeLibrary, ScopeID: lib.ID}, LibraryIDs: []int64{lib.ID}, Trigger: task.TriggerManual, CanCancel: true}, func(ctx context.Context, _ *runhandle.Handle) (task.Result, error) {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		<-exit
		return task.Result{}, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	service := libraries.New(store, engine, libraries.Effects{Watcher: watcher})
	done := make(chan error, 1)
	go func() {
		result, err := service.Delete(context.Background(), lib.ID)
		if err == nil && !result.Deleted {
			err = errors.New("not deleted")
		}
		done <- err
	}()
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("deletion did not request cancellation")
	}
	if _, err := store.GetLibrary(context.Background(), lib.ID); err != nil {
		t.Fatal("deleted before body exit", err)
	}
	if _, err := engine.AcquireLibraryRead(lib.ID); !errors.Is(err, task.ErrLibraryUnavailable) {
		t.Fatal("draining reopened admission", err)
	}
	if snapshot, err := engine.SnapshotOf(context.Background(), launched.Run.ID); err != nil || snapshot.Run.Status != task.StatusCancelling {
		t.Fatalf("run=%+v %v", snapshot, err)
	}
	close(exit)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("deletion did not finish")
	}
}

func TestDeleteFailureRestoresWatchingAndAdmission(t *testing.T) {
	for _, restoreFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "restored", true: "restore-warning"}[restoreFails], func(t *testing.T) {
			store, engine, lib, watcher := deletionFixture(t)
			watcher.fail = restoreFails
			if _, err := store.DB().Exec(`CREATE TRIGGER reject_delete BEFORE DELETE ON libraries BEGIN SELECT RAISE(ABORT,'delete failed'); END`); err != nil {
				t.Fatal(err)
			}
			service := libraries.New(store, engine, libraries.Effects{Watcher: watcher})
			result, err := service.Delete(context.Background(), lib.ID)
			if err == nil || result.Deleted {
				t.Fatalf("fault delete=%+v %v", result, err)
			}
			if restoreFails {
				if len(result.Warnings) != 1 || watcher.active {
					t.Fatalf("restore failure concealed: %+v", result)
				}
			} else if !watcher.active {
				t.Fatal("existing watch not restored")
			}
			release, err := engine.AcquireLibraryRead(lib.ID)
			if err != nil {
				t.Fatalf("failed deletion retained reservation: %v", err)
			}
			release()
			if _, err := store.GetLibrary(context.Background(), lib.ID); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDeleteFailureRetainsQueuedCancellationFactsWithoutRestart(t *testing.T) {
	store, _, lib, watcher := deletionFixture(t)
	engine := task.New(task.Config{Store: taskstore.New(store.DB()), Slots: func() int { return 1 }, RunBackground: func(fn func()) { go fn() }})
	entered, leave, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	_, err := engine.Start(context.Background(), task.RunSpec{Identity: task.Identity{Type: "other", Scope: task.ScopeLibrary, ScopeID: lib.ID + 1}, LibraryIDs: []int64{lib.ID + 1}, Trigger: task.TriggerManual}, func(context.Context, *runhandle.Handle) (task.Result, error) {
		close(entered)
		<-leave
		close(finished)
		return task.Result{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	defer func() { close(leave); <-finished }()
	started := make(chan struct{}, 1)
	queued, err := engine.Start(context.Background(), task.RunSpec{Identity: task.Identity{Type: "local", Scope: task.ScopeLibrary, ScopeID: lib.ID}, LibraryIDs: []int64{lib.ID}, Trigger: task.TriggerManual, CanCancel: true}, func(context.Context, *runhandle.Handle) (task.Result, error) {
		started <- struct{}{}
		return task.Result{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`CREATE TRIGGER reject_cancelled_delete BEFORE DELETE ON libraries BEGIN SELECT RAISE(ABORT,'delete failed'); END`); err != nil {
		t.Fatal(err)
	}
	service := libraries.New(store, engine, libraries.Effects{Watcher: watcher})
	result, err := service.Delete(context.Background(), lib.ID)
	if err == nil || result.Deleted || len(result.CancellationRequested) != 1 || result.CancellationRequested[0] != queued.Run.ID || len(result.ExitedRuns) != 1 {
		t.Fatalf("failure lost cancellation facts: %+v %v", result, err)
	}
	if !watcher.active {
		t.Fatal("watch not restored")
	}
	if run, err := engine.SnapshotOf(context.Background(), queued.Run.ID); err != nil || run.Run.Status != task.StatusCancelled {
		t.Fatalf("cancelled work resurrected: %+v %v", run, err)
	}
	select {
	case <-started:
		t.Fatal("cancelled queued body executed")
	default:
	}
	if release, err := engine.AcquireLibraryRead(lib.ID); err != nil {
		t.Fatal(err)
	} else {
		release()
	}
}

type blockedDeletion struct {
	database.Store
	reached, proceed chan struct{}
}

func (s blockedDeletion) DeleteLibrary(ctx context.Context, id int64) error {
	close(s.reached)
	select {
	case <-s.proceed:
	case <-ctx.Done():
		return ctx.Err()
	}
	return s.Store.DeleteLibrary(ctx, id)
}

func TestDeleteHoldsEngineExclusivityUntilCommitAndCancellationReleasesIt(t *testing.T) {
	store, engine, lib, watcher := deletionFixture(t)
	blocked := blockedDeletion{Store: store, reached: make(chan struct{}), proceed: make(chan struct{})}
	service := libraries.New(blocked, engine, libraries.Effects{Watcher: watcher})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := service.Delete(ctx, lib.ID); done <- err }()
	select {
	case <-blocked.reached:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, err := engine.AcquireLibraryRead(lib.ID); !errors.Is(err, task.ErrLibraryUnavailable) {
		t.Fatalf("read crossed delete commit: %v", err)
	}
	if _, err := engine.AcquireLibraryExclusive(context.Background(), lib.ID); !errors.Is(err, task.ErrLibraryUnavailable) {
		t.Fatalf("migration crossed delete: %v", err)
	}
	if release, err := engine.AcquireLibraryRead(lib.ID + 1); err != nil {
		t.Fatal(err)
	} else {
		release()
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled delete=%v", err)
	}
	if !watcher.active {
		t.Fatal("cancelled commit did not restore watching")
	}
	release, err := engine.AcquireLibraryRead(lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if _, err := store.GetLibrary(context.Background(), lib.ID); err != nil {
		t.Fatal(err)
	}
}

type changedLibrarySettings struct{ database.Store }

func (s changedLibrarySettings) DeleteLibrary(ctx context.Context, id int64) error {
	current, err := s.GetLibrary(ctx, id)
	if err != nil {
		return err
	}
	_, err = s.UpdateLibrary(ctx, database.UpdateLibraryParams{ID: id, Name: current.Name, Path: current.Path, ScanMode: "none", ScanFormats: current.ScanFormats, ScanInterval: current.ScanInterval, KoreaderSyncEnabled: current.KoreaderSyncEnabled})
	if err != nil {
		return err
	}
	return errors.New("delete rejected after settings changed")
}

func TestDeleteFailureUsesCurrentWatchSettings(t *testing.T) {
	store, engine, lib, watcher := deletionFixture(t)
	service := libraries.New(changedLibrarySettings{store}, engine, libraries.Effects{Watcher: watcher})
	result, err := service.Delete(context.Background(), lib.ID)
	if err == nil || result.Deleted {
		t.Fatalf("failed delete=%+v %v", result, err)
	}
	if watcher.active || len(result.Warnings) != 0 {
		t.Fatalf("restored obsolete watch settings: active=%v result=%+v", watcher.active, result)
	}
}

func TestCancelledDeleteDoesNotReserveOrMutateLibrary(t *testing.T) {
	store, engine, lib, watcher := deletionFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := libraries.New(store, engine, libraries.Effects{Watcher: watcher}).Delete(ctx, lib.ID)
	if !errors.Is(err, context.Canceled) || result.Deleted || !watcher.active {
		t.Fatalf("cancelled delete=%+v %v", result, err)
	}
	release, err := engine.AcquireLibraryRead(lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func TestDeleteAbandonmentPreservesCancellationButNotFalseExit(t *testing.T) {
	for _, mode := range []string{"timeout", "request-cancel"} {
		t.Run(mode, func(t *testing.T) {
			store, _, lib, watcher := deletionFixture(t)
			bodyCancelled, leave, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
			engine := task.New(task.Config{Store: taskstore.New(store.DB()), RunBackground: func(fn func()) { go func() { fn(); close(finished) }() }})
			launched, err := engine.Start(context.Background(), task.RunSpec{Identity: task.Identity{Type: "draining", Scope: task.ScopeLibrary, ScopeID: lib.ID}, LibraryIDs: []int64{lib.ID}, Trigger: task.TriggerManual, CanCancel: true}, func(ctx context.Context, _ *runhandle.Handle) (task.Result, error) {
				<-ctx.Done()
				close(bodyCancelled)
				<-leave
				return task.Result{}, ctx.Err()
			})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { close(leave); <-finished }()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			type outcome struct {
				result libraries.DeleteResult
				err    error
			}
			done := make(chan outcome, 1)
			go func() {
				result, err := libraries.New(store, engine, libraries.Effects{Watcher: watcher}).Delete(ctx, lib.ID)
				done <- outcome{result, err}
			}()
			select {
			case <-bodyCancelled:
			case <-time.After(3 * time.Second):
				t.Fatal("no cancellation")
			}
			if _, err := engine.MarkInterrupted(context.Background()); err != nil {
				t.Fatal(err)
			}
			if mode == "request-cancel" {
				_, err = store.UpdateLibrary(context.Background(), database.UpdateLibraryParams{ID: lib.ID, Name: lib.Name, Path: lib.Path, ScanMode: "none", ScanFormats: lib.ScanFormats, ScanInterval: lib.ScanInterval, KoreaderSyncEnabled: lib.KoreaderSyncEnabled})
				if err != nil {
					t.Fatal(err)
				}
				cancel()
			}
			var got outcome
			select {
			case got = <-done:
			case <-time.After(4 * time.Second):
				t.Fatal("drain exceeded bound")
			}
			if got.result.Deleted || len(got.result.CancellationRequested) != 1 || got.result.CancellationRequested[0] != launched.Run.ID || len(got.result.ExitedRuns) != 0 || len(got.result.Conflicts) != 1 {
				t.Fatalf("lost partial outcome: %+v %v", got.result, got.err)
			}
			if mode == "timeout" && (!got.result.DrainTimedOut || !errors.Is(got.err, task.ErrLibraryUnavailable) || !watcher.active) {
				t.Fatalf("timeout=%+v %v watch=%v", got.result, got.err, watcher.active)
			}
			if mode == "request-cancel" && (!errors.Is(got.err, context.Canceled) || got.result.DrainTimedOut || watcher.active) {
				t.Fatalf("cancel=%+v %v watch=%v", got.result, got.err, watcher.active)
			}
			if _, err := store.GetLibrary(context.Background(), lib.ID); err != nil {
				t.Fatal(err)
			}
			if release, err := engine.AcquireLibraryRead(lib.ID); err != nil {
				t.Fatal("reservation leaked", err)
			} else {
				release()
			}
			if _, err := engine.AcquireLibraryExclusive(context.Background(), lib.ID); !errors.Is(err, task.ErrLibraryUnavailable) {
				t.Fatal("terminal body access lost", err)
			}
		})
	}
}

type cancelAfterDeleteCommit struct {
	database.Store
	cancel context.CancelFunc
}

func (s cancelAfterDeleteCommit) DeleteLibrary(ctx context.Context, id int64) error {
	if err := s.Store.DeleteLibrary(ctx, id); err != nil {
		return err
	}
	s.cancel()
	return nil
}

func TestCommittedDeleteRemainsCommittedWhenRequestEndsBeforeHandoff(t *testing.T) {
	store, engine, lib, watcher := deletionFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	invalidated := false
	service := libraries.New(cancelAfterDeleteCommit{Store: store, cancel: cancel}, engine, libraries.Effects{Watcher: watcher, AfterDelete: func(int64) { invalidated = true }})
	result, err := service.Delete(ctx, lib.ID)
	if err != nil || !result.Deleted || !invalidated || watcher.active || ctx.Err() == nil {
		t.Fatalf("commit lost at cancelled handoff: %+v %v", result, err)
	}
	if _, err := store.GetLibrary(context.Background(), lib.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("commit missing", err)
	}
	if _, err := engine.AcquireLibraryRead(lib.ID); !errors.Is(err, task.ErrLibraryUnavailable) {
		t.Fatal("deleted identity admitted", err)
	}
}
