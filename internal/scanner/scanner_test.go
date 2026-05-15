package scanner

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"testing"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
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
