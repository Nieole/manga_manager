// 业务说明：本文件是业务回归测试，属于漫画库扫描链路，负责发现文件、建立书籍和系列记录、提取封面、同步索引并维护任务进度。
// 它通过自动化断言保护对应业务场景在扫描、读取、展示或配置变更后仍保持兼容。
// 维护时应让用例名称、测试数据和断言结果直接反映真实用户流程，而不是只覆盖实现细节。

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
