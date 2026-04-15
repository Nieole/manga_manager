package koreader

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

func newTestService(t *testing.T, matchMode string) (*Service, database.Store, string) {
	t.Helper()

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	if err := database.Migrate(dbPath); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}

	store, err := database.NewStore(dbPath)
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	cfg := &config.Config{}
	cfg.Database.Path = dbPath
	cfg.KOReader.MatchMode = matchMode
	config.NormalizeConfig(cfg)

	return NewService(store, config.NewManager(cfg)), store, tempDir
}

func seedServiceBook(t *testing.T, store database.Store, rootDir, libraryName, seriesName, bookName string) (database.Library, database.Book) {
	t.Helper()

	libPath := filepath.Join(rootDir, libraryName)
	if err := os.MkdirAll(libPath, 0o755); err != nil {
		t.Fatalf("mkdir library failed: %v", err)
	}

	lib, err := store.CreateLibrary(context.Background(), database.CreateLibraryParams{
		Name:                libraryName,
		Path:                libPath,
		AutoScan:            false,
		KOReaderSyncEnabled: true,
		ScanInterval:        60,
		ScanFormats:         config.DefaultScanFormatsCSV,
	})
	if err != nil {
		t.Fatalf("create library failed: %v", err)
	}

	seriesPath := filepath.Join(libPath, seriesName)
	if err := os.MkdirAll(seriesPath, 0o755); err != nil {
		t.Fatalf("mkdir series failed: %v", err)
	}

	series, err := store.CreateSeries(context.Background(), database.CreateSeriesParams{
		LibraryID: lib.ID,
		Name:      seriesName,
		Path:      seriesPath,
	})
	if err != nil {
		t.Fatalf("create series failed: %v", err)
	}

	bookPath := filepath.Join(seriesPath, bookName)
	book, err := store.CreateBook(context.Background(), database.CreateBookParams{
		SeriesID:       series.ID,
		LibraryID:      lib.ID,
		Name:           bookName,
		Path:           bookPath,
		Size:           16,
		FileModifiedAt: time.Now(),
		PageCount:      32,
		Title:          sql.NullString{String: bookName, Valid: true},
	})
	if err != nil {
		t.Fatalf("create book failed: %v", err)
	}

	return lib, book
}

func loadBookIdentity(t *testing.T, store database.Store, bookID int64) (string, string, string) {
	t.Helper()

	sqlStore, ok := store.(*database.SqlStore)
	if !ok {
		t.Fatal("store is not SqlStore")
	}

	var fileHash, pathFingerprint, pathFingerprintNoExt sql.NullString
	err := sqlStore.DB().QueryRow(`
		SELECT file_hash, path_fingerprint, path_fingerprint_no_ext
		FROM books
		WHERE id = ?
	`, bookID).Scan(&fileHash, &pathFingerprint, &pathFingerprintNoExt)
	if err != nil {
		t.Fatalf("load book identity failed: %v", err)
	}

	return fileHash.String, pathFingerprint.String, pathFingerprintNoExt.String
}

func TestRebuildBookIdentitiesProcessesAllBatches(t *testing.T) {
	service, store, rootDir := newTestService(t, config.KOReaderMatchModeBinaryHash)

	_, book1 := seedServiceBook(t, store, rootDir, "LibraryA", "SeriesA", "Book1.cbz")
	_, book2 := seedServiceBook(t, store, rootDir, "LibraryB", "SeriesB", "Book2.cbz")
	_, book3 := seedServiceBook(t, store, rootDir, "LibraryC", "SeriesC", "Book3.cbz")

	for _, book := range []database.Book{book1, book2, book3} {
		if err := os.WriteFile(book.Path, []byte(book.Name), 0o644); err != nil {
			t.Fatalf("write book file failed: %v", err)
		}
	}

	updated, total, err := service.RebuildBookIdentities(context.Background(), 2, nil)
	if err != nil {
		t.Fatalf("rebuild identities failed: %v", err)
	}
	if total != 3 || updated != 3 {
		t.Fatalf("expected updated=3 total=3, got updated=%d total=%d", updated, total)
	}

	for _, book := range []database.Book{book1, book2, book3} {
		fileHash, _, _ := loadBookIdentity(t, store, book.ID)
		if fileHash == "" {
			t.Fatalf("expected file hash for book %d", book.ID)
		}
	}
}

func TestRebuildBookIdentitiesUsesPathIndexesInFilePathMode(t *testing.T) {
	service, store, rootDir := newTestService(t, config.KOReaderMatchModeFilePath)

	lib, book := seedServiceBook(t, store, rootDir, "Library", "Parent/Series", "Volume01.cbz")
	_ = lib

	updated, total, err := service.RebuildBookIdentities(context.Background(), 1, nil)
	if err != nil {
		t.Fatalf("rebuild identities failed: %v", err)
	}
	if total != 1 || updated != 1 {
		t.Fatalf("expected updated=1 total=1, got updated=%d total=%d", updated, total)
	}

	fileHash, pathFingerprint, pathFingerprintNoExt := loadBookIdentity(t, store, book.ID)
	if fileHash != "" {
		t.Fatalf("expected file hash to remain empty in file_path mode, got %q", fileHash)
	}
	if pathFingerprint == "" || pathFingerprintNoExt == "" {
		t.Fatalf("expected path fingerprints to be built, got exact=%q noext=%q", pathFingerprint, pathFingerprintNoExt)
	}
}

func TestAuthenticateUsesKOReaderSyncKey(t *testing.T) {
	service, store, _ := newTestService(t, config.KOReaderMatchModeBinaryHash)

	if _, err := store.UpsertKOReaderSettings(context.Background(), database.UpsertKOReaderSettingsParams{
		Username: "reader",
		SyncKey:  HashKey("secret-key"),
	}); err != nil {
		t.Fatalf("UpsertKOReaderSettings failed: %v", err)
	}

	if _, err := service.Authenticate(context.Background(), Credentials{
		Username: "reader",
		Key:      HashKey("secret-key"),
	}); err != nil {
		t.Fatalf("Authenticate failed: %v", err)
	}
}

func TestAuthenticateRejectsLegacyInvalidStoredKey(t *testing.T) {
	service, store, _ := newTestService(t, config.KOReaderMatchModeBinaryHash)

	if _, err := store.UpsertKOReaderSettings(context.Background(), database.UpsertKOReaderSettingsParams{
		Username: "reader",
		SyncKey:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}); err != nil {
		t.Fatalf("UpsertKOReaderSettings failed: %v", err)
	}

	if _, err := service.Authenticate(context.Background(), Credentials{
		Username: "reader",
		Key:      HashKey("secret-key"),
	}); err != ErrForbidden {
		t.Fatalf("expected ErrForbidden for invalid stored sync key, got %v", err)
	}
}
