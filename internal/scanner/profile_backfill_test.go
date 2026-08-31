// 增量跳过的判据：文件没变还不够，还要当前档位补不出这本书缺的数据才允许跳过。
// 破了它，换档位后普通扫描永远补不上页数与封面（只有强制扫描能修）；反向破了它，
// fast 档位会退化成每次全量重读。

package scanner

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
)

// newProfileScanTestConfig 配出一台按 profile 档位扫描、缩略图落在临时目录的扫描器配置。
func newProfileScanTestConfig(t *testing.T, profile string) *config.Config {
	t.Helper()
	cfg := &config.Config{}
	cfg.Scanner.Workers = 2
	cfg.Scanner.ScanProfile = profile
	cfg.Scanner.ThumbnailFormat = "webp"
	cfg.Cache.Dir = filepath.Join(t.TempDir(), "thumbs")
	config.NormalizeConfig(cfg)
	return cfg
}

// seedProfileScanBook 在库里铺一本三页的归档，返回它的路径。
func seedProfileScanBook(t *testing.T, libraryPath string) string {
	t.Helper()
	bookPath := filepath.Join(libraryPath, "Series Alpha", "Vol 01.cbz")
	if err := writeScannerTestCBZ(bookPath, map[string][]byte{
		"001.png": testPNG1x1,
		"002.png": testPNG1x1,
		"003.png": testPNG1x1,
	}); err != nil {
		t.Fatalf("write cbz failed: %v", err)
	}
	return bookPath
}

// onlyBook 取库里唯一那本书，顺带守住「本用例只该有一本书」。
func onlyBook(t *testing.T, store database.Store, libraryID int64) database.ListBooksByLibraryRow {
	t.Helper()
	books, err := store.ListBooksByLibrary(context.Background(), libraryID)
	if err != nil {
		t.Fatalf("list books failed: %v", err)
	}
	if len(books) != 1 {
		t.Fatalf("入库 %d 本, want 1", len(books))
	}
	return books[0]
}

func TestIncrementalSkipRespectsScanProfileCapability(t *testing.T) {
	t.Run("fast 首扫后改回 metadata 档，普通扫描补上页数与封面", func(t *testing.T) {
		_, store, lib, libraryPath := newScannerTestLibrary(t)
		seedProfileScanBook(t, libraryPath)

		cfg := newProfileScanTestConfig(t, config.ScanProfileFast)
		manager := config.NewManager(cfg)
		s := NewScanner(store, manager)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := s.ScanLibrary(ctx, lib.ID, libraryPath, false, nil); err != nil {
			t.Fatalf("fast 首扫: %v", err)
		}
		if got := onlyBook(t, store, lib.ID); got.PageCount != 0 {
			t.Fatalf("fast 首扫后页数为 %d, want 0 —— 该档位不开归档", got.PageCount)
		}

		cfg.Scanner.ScanProfile = config.ScanProfileMetadata
		manager.Replace(cfg)

		observer := &spyObserver{}
		if err := s.ScanLibrary(ctx, lib.ID, libraryPath, false, observer); err != nil {
			t.Fatalf("metadata 普通扫描: %v", err)
		}
		metrics := observer.lastMetrics()
		if metrics.SkippedArchives != 0 || metrics.ProcessedArchives != 1 {
			t.Fatalf("跳过/解析 %d/%d, want 0/1 —— 页数缺着而本档位补得上，不该跳过",
				metrics.SkippedArchives, metrics.ProcessedArchives)
		}

		book := onlyBook(t, store, lib.ID)
		waitForScannerBookCover(t, s, store, book.ID)
		if got := onlyBook(t, store, lib.ID); got.PageCount != 3 {
			t.Fatalf("补扫后页数为 %d, want 3", got.PageCount)
		}
	})

	t.Run("fast 档位下重复扫描仍跳过未变文件", func(t *testing.T) {
		_, store, lib, libraryPath := newScannerTestLibrary(t)
		seedProfileScanBook(t, libraryPath)

		s := NewScanner(store, config.NewManager(newProfileScanTestConfig(t, config.ScanProfileFast)))
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := s.ScanLibrary(ctx, lib.ID, libraryPath, false, nil); err != nil {
			t.Fatalf("fast 首扫: %v", err)
		}

		observer := &spyObserver{}
		if err := s.ScanLibrary(ctx, lib.ID, libraryPath, false, observer); err != nil {
			t.Fatalf("fast 二次扫描: %v", err)
		}
		metrics := observer.lastMetrics()
		if metrics.SkippedArchives != 1 || metrics.ProcessedArchives != 0 {
			t.Fatalf("跳过/解析 %d/%d, want 1/0 —— fast 档位补不上页数与封面，缺着也不该重读",
				metrics.SkippedArchives, metrics.ProcessedArchives)
		}
	})

	t.Run("系列扫描与库扫描同口径，也补得上页数", func(t *testing.T) {
		_, store, lib, libraryPath := newScannerTestLibrary(t)
		seedProfileScanBook(t, libraryPath)

		cfg := newProfileScanTestConfig(t, config.ScanProfileFast)
		manager := config.NewManager(cfg)
		s := NewScanner(store, manager)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := s.ScanLibrary(ctx, lib.ID, libraryPath, false, nil); err != nil {
			t.Fatalf("fast 首扫: %v", err)
		}
		seriesList, err := store.ListSeriesByLibraryLite(ctx, lib.ID)
		if err != nil || len(seriesList) != 1 {
			t.Fatalf("list series: %v, 系列数 %d, want 1", err, len(seriesList))
		}

		cfg.Scanner.ScanProfile = config.ScanProfileMetadata
		manager.Replace(cfg)
		if err := s.ScanSeries(ctx, seriesList[0].ID, false, nil); err != nil {
			t.Fatalf("metadata 普通系列扫描: %v", err)
		}
		if got := onlyBook(t, store, lib.ID); got.PageCount != 3 {
			t.Fatalf("补扫后页数为 %d, want 3", got.PageCount)
		}
	})

	t.Run("metadata 档下数据已齐的书仍被跳过", func(t *testing.T) {
		_, store, lib, libraryPath := newScannerTestLibrary(t)
		seedProfileScanBook(t, libraryPath)

		s := NewScanner(store, config.NewManager(newProfileScanTestConfig(t, config.ScanProfileMetadata)))
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := s.ScanLibrary(ctx, lib.ID, libraryPath, false, nil); err != nil {
			t.Fatalf("metadata 首扫: %v", err)
		}
		waitForScannerBookCover(t, s, store, onlyBook(t, store, lib.ID).ID)

		observer := &spyObserver{}
		if err := s.ScanLibrary(ctx, lib.ID, libraryPath, false, observer); err != nil {
			t.Fatalf("metadata 二次扫描: %v", err)
		}
		metrics := observer.lastMetrics()
		if metrics.SkippedArchives != 1 || metrics.ProcessedArchives != 0 || metrics.OpenedArchives != 0 {
			t.Fatalf("跳过/解析/开档 %d/%d/%d, want 1/0/0 —— 数据已齐，增量扫描不该重读归档",
				metrics.SkippedArchives, metrics.ProcessedArchives, metrics.OpenedArchives)
		}
	})
}
