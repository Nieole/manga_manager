// 业务说明：本文件是业务回归测试，属于 SQLite 数据访问层，负责把漫画库、系列、阅读进度、任务和元数据状态持久化为稳定数据模型。
// 它通过自动化断言保护对应业务场景在扫描、读取、展示或配置变更后仍保持兼容。
// 维护时应让用例名称、测试数据和断言结果直接反映真实用户流程，而不是只覆盖实现细节。

package database

import (
	"database/sql"
	"errors"
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

// TestMigrateRebuildsLegacyFTSSchema 覆盖“旧版 FTS 表结构升级”这条真实路径：
// 老库的 series_search_fts 含冗余 path 列、book_search_fts 含 series_name 列。
// 修复前 migrateFTSTables 在建表之后执行，DROP 掉旧表却无人重建，
// rebuildSeriesSearchIndex 的 DELETE 会因“no such table”让 Migrate 失败、服务器首启崩溃。
func TestMigrateRebuildsLegacyFTSSchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy_fts.db")

	// 1. 先正常迁移一次，得到完整的当前 schema（含新版 FTS 表与全部基础表）。
	if err := Migrate(dbPath); err != nil {
		t.Fatalf("initial migrate failed: %v", err)
	}

	// 2. 把 FTS 表回退成旧结构（带冗余列），并塞入一条待重建的系列数据。
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db failed: %v", err)
	}
	_, err = db.Exec(`
		DROP TRIGGER IF EXISTS trg_series_search_fts_ai;
		DROP TRIGGER IF EXISTS trg_series_search_fts_ad;
		DROP TRIGGER IF EXISTS trg_series_search_fts_au;
		DROP TRIGGER IF EXISTS trg_book_search_fts_ai;
		DROP TRIGGER IF EXISTS trg_book_search_fts_ad;
		DROP TRIGGER IF EXISTS trg_book_search_fts_au;
		DROP TABLE IF EXISTS series_search_fts;
		DROP TABLE IF EXISTS book_search_fts;
		CREATE VIRTUAL TABLE series_search_fts USING fts5(library_id UNINDEXED, name, title, path, tokenize = 'trigram');
		CREATE VIRTUAL TABLE book_search_fts USING fts5(series_id UNINDEXED, library_id UNINDEXED, name, title, series_name, tokenize = 'trigram');
		INSERT INTO libraries (name, path) VALUES ('L', '/tmp/l');
		INSERT INTO series (library_id, name, path) VALUES (1, 'Berserk', '/tmp/l/berserk');
	`)
	if err != nil {
		_ = db.Close()
		t.Fatalf("downgrade FTS schema failed: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close db failed: %v", err)
	}

	// 3. 二次迁移：修复前此处返回 "no such table: series_search_fts"。
	if err := Migrate(dbPath); err != nil {
		t.Fatalf("migrate over legacy FTS schema failed: %v", err)
	}

	// 4. 断言 FTS 已重建为新结构（无冗余 path 列）且搜索可用。
	db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("reopen db failed: %v", err)
	}
	defer db.Close()

	if testColumnExists(t, db, "series_search_fts", "path") {
		t.Fatal("expected legacy series_search_fts.path column to be dropped after migrate")
	}

	var rowid int64
	err = db.QueryRow(`SELECT rowid FROM series_search_fts WHERE series_search_fts MATCH ?`, "Berserk").Scan(&rowid)
	if err != nil {
		t.Fatalf("search over rebuilt FTS failed: %v", err)
	}
	if rowid != 1 {
		t.Fatalf("expected rebuilt FTS to return series rowid 1, got %d", rowid)
	}
}

// TestMigrateSetsSchemaVersionAndSkipsRebackfill 验证 H3：首次迁移写入 user_version，
// 二次迁移在版本未升级时跳过昂贵的全量回填（用 series_stats 哨兵值是否被覆写来判定）。
func TestMigrateSetsSchemaVersionAndSkipsRebackfill(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "versioned.db")

	if err := Migrate(dbPath); err != nil {
		t.Fatalf("first migrate failed: %v", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db failed: %v", err)
	}
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		_ = db.Close()
		t.Fatalf("read user_version failed: %v", err)
	}
	if version != currentSchemaVersion {
		_ = db.Close()
		t.Fatalf("expected user_version=%d after migrate, got %d", currentSchemaVersion, version)
	}

	// 写入一条 series 及带哨兵缓存的 series_stats。若二次迁移错误地重跑全量回填，
	// refreshSeriesStats 的 ON CONFLICT DO UPDATE 会把 tag_names_cache 覆写为空。
	if _, err := db.Exec(`
		INSERT INTO libraries (name, path) VALUES ('L', '/tmp/l');
		INSERT INTO series (library_id, name, path) VALUES (1, 'S', '/tmp/l/s');
		INSERT INTO series_stats (series_id, tag_names_cache) VALUES (1, 'SENTINEL');
	`); err != nil {
		_ = db.Close()
		t.Fatalf("seed sentinel failed: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close db failed: %v", err)
	}

	if err := Migrate(dbPath); err != nil {
		t.Fatalf("second migrate failed: %v", err)
	}

	db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("reopen db failed: %v", err)
	}
	defer db.Close()
	var cache string
	if err := db.QueryRow(`SELECT tag_names_cache FROM series_stats WHERE series_id = 1`).Scan(&cache); err != nil {
		t.Fatalf("read series_stats failed: %v", err)
	}
	if cache != "SENTINEL" {
		t.Fatalf("second migrate should skip full backfill and preserve series_stats, got %q", cache)
	}
}

func testTableExists(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	var name string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
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
	if errors.Is(err, sql.ErrNoRows) {
		return false
	}
	if err != nil {
		t.Fatalf("read index %s failed: %v", index, err)
	}
	return true
}
