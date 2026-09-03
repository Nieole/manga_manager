// 守 fast 档位的「读不出来就别写」：该档位不开归档，页数与封面恒空，入库不得据此清零。
//
// 改名是最狠的一刀——改名重连保住了 books.id，但保留分支按**路径**查旧快照，
// 而改名恰恰是路径变了。页数与封面因此被清零，之后每次 fast 扫描又因 mtime+size 未变而跳过，
// 永不自愈：阅读进度百分比、读完判定、系列 total_pages 一并算错。

package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"manga-manager/internal/config"
)

func TestFastScanPreservesPageCountAndCoverAcrossRename(t *testing.T) {
	_, store, lib, libraryPath := newScannerTestLibrary(t)
	seriesDir := filepath.Join(libraryPath, "Series Alpha")
	oldPath := filepath.Join(seriesDir, "Vol 01.cbz")
	if err := writeScannerTestCBZ(oldPath, map[string][]byte{
		"001.png": testPNG1x1,
		"002.png": testPNG1x1,
		"003.png": testPNG1x1,
	}); err != nil {
		t.Fatalf("write cbz failed: %v", err)
	}

	cfg := newProfileScanTestConfig(t, config.ScanProfileMetadata)
	manager := config.NewManager(cfg)
	s := NewScanner(store, manager)

	seeded := scanAndSettleCover(t, s, lib, libraryPath, false)
	if seeded.PageCount != 3 || seeded.CoverPath.String == "" {
		t.Fatalf("metadata 首扫后页数 %d、封面 %q, want 3 与非空", seeded.PageCount, seeded.CoverPath.String)
	}

	// 用户把扫描等级调到 fast，然后在文件管理器里改了个名。
	cfg.Scanner.ScanProfile = config.ScanProfileFast
	manager.Replace(cfg)
	newPath := filepath.Join(seriesDir, "Volume 001.cbz")
	if err := os.Rename(oldPath, newPath); err != nil {
		t.Fatalf("rename failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.ScanLibrary(ctx, lib.ID, libraryPath, false, nil); err != nil {
		t.Fatalf("fast 增量扫描: %v", err)
	}

	renamed := currentBook(t, store, lib.ID)
	if renamed.ID != seeded.ID {
		t.Fatalf("改名后 books.id 变成 %d, want %d —— 改名重连本身就断了", renamed.ID, seeded.ID)
	}
	if renamed.Path != newPath {
		t.Fatalf("改名后路径为 %q, want %q", renamed.Path, newPath)
	}
	if renamed.PageCount != 3 {
		t.Fatalf("fast 档位改名后页数为 %d, want 3 —— 该档位读不出页数，不该据此清零", renamed.PageCount)
	}
	if renamed.CoverPath.String != seeded.CoverPath.String {
		t.Fatalf("fast 档位改名后封面为 %q, want %q", renamed.CoverPath.String, seeded.CoverPath.String)
	}

	// 停在 fast 档位继续扫：既不该退化成全量重读，页数与封面也不该再被动过。
	observer := &spyObserver{}
	if err := s.ScanLibrary(ctx, lib.ID, libraryPath, false, observer); err != nil {
		t.Fatalf("fast 二次扫描: %v", err)
	}
	metrics := observer.lastMetrics()
	if metrics.SkippedArchives != 1 || metrics.ProcessedArchives != 0 {
		t.Fatalf("跳过/解析 %d/%d, want 1/0 —— fast 档位不该退化成全量重读",
			metrics.SkippedArchives, metrics.ProcessedArchives)
	}
	settled := currentBook(t, store, lib.ID)
	if settled.PageCount != 3 || settled.CoverPath.String != seeded.CoverPath.String {
		t.Fatalf("fast 二次扫描后页数 %d、封面 %q, want 3 与 %q",
			settled.PageCount, settled.CoverPath.String, seeded.CoverPath.String)
	}
}

// TestFastForceScanPreservesPageCountAndCover 守强制扫描这条支路：force 只跳过增量拦截，
// 不改变「fast 档位读不出页数与封面」这个事实。
func TestFastForceScanPreservesPageCountAndCover(t *testing.T) {
	_, store, lib, libraryPath := newScannerTestLibrary(t)
	seedProfileScanBook(t, libraryPath)

	cfg := newProfileScanTestConfig(t, config.ScanProfileMetadata)
	manager := config.NewManager(cfg)
	s := NewScanner(store, manager)
	seeded := scanAndSettleCover(t, s, lib, libraryPath, false)

	cfg.Scanner.ScanProfile = config.ScanProfileFast
	manager.Replace(cfg)

	after := scanAndSettleCover(t, s, lib, libraryPath, true)
	if after.PageCount != seeded.PageCount || after.CoverPath.String != seeded.CoverPath.String {
		t.Fatalf("fast 强制扫描后页数 %d、封面 %q, want %d 与 %q",
			after.PageCount, after.CoverPath.String, seeded.PageCount, seeded.CoverPath.String)
	}
}

// TestScanZeroesPageCountWhenArchiveTrulyHasNoPages 是反向守卫：保留的判据是
// 「本次扫描有没有读归档」，不是「读出来的值是不是 0」——读过归档且确实一页都没有时，
// page_count 必须归零，否则系列统计会一直挂着一个再也不存在的页数。
func TestScanZeroesPageCountWhenArchiveTrulyHasNoPages(t *testing.T) {
	_, store, lib, libraryPath := newScannerTestLibrary(t)
	bookPath := filepath.Join(libraryPath, "Series Alpha", "Vol 01.cbz")
	if err := writeScannerTestCBZ(bookPath, map[string][]byte{"001.png": testPNG1x1}); err != nil {
		t.Fatalf("write cbz failed: %v", err)
	}

	s := NewScanner(store, config.NewManager(newProfileScanTestConfig(t, config.ScanProfileMetadata)))
	if seeded := scanAndSettleCover(t, s, lib, libraryPath, false); seeded.PageCount != 1 {
		t.Fatalf("首扫页数为 %d, want 1", seeded.PageCount)
	}

	// 归档被替换成只剩说明文件的版本：一页都没有了。
	if err := writeScannerTestCBZ(bookPath, map[string][]byte{"notes.txt": []byte("no pages here")}); err != nil {
		t.Fatalf("rewrite cbz failed: %v", err)
	}
	bumpModTime(t, bookPath)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.ScanLibrary(ctx, lib.ID, libraryPath, false, nil); err != nil {
		t.Fatalf("metadata 增量扫描: %v", err)
	}
	if after := currentBook(t, store, lib.ID); after.PageCount != 0 {
		t.Fatalf("空归档重扫后页数为 %d, want 0", after.PageCount)
	}
}
