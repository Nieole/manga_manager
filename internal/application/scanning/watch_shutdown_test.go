package scanning_test

import (
	"context"
	"github.com/fsnotify/fsnotify"
	"manga-manager/internal/config"
	"manga-manager/internal/runhandle"
	"manga-manager/internal/scanner"
	"manga-manager/internal/task"
	"os"
	"sync"
	"testing"
	"time"
)

type watcherInput struct {
	events chan fsnotify.Event
	once   sync.Once
}

func (s *watcherInput) Events() <-chan fsnotify.Event { return s.events }
func (s *watcherInput) Errors() <-chan error          { return nil }
func (s *watcherInput) Add(string) error              { return nil }
func (s *watcherInput) Remove(string) error           { return nil }
func (s *watcherInput) Close() error                  { s.once.Do(func() { close(s.events) }); return nil }

func TestWatcherStopDoesNotWaitForFrozenQueuedCleanup(t *testing.T) {
	service, store, lib, scan, engine, manager := newCoverService(t, func(fn func()) { go fn() }, func() int { return 1 })
	cfg := manager.Snapshot()
	cfg.Scanner.ScanProfile = config.ScanProfileFast
	manager.Replace(&cfg)
	coverArchive(t, lib, "old.cbz")
	if err := service.WatchLibrary(context.Background(), lib.ID); err != nil {
		t.Fatal(err)
	}
	books, err := store.ListBooksByLibrary(context.Background(), lib.ID)
	if err != nil || len(books) != 1 {
		t.Fatalf("seed=%v %v", books, err)
	}
	if err = os.Remove(books[0].Path); err != nil {
		t.Fatal(err)
	}
	blockerID := make(chan int64, 1)
	source := &watcherInput{events: make(chan fsnotify.Event)}
	watcher, err := scanner.NewFileWatcherWithSource(scanner.WatcherHooks{
		ScanLibrary: func(ctx context.Context, id int64) (scanner.ScanOutcome, error) {
			outcome, err := service.WatchIngestion(ctx, id)
			if err != nil {
				return outcome, err
			}
			entered := make(chan struct{})
			blocker, err := engine.Start(ctx, task.RunSpec{Identity: task.Identity{Type: "blocking_watch_cleanup", Scope: task.ScopeSystem}, Trigger: task.TriggerManual, CanCancel: true}, func(ctx context.Context, _ *runhandle.Handle) (task.Result, error) {
				close(entered)
				<-ctx.Done()
				return task.Result{}, ctx.Err()
			})
			if err != nil {
				return scanner.ScanOutcome{}, err
			}
			<-entered
			blockerID <- blocker.Run.ID
			return outcome, nil
		}, CleanupLibrary: service.WatchCleanupWhen, LibraryScanRunning: scan.LibraryScanActive}, source)
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.Stop()
	defer engine.Shutdown()
	if err = watcher.WatchLibrary(lib.ID, lib.Path, lib.ScanFormats); err != nil {
		t.Fatal(err)
	}
	watcher.Start(nil)
	source.events <- fsnotify.Event{Name: books[0].Path, Op: fsnotify.Remove}
	var blocker int64
	select {
	case blocker = <-blockerID:
	case <-time.After(12 * time.Second):
		t.Fatal("watcher scan did not actually complete")
	}
	deadline := time.Now().Add(3 * time.Second)
	var cleanup task.Snapshot
	for {
		snapshots, err := engine.ListSnapshots(context.Background(), task.RunFilter{Types: []task.Type{"cleanup_library"}})
		if err != nil {
			t.Fatal(err)
		}
		if len(snapshots) == 1 && snapshots[0].Run.Status == task.StatusQueued {
			cleanup = snapshots[0]
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("cleanup not queued: %+v", snapshots)
		}
		time.Sleep(time.Millisecond)
	}
	engine.Shutdown()
	stopped := make(chan struct{})
	go func() { watcher.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("Stop waited for frozen work that never received a body")
	}
	if err = engine.AwaitBodyExit(context.Background(), blocker); err != nil {
		t.Fatal(err)
	}
	current, err := engine.SnapshotOf(context.Background(), cleanup.Run.ID)
	if err != nil || current.Run.Status != task.StatusQueued {
		t.Fatalf("queued history=%+v %v", current, err)
	}
	if _, err = store.GetBook(context.Background(), books[0].ID); err != nil {
		t.Fatalf("Stop lost the old identity: %v", err)
	}
}
