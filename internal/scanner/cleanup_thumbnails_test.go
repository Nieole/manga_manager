// 守 CleanupThumbnails 只清缩略图、不越界删页图磁盘缓存。
//
// 缩略图目录就是 cache.dir 本身，页图磁盘缓存是它下面的 pages/ 子目录。清理器按
// 「不在 cover_path 集合里就删」逐文件判定，一旦把 pages/ 也走进去，一次「清理未引用的封面」
// 就会连整个页图缓存与 pages/ 目录一起抹掉，此后每一页都要重新解码转码。

package scanner

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
)

// writeCacheFile 在 cacheDir 下按相对路径落一个占位文件。
func writeCacheFile(t *testing.T, cacheDir, relPath string) string {
	t.Helper()
	full := filepath.Join(cacheDir, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
	return full
}

// TestCleanupThumbnailsKeepsPageDiskCache 覆盖清理缩略图任务的目录边界与过滤功能本身。
func TestCleanupThumbnailsKeepsPageDiskCache(t *testing.T) {
	_, store, lib, _ := newScannerTestLibrary(t)
	ctx := context.Background()

	cacheDir := t.TempDir()
	cfg := &config.Config{}
	cfg.Scanner.Workers = 1
	cfg.Scanner.ScanProfile = config.ScanProfileFast
	cfg.Cache.Dir = cacheDir
	cfg.Cache.PageDiskCacheEnabled = true
	s := NewScanner(store, config.NewManager(cfg))

	// 一本书引用了一张封面；数据库里没有第二条 cover_path。
	const referencedCover = "45/453e3d0000000000000000000000000000000000000000000000000000000000.webp"
	series, err := store.CreateSeries(ctx, database.CreateSeriesParams{
		LibraryID:   lib.ID,
		Name:        "Series Alpha",
		Path:        filepath.Join(lib.Path, "Series Alpha"),
		NameInitial: "S",
	})
	if err != nil {
		t.Fatalf("create series: %v", err)
	}
	if _, err := store.CreateBook(ctx, database.CreateBookParams{
		SeriesID:       series.ID,
		LibraryID:      lib.ID,
		Name:           "v1.cbz",
		Path:           filepath.Join(lib.Path, "Series Alpha", "v1.cbz"),
		Size:           1,
		FileModifiedAt: time.Now(),
		CoverPath:      sql.NullString{String: referencedCover, Valid: true},
	}); err != nil {
		t.Fatalf("create book: %v", err)
	}

	keptCover := writeCacheFile(t, cacheDir, referencedCover)
	orphanCover := writeCacheFile(t, cacheDir, "99/9900000000000000000000000000000000000000000000000000000000000000.webp")
	pageCacheFile := writeCacheFile(t, cacheDir, "pages/ab/deadbeef.webp")
	pageCacheDir := filepath.Join(cacheDir, "pages")

	if err := s.CleanupThumbnails(ctx, nil); err != nil {
		t.Fatalf("CleanupThumbnails: %v", err)
	}

	t.Run("页图缓存文件不被删", func(t *testing.T) {
		if _, err := os.Stat(pageCacheFile); err != nil {
			t.Fatalf("页图磁盘缓存被清理缩略图任务删掉了（%s）——用户只想清封面，代价却是每一页都要重新解码转码：%v",
				pageCacheFile, err)
		}
	})

	t.Run("pages 目录本身还在", func(t *testing.T) {
		info, err := os.Stat(pageCacheDir)
		if err != nil {
			t.Fatalf("pages 目录被一并移除了（%s）：%v", pageCacheDir, err)
		}
		if !info.IsDir() {
			t.Fatalf("%s 不再是目录", pageCacheDir)
		}
	})

	t.Run("被引用的封面保住", func(t *testing.T) {
		if _, err := os.Stat(keptCover); err != nil {
			t.Fatalf("被引用的封面被删了（%s）：%v", keptCover, err)
		}
	})

	t.Run("未被引用的封面仍被清掉", func(t *testing.T) {
		if _, err := os.Stat(orphanCover); !os.IsNotExist(err) {
			t.Fatalf("未被引用的封面没有被清掉（%s），清理功能失效：err=%v", orphanCover, err)
		}
	})
}
