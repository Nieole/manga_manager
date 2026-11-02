package scanner

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// blockedWatchSource models Windows registration waiting for the notification reader.
// Its unbuffered events require the consumer to keep reading even while Remove waits.
type blockedWatchSource struct {
	events   chan fsnotify.Event
	closed   chan struct{}
	removing chan struct{}
	once     sync.Once
}

func (s *blockedWatchSource) Events() <-chan fsnotify.Event { return s.events }
func (s *blockedWatchSource) Errors() <-chan error          { return nil }
func (s *blockedWatchSource) Add(string) error              { return nil }
func (s *blockedWatchSource) Remove(string) error {
	s.once.Do(func() { close(s.removing) })
	<-s.closed
	return nil
}
func (s *blockedWatchSource) Close() error { close(s.closed); return nil }

func TestWatcherDrainsBurstWhileRegistrationIsBlockedAndStops(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "Series")
	if err := os.Mkdir(child, 0755); err != nil {
		t.Fatal(err)
	}
	source := &blockedWatchSource{events: make(chan fsnotify.Event), closed: make(chan struct{}), removing: make(chan struct{})}
	scanned := make(chan struct{}, 1)
	hooks := stubWatcherHooks()
	hooks.ScanLibrary = completedWatchScan(func(context.Context, int64) error {
		select {
		case scanned <- struct{}{}:
		default:
		}
		return nil
	})
	fw, err := NewFileWatcherWithSource(hooks, source)
	if err != nil {
		t.Fatal(err)
	}
	fw.timings = watcherTimings{tick: time.Millisecond, scanDebounce: time.Millisecond, cleanupDebounce: time.Millisecond, cleanupMaxDeferral: time.Second}
	if err := fw.WatchLibrary(1, root, ""); err != nil {
		t.Fatal(err)
	}
	fw.Start(nil)
	t.Cleanup(fw.Stop)
	source.events <- fsnotify.Event{Name: child, Op: fsnotify.Remove}
	select {
	case <-source.removing:
	case <-time.After(time.Second):
		t.Fatal("registration did not start")
	}
	delivered := make(chan struct{})
	go func() {
		defer close(delivered)
		for range 256 {
			select {
			case source.events <- fsnotify.Event{Name: filepath.Join(root, "changed.cbz"), Op: fsnotify.Create}:
			case <-source.closed:
				return
			}
		}
	}()
	select {
	case <-delivered:
	case <-time.After(time.Second):
		t.Fatal("event consumer stopped draining behind a blocked registration")
	}
	select {
	case <-scanned:
	case <-time.After(time.Second):
		t.Fatal("delivered changes were lost")
	}
	stopped := make(chan struct{})
	go func() { fw.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop did not close the source and wait for registration")
	}
}

func TestRealWatcherStopsWhileDirectoriesAreRenamedAndRecreated(t *testing.T) {
	root := t.TempDir()
	fw, err := NewFileWatcher(stubWatcherHooks())
	if err != nil {
		t.Fatal(err)
	}
	if err = fw.WatchLibrary(1, root, ""); err != nil {
		fw.Stop()
		t.Fatal(err)
	}
	fw.timings = watcherTimings{tick: time.Millisecond, scanDebounce: time.Millisecond, cleanupDebounce: time.Millisecond, cleanupMaxDeferral: time.Second}
	fw.Start(nil)
	t.Cleanup(fw.Stop)
	first, produced := make(chan struct{}), make(chan error, 1)
	go func() {
		for index := range 128 {
			dir := filepath.Join(root, "series")
			if err := os.Mkdir(dir, 0755); err != nil {
				produced <- err
				return
			}
			renamed := filepath.Join(root, "renamed")
			if err := os.Rename(dir, renamed); err != nil {
				produced <- err
				return
			}
			if err := os.Remove(renamed); err != nil {
				produced <- err
				return
			}
			if index == 0 {
				close(first)
			}
		}
		produced <- nil
	}()
	select {
	case <-first:
	case err := <-produced:
		t.Fatalf("producer stopped: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("filesystem producer never started")
	}
	stopped := make(chan struct{})
	go func() { fw.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("real watcher Stop blocked behind rename registration")
	}
	select {
	case err := <-produced:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("filesystem producer remained blocked")
	}
}

type blockedAddSource struct {
	*blockedWatchSource
	adding   chan struct{}
	addCount atomic.Int64
}

func (s *blockedAddSource) Add(string) error {
	if s.addCount.Add(1) == 1 {
		close(s.adding)
	}
	<-s.closed
	return nil
}

func TestStopCancelsAnInFlightRecursiveRegistration(t *testing.T) {
	root := t.TempDir()
	for index := range 64 {
		if err := os.Mkdir(filepath.Join(root, strconv.Itoa(index)), 0755); err != nil {
			t.Fatal(err)
		}
	}
	source := &blockedAddSource{blockedWatchSource: &blockedWatchSource{events: make(chan fsnotify.Event), closed: make(chan struct{}), removing: make(chan struct{})}, adding: make(chan struct{})}
	watcher, err := NewFileWatcherWithSource(stubWatcherHooks(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.Stop()
	watched := make(chan error, 1)
	go func() { watched <- watcher.WatchLibrary(1, root, "") }()
	select {
	case <-source.adding:
	case <-time.After(time.Second):
		t.Fatal("registration did not start")
	}
	watcher.Stop()
	select {
	case <-watched:
	case <-time.After(time.Second):
		t.Fatal("recursive registration did not exit")
	}
	if count := source.addCount.Load(); count != 1 {
		t.Fatalf("registered %d paths after the source was closed", count-1)
	}
}
