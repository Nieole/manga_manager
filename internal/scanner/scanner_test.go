// 守扫描主体的对外承诺：同库/同系列的并发扫描必须被拒而不是静默成功；取消要能穿到打开归档
// 之前；增量比对按 mtime+size 跳过未变归档、尺寸一变即失效；fast 档位的强制扫描不清空已入库的
// 页数与封面；worker 数与存储令牌遵守卷的存储策略（identity 档位叠加外置盘策略时不得自死锁）；
// CleanupLibrary 只删文件确已消失的系列，库根散落的归档要保住。

package scanner

import (
	"archive/zip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/parser"
	"manga-manager/internal/taskcontrol"
)

var testPNG1x1 = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x44, 0x41,
	0x54, 0x78, 0x9c, 0x63, 0xf8, 0xcf, 0xc0, 0xf0,
	0x1f, 0x00, 0x05, 0x00, 0x01, 0xff, 0x89, 0x99,
	0x3d, 0x1d, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45,
	0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}

func TestScanLibraryReturnsContextCancelled(t *testing.T) {
	_, store, lib, libraryPath := newScannerTestLibrary(t)
	cfg := &config.Config{}
	cfg.Scanner.Workers = 1
	cfg.Scanner.ScanProfile = config.ScanProfileFast
	s := NewScanner(store, config.NewManager(cfg))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := s.ScanLibrary(ctx, lib.ID, libraryPath, true, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}

func TestWorkerCountUsesExternalHDDPolicy(t *testing.T) {
	cfg := config.Config{}
	cfg.Scanner.Workers = 16
	cfg.Scanner.ScanProfile = config.ScanProfileMetadata
	cfg.Library.StorageProfile = config.StorageProfileHDDExternal
	config.NormalizeConfig(&cfg)

	workers := WorkerCount(cfg, `E:\Manga`, ScanOptions{Profile: ScanProfileMetadata})

	if workers != 1 {
		t.Fatalf("expected external HDD metadata scan to use one worker, got %d", workers)
	}
}

// newWorkerCountPolicyConfig 摆一档四项并发**各不相同**的存储策略。
//
// 外置硬盘那一档四项全为一，收窄与否都得一，测不出哪一步在起作用；三项各异才看得出档位一路加码时
// worker 数逐级收窄。custom 档保留显式填入的正值（见 config.NormalizeStorageIOPolicy）。
func newWorkerCountPolicyConfig(t *testing.T, workers int) (config.Config, string) {
	t.Helper()
	libraryPath := t.TempDir()
	cfg := config.Config{}
	cfg.Scanner.Workers = workers
	cfg.Library.StoragePolicies = []config.LibraryStoragePolicy{{
		Path:           libraryPath,
		StorageProfile: config.StorageProfileCustom,
		IOPolicy: config.StorageIOPolicy{
			ScanConcurrency:        8,
			ArchiveOpenConcurrency: 4,
			HashConcurrency:        2,
			CoverConcurrency:       6,
		},
	}}
	config.NormalizeConfig(&cfg)
	return cfg, libraryPath
}

// TestWorkerCountNarrowsPerProfile 守两次收窄各自生效：开归档的档位与归档打开并发取小，
// 还要算哈希的再与哈希并发取小。封面并发不参与，它归磁盘作业管。
func TestWorkerCountNarrowsPerProfile(t *testing.T) {
	cfg, libraryPath := newWorkerCountPolicyConfig(t, 16)

	cases := []struct {
		profile ScanProfile
		want    int
		why     string
	}{
		{ScanProfileFast, 8, "不开归档，只受扫描并发约束"},
		{ScanProfileMetadata, 4, "开归档，与归档打开并发取小"},
		{ScanProfileIdentity, 2, "还要算哈希，再与哈希并发取小"},
		{ScanProfileRepair, 2, "同 identity"},
	}
	for _, tc := range cases {
		t.Run(string(tc.profile), func(t *testing.T) {
			if got := WorkerCount(cfg, libraryPath, ScanOptions{Profile: tc.profile}); got != tc.want {
				t.Fatalf("worker 数为 %d, want %d（%s）", got, tc.want, tc.why)
			}
		})
	}
}

// TestWorkerCountKeepsConfiguredWorkersBelowLimit 守收尾那一步：配置的 worker 数本就比上限小时
// 以它为准，上限不会把它抬上去。
func TestWorkerCountKeepsConfiguredWorkersBelowLimit(t *testing.T) {
	cfg, libraryPath := newWorkerCountPolicyConfig(t, 3)

	if got := WorkerCount(cfg, libraryPath, ScanOptions{Profile: ScanProfileFast}); got != 3 {
		t.Fatalf("worker 数为 %d, want 3——配置值比上限小时应以配置值为准", got)
	}
}

func TestScanLibraryPauseCheckpointBlocksBeforeOpeningArchive(t *testing.T) {
	rootDir, store, lib, libraryPath := newScannerTestLibrary(t)
	seriesPath := filepath.Join(libraryPath, "Series Alpha")
	archivePath := filepath.Join(seriesPath, "Alpha 01.cbz")
	if err := writeScannerTestCBZ(archivePath, map[string][]byte{"001.png": testPNG1x1}); err != nil {
		t.Fatalf("write cbz failed: %v", err)
	}

	cfg := &config.Config{}
	cfg.Scanner.Workers = 1
	cfg.Scanner.ScanProfile = config.ScanProfileMetadata
	cfg.Scanner.ThumbnailFormat = "webp"
	cfg.Cache.Dir = filepath.Join(rootDir, "thumbs")
	s := NewScanner(store, config.NewManager(cfg))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gate := taskcontrol.NewPauseGate()
	ctx = taskcontrol.WithPauseGate(ctx, gate)
	gate.Pause()

	var openCount atomic.Int64
	s.openArchive = func(path string) (parser.Archive, error) {
		openCount.Add(1)
		return parser.OpenArchive(path)
	}

	done := make(chan error, 1)
	go func() {
		done <- s.ScanLibrary(ctx, lib.ID, libraryPath, true, nil)
	}()

	select {
	case err := <-done:
		t.Fatalf("expected scan to block while paused, got %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if openCount.Load() != 0 {
		t.Fatalf("expected paused scan to block before opening archive, opened %d archives", openCount.Load())
	}

	gate.Resume()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected scan to finish after resume, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("expected scan to finish after resume")
	}
	if openCount.Load() == 0 {
		t.Fatal("expected resumed scan to open archive")
	}
	books, err := store.ListBooksByLibrary(context.Background(), lib.ID)
	if err != nil {
		t.Fatalf("list books failed: %v", err)
	}
	if len(books) != 1 {
		t.Fatalf("expected one scanned book after resume, got %d", len(books))
	}
	waitForScannerBookCover(t, s, store, books[0].ID)
}

func TestScanLibraryRecordsPageCount(t *testing.T) {
	rootDir := t.TempDir()
	dbPath := filepath.Join(rootDir, "manga.db")
	if err := database.Migrate(dbPath); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}
	store, err := database.NewStore(dbPath)
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	libraryPath := filepath.Join(rootDir, "library")
	seriesPath := filepath.Join(libraryPath, "Series Alpha")
	if err := os.MkdirAll(seriesPath, 0o755); err != nil {
		t.Fatalf("mkdir series failed: %v", err)
	}
	archivePath := filepath.Join(seriesPath, "Alpha 01.cbz")
	if err := writeScannerTestCBZ(archivePath, map[string][]byte{
		"002.png":   testPNG1x1,
		"001.png":   testPNG1x1,
		"notes.txt": []byte("ignored"),
	}); err != nil {
		t.Fatalf("write cbz failed: %v", err)
	}

	lib, err := store.CreateLibrary(context.Background(), database.CreateLibraryParams{
		Name:                "Library",
		Path:                libraryPath,
		ScanMode:            "none",
		KoreaderSyncEnabled: true,
		ScanInterval:        60,
		ScanFormats:         "zip,cbz,rar,cbr",
	})
	if err != nil {
		t.Fatalf("create library failed: %v", err)
	}

	cfg := &config.Config{}
	cfg.Scanner.Workers = 1
	cfg.Scanner.ThumbnailFormat = "webp"
	cfg.Cache.Dir = filepath.Join(rootDir, "thumbs")
	scanner := NewScanner(store, config.NewManager(cfg))
	if err := scanner.ScanLibrary(context.Background(), lib.ID, libraryPath, true, nil); err != nil {
		t.Fatalf("scan library failed: %v", err)
	}

	books, err := store.ListBooksByLibrary(context.Background(), lib.ID)
	if err != nil {
		t.Fatalf("list books failed: %v", err)
	}
	if len(books) != 1 {
		t.Fatalf("expected one scanned book, got %d", len(books))
	}
	book, err := store.GetBook(context.Background(), books[0].ID)
	if err != nil {
		t.Fatalf("get scanned book failed: %v", err)
	}
	if book.PageCount != 2 {
		t.Fatalf("expected scanned book page count 2, got %d", book.PageCount)
	}
	if book.FileHash.Valid && book.FileHash.String != "" {
		t.Fatalf("expected default metadata scan with KOReader disabled to skip full hash, got %q", book.FileHash.String)
	}
	if book.QuickHash.Valid && book.QuickHash.String != "" {
		t.Fatalf("expected default metadata scan to skip quick hash, got %q", book.QuickHash.String)
	}
	waitForScannerBookCover(t, scanner, store, book.ID)
}

