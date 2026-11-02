package scanning_test

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/runhandle"
	"manga-manager/internal/scanner"
	"manga-manager/internal/task"
)

func TestQueuedWatchCleanupRechecksInputAndChangeQualification(t *testing.T) {
	for _, mode := range []string{"new_change", "cancelled_input", "shutdown_frozen_queue"} {
		t.Run(mode, func(t *testing.T) {
			service, store, lib, _, engine, manager := newCoverService(t, func(fn func()) { go fn() }, func() int { return 1 })
			cfg := manager.Snapshot()
			cfg.Scanner.ScanProfile = config.ScanProfileFast
			manager.Replace(&cfg)
			coverArchive(t, lib, "old.cbz")
			if err := service.WatchLibrary(context.Background(), lib.ID); err != nil {
				t.Fatal(err)
			}
			books, err := store.ListBooksByLibrary(context.Background(), lib.ID)
			if err != nil || len(books) != 1 {
				t.Fatalf("books=%v err=%v", books, err)
			}
			if err = os.Remove(books[0].Path); err != nil {
				t.Fatal(err)
			}
			entered, release := make(chan struct{}), make(chan struct{})
			blocker, err := engine.Start(context.Background(), task.RunSpec{Identity: task.Identity{Type: "blocking", Scope: task.ScopeSystem}, CanCancel: true, Trigger: task.TriggerManual}, func(ctx context.Context, _ *runhandle.Handle) (task.Result, error) {
				close(entered)
				select {
				case <-release:
				case <-ctx.Done():
				}
				return task.Result{}, ctx.Err()
			})
			if err != nil {
				t.Fatal(err)
			}
			<-entered
			defer engine.Shutdown()
			var eligible atomic.Bool
			eligible.Store(true)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			joined := make(chan error, 1)
			go func() { joined <- service.WatchCleanupWhen(ctx, lib.ID, eligible.Load) }()
			var cleanup task.Snapshot
			deadline := time.Now().Add(3 * time.Second)
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
			if mode == "new_change" {
				eligible.Store(false)
			} else {
				if mode == "shutdown_frozen_queue" {
					engine.Shutdown()
				}
				cancel()
			}
			if mode != "new_change" {
				select {
				case err := <-joined:
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("input join=%v", err)
					}
				case <-time.After(time.Second):
					t.Fatal("cancelled queued input waited for a nonexistent callback")
				}
			}
			close(release)
			if err = engine.AwaitBodyExit(context.Background(), blocker.Run.ID); err != nil {
				t.Fatal(err)
			}
			if mode == "shutdown_frozen_queue" {
				snapshot, err := engine.SnapshotOf(context.Background(), cleanup.Run.ID)
				if err != nil || snapshot.Run.Status != task.StatusQueued {
					t.Fatalf("queue was rewritten: %+v %v", snapshot, err)
				}
			} else {
				wait, stop := context.WithTimeout(context.Background(), 3*time.Second)
				defer stop()
				if _, err = engine.Await(wait, cleanup.Run.ID); err != nil {
					t.Fatal(err)
				}
				if mode == "new_change" {
					select {
					case err := <-joined:
						if !errors.Is(err, scanner.ErrCleanupDeferred) {
							t.Fatalf("stale qualification=%v", err)
						}
					case <-time.After(time.Second):
						t.Fatal("cleanup never joined")
					}
				}
			}
			if _, err = store.GetBook(context.Background(), books[0].ID); err != nil {
				t.Fatalf("unqualified queued cleanup deleted old identity: %v", err)
			}
		})
	}
}
