// 守「用户自己设的封面不被扫描改回自动封面」：设过封面的书带上封面锁，入库侧不得覆盖它。
//
// 自动缩略图按 sha1(path|mtime|size) 命名，自定义封面按图片内容 sha1 命名，两个名字永不相等；
// 少了这道锁，任一次强制扫描（前端的「重新扫描该系列」按钮发的就是 force=true）都会把
// cover_path 改回自动那张，或在自动缩略图已被清掉时写成 NULL 再重新生成一张。

package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
)

// scanAndSettleCover 跑一次整库扫描并等封面队列排空，返回那本书当时的行。
func scanAndSettleCover(t *testing.T, s *Scanner, lib database.Library, libraryPath string, force bool) database.Book {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.ScanLibrary(ctx, lib.ID, libraryPath, force, nil); err != nil {
		t.Fatalf("scan library (force=%v) failed: %v", force, err)
	}
	if err := s.waitForCoverQueue(ctx); err != nil {
		t.Fatalf("wait cover queue failed: %v", err)
	}
	return currentBook(t, s.store, lib.ID)
}

// currentBook 取库里唯一那本书的完整行。
func currentBook(t *testing.T, store database.Store, libraryID int64) database.Book {
	t.Helper()
	row := onlyBook(t, store, libraryID)
	book, err := store.GetBook(context.Background(), row.ID)
	if err != nil {
		t.Fatalf("get book failed: %v", err)
	}
	return book
}

// setCustomCover 走「把第 N 页设为封面」这条真实路径，返回它写下的封面相对路径。
func setCustomCover(t *testing.T, s *Scanner, book database.Book, page int) string {
	t.Helper()
	custom, err := s.SetBookCoverFromPage(context.Background(), book, page)
	if err != nil {
		t.Fatalf("set custom cover failed: %v", err)
	}
	if custom == "" {
		t.Fatal("自定义封面路径为空")
	}
	return custom
}

func TestCustomCoverSurvivesRescan(t *testing.T) {
	t.Run("强制整库扫描后自定义封面还在", func(t *testing.T) {
		_, store, lib, libraryPath := newScannerTestLibrary(t)
		seedProfileScanBook(t, libraryPath)

		s := NewScanner(store, config.NewManager(newProfileScanTestConfig(t, config.ScanProfileMetadata)))
		book := scanAndSettleCover(t, s, lib, libraryPath, false)
		auto := book.CoverPath.String
		if auto == "" {
			t.Fatal("首扫没有落下自动封面")
		}

		custom := setCustomCover(t, s, book, 2)
		if custom == auto {
			t.Fatalf("自定义封面与自动封面同名 (%s)，用例失去区分力", custom)
		}

		after := scanAndSettleCover(t, s, lib, libraryPath, true)
		if after.CoverPath.String != custom {
			t.Fatalf("强制扫描后封面为 %q, want %q（用户设的那张）", after.CoverPath.String, custom)
		}
	})

	t.Run("单系列重扫（force=true）后自定义封面还在", func(t *testing.T) {
		_, store, lib, libraryPath := newScannerTestLibrary(t)
		seedProfileScanBook(t, libraryPath)

		s := NewScanner(store, config.NewManager(newProfileScanTestConfig(t, config.ScanProfileMetadata)))
		book := scanAndSettleCover(t, s, lib, libraryPath, false)
		custom := setCustomCover(t, s, book, 3)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		seriesList, err := store.ListSeriesByLibraryLite(ctx, lib.ID)
		if err != nil || len(seriesList) != 1 {
			t.Fatalf("list series: %v, 系列数 %d, want 1", err, len(seriesList))
		}
		if err := s.ScanSeries(ctx, seriesList[0].ID, true, nil); err != nil {
			t.Fatalf("单系列强制重扫: %v", err)
		}
		if err := s.waitForCoverQueue(ctx); err != nil {
			t.Fatalf("wait cover queue failed: %v", err)
		}

		if after := currentBook(t, store, lib.ID); after.CoverPath.String != custom {
			t.Fatalf("单系列重扫后封面为 %q, want %q（用户设的那张）", after.CoverPath.String, custom)
		}
	})

	t.Run("自动缩略图已从盘上消失时也不被清空", func(t *testing.T) {
		_, store, lib, libraryPath := newScannerTestLibrary(t)
		seedProfileScanBook(t, libraryPath)

		cfg := newProfileScanTestConfig(t, config.ScanProfileMetadata)
		s := NewScanner(store, config.NewManager(cfg))
		book := scanAndSettleCover(t, s, lib, libraryPath, false)
		auto := book.CoverPath.String
		custom := setCustomCover(t, s, book, 1)

		// 模拟「重建缩略图」之后的盘面：自动那张已被删掉，只剩用户那张。
		if err := os.Remove(filepath.Join(config.ThumbnailDir(*cfg), filepath.FromSlash(auto))); err != nil {
			t.Fatalf("remove auto thumbnail failed: %v", err)
		}

		after := scanAndSettleCover(t, s, lib, libraryPath, true)
		if after.CoverPath.String != custom {
			t.Fatalf("强制扫描后封面为 %q, want %q（用户设的那张）", after.CoverPath.String, custom)
		}
	})
}