func TestScanLibrarySkipsUnchangedArchives(t *testing.T) {
	rootDir, store, lib, libraryPath := newScannerTestLibrary(t)
	seriesPath := filepath.Join(libraryPath, "Series Alpha")
	archivePath := filepath.Join(seriesPath, "Alpha 01.cbz")
	if err := writeScannerTestCBZ(archivePath, map[string][]byte{"001.png": testPNG1x1}); err != nil {
		t.Fatalf("write cbz failed: %v", err)
	}

	cfg := &config.Config{}
	cfg.Scanner.Workers = 1
	cfg.Scanner.ScanProfile = config.ScanProfileMetadata
	cfg.Scanner.ThumbnailFormat = "webp"
	cfg.Cache.Dir = filepath.Join(rootDir, "thumbs")
	s := NewScanner(store, config.NewManager(cfg))

	var openCount atomic.Int64
	s.openArchive = func(path string) (parser.Archive, error) {
		openCount.Add(1)
		return parser.OpenArchive(path)
	}

	if err := s.ScanLibrary(context.Background(), lib.ID, libraryPath, true, nil); err != nil {
		t.Fatalf("initial scan failed: %v", err)
	}
	if openCount.Load() == 0 {
		t.Fatal("expected initial scan to open archive")
	}
	books, err := store.ListBooksByLibrary(context.Background(), lib.ID)
	if err != nil || len(books) != 1 {
		t.Fatalf("list books failed: books=%d err=%v", len(books), err)
	}
	waitForScannerBookCover(t, s, store, books[0].ID)

	openCount.Store(0)
	if err := s.ScanLibrary(context.Background(), lib.ID, libraryPath, false, nil); err != nil {
		t.Fatalf("incremental scan failed: %v", err)
	}
	if openCount.Load() != 0 {
		t.Fatalf("expected unchanged archive to be skipped, opened %d times", openCount.Load())
	}
}

