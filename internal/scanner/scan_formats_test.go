// 守库级 scan_formats 真的被扫描器与监听器执行：用户勾了「只扫 cbz」，rar/zip 就不该入库。
//
// 用例必须跑 fast 档位。该档位不打开归档（opensArchive() 为 false），.cbr 夹具才能只是个占位
// 文件；换成 metadata 档位，parser 会按扩展名分发到 RarArchive 并因打不开而丢弃该文件，
// 「格式过滤是否生效」就被「归档能不能打开」掩盖，用例什么也证明不了。

package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
)

func newFormatTestScanner(t *testing.T, store database.Store) *Scanner {
	t.Helper()
	cfg := &config.Config{}
	cfg.Scanner.Workers = 1
	cfg.Scanner.ScanProfile = config.ScanProfileFast
	cfg.Cache.Dir = t.TempDir()
	return NewScanner(store, config.NewManager(cfg))
}

// setLibraryScanFormats 直接改库行的 scan_formats（绕开 HTTP 层的归一化，用例要能塞非法值）。
func setLibraryScanFormats(t *testing.T, store database.Store, libraryID int64, formats string) {
	t.Helper()
	sqlStore, ok := store.(*database.SqlStore)
	if !ok {
		t.Fatalf("需要 *SqlStore 才能直改库行，得到 %T", store)
	}
	if _, err := sqlStore.DB().ExecContext(context.Background(),
		`UPDATE libraries SET scan_formats = ? WHERE id = ?`, formats, libraryID); err != nil {
		t.Fatalf("update scan_formats: %v", err)
	}
}

func seedTwoFormats(t *testing.T, libraryPath string) {
	t.Helper()
	dir := filepath.Join(libraryPath, "Series Alpha")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := writeScannerTestCBZ(filepath.Join(dir, "v1.cbz"), map[string][]byte{"001.png": testPNG1x1}); err != nil {
		t.Fatalf("write cbz: %v", err)
	}
	// fast 档位不打开归档，占位内容即可（见文件头注释）。
	if err := os.WriteFile(filepath.Join(dir, "v2.cbr"), []byte("placeholder"), 0o644); err != nil {
		t.Fatalf("write cbr: %v", err)
	}
}

func TestScanRespectsLibraryScanFormats(t *testing.T) {
	cases := []struct {
		name        string
		scanFormats string
		wantBooks   int
		wantFilter  int64
	}{
		{"只扫 cbz", "cbz", 1, 1},
		{"cbz 与 cbr 都扫", "cbz,cbr", 2, 0},
		{"空值回落到全部格式", "", 2, 0},
		{"全非法值回落到全部格式", "pdf,epub", 2, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, store, lib, libraryPath := newScannerTestLibrary(t)
			ctx := context.Background()
			setLibraryScanFormats(t, store, lib.ID, tc.scanFormats)
			seedTwoFormats(t, libraryPath)

			s := newFormatTestScanner(t, store)
			var report ScanMetricsReport
			s.SetScanMetricsCallback(func(r ScanMetricsReport) { report = r })

			if err := s.ScanLibrary(ctx, lib.ID, lib.Path, false); err != nil {
				t.Fatalf("ScanLibrary: %v", err)
			}

			books, err := store.ListBooksByLibrary(ctx, lib.ID)
			if err != nil {
				t.Fatalf("ListBooksByLibrary: %v", err)
			}
			if len(books) != tc.wantBooks {
				paths := make([]string, 0, len(books))
				for _, b := range books {
					paths = append(paths, filepath.Base(b.Path))
				}
				t.Fatalf("入库 %d 本, want %d（scan_formats=%q）: %v", len(books), tc.wantBooks, tc.scanFormats, paths)
			}
			if report.FormatFilteredArchives != tc.wantFilter {
				t.Fatalf("format_filtered_archives = %d, want %d —— 静默少扫必须可见",
					report.FormatFilteredArchives, tc.wantFilter)
			}
		})
	}
}

