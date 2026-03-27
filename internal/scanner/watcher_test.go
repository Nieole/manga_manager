package scanner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWatchLibraryWatchesNestedDirectories(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested failed: %v", err)
	}

	fw, err := NewFileWatcher(nil)
	if err != nil {
		t.Fatalf("NewFileWatcher failed: %v", err)
	}
	defer fw.Stop()

	if err := fw.WatchLibrary(1, root); err != nil {
		t.Fatalf("WatchLibrary failed: %v", err)
	}

	fw.mu.Lock()
	_, hasRoot := fw.watched[root]
	_, hasNestedA := fw.watched[filepath.Join(root, "a")]
	_, hasNestedB := fw.watched[nested]
	fw.mu.Unlock()

	if !hasRoot || !hasNestedA || !hasNestedB {
		t.Fatalf("expected recursive watch registration for %q", root)
	}
}

func TestUnwatchLibraryRemovesNestedDirectories(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "child", "grandchild")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested failed: %v", err)
	}

	fw, err := NewFileWatcher(nil)
	if err != nil {
		t.Fatalf("NewFileWatcher failed: %v", err)
	}
	defer fw.Stop()

	if err := fw.WatchLibrary(1, root); err != nil {
		t.Fatalf("WatchLibrary failed: %v", err)
	}

	fw.UnwatchLibrary(root)

	fw.mu.Lock()
	defer fw.mu.Unlock()
	for watchedPath := range fw.watched {
		if watchedPath == root || strings.HasPrefix(watchedPath, root+string(filepath.Separator)) {
			t.Fatalf("expected watched paths under %q to be removed, found %q", root, watchedPath)
		}
	}
}
