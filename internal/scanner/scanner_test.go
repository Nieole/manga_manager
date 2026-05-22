package scanner

import (
	"archive/zip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/parser"
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

func TestScannerPreventsDuplicateLibraryScans(t *testing.T) {
	s := NewScanner(nil, nil, config.NewManager(&config.Config{}))

	if !s.beginLibraryScan(1) {
		t.Fatal("expected first library scan to start")
	}
	if s.beginLibraryScan(1) {
		t.Fatal("expected duplicate library scan to be rejected")
	}

	s.endLibraryScan(1)

	if !s.beginLibraryScan(1) {
		t.Fatal("expected library scan to be allowed after release")
	}
}

func TestScannerPreventsDuplicateSeriesScans(t *testing.T) {
	s := NewScanner(nil, nil, config.NewManager(&config.Config{}))

	if !s.beginSeriesScan(42) {
		t.Fatal("expected first series scan to start")
	}
	if s.beginSeriesScan(42) {
		t.Fatal("expected duplicate series scan to be rejected")
	}

	s.endSeriesScan(42)

	if !s.beginSeriesScan(42) {
		t.Fatal("expected series scan to be allowed after release")
	}
}

func TestScanLibraryReturnsContextCancelled(t *testing.T) {
	_, store, lib, libraryPath := newScannerTestLibrary(t)
	cfg := &config.Config{}
	cfg.Scanner.Workers = 1
	cfg.Scanner.ScanProfile = config.ScanProfileFast
	s := NewScanner(store, nil, config.NewManager(cfg))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := s.ScanLibrary(ctx, lib.ID, libraryPath, true)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
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
	scanner := NewScanner(store, nil, config.NewManager(cfg))
	if err := scanner.ScanLibrary(context.Background(), lib.ID, libraryPath, true); err != nil {
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
	s := NewScanner(store, nil, config.NewManager(cfg))

	openCount := 0
	s.openArchive = func(path string) (parser.Archive, error) {
		openCount++
		return parser.OpenArchive(path)
	}

	if err := s.ScanLibrary(context.Background(), lib.ID, libraryPath, true); err != nil {
		t.Fatalf("initial scan failed: %v", err)
	}
	if openCount == 0 {
		t.Fatal("expected initial scan to open archive")
	}
	books, err := store.ListBooksByLibrary(context.Background(), lib.ID)
	if err != nil || len(books) != 1 {
		t.Fatalf("list books failed: books=%d err=%v", len(books), err)
	}
	waitForScannerBookCover(t, s, store, books[0].ID)

	openCount = 0
	if err := s.ScanLibrary(context.Background(), lib.ID, libraryPath, false); err != nil {
		t.Fatalf("incremental scan failed: %v", err)
	}
	if openCount != 0 {
		t.Fatalf("expected unchanged archive to be skipped, opened %d times", openCount)
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
	s := NewScanner(store, nil, config.NewManager(cfg))
	if err := s.ScanLibrary(context.Background(), lib.ID, libraryPath, true); err != nil {
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

	openCount := 0
	s.openArchive = func(path string) (parser.Archive, error) {
		openCount++
		return parser.OpenArchive(path)
	}
	if err := s.ScanLibrary(context.Background(), lib.ID, libraryPath, false); err != nil {
		t.Fatalf("incremental scan failed: %v", err)
	}
	if openCount == 0 {
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
	s := NewScanner(store, nil, config.NewManager(cfg))
	s.openArchive = func(path string) (parser.Archive, error) {
		t.Fatalf("fast scan should not open archive: %s", path)
		return nil, nil
	}

	if err := s.ScanLibrary(context.Background(), lib.ID, libraryPath, true); err != nil {
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
	s := NewScanner(store, nil, config.NewManager(cfg))

	if err := s.ScanLibrary(context.Background(), lib.ID, libraryPath, true); err != nil {
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
	s := NewScanner(store, nil, config.NewManager(cfg))

	updated := make(chan struct{}, 1)
	s.SetBatchCallback(func(action string) {
		if action == "thumbnail_updated" {
			select {
			case updated <- struct{}{}:
			default:
			}
		}
	})

	if err := s.ScanLibrary(context.Background(), lib.ID, libraryPath, true); err != nil {
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
	s := NewScanner(store, nil, config.NewManager(cfg))
	if err := s.ScanLibrary(context.Background(), lib.ID, libraryPath, true); err != nil {
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
		if err := s.ScanLibrary(context.Background(), lib.ID, libraryPath, false); err != nil {
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
