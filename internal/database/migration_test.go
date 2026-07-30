// 业务说明：本文件是业务回归测试，属于 SQLite 数据访问层，负责把漫画库、系列、阅读进度、任务和元数据状态持久化为稳定数据模型。
// 它通过自动化断言保护对应业务场景在扫描、读取、展示或配置变更后仍保持兼容。
// 维护时应让用例名称、测试数据和断言结果直接反映真实用户流程，而不是只覆盖实现细节。

package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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

// TestMigrateBackfillsTagSeriesCount 验证 M23：版本升级时一次性回填 tags.series_count，
// 修正老库遗留的错误计数（触发器只维护增量、历史数据靠此处补算）。
func TestMigrateBackfillsTagSeriesCount(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tagcount.db")

	if err := Migrate(dbPath); err != nil {
		t.Fatalf("first migrate failed: %v", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db failed: %v", err)
	}
	// 关联 1 个标签到 2 个系列（触发器会把 series_count 维护为 2），随后人为改成 0 模拟老库遗留，
	// 并把 user_version 降回 1 模拟“已升级到 v1、尚未跑 tag 回填”的库。
	if _, err := db.Exec(`
		INSERT INTO libraries (name, path) VALUES ('L', '/tmp/l');
		INSERT INTO series (library_id, name, path) VALUES (1, 'S1', '/tmp/l/s1'), (1, 'S2', '/tmp/l/s2');
		INSERT INTO tags (name) VALUES ('action');
		INSERT INTO series_tags (series_id, tag_id) VALUES (1, 1), (2, 1);
		UPDATE tags SET series_count = 0 WHERE id = 1;
		PRAGMA user_version = 1;
	`); err != nil {
		_ = db.Close()
		t.Fatalf("seed failed: %v", err)
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
	var count int
	if err := db.QueryRow(`SELECT series_count FROM tags WHERE id = 1`).Scan(&count); err != nil {
		t.Fatalf("read series_count failed: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected backfilled tags.series_count=2, got %d", count)
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

// TestMigrateReadingBookmarksAddsUserScope 验证书签表从「全局共享」到「按用户隔离」的迁移。
//
// 这条迁移不能靠 ALTER TABLE ADD COLUMN 了事：旧表带 UNIQUE(book_id, page)，
// 而 SQLite 不支持修改已存在的约束，那个隐式唯一索引会一直生效，导致第二个用户
// 给同一本书的同一页加书签时报约束冲突。故必须重建整表。
func TestMigrateReadingBookmarksAddsUserScope(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")

	// 先造一个「旧版」库：书签表是加 user_id 之前的形态。
	legacy, err := sql.Open("sqlite", sqliteDSN(dbPath))
	if err != nil {
		t.Fatalf("open legacy db failed: %v", err)
	}
	if _, err := legacy.Exec(`
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
		INSERT INTO libraries (id, name, path) VALUES (1, 'Lib', '/lib');
		INSERT INTO series (id, library_id, name, path) VALUES (1, 1, 'S', '/lib/S');
		INSERT INTO books (id, series_id, library_id, name, path, size, file_modified_at)
			VALUES (1, 1, 1, 'b.cbz', '/lib/S/b.cbz', 1, CURRENT_TIMESTAMP);
		CREATE TABLE reading_bookmarks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			book_id INTEGER NOT NULL,
			page INTEGER NOT NULL,
			note TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(book_id, page),
			FOREIGN KEY(book_id) REFERENCES books(id) ON DELETE CASCADE
		);
		INSERT INTO reading_bookmarks (book_id, page, note) VALUES (1, 7, 'legacy note');
	`); err != nil {
		t.Fatalf("seed legacy schema failed: %v", err)
	}
	_ = legacy.Close()

	if err := Migrate(dbPath); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}

	db, err := sql.Open("sqlite", sqliteDSN(dbPath))
	if err != nil {
		t.Fatalf("reopen db failed: %v", err)
	}
	defer db.Close()

	// 存量书签保留下来，归到 user_id=0。
	var note string
	var userID int64
	if err := db.QueryRow(`SELECT user_id, note FROM reading_bookmarks WHERE book_id = 1 AND page = 7`).
		Scan(&userID, &note); err != nil {
		t.Fatalf("legacy bookmark lost after migration: %v", err)
	}
	if userID != 0 || note != "legacy note" {
		t.Fatalf("unexpected migrated bookmark: user_id=%d note=%q", userID, note)
	}

	// 关键断言：两个不同用户可以给同一本书的同一页各存一条。
	// 旧的 UNIQUE(book_id, page) 若残留，第二条插入会报约束冲突。
	if _, err := db.Exec(`INSERT INTO reading_bookmarks (user_id, book_id, page, note) VALUES (1, 1, 7, 'alice')`); err != nil {
		t.Fatalf("insert for user 1 failed: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO reading_bookmarks (user_id, book_id, page, note) VALUES (2, 1, 7, 'bob')`); err != nil {
		t.Fatalf("per-user unique constraint not applied, second user rejected: %v", err)
	}

	// 同一用户重复同一页仍应冲突（唯一性没被削弱）。
	if _, err := db.Exec(`INSERT INTO reading_bookmarks (user_id, book_id, page, note) VALUES (1, 1, 7, 'dup')`); err == nil {
		t.Fatal("expected duplicate (user_id, book_id, page) to be rejected")
	}

	// 迁移可重复执行（幂等）。
	if err := Migrate(dbPath); err != nil {
		t.Fatalf("second migrate failed: %v", err)
	}
}

// TestBackfillSeriesInitialsCoversAllBatches 锁住首字母回填的分批正确性。
//
// 回填改成了 keyset 分批（旧实现把整张 series 载入内存并在单个长事务里逐行 UPDATE，
// 大库上既占内存又长时间持有写锁）。分批实现最容易错的是边界：跨批时 lastID 没推进
// 会死循环，推进过头会漏行。这里刻意造出「行数正好跨多个批次」的数据来覆盖。
func TestBackfillSeriesInitialsCoversAllBatches(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "backfill.db")
	if err := Migrate(dbPath); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	lib, err := store.CreateLibrary(ctx, CreateLibraryParams{
		Name: "Lib", Path: "/lib", ScanMode: "none", ScanInterval: 60, ScanFormats: "cbz",
	})
	if err != nil {
		t.Fatalf("create library failed: %v", err)
	}

	// 造 250 个系列，并把 name_initial 全部写成一个错误值，模拟「需要回填」。
	const total = 250
	names := make([]string, 0, total)
	for i := range total {
		name := fmt.Sprintf("Series %03d", i)
		names = append(names, name)
		if _, err := store.CreateSeries(ctx, CreateSeriesParams{
			LibraryID:   lib.ID,
			Name:        name,
			Path:        fmt.Sprintf("/lib/%s", name),
			NameInitial: "#",
		}); err != nil {
			t.Fatalf("create series %d failed: %v", i, err)
		}
	}
	db := store.(*SqlStore).DB()
	if _, err := db.ExecContext(ctx, `UPDATE series SET name_initial = 'WRONG'`); err != nil {
		t.Fatalf("seed wrong initials failed: %v", err)
	}

	if err := backfillSeriesInitials(db); err != nil {
		t.Fatalf("backfill failed: %v", err)
	}

	// 一行都不能漏：任何仍是 WRONG 的行都说明分批把它跳过了。
	var stale int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM series WHERE name_initial = 'WRONG'`).Scan(&stale); err != nil {
		t.Fatalf("count stale failed: %v", err)
	}
	if stale != 0 {
		t.Fatalf("backfill skipped %d rows across batch boundaries", stale)
	}

	// 且回填出的值必须与逐行计算的结果一致。
	for _, name := range names {
		var got string
		if err := db.QueryRowContext(ctx, `SELECT name_initial FROM series WHERE name = ?`, name).Scan(&got); err != nil {
			t.Fatalf("read initial for %q failed: %v", name, err)
		}
		if want := SeriesInitial("", name); got != want {
			t.Fatalf("series %q: initial = %q, want %q", name, got, want)
		}
	}

	// 幂等：再跑一次不应改变任何东西，也不应死循环。
	if err := backfillSeriesInitials(db); err != nil {
		t.Fatalf("second backfill failed: %v", err)
	}
}

// legacyMetadataReviewSchema 是带 status 列的旧版 metadata_review_fields，
// 连同它依赖的最小骨架。用来验证存量库能被迁移真正改造，而不是只改 schema.sql 就以为完事
// ——execSchemaStatements 用的是 CREATE TABLE IF NOT EXISTS，老库根本不会重建。
const legacyMetadataReviewSchema = `
CREATE TABLE libraries (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL,
	path TEXT NOT NULL UNIQUE,
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
CREATE TABLE metadata_reviews (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	series_id INTEGER NOT NULL,
	provider TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'pending',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	FOREIGN KEY(series_id) REFERENCES series(id) ON DELETE CASCADE
);
CREATE TABLE metadata_review_fields (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	review_id INTEGER NOT NULL,
	field_name TEXT NOT NULL,
	current_value TEXT NOT NULL DEFAULT '',
	proposed_value TEXT NOT NULL DEFAULT '',
	confidence REAL NOT NULL DEFAULT 0,
	source TEXT NOT NULL DEFAULT '',
	source_url TEXT NOT NULL DEFAULT '',
	locked BOOLEAN NOT NULL DEFAULT FALSE,
	status TEXT NOT NULL DEFAULT 'pending',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(review_id, field_name),
	FOREIGN KEY(review_id) REFERENCES metadata_reviews(id) ON DELETE CASCADE
);
INSERT INTO libraries (id, name, path) VALUES (1, 'Lib', '/tmp/lib');
INSERT INTO series (id, library_id, name, path) VALUES (1, 1, 'S', '/tmp/lib/S');
INSERT INTO metadata_reviews (id, series_id, provider) VALUES (1, 1, 'bangumi');
INSERT INTO metadata_review_fields (review_id, field_name, proposed_value, status)
	VALUES (1, 'title', 'External Title', 'pending');
`

func columnNames(t *testing.T, db *sql.DB, table string) map[string]bool {
	t.Helper()
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatalf("PRAGMA table_info(%s): %v", table, err)
	}
	defer rows.Close()
	cols := map[string]bool{}
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
			t.Fatalf("scan: %v", err)
		}
		cols[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return cols
}

// TestMigrateDropsMetadataReviewFieldStatusColumn 守卫「字段级 status 死列被真正清掉」。
//
// 该列自建表起只有一个硬编码 'pending' 的写入点，全仓没有 UPDATE/DELETE，
// 而把 fields 送到前端的两条读路径都只查 pending review——就算写进别的值也永远读不出来。
// 它只会变成 series_metadata_provenance 之外的第二真相源。
func TestMigrateDropsMetadataReviewFieldStatusColumn(t *testing.T) {
	t.Run("存量库首次迁移即删除该列且不损坏数据", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "legacy.db")
		db, err := sql.Open("sqlite", dbPath)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		if _, err := db.Exec(legacyMetadataReviewSchema); err != nil {
			t.Fatalf("建旧表失败: %v", err)
		}
		if err := db.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}

		if err := Migrate(dbPath); err != nil {
			t.Fatalf("Migrate: %v", err)
		}

		db, err = sql.Open("sqlite", dbPath)
		if err != nil {
			t.Fatalf("reopen: %v", err)
		}
		defer db.Close()

		if columnNames(t, db, "metadata_review_fields")["status"] {
			t.Fatal("存量库里 status 列还在 —— 只改 schema.sql 不会重建老表，必须靠迁移里的 DROP")
		}
		var fieldName, proposed string
		if err := db.QueryRow(
			`SELECT field_name, proposed_value FROM metadata_review_fields WHERE review_id = 1`,
		).Scan(&fieldName, &proposed); err != nil {
			t.Fatalf("原有行读不回来了: %v", err)
		}
		if fieldName != "title" || proposed != "External Title" {
			t.Fatalf("ALTER 把行数据改坏了：field_name=%q proposed=%q", fieldName, proposed)
		}
	})

	t.Run("存量库重复迁移保持幂等", func(t *testing.T) {
		// 这才是裸 ALTER（不做存在性探测）会翻车的真实路径：
		// 第一次 DROP 成功，第二次必须是无操作，而不是 no such column。
		dbPath := filepath.Join(t.TempDir(), "legacy.db")
		db, err := sql.Open("sqlite", dbPath)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		if _, err := db.Exec(legacyMetadataReviewSchema); err != nil {
			t.Fatalf("建旧表失败: %v", err)
		}
		if err := db.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}

		for i := 1; i <= 2; i++ {
			if err := Migrate(dbPath); err != nil {
				t.Fatalf("第 %d 次 Migrate 失败: %v —— DROP COLUMN 本身不幂等，必须先探测", i, err)
			}
		}

		db, err = sql.Open("sqlite", dbPath)
		if err != nil {
			t.Fatalf("reopen: %v", err)
		}
		defer db.Close()
		if columnNames(t, db, "metadata_review_fields")["status"] {
			t.Fatal("重复迁移后 status 列又冒出来了")
		}
	})

	t.Run("全新库不含该列且写入路径可用", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "fresh.db")
		if err := Migrate(dbPath); err != nil {
			t.Fatalf("Migrate: %v", err)
		}
		db, err := sql.Open("sqlite", dbPath)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		defer db.Close()
		if columnNames(t, db, "metadata_review_fields")["status"] {
			t.Fatal("全新库仍带 status 列 —— schema.sql 没改干净")
		}
		// 不带 status 列的 INSERT 必须能跑通（对应 CreateMetadataReviewField 的新形态）。
		if _, err := db.Exec(`INSERT INTO libraries (id, name, path) VALUES (1, 'L', '/tmp/l')`); err != nil {
			t.Fatalf("seed library: %v", err)
		}
		if _, err := db.Exec(`INSERT INTO series (id, library_id, name, path) VALUES (1, 1, 'S', '/tmp/l/S')`); err != nil {
			t.Fatalf("seed series: %v", err)
		}
		if _, err := db.Exec(`INSERT INTO metadata_reviews (id, series_id, provider) VALUES (1, 1, 'bangumi')`); err != nil {
			t.Fatalf("seed review: %v", err)
		}
		if _, err := db.Exec(`INSERT INTO metadata_review_fields (review_id, field_name, proposed_value)
			VALUES (1, 'title', 'X')`); err != nil {
			t.Fatalf("不带 status 的插入失败: %v", err)
		}
	})
}