func TestScanLibraryInvalidatesIncrementalCacheWhenSizeChanges(t *testing.T) {
	rootDir, store, lib, libraryPath := newScannerTestLibrary(t)
	seriesPath := filepath.Join(libraryPath, "Series Alpha")
	archivePath := filepath.Join(seriesPath, "Alpha 01.cbz")
	if err := writeScannerTestCBZ(archivePath, map[string][]byte{"001.png": testPNG1x1}); err != nil {
		t.Fatalf("write cbz failed: %v", err)
	}

	cfg := &config.Config{}
	cfg.Scanner.Workers = 1
	cfg.Scanner.ScanProfile = config.ScanProfileMetadata
	cfg.Scanner.ThumbnailFormat = "webp"
	cfg.Cache.Dir = filepath.Join(rootDir, "thumbs")
	s := NewScanner(store, config.NewManager(cfg))
	if err := s.ScanLibrary(context.Background(), lib.ID, libraryPath, true, nil); err != nil {
		t.Fatalf("initial scan failed: %v", err)
	}
	books, err := store.ListBooksByLibrary(context.Background(), lib.ID)
	if err != nil || len(books) != 1 {
		t.Fatalf("list books failed: books=%d err=%v", len(books), err)
	}
	waitForScannerBookCover(t, s, store, books[0].ID)
	originalModTime := books[0].FileModifiedAt

	if err := writeScannerTestCBZ(archivePath, map[string][]byte{
		"001.png": testPNG1x1,
		"002.png": testPNG1x1,
	}); err != nil {
		t.Fatalf("rewrite cbz failed: %v", err)
	}
	if err := os.Chtimes(archivePath, originalModTime, originalModTime); err != nil {
		t.Fatalf("restore archive mtime failed: %v", err)
	}

	var openCount atomic.Int64
	s.openArchive = func(path string) (parser.Archive, error) {
		openCount.Add(1)
		return parser.OpenArchive(path)
	}
	if err := s.ScanLibrary(context.Background(), lib.ID, libraryPath, false, nil); err != nil {
		t.Fatalf("incremental scan failed: %v", err)
	}
	if openCount.Load() == 0 {
		t.Fatal("expected size-only change to trigger archive open")
	}
	updatedBooks, err := store.ListBooksByLibrary(context.Background(), lib.ID)
	if err != nil || len(updatedBooks) != 1 {
		t.Fatalf("list updated books failed: books=%d err=%v", len(updatedBooks), err)
	}
	updated, err := store.GetBook(context.Background(), updatedBooks[0].ID)
	if err != nil {
		t.Fatalf("get updated book failed: %v", err)
	}
	if updated.PageCount != 2 {
		t.Fatalf("expected size-only change to refresh page count, got %d", updated.PageCount)
	}
	waitForScannerBookCover(t, s, store, updatedBooks[0].ID)
}

func TestFastScanDoesNotOpenArchive(t *testing.T) {
	rootDir, store, lib, libraryPath := newScannerTestLibrary(t)
	seriesPath := filepath.Join(libraryPath, "Series Alpha")
	archivePath := filepath.Join(seriesPath, "Alpha 01.cbz")
	if err := writeScannerTestCBZ(archivePath, map[string][]byte{"001.png": testPNG1x1}); err != nil {
		t.Fatalf("write cbz failed: %v", err)
	}

	cfg := &config.Config{}
	cfg.Scanner.Workers = 1
	cfg.Scanner.ScanProfile = config.ScanProfileFast
	cfg.Cache.Dir = filepath.Join(rootDir, "thumbs")
	s := NewScanner(store, config.NewManager(cfg))
	s.openArchive = func(path string) (parser.Archive, error) {
		t.Fatalf("fast scan should not open archive: %s", path)
		return nil, nil
	}

	if err := s.ScanLibrary(context.Background(), lib.ID, libraryPath, true, nil); err != nil {
		t.Fatalf("fast scan failed: %v", err)
	}
	books, err := store.ListBooksByLibrary(context.Background(), lib.ID)
	if err != nil {
		t.Fatalf("list books failed: %v", err)
	}
	if len(books) != 1 {
		t.Fatalf("expected one discovered book, got %d", len(books))
	}
	book, err := store.GetBook(context.Background(), books[0].ID)
	if err != nil {
		t.Fatalf("get book failed: %v", err)
	}
	if book.PageCount != 0 {
		t.Fatalf("expected fast scan placeholder page count 0, got %d", book.PageCount)
	}
}

