package database

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestMigrateAddsIdentityColumnsBeforeDependentIndexes(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open legacy db failed: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE libraries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			path TEXT NOT NULL UNIQUE,
			scan_interval INTEGER NOT NULL DEFAULT 60,
			scan_formats TEXT NOT NULL DEFAULT 'zip,cbz,rar,cbr',
			auto_scan BOOLEAN NOT NULL DEFAULT FALSE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE series (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			library_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			path TEXT NOT NULL UNIQUE,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(library_id) REFERENCES libraries(id) ON DELETE CASCADE
		);
		CREATE TABLE books (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			series_id INTEGER NOT NULL,
			library_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			path TEXT NOT NULL UNIQUE,
			size INTEGER NOT NULL,
			file_modified_at DATETIME NOT NULL,
			volume TEXT NOT NULL DEFAULT '',
			page_count INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(series_id) REFERENCES series(id) ON DELETE CASCADE,
			FOREIGN KEY(library_id) REFERENCES libraries(id) ON DELETE CASCADE
		);
		CREATE TABLE koreader_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			username TEXT NOT NULL DEFAULT '',
			password_hash TEXT NOT NULL DEFAULT '',
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		_ = db.Close()
		t.Fatalf("create legacy schema failed: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db failed: %v", err)
	}

	if err := Migrate(dbPath); err != nil {
		t.Fatalf("migrate legacy db failed: %v", err)
	}

	db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("reopen migrated db failed: %v", err)
	}
	defer db.Close()

	for _, column := range []string{"file_hash", "quick_hash", "path_fingerprint", "path_fingerprint_no_ext", "filename_fingerprint"} {
		if !testColumnExists(t, db, "books", column) {
			t.Fatalf("expected migrated books.%s column to exist", column)
		}
	}

	for _, index := range []string{
		"idx_books_quick_hash",
		"idx_books_path_fingerprint",
		"idx_books_path_fingerprint_no_ext",
		"idx_series_library_initial_name",
		"idx_series_library_status_books",
		"idx_books_read_progress_series",
		"idx_books_cover_pick",
		"idx_series_stats_read_pages",
	} {
		if !testIndexExists(t, db, index) {
			t.Fatalf("expected migrated index %s to exist", index)
		}
	}

	if !testTableExists(t, db, "series_stats") {
		t.Fatal("expected migrated series_stats table to exist")
	}
}

func testTableExists(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	var name string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
	if err == sql.ErrNoRows {
		return false
	}
	if err != nil {
		t.Fatalf("read table %s failed: %v", table, err)
	}
	return true
}

func testColumnExists(t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatalf("read table info failed: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			name       string
			colType    string
			notNull    int
			defaultVal sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultVal, &pk); err != nil {
			t.Fatalf("scan table info failed: %v", err)
		}
		if name == column {
			return true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate table info failed: %v", err)
	}
	return false
}

func testIndexExists(t *testing.T, db *sql.DB, index string) bool {
	t.Helper()
	var name string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'index' AND name = ?`, index).Scan(&name)
	if err == sql.ErrNoRows {
		return false
	}
	if err != nil {
		t.Fatalf("read index %s failed: %v", index, err)
	}
	return true
}
