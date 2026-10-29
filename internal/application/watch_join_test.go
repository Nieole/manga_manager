package application_test

import (
	"context"
	"errors"
	"manga-manager/internal/runhandle"
	"manga-manager/internal/task"
	"testing"
	"time"
)

func TestJoinWatchedCancelsOwnedBodyAndWaitsForItsExit(t *testing.T) {
	runs, engine := testRuns(t, func(fn func()) { go fn() }, nil)
	entered, cancelled, release, exited := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	launched, err := runs.Start(context.Background(), task.RunSpec{Identity: task.Identity{Type: "scan_library", Scope: task.ScopeLibrary, ScopeID: 1}, Trigger: task.TriggerWatch, CanCancel: true}, func(ctx context.Context, _ *runhandle.Handle) (task.Result, error) {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		<-release
		close(exited)
		return task.Result{}, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	joined := make(chan error, 1)
	go func() { joined <- runs.JoinWatched(ctx, launched, nil) }()
	cancel()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("owned scan never received cancellation")
	}
	select {
	case err := <-joined:
		close(release)
		t.Fatalf("join returned before body exit: %v", err)
	default:
	}
	close(release)
	select {
	case err := <-joined:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("join=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("join did not finish")
	}
	select {
	case <-exited:
	default:
		t.Fatal("body remained active")
	}
	if _, err := engine.Await(context.Background(), launched.Run.ID); err != nil {
		t.Fatal(err)
	}
}

func TestJoinWatchedWaitsForNonCancelableBody(t *testing.T) {
	runs, _ := testRuns(t, func(fn func()) { go fn() }, nil)
	entered, release := make(chan struct{}), make(chan struct{})
	launched, err := runs.Start(context.Background(), task.RunSpec{Identity: task.Identity{Type: "cleanup_library", Scope: task.ScopeLibrary, ScopeID: 1}, Trigger: task.TriggerWatch}, func(context.Context, *runhandle.Handle) (task.Result, error) {
		close(entered)
		<-release
		return task.Result{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	joined := make(chan error, 1)
	go func() { joined <- runs.JoinWatched(ctx, launched, nil) }()
	select {
	case err := <-joined:
		close(release)
		t.Fatalf("uncancelable body not joined: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-joined:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("join=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("noncancelable body never joined")
	}
}

func TestJoinWatchedWaitsPastTerminalSnapshot(t *testing.T) {
	runs, engine := testRuns(t, func(fn func()) { go fn() }, nil)
	entered, release := make(chan struct{}), make(chan struct{})
	launched, err := runs.Start(context.Background(), task.RunSpec{Identity: task.Identity{Type: "scan_library", Scope: task.ScopeLibrary, ScopeID: 1}, Trigger: task.TriggerWatch}, func(context.Context, *runhandle.Handle) (task.Result, error) {
		close(entered)
		<-release
		return task.Result{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	if _, err = engine.MarkInterrupted(context.Background()); err != nil {
		t.Fatal(err)
	}
	joined := make(chan error, 1)
	go func() { joined <- runs.JoinWatched(context.Background(), launched, nil) }()
	select {
	case err := <-joined:
		close(release)
		t.Fatalf("terminal snapshot was mistaken for body exit: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-joined:
		if err == nil {
			t.Fatal("interrupted run became success")
		}
	case <-time.After(time.Second):
		t.Fatal("body exit not observed")
	}
}