// TestForcedFastScanKeepsIndexedPageCountAndCover 守「fast 档位的强制扫描不抹掉已入库的页数与封面」。
//
// fast 档位不开归档、也不排封面任务，upsert 一旦把 page_count 写 0、cover_path 写空，
// 之后的增量扫描又因 mtime+size 没变而跳过这些书，页数与封面永不自愈。
// force=true 不止来自手动强制重扫：重建搜索索引与重建缩略图内部也走这条路。
func TestForcedFastScanKeepsIndexedPageCountAndCover(t *testing.T) {
	t.Run("整库强制扫描", func(t *testing.T) {
		store, lib, s, baseline := seedFastScanProfileSwitch(t)
		if err := s.ScanLibrary(context.Background(), lib.ID, lib.Path, true, nil); err != nil {
			t.Fatalf("forced fast library scan failed: %v", err)
		}
		assertBookKeepsPagesAndCover(t, store, baseline)
	})

	t.Run("单系列强制扫描", func(t *testing.T) {
		store, _, s, baseline := seedFastScanProfileSwitch(t)
		if err := s.ScanSeries(context.Background(), baseline.SeriesID, true, nil); err != nil {
			t.Fatalf("forced fast series scan failed: %v", err)
		}
		assertBookKeepsPagesAndCover(t, store, baseline)
	})
}

// seedFastScanProfileSwitch 先用 metadata 档位把一本两页的书连页数带封面完整入库，
// 再把 scanner.scan_profile 切到 fast，交出切换后的扫描器与入库时的基线书行。
func seedFastScanProfileSwitch(t *testing.T) (database.Store, database.Library, *Scanner, database.Book) {
	t.Helper()
	rootDir, store, lib, libraryPath := newScannerTestLibrary(t)
	archivePath := filepath.Join(libraryPath, "Series Alpha", "Alpha 01.cbz")
	if err := writeScannerTestCBZ(archivePath, map[string][]byte{"001.png": testPNG1x1, "002.png": testPNG1x1}); err != nil {
		t.Fatalf("write cbz failed: %v", err)
	}

	cfg := &config.Config{}
	cfg.Scanner.Workers = 1
	cfg.Scanner.ScanProfile = config.ScanProfileMetadata
	cfg.Scanner.ThumbnailFormat = "webp"
	cfg.Cache.Dir = filepath.Join(rootDir, "thumbs")
	manager := config.NewManager(cfg)
	s := NewScanner(store, manager)

	if err := s.ScanLibrary(context.Background(), lib.ID, libraryPath, true, nil); err != nil {
		t.Fatalf("metadata scan failed: %v", err)
	}
	books, err := store.ListBooksByLibrary(context.Background(), lib.ID)
	if err != nil || len(books) != 1 {
		t.Fatalf("list books failed: books=%d err=%v", len(books), err)
	}
	waitForScannerBookCover(t, s, store, books[0].ID)
	baseline, err := store.GetBook(context.Background(), books[0].ID)
	if err != nil {
		t.Fatalf("get baseline book failed: %v", err)
	}
	if baseline.PageCount != 2 {
		t.Fatalf("metadata 档位应记下 2 页，实际 %d", baseline.PageCount)
	}

	cfg.Scanner.ScanProfile = config.ScanProfileFast
	manager.Replace(cfg)
	return store, lib, s, baseline
}

func assertBookKeepsPagesAndCover(t *testing.T, store database.Store, baseline database.Book) {
	t.Helper()
	book, err := store.GetBook(context.Background(), baseline.ID)
	if err != nil {
		t.Fatalf("get book after forced fast scan failed: %v", err)
	}
	if book.PageCount != baseline.PageCount {
		t.Fatalf("page_count = %d，want %d —— fast 档位的强制扫描清零了已入库页数，"+
			"之后的增量扫描还会因 mtime+size 未变而跳过它，页数不会自愈", book.PageCount, baseline.PageCount)
	}
	if book.CoverPath.String != baseline.CoverPath.String {
		t.Fatalf("cover_path = %q，want %q —— fast 档位的强制扫描抹掉了已入库封面，"+
			"而它既不开归档也不排封面任务，封面不会自愈", book.CoverPath.String, baseline.CoverPath.String)
	}
}

func TestScanMetricsSnapshot(t *testing.T) {
	metrics := &scanMetrics{}
	metrics.discoveredArchives.Add(2)
	metrics.skippedArchives.Add(1)
	metrics.processedArchives.Add(1)
	metrics.openedArchives.Add(1)
	metrics.hashedFiles.Add(2)

	snapshot := metrics.snapshot()
	if snapshot.discoveredArchives != 2 ||
		snapshot.skippedArchives != 1 ||
		snapshot.processedArchives != 1 ||
		snapshot.openedArchives != 1 ||
		snapshot.hashedFiles != 2 {
		t.Fatalf("unexpected scan metrics snapshot: %+v", snapshot)
	}
}