// TestThumbnailRebuildReleasesCoverLock 守「重建缩略图」这条支路：它把缩略图文件连同用户
// 上传的那张一并删光，封面锁必须跟着解开——留着锁而封面已空，扫描会永远不敢写回自动封面。
func TestThumbnailRebuildReleasesCoverLock(t *testing.T) {
	_, store, lib, libraryPath := newScannerTestLibrary(t)
	seedProfileScanBook(t, libraryPath)

	cfg := newProfileScanTestConfig(t, config.ScanProfileMetadata)
	s := NewScanner(store, config.NewManager(cfg))
	book := scanAndSettleCover(t, s, lib, libraryPath, false)
	custom := setCustomCover(t, s, book, 2)

	// 重建缩略图任务的两步：删光缩略图目录，再清空所有 cover_path。
	if err := os.RemoveAll(config.ThumbnailDir(*cfg)); err != nil {
		t.Fatalf("clear thumbnail dir failed: %v", err)
	}
	if err := store.ClearAllBookCoverPaths(context.Background()); err != nil {
		t.Fatalf("clear cover paths failed: %v", err)
	}

	rebuilt := scanAndSettleCover(t, s, lib, libraryPath, true)
	if rebuilt.CoverPath.String == "" {
		t.Fatal("重建后封面仍为空 —— 封面锁没解开，扫描不敢写回自动封面")
	}
	if rebuilt.CoverPath.String == custom {
		t.Fatalf("重建后封面仍是 %q，那张文件已经不在盘上了", custom)
	}
	if rebuilt.CoverLocked {
		t.Fatal("重建后封面锁仍立着 —— 这张已是自动封面，不该再挡住扫描")
	}
}

// TestScanStillRefreshesAutoCoverAndPageCount 是反向守卫：没被用户改过的书，
// 文件内容真的变了时，页数与自动封面仍要跟着更新——否则「不覆盖」就矫枉过正了。
func TestScanStillRefreshesAutoCoverAndPageCount(t *testing.T) {
	_, store, lib, libraryPath := newScannerTestLibrary(t)
	bookPath := filepath.Join(libraryPath, "Series Alpha", "Vol 01.cbz")
	if err := writeScannerTestCBZ(bookPath, map[string][]byte{"001.png": testPNG1x1}); err != nil {
		t.Fatalf("write cbz failed: %v", err)
	}

	s := NewScanner(store, config.NewManager(newProfileScanTestConfig(t, config.ScanProfileMetadata)))
	before := scanAndSettleCover(t, s, lib, libraryPath, false)
	if before.PageCount != 1 {
		t.Fatalf("首扫页数为 %d, want 1", before.PageCount)
	}

	// 用户在磁盘上把这本书换成了三页的版本。
	if err := writeScannerTestCBZ(bookPath, map[string][]byte{
		"001.png": testPNG1x1,
		"002.png": testPNG1x1,
		"003.png": testPNG1x1,
	}); err != nil {
		t.Fatalf("rewrite cbz failed: %v", err)
	}
	bumpModTime(t, bookPath)

	after := scanAndSettleCover(t, s, lib, libraryPath, false)
	if after.PageCount != 3 {
		t.Fatalf("内容变更后页数为 %d, want 3", after.PageCount)
	}
	if after.CoverPath.String == before.CoverPath.String {
		t.Fatalf("内容变更后封面仍是 %q，自动封面没有跟着刷新", after.CoverPath.String)
	}
	if after.CoverPath.String == "" {
		t.Fatal("内容变更后封面为空，自动封面没有重建")
	}
}

// bumpModTime 把文件 mtime 推后一分钟，让增量拦截确实看见「文件变了」。
func bumpModTime(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	next := info.ModTime().Add(time.Minute)
	if err := os.Chtimes(path, next, next); err != nil {
		t.Fatalf("chtimes failed: %v", err)
	}
}
