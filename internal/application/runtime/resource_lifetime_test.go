package runtime_test

import (
	"archive/zip"
	"context"
	"fmt"
	"manga-manager/internal/application/runtime"
	"manga-manager/internal/parser"
	"manga-manager/internal/runhandle"
	"manga-manager/internal/task"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestShutdownRetiresResourcesAfterAdmittedReaderExits(t *testing.T) {
	store, cfg := runtimeFixture(t)
	app, err := runtime.New(store, cfg, runtime.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Close)
	current := cfg.Snapshot()
	current.Scanner.ArchivePoolSize = 2
	if err := app.Resources.Apply(&current); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "reading.cbz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("001.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("reader still owns this page")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	entered, leave := make(chan struct{}), make(chan struct{})
	observed := make(chan error, 1)
	var leaveOnce sync.Once
	unblock := func() { leaveOnce.Do(func() { close(leave) }) }
	t.Cleanup(func() { unblock(); app.Close() })
	_, err = app.Runs.Start(context.Background(), task.RunSpec{Identity: task.Identity{Type: "resource_reader", Scope: task.ScopeSystem}, Key: "resource_reader", Trigger: task.TriggerManual, CanCancel: true}, func(ctx context.Context, _ *runhandle.Handle) (task.Result, error) {
		archive, release, err := parser.GetArchiveFromPool(path)
		if err != nil {
			observed <- err
			close(entered)
			return task.Result{}, err
		}
		defer release()
		close(entered)
		<-ctx.Done()
		data, err := archive.ReadPage("001.png")
		if err == nil && (string(data) != "reader still owns this page" || parser.PoolMaxSize() != 2) {
			err = fmt.Errorf("resource retired before task exited")
		}
		observed <- err
		<-leave
		return task.Result{}, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("reader did not enter")
	}
	closed := make(chan struct{})
	go func() { app.Close(); close(closed) }()
	select {
	case err := <-observed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not cancel reader")
	}
	select {
	case <-closed:
		t.Fatal("shutdown passed an active reader")
	default:
	}
	unblock()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not join reader")
	}
	if parser.PoolMaxSize() != 0 || parser.ActiveArchiveBorrows(path) != 0 {
		t.Fatal("shutdown retained archive resources")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}