func TestKOReaderEnabledMetadataScanDefersBinaryHash(t *testing.T) {
	rootDir, store, lib, libraryPath := newScannerTestLibrary(t)
	seriesPath := filepath.Join(libraryPath, "Series Alpha")
	archivePath := filepath.Join(seriesPath, "Alpha 01.cbz")
	if err := writeScannerTestCBZ(archivePath, map[string][]byte{"001.png": testPNG1x1}); err != nil {
		t.Fatalf("write cbz failed: %v", err)
	}

	cfg := &config.Config{}
	cfg.Scanner.Workers = 1
	cfg.Scanner.ScanProfile = config.ScanProfileMetadata
	cfg.Scanner.ThumbnailFormat = "webp"
	cfg.Cache.Dir = filepath.Join(rootDir, "thumbs")
	cfg.KOReader.Enabled = true
	cfg.KOReader.MatchMode = config.KOReaderMatchModeBinaryHash
	s := NewScanner(store, config.NewManager(cfg))

	if err := s.ScanLibrary(context.Background(), lib.ID, libraryPath, true, nil); err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	books, err := store.ListBooksByLibrary(context.Background(), lib.ID)
	if err != nil || len(books) != 1 {
		t.Fatalf("list books failed: books=%d err=%v", len(books), err)
	}
	book, err := store.GetBook(context.Background(), books[0].ID)
	if err != nil {
		t.Fatalf("get scanned book failed: %v", err)
	}
	if book.FileHash.Valid && book.FileHash.String != "" {
		t.Fatalf("expected KOReader-enabled metadata scan to defer binary file hash, got %q", book.FileHash.String)
	}
	waitForScannerBookCover(t, s, store, book.ID)
}

func TestIdentityScanWithExternalHDDPolicyDoesNotSelfDeadlock(t *testing.T) {
	rootDir, store, lib, libraryPath := newScannerTestLibrary(t)
	seriesPath := filepath.Join(libraryPath, "Series Alpha")
	archivePath := filepath.Join(seriesPath, "Alpha 01.cbz")
	if err := writeScannerTestCBZ(archivePath, map[string][]byte{"001.png": testPNG1x1}); err != nil {
		t.Fatalf("write cbz failed: %v", err)
	}

	cfg := &config.Config{}
	cfg.Scanner.Workers = 4
	cfg.Scanner.ScanProfile = config.ScanProfileIdentity
	cfg.Scanner.ThumbnailFormat = "webp"
	cfg.Cache.Dir = filepath.Join(rootDir, "thumbs")
	cfg.Library.StorageProfile = config.StorageProfileHDDExternal
	cfg.KOReader.Enabled = true
	cfg.KOReader.MatchMode = config.KOReaderMatchModeBinaryHash
	config.NormalizeConfig(cfg)
	s := NewScanner(store, config.NewManager(cfg))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.ScanLibrary(ctx, lib.ID, libraryPath, true, nil); err != nil {
		t.Fatalf("identity scan failed: %v", err)
	}
	books, err := store.ListBooksByLibrary(context.Background(), lib.ID)
	if err != nil || len(books) != 1 {
		t.Fatalf("list books failed: books=%d err=%v", len(books), err)
	}
	book, err := store.GetBook(context.Background(), books[0].ID)
	if err != nil {
		t.Fatalf("get scanned book failed: %v", err)
	}
	if !book.FileHash.Valid || book.FileHash.String == "" || !book.QuickHash.Valid || book.QuickHash.String == "" {
		t.Fatalf("expected identity scan to populate hashes, got file=%q quick=%q", book.FileHash.String, book.QuickHash.String)
	}
	waitForScannerBookCover(t, s, store, book.ID)
}

func TestScanLibraryQueuesMissingCoverGeneration(t *testing.T) {
	rootDir, store, lib, libraryPath := newScannerTestLibrary(t)
	seriesPath := filepath.Join(libraryPath, "Series Alpha")
	archivePath := filepath.Join(seriesPath, "Alpha 01.cbz")
	if err := writeScannerTestCBZ(archivePath, map[string][]byte{"001.png": testPNG1x1}); err != nil {
		t.Fatalf("write cbz failed: %v", err)
	}

	cfg := &config.Config{}
	cfg.Scanner.Workers = 1
	cfg.Scanner.ScanProfile = config.ScanProfileMetadata
	cfg.Scanner.ThumbnailFormat = "webp"
	cfg.Cache.Dir = filepath.Join(rootDir, "thumbs")
	s := NewScanner(store, config.NewManager(cfg))

	updated := make(chan struct{}, 1)
	s.SetBatchCallback(func(action string) {
		if action == "thumbnail_updated" {
			select {
			case updated <- struct{}{}:
			default:
			}
		}
	})

	if err := s.ScanLibrary(context.Background(), lib.ID, libraryPath, true, nil); err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	books, err := store.ListBooksByLibrary(context.Background(), lib.ID)
	if err != nil || len(books) != 1 {
		t.Fatalf("list books failed: books=%d err=%v", len(books), err)
	}

	waitForScannerBookCover(t, s, store, books[0].ID)
	select {
	case <-updated:
	default:
		t.Fatal("expected thumbnail_updated callback after queued cover generation")
	}
}

