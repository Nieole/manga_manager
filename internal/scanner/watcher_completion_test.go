package scanner

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

func TestWatcherRequiresScanAfterEventsArrivingDuringIngestion(t *testing.T) {
	source := &blockedWatchSource{events: make(chan fsnotify.Event), closed: make(chan struct{}), removing: make(chan struct{})}
	first, second := make(chan struct{}), make(chan struct{})
	firstGate, secondGate := make(chan struct{}), make(chan struct{})
	cleaned := make(chan struct{}, 1)
	var scans atomic.Int64
	hooks := stubWatcherHooks()
	hooks.ScanLibrary = func(ctx context.Context, _ int64) (ScanOutcome, error) {
		var gate chan struct{}
		switch scans.Add(1) {
		case 1:
			close(first)
			gate = firstGate
		case 2:
			close(second)
			gate = secondGate
		default:
			return ScanOutcome{Status: ScanComplete}, nil
		}
		select {
		case <-gate:
			return ScanOutcome{Status: ScanComplete}, nil
		case <-ctx.Done():
			return ScanOutcome{}, ctx.Err()
		}
	}
	hooks.CleanupLibrary = qualifiedWatchCleanup(func(context.Context, int64) error {
		select {
		case cleaned <- struct{}{}:
		default:
		}
		return nil
	})
	fw, err := NewFileWatcherWithSource(hooks, source)
	if err != nil {
		t.Fatal(err)
	}
	fw.timings = watcherTimings{tick: time.Millisecond, scanDebounce: 5 * time.Millisecond, cleanupDebounce: time.Millisecond, cleanupMaxDeferral: time.Second}
	root := t.TempDir()
	if err := fw.WatchLibrary(1, root, ""); err != nil {
		t.Fatal(err)
	}
	fw.Start(nil)
	t.Cleanup(fw.Stop)
	source.events <- fsnotify.Event{Name: filepath.Join(root, "old.cbz"), Op: fsnotify.Rename}
	select {
	case <-first:
	case <-time.After(time.Second):
		t.Fatal("first scan not dispatched")
	}
	source.events <- fsnotify.Event{Name: filepath.Join(root, "another.cbz"), Op: fsnotify.Rename}
	source.events <- fsnotify.Event{Name: filepath.Join(root, "new.cbz"), Op: fsnotify.Create}
	close(firstGate)
	select {
	case <-second:
	case <-time.After(time.Second):
		t.Fatal("in-flight changes not scanned")
	}
	select {
	case <-cleaned:
		t.Fatal("cleanup used a scan that predates a newer rename")
	default:
	}
	close(secondGate)
	select {
	case <-cleaned:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not follow the covering scan")
	}
}