// TestScanSeriesRespectsLibraryScanFormats 保证系列扫描与库扫描同口径。
// 不同口径的话，「单系列重扫」会把库扫描刚过滤掉的文件重新灌进来。
func TestScanSeriesRespectsLibraryScanFormats(t *testing.T) {
	_, store, lib, libraryPath := newScannerTestLibrary(t)
	ctx := context.Background()
	setLibraryScanFormats(t, store, lib.ID, "cbz")
	seedTwoFormats(t, libraryPath)

	s := newFormatTestScanner(t, store)
	if err := s.ScanLibrary(ctx, lib.ID, lib.Path, false); err != nil {
		t.Fatalf("ScanLibrary: %v", err)
	}
	books, _ := store.ListBooksByLibrary(ctx, lib.ID)
	if len(books) != 1 {
		t.Fatalf("库扫描后应有 1 本，实际 %d", len(books))
	}

	book, err := store.GetBook(ctx, books[0].ID)
	if err != nil {
		t.Fatalf("GetBook: %v", err)
	}
	if err := s.ScanSeries(ctx, book.SeriesID, true); err != nil {
		t.Fatalf("ScanSeries: %v", err)
	}
	books, _ = store.ListBooksByLibrary(ctx, lib.ID)
	if len(books) != 1 {
		t.Fatalf("系列重扫后变成 %d 本 —— 系列扫描没有尊重 scan_formats，把库扫描过滤掉的文件灌回来了", len(books))
	}
}

// TestIgnoreFormatFilterSeesAllIndexedBooks 守卫「重建缩略图不会永久丢掉封面」。
//
// 重建缩略图会先删光缩略图文件、清空所有 cover_path，再靠一次强制扫描重建。
// 若那次扫描仍按 scan_formats 过滤，被排除格式的书就再也不会被访问到——
// 封面永久消失。格式过滤的语义是「导入哪些文件」，不该殃及已入库的内容。
func TestIgnoreFormatFilterSeesAllIndexedBooks(t *testing.T) {
	_, store, lib, libraryPath := newScannerTestLibrary(t)
	ctx := context.Background()
	// 先用全格式入库两本。
	setLibraryScanFormats(t, store, lib.ID, "cbz,cbr")
	seedTwoFormats(t, libraryPath)

	s := newFormatTestScanner(t, store)
	if err := s.ScanLibrary(ctx, lib.ID, lib.Path, false); err != nil {
		t.Fatalf("ScanLibrary: %v", err)
	}
	if books, _ := store.ListBooksByLibrary(ctx, lib.ID); len(books) != 2 {
		t.Fatalf("初始应有 2 本，实际 %d", len(books))
	}

	// 用户随后把格式收窄到只剩 cbz。
	setLibraryScanFormats(t, store, lib.ID, "cbz")

	// 普通扫描会过滤掉 .cbr（但不删已入库的行）。
	var normal ScanMetricsReport
	s.SetScanMetricsCallback(func(r ScanMetricsReport) { normal = r })
	if err := s.ScanLibrary(ctx, lib.ID, lib.Path, true); err != nil {
		t.Fatalf("ScanLibrary: %v", err)
	}
	if normal.FormatFilteredArchives != 1 {
		t.Fatalf("普通扫描应过滤掉 1 个文件，实际 %d", normal.FormatFilteredArchives)
	}
	if books, _ := store.ListBooksByLibrary(ctx, lib.ID); len(books) != 2 {
		t.Fatalf("格式收窄不该删除已入库的书，实际剩 %d 本", len(books))
	}

	// 而维护用的扫描必须看得见全部两本。
	var maintenance ScanMetricsReport
	s.SetScanMetricsCallback(func(r ScanMetricsReport) { maintenance = r })
	if err := s.ScanLibraryWithOptions(ctx, lib.ID, lib.Path, LibraryScanOptions{
		Force: true, IgnoreFormatFilter: true,
	}); err != nil {
		t.Fatalf("ScanLibraryWithOptions: %v", err)
	}
	if maintenance.FormatFilteredArchives != 0 {
		t.Fatalf("维护扫描不该过滤任何文件，实际过滤了 %d 个 —— 被排除格式的书会永久失去封面",
			maintenance.FormatFilteredArchives)
	}
	if maintenance.DiscoveredArchives != 2 {
		t.Fatalf("维护扫描应发现 2 个归档，实际 %d", maintenance.DiscoveredArchives)
	}
}