// TestScanLibraryDeferredRefreshPopulatesStats 守护读模型刷新节流化：扫描把每批全量重算改为扫描末尾/10s
// 兜底刷新后，扫描结束时 series.book_count / total_pages 仍必须正确（refreshDirtySeries 的 UpdateSeriesStatistics
// 在扫描结束 !ok 分支运行）。两个系列各两本、每本两页。
func TestScanLibraryDeferredRefreshPopulatesStats(t *testing.T) {
	rootDir, store, lib, libraryPath := newScannerTestLibrary(t)
	for _, sp := range []struct {
		series string
		books  []string
	}{
		{"Series Alpha", []string{"Alpha 01.cbz", "Alpha 02.cbz"}},
		{"Series Beta", []string{"Beta 01.cbz", "Beta 02.cbz"}},
	} {
		seriesPath := filepath.Join(libraryPath, sp.series)
		if err := os.MkdirAll(seriesPath, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", sp.series, err)
		}
		for _, b := range sp.books {
			if err := writeScannerTestCBZ(filepath.Join(seriesPath, b), map[string][]byte{
				"001.png": testPNG1x1, "002.png": testPNG1x1,
			}); err != nil {
				t.Fatalf("write cbz %s: %v", b, err)
			}
		}
	}

	cfg := &config.Config{}
	cfg.Scanner.Workers = 1
	cfg.Scanner.ThumbnailFormat = "webp"
	cfg.Cache.Dir = filepath.Join(rootDir, "thumbs")
	scanner := NewScanner(store, config.NewManager(cfg))
	if err := scanner.ScanLibrary(context.Background(), lib.ID, libraryPath, true, nil); err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	// 排空异步封面队列，避免其在 t.Cleanup 关闭 store / 删除临时目录时仍运行导致清理竞态。
	drainCtx, cancelDrain := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelDrain()
	_ = scanner.waitForCoverQueue(drainCtx)

	rows, total, err := store.SearchSeriesPaged(context.Background(), lib.ID, database.SeriesListFilters{}, 50, 0, "name_asc")
	if err != nil {
		t.Fatalf("search series: %v", err)
	}
	if total != 2 || len(rows) != 2 {
		t.Fatalf("expected 2 series after scan, got total=%d rows=%d", total, len(rows))
	}
	for _, r := range rows {
		if r.BookCount != 2 {
			t.Fatalf("series %q book_count=%d, want 2 (deferred refresh must populate series stats)", r.Name, r.BookCount)
		}
		if !r.TotalPages.Valid || r.TotalPages.Float64 != 4 {
			t.Fatalf("series %q total_pages=%v, want 4 (2 books x 2 pages)", r.Name, r.TotalPages)
		}
	}
}

func BenchmarkScanLibrary_Incremental_NoChanges(b *testing.B) {
	rootDir, store, lib, libraryPath := newScannerTestLibrary(b)
	seriesPath := filepath.Join(libraryPath, "Series Alpha")
	for i := 0; i < 20; i++ {
		archivePath := filepath.Join(seriesPath, "Alpha "+strconv.Itoa(i+1)+".cbz")
		if err := writeScannerTestCBZ(archivePath, map[string][]byte{"001.png": testPNG1x1}); err != nil {
			b.Fatalf("write cbz failed: %v", err)
		}
	}

	cfg := &config.Config{}
	cfg.Scanner.Workers = 1
	cfg.Scanner.ScanProfile = config.ScanProfileMetadata
	cfg.Scanner.ThumbnailFormat = "webp"
	cfg.Cache.Dir = filepath.Join(rootDir, "thumbs")
	s := NewScanner(store, config.NewManager(cfg))
	if err := s.ScanLibrary(context.Background(), lib.ID, libraryPath, true, nil); err != nil {
		b.Fatalf("initial scan failed: %v", err)
	}
	books, err := store.ListBooksByLibrary(context.Background(), lib.ID)
	if err != nil {
		b.Fatalf("list books failed: %v", err)
	}
	for _, book := range books {
		waitForScannerBookCover(b, s, store, book.ID)
	}
	s.openArchive = func(path string) (parser.Archive, error) {
		b.Fatalf("incremental no-change benchmark should not open archive: %s", path)
		return nil, nil
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := s.ScanLibrary(context.Background(), lib.ID, libraryPath, false, nil); err != nil {
			b.Fatalf("incremental scan failed: %v", err)
		}
	}
}

func newScannerTestLibrary(t testing.TB) (string, database.Store, database.Library, string) {
	t.Helper()
	rootDir := t.TempDir()
	dbPath := filepath.Join(rootDir, "manga.db")
	if err := database.Migrate(dbPath); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}
	store, err := database.NewStore(dbPath)
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	libraryPath := filepath.Join(rootDir, "library")
	seriesPath := filepath.Join(libraryPath, "Series Alpha")
	if err := os.MkdirAll(seriesPath, 0o755); err != nil {
		t.Fatalf("mkdir series failed: %v", err)
	}
	lib, err := store.CreateLibrary(context.Background(), database.CreateLibraryParams{
		Name:                "Library",
		Path:                libraryPath,
		ScanMode:            "none",
		KoreaderSyncEnabled: true,
		ScanInterval:        60,
		ScanFormats:         config.DefaultScanFormatsCSV,
	})
	if err != nil {
		t.Fatalf("create library failed: %v", err)
	}
	return rootDir, store, lib, libraryPath
}

