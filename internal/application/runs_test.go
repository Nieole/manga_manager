package application_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"manga-manager/internal/application"
	"manga-manager/internal/config"
	"manga-manager/internal/logger"
	"manga-manager/internal/runhandle"
	"manga-manager/internal/task"
	"manga-manager/internal/taskstore"
	_ "modernc.org/sqlite"
)

func testRuns(t *testing.T, background func(func()), publish func(task.Snapshot)) (*application.Runs, *task.Engine) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "tasks.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout=15000")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := taskstore.Migrate(db); err != nil {
		t.Fatal(err)
	}
	engine := task.New(task.Config{Store: taskstore.New(db), RunBackground: background, Publish: publish, DecorateRunContext: application.DecorateRunContext})
	return application.NewRuns(engine, nil), engine
}

func TestCleanupPrunesHistoryAndPreservesLiveRuns(t *testing.T) {
	var pending []func()
	synchronous := true
	runs, engine := testRuns(t, func(fn func()) {
		if synchronous {
			fn()
		} else {
			pending = append(pending, fn)
		}
	}, nil)
	ctx := context.Background()
	spec := task.RunSpec{Identity: task.Identity{Type: "scan_library", Scope: task.ScopeLibrary, ScopeID: 1}, Trigger: task.TriggerManual, Key: "diagnostic-only"}
	body := func(ctx context.Context, _ *runhandle.Handle) (task.Result, error) {
		if logger.RunIDFrom(ctx) == 0 || logger.TaskKeyFrom(ctx) != "diagnostic-only" {
			t.Error("missing run log context")
		}
		return task.Result{}, nil
	}
	for range 3 {
		if _, err := runs.Start(ctx, spec, body); err != nil {
			t.Fatal(err)
		}
	}
	synchronous = false
	for range 2 {
		if _, err := runs.Start(ctx, spec, body); err != nil {
			t.Fatal(err)
		}
	}
	synchronous = true
	cfg := config.Config{}
	cfg.Tasks.RetainRunsPerTask = 1
	if _, err := runs.CleanupRunHistory(ctx, cfg, task.TriggerChained); err != nil {
		t.Fatal(err)
	}
	snapshots, err := engine.ListSnapshots(ctx, task.RunFilter{})
	if err != nil {
		t.Fatal(err)
	}
	statuses := map[task.RunStatus]int{}
	var pruned int64
	for _, snap := range snapshots {
		statuses[snap.Run.Status]++
		pruned += snap.Side.Metrics["pruned_runs"]
	}
	if statuses[task.StatusRunning] != 1 || statuses[task.StatusQueued] != 1 || pruned != 2 {
		t.Fatalf("statuses %v, pruned %d", statuses, pruned)
	}
	// The application and compatibility callers submit to the same admission source.
	merged, err := engine.Start(ctx, spec, body)
	if err != nil {
		t.Fatal(err)
	}
	if !merged.Coalesced {
		t.Fatalf("expected shared merge: %+v", merged)
	}
	for len(pending) > 0 {
		fn := pending[0]
		pending = pending[1:]
		fn()
	}
}

func TestJanitorImmediatelyCleansAndStops(t *testing.T) {
	observed := make(chan task.Snapshot, 16)
	runs, _ := testRuns(t, func(fn func()) { fn() }, func(s task.Snapshot) { observed <- s })
	done, exited := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(exited)
		runs.RunHistoryJanitor(done, func() config.Config { return config.Config{} })
	}()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case snapshot := <-observed:
			if snapshot.Run.Status == task.StatusCompleted {
				if snapshot.Run.Trigger != task.TriggerChained || snapshot.Side.Metrics["pruned_runs"] != 0 {
					t.Errorf("cleanup observation: %+v", snapshot)
				}
				close(done)
				select {
				case <-exited:
					return
				case <-timeout:
					t.Fatal("janitor failed to stop")
				}
			}
		case <-timeout:
			close(done)
			t.Fatal("janitor did not run immediately")
		}
	}
}

func TestHistoryCleanupIsAnObservedRun(t *testing.T) {
	runs, engine := testRuns(t, func(fn func()) { fn() }, nil)
	if _, err := runs.CleanupRunHistory(context.Background(), config.Config{}, task.TriggerChained); err != nil {
		t.Fatal(err)
	}
	history, err := engine.ListSnapshots(context.Background(), task.RunFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].Run.Status != task.StatusCompleted {
		t.Fatalf("history: %+v", history)
	}
	if history[0].Run.MessageParams["runs"] != "0" {
		t.Fatalf("cleanup result: %+v", history[0])
	}
}
