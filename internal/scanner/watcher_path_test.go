// 业务说明：本文件守卫「文件事件被记到正确的资料库上」。
//
// 此前用无分隔符的 strings.HasPrefix 判断事件路径属于哪个库，于是 /data/manga2 的事件
// 会被判成属于 /data/manga。而两处匹配都是「第一个命中就 break」，Go 的 map 迭代顺序又是随机的
// ——受害的是哪个库每次都不一样：删除事件被记到错误的库上，真正该清理的库留下幽灵记录。

package scanner

import (
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestPathUnderRoot(t *testing.T) {
	cases := []struct {
		name  string
		child string
		root  string
		want  bool
	}{
		{"同一路径", "/data/manga", "/data/manga", true},
		{"直接子项", "/data/manga/series/vol1.cbz", "/data/manga", true},
		{"深层子项", "/data/manga/a/b/c/d.cbz", "/data/manga", true},

		// 这一组是缺陷的核心：前缀相同的兄弟目录。
		{"兄弟目录不算子项", "/data/manga2/x.cbz", "/data/manga", false},
		{"兄弟目录（无分隔符前缀）", "/data/manga-backup/x.cbz", "/data/manga", false},
		{"根本身是前缀但非父目录", "/data/mangaX", "/data/manga", false},

		{"父目录不算子项", "/data", "/data/manga", false},
		{"完全无关", "/srv/other/x.cbz", "/data/manga", false},
		{"含 .. 的路径被规范化", "/data/manga/../manga2/x.cbz", "/data/manga", false},
		{"含 . 的路径被规范化", "/data/manga/./a/x.cbz", "/data/manga", true},
		{"root 带尾部分隔符", "/data/manga/a.cbz", "/data/manga/", true},
		{"空 root", "/data/manga/a.cbz", "", false},
		{"空白 root", "/data/manga/a.cbz", "   ", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pathUnderRoot(tc.child, tc.root); got != tc.want {
				t.Fatalf("pathUnderRoot(%q, %q) = %v, want %v", tc.child, tc.root, got, tc.want)
			}
		})
	}
}

func TestPathUnderRootWindowsCaseInsensitive(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("仅在 Windows 上，文件系统才是大小写不敏感的")
	}
	if !pathUnderRoot(`C:\Data\Manga\a.cbz`, `c:\data\manga`) {
		t.Fatal("Windows 上大小写不同的同一目录应当判为同一路径")
	}
}

// TestHandleRemovalSchedulesOnlyOwningLibrary 是行为级判据：
// 对当前的 HasPrefix 实现，map 迭代顺序随机，100 次里必然有若干次记到错误的库上。
func TestHandleRemovalSchedulesOnlyOwningLibrary(t *testing.T) {
	fw, err := NewFileWatcher(nil)
	if err != nil {
		t.Fatalf("NewFileWatcher: %v", err)
	}
	t.Cleanup(fw.Stop)

	const (
		libA int64 = 1 // /data/manga
		libB int64 = 2 // /data/manga2
	)
	rootA := filepath.FromSlash("/data/manga")
	rootB := filepath.FromSlash("/data/manga2")
	victim := filepath.FromSlash("/data/manga2/series/vol1.cbz")

	// 反复跑：单次可能侥幸命中正确的库，随机迭代顺序下 100 次必然暴露。
	for i := 0; i < 100; i++ {
		fw.mu.Lock()
		fw.libs = map[string]int64{rootA: libA, rootB: libB}
		fw.pendingCleanup = map[int64]time.Time{}
		fw.mu.Unlock()

		fw.handleRemoval(victim)

		fw.mu.Lock()
		_, scheduledA := fw.pendingCleanup[libA]
		_, scheduledB := fw.pendingCleanup[libB]
		fw.mu.Unlock()

		if scheduledA {
			t.Fatalf("第 %d 次：/data/manga2 下的删除事件被记到了 /data/manga 上 —— 两个库互相串台", i)
		}
		if !scheduledB {
			t.Fatalf("第 %d 次：真正所属的库没有被排期清理", i)
		}
	}
}

// TestHandleRemovalSchedulesAllContainingLibraries 覆盖嵌套库：
// 一个库的根在另一个库之内时，两边共享同一棵子树，都需要清理幽灵记录。
// 「找到一个就 break」会随机漏掉其中一个。
func TestHandleRemovalSchedulesAllContainingLibraries(t *testing.T) {
	fw, err := NewFileWatcher(nil)
	if err != nil {
		t.Fatalf("NewFileWatcher: %v", err)
	}
	t.Cleanup(fw.Stop)

	const (
		outer int64 = 1 // /data
		inner int64 = 2 // /data/manga
	)
	fw.mu.Lock()
	fw.libs = map[string]int64{
		filepath.FromSlash("/data"):       outer,
		filepath.FromSlash("/data/manga"): inner,
	}
	fw.pendingCleanup = map[int64]time.Time{}
	fw.mu.Unlock()

	fw.handleRemoval(filepath.FromSlash("/data/manga/series/vol1.cbz"))

	fw.mu.Lock()
	defer fw.mu.Unlock()
	if _, ok := fw.pendingCleanup[inner]; !ok {
		t.Error("内层库没有被排期清理")
	}
	if _, ok := fw.pendingCleanup[outer]; !ok {
		t.Error("外层库没有被排期清理 —— 嵌套库共享子树，两边都会留下幽灵记录")
	}
}