func waitForScannerBookCover(t testing.TB, s *Scanner, store database.Store, bookID int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := s.waitForCoverQueue(ctx); err != nil {
		t.Fatalf("wait cover queue failed: %v", err)
	}
	book, err := store.GetBook(context.Background(), bookID)
	if err != nil {
		t.Fatalf("get book after cover queue failed: %v", err)
	}
	if !book.CoverPath.Valid || book.CoverPath.String == "" {
		t.Fatalf("expected queued cover path for book %d", bookID)
	}
}

// writeScannerTestSeries 在 seriesPath 下铺一串单页归档，返回它们的完整路径（与 names 同序）。
func writeScannerTestSeries(t testing.TB, seriesPath string, names ...string) []string {
	t.Helper()
	paths := make([]string, 0, len(names))
	for _, name := range names {
		path := filepath.Join(seriesPath, name)
		if err := writeScannerTestCBZ(path, map[string][]byte{"001.png": testPNG1x1}); err != nil {
			t.Fatalf("write cbz %s failed: %v", name, err)
		}
		paths = append(paths, path)
	}
	return paths
}

func writeScannerTestCBZ(path string, files map[string][]byte) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	for name, data := range files {
		w, err := zw.Create(name)
		if err != nil {
			_ = zw.Close()
			return err
		}
		if _, err := w.Write(data); err != nil {
			_ = zw.Close()
			return err
		}
	}
	return zw.Close()
}

// TestCleanupLibraryKeepsRootLevelLooseArchives 锁住散装归档的清理语义。
//
// 库根目录直放的 <root>/Loose Volume.cbz 会被归到合成系列路径 <root>/Loose Volume 下，
// 而该目录在磁盘上从不存在。CleanupLibrary 若只按目录是否存在判定，就会把这个系列连同
// 它的书与每用户阅读进度一并 CASCADE 删掉——而书文件明明还在。更糟的是下次扫描会重建，
// 再下次清理再删，进度反复丢失。50% 熔断在散装文件占少数时完全不触发。
func TestCleanupLibraryKeepsRootLevelLooseArchives(t *testing.T) {
	rootDir, store, lib, libraryPath := newScannerTestLibrary(t)
	ctx := context.Background()

	// 两个规规矩矩放在子目录里的系列，把散装系列的占比压到 1/3，明确低于 50% 熔断线，
	// 使本用例不依赖熔断阈值的边界比较语义。
	for _, name := range []string{"Series Alpha", "Series Beta"} {
		dir := filepath.Join(libraryPath, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s failed: %v", name, err)
		}
		if err := writeScannerTestCBZ(filepath.Join(dir, name+" 01.cbz"), map[string][]byte{"001.png": testPNG1x1}); err != nil {
			t.Fatalf("write nested cbz for %s failed: %v", name, err)
		}
	}
	// 直接躺在库根目录下的散装归档。
	looseArchive := filepath.Join(libraryPath, "Loose Volume.cbz")
	if err := writeScannerTestCBZ(looseArchive, map[string][]byte{"001.png": testPNG1x1}); err != nil {
		t.Fatalf("write loose cbz failed: %v", err)
	}

	cfg := &config.Config{}
	cfg.Scanner.Workers = 1
	cfg.Scanner.ThumbnailFormat = "webp"
	cfg.Cache.Dir = filepath.Join(rootDir, "thumbs")
	s := NewScanner(store, config.NewManager(cfg))
	if err := s.ScanLibrary(ctx, lib.ID, libraryPath, true, nil); err != nil {
		t.Fatalf("scan library failed: %v", err)
	}

	booksBefore, err := store.ListBooksByLibrary(ctx, lib.ID)
	if err != nil {
		t.Fatalf("list books failed: %v", err)
	}
	if len(booksBefore) != 3 {
		t.Fatalf("expected 3 scanned books, got %d", len(booksBefore))
	}

	// 等封面队列排空再清理：ScanLibrary 返回时封面生成仍在异步写 books.cover_path，
	// 而 CleanupLibrary 会 CASCADE 删除 books——两者重叠会让断言看到中间状态。
	waitForScannerCoverQueue(t, s)

	if err := s.CleanupLibrary(ctx, lib.ID); err != nil {
		t.Fatalf("cleanup library failed: %v", err)
	}

	booksAfter, err := store.ListBooksByLibrary(ctx, lib.ID)
	if err != nil {
		t.Fatalf("list books after cleanup failed: %v", err)
	}
	if len(booksAfter) != len(booksBefore) {
		t.Fatalf("cleanup deleted books whose files still exist: had %d, left %d", len(booksBefore), len(booksAfter))
	}

	var sawLoose bool
	for _, b := range booksAfter {
		if b.Path == looseArchive {
			sawLoose = true
		}
	}
	if !sawLoose {
		t.Fatalf("loose root-level archive %q was removed by cleanup", looseArchive)
	}
}

