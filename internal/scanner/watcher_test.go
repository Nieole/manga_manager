// 守注册与注销的递归对称：WatchLibrary 要覆盖库根下的嵌套目录，UnwatchLibrary 要全部收回。
// 注销不干净，已移除资料库的目录仍会产生事件并触发对该库的扫描。

package scanner

import (
	"context"
	"manga-manager/internal/config"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubWatcherHooks 是监听器派生工作的三个空出口。用例要观察哪一个就直接换掉对应的字段
// （见 FileWatcher 的注入字段）——构造期必须交齐，是为了让生产漏掉一个时编译不过。
func stubWatcherHooks() WatcherHooks {
	return WatcherHooks{
		ScanLibrary:        func(context.Context, int64) error { return nil },
		CleanupLibrary:     func(context.Context, int64) error { return nil },
		LibraryScanRunning: func(int64) bool { return false },
	}
}

func TestWatchLibraryWatchesNestedDirectories(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested failed: %v", err)
	}

	fw, err := NewFileWatcher(stubWatcherHooks())
	if err != nil {
		t.Fatalf("NewFileWatcher failed: %v", err)
	}
	defer fw.Stop()

	if err := fw.WatchLibrary(1, root, config.DefaultScanFormatsCSV); err != nil {
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

	fw, err := NewFileWatcher(stubWatcherHooks())
	if err != nil {
		t.Fatalf("NewFileWatcher failed: %v", err)
	}
	defer fw.Stop()

	if err := fw.WatchLibrary(1, root, config.DefaultScanFormatsCSV); err != nil {
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