// TestCleanupLibraryRemovesSeriesWhenFilesAreGone 确认上面的保护没有把清理彻底废掉：
// 目录和书文件都真的不在了，该系列仍应被删除。
func TestCleanupLibraryRemovesSeriesWhenFilesAreGone(t *testing.T) {
	rootDir, store, lib, libraryPath := newScannerTestLibrary(t)
	ctx := context.Background()

	for _, name := range []string{"Series Alpha", "Series Beta", "Series Gamma"} {
		dir := filepath.Join(libraryPath, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s failed: %v", name, err)
		}
		if err := writeScannerTestCBZ(filepath.Join(dir, name+" 01.cbz"), map[string][]byte{"001.png": testPNG1x1}); err != nil {
			t.Fatalf("write cbz for %s failed: %v", name, err)
		}
	}

	cfg := &config.Config{}
	cfg.Scanner.Workers = 1
	cfg.Scanner.ThumbnailFormat = "webp"
	cfg.Cache.Dir = filepath.Join(rootDir, "thumbs")
	s := NewScanner(store, config.NewManager(cfg))
	if err := s.ScanLibrary(ctx, lib.ID, libraryPath, true, nil); err != nil {
		t.Fatalf("scan library failed: %v", err)
	}

	waitForScannerCoverQueue(t, s)

	// 只删掉一个系列（1/3 < 50% 熔断线），它的目录与书文件都消失。
	if err := os.RemoveAll(filepath.Join(libraryPath, "Series Gamma")); err != nil {
		t.Fatalf("remove series dir failed: %v", err)
	}

	if err := s.CleanupLibrary(ctx, lib.ID); err != nil {
		t.Fatalf("cleanup library failed: %v", err)
	}

	seriesList, err := store.ListSeriesByLibraryLite(ctx, lib.ID)
	if err != nil {
		t.Fatalf("list series failed: %v", err)
	}
	for _, sr := range seriesList {
		if filepath.Base(sr.Path) == "Series Gamma" {
			t.Fatalf("expected genuinely missing series to be removed, still present: %s", sr.Path)
		}
	}
	if len(seriesList) != 2 {
		t.Fatalf("expected 2 surviving series, got %d", len(seriesList))
	}
}

// TestScanLibraryIsNotRefusedWhileAnotherScanIsInFlight 守扫描器**不自判目标冲突**：
// 「同一件事不会同时跑两遍」只由任务那一处的准入回答（ADR 0005）。
//
// 扫描器这边留下的只是记账，它必须是**计数**：写成集合的话，里层这次扫描收尾就把外层那次的
// 记录一并抹掉，而文件监听器正靠这个记录判断敢不敢清理——抹早了就是把还没重连上的行
// 当成「文件已消失」删掉。
func TestScanLibraryIsNotRefusedWhileAnotherScanIsInFlight(t *testing.T) {
	_, store, lib, libraryPath := newScannerTestLibrary(t)
	s := NewScanner(store, config.NewManager(&config.Config{}))

	// 假装这个库上已经有一次扫描在飞（重建缩略图的逐库强扫就是这么一次）。
	s.enterLibraryScan(lib.ID)
	defer s.leaveLibraryScan(lib.ID)

	if err := s.ScanLibrary(context.Background(), lib.ID, libraryPath, false, nil); err != nil {
		t.Fatalf("同一个库上的第二次扫描被拒绝了: %v", err)
	}
	if !s.LibraryScanActive(lib.ID) {
		t.Fatal("第二次扫描收尾把第一次的记账也抹掉了 —— 监听器会以为没人在扫，抢先删掉待重连的行")
	}
}

// activeProbeObserver 是只做一件事的**扫描观察者**：在扫描进行中问一句记账，并把答案记下来。
type activeProbeObserver struct {
	active func() bool
	saw    *atomic.Bool
}

func (o *activeProbeObserver) Progress(ScanProgressReport) {
	if o.active() {
		o.saw.Store(true)
	}
}

func (o *activeProbeObserver) Metrics(ScanMetricsReport) {}

func (o *activeProbeObserver) ItemFailed(ItemFailure) {}

func (o *activeProbeObserver) Warn(ScanWarning) {}

// TestLibraryScanActiveTracksTheScanInFlight 守那笔记账在扫描**期间**为真、返回后为假：
// 文件监听器按它决定敢不敢清理，答错任一头都是丢阅读进度。
func TestLibraryScanActiveTracksTheScanInFlight(t *testing.T) {
	_, store, lib, libraryPath := newScannerTestLibrary(t)
	s := NewScanner(store, config.NewManager(&config.Config{}))

	if s.LibraryScanActive(lib.ID) {
		t.Fatal("一次扫描都还没发起，记账就说有扫描在跑")
	}

	// 观察点取**扫描进行中**的报文：它一定在扫描内部发出，不必依赖库里有没有归档。
	var sawItselfActive atomic.Bool
	probe := &activeProbeObserver{active: func() bool { return s.LibraryScanActive(lib.ID) }, saw: &sawItselfActive}
	if err := s.ScanLibrary(context.Background(), lib.ID, libraryPath, true, probe); err != nil {
		t.Fatalf("扫描失败: %v", err)
	}

	if !sawItselfActive.Load() {
		t.Fatal("扫描进行中记账却说没有扫描在跑")
	}
	if s.LibraryScanActive(lib.ID) {
		t.Fatal("扫描返回之后记账没有归零")
	}
}

// waitForScannerCoverQueue 等待进程级封面生成队列排空。
// 扫描是「入库同步、封面异步」，不等它就断言的用例会偶发看到中间状态。
func waitForScannerCoverQueue(t testing.TB, s *Scanner) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.waitForCoverQueue(ctx); err != nil {
		t.Fatalf("wait cover queue failed: %v", err)
	}
}
