// 本文件由 store.go 拆分而来，属于 SQLite 数据访问层的「schema 迁移与回填」子域。
// 它负责建表、幂等 DDL 重放、按 user_version 门控的一次性回填、FTS 结构升级与历史数据迁移。
// 维护时应保证每条语句幂等、老库升级路径不崩，并把昂贵的全量回填挡在版本门控之后。

package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Migrate 供启动时执行迁移
// currentSchemaVersion 是当前 schema 的迁移版本号，存入 SQLite 的 PRAGMA user_version。
// 昂贵的一次性全量回填（series_stats、name_initial、tag_series_count、provenance、FTS 索引重建）
// 仅在库版本低于此值时执行一次，之后每次启动跳过——这些操作成本随库规模线性增长，运行期已由触发器与
// RefreshSeriesStats 增量维护。新增需要全量回填的 schema 变更时，把该值 +1。
// v2：新增 tags.series_count 的一次性回填，确保已升级到 v1 的库也会补算。
// v3：新增短关键字的 2-gram 辅助索引（series_gram_fts / book_gram_fts），存量库需要回填一次。
//
//	注意 user_version 是在回填**成功之后**才写的，所以回填中途被杀不会留下「已完成」的假象，
//	下次启动会整份重来——这正是我们要的语义，别改成「表非空就算已回填」那种探针。
const currentSchemaVersion = 3

func Migrate(dbPath string) error {
	db, err := sql.Open("sqlite", sqliteDSN(dbPath))
	if err != nil {
		return err
	}
	defer db.Close()

	var userVersion int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&userVersion); err != nil {
		return err
	}

	// 必须在建表/建触发器之前迁移旧版 FTS 结构：migrateFTSTables 会 DROP 掉带冗余列的旧
	// series_search_fts / book_search_fts 及其触发器，随后的 execSchemaStatements(false)
	// 以新 schema 重建虚拟表、CREATE TRIGGER IF NOT EXISTS 重建触发器。若放在建表之后，
	// DROP 完却没有任何语句重建虚拟表，rebuildSeriesSearchIndex 的 DELETE 会因表不存在而
	// 让整个 Migrate 失败（老库升级首启崩溃）。全新库上该 SELECT 探测直接报错、needRebuild 保持 false，为无操作。
	ftsRebuilt, err := migrateFTSTables(db)
	if err != nil {
		return err
	}

	if err = execSchemaStatements(db, false); err != nil {
		return err
	}

	// 书签表加 user_id 必须重建整表：SQLite 无法修改已存在的 UNIQUE 约束，
	// 而旧表上的 UNIQUE(book_id, page) 会让两个用户无法各自给同一页加书签。
	if err := migrateReadingBookmarksUserScope(db); err != nil {
		return err
	}

	for _, column := range []struct {
		table      string
		name       string
		definition string
	}{
		{table: "libraries", name: "koreader_sync_enabled", definition: "BOOLEAN NOT NULL DEFAULT TRUE"},
		{table: "libraries", name: "scan_mode", definition: "TEXT NOT NULL DEFAULT 'none'"},
		{table: "books", name: "file_hash", definition: "TEXT"},
		{table: "books", name: "quick_hash", definition: "TEXT"},
		{table: "books", name: "path_fingerprint", definition: "TEXT"},
		{table: "books", name: "path_fingerprint_no_ext", definition: "TEXT"},
		{table: "books", name: "filename_fingerprint", definition: "TEXT"},
		{table: "books", name: "title", definition: "TEXT"},
		{table: "books", name: "summary", definition: "TEXT"},
		{table: "books", name: "number", definition: "TEXT"},
		{table: "books", name: "sort_number", definition: "REAL"},
		{table: "books", name: "cover_path", definition: "TEXT"},
		{table: "books", name: "last_read_page", definition: "INTEGER"},
		{table: "books", name: "last_read_at", definition: "DATETIME"},
		{table: "series", name: "title", definition: "TEXT"},
		{table: "series", name: "summary", definition: "TEXT"},
		{table: "series", name: "publisher", definition: "TEXT"},
		{table: "series", name: "status", definition: "TEXT"},
		{table: "series", name: "rating", definition: "REAL"},
		{table: "series", name: "language", definition: "TEXT"},
		{table: "series", name: "locked_fields", definition: "TEXT DEFAULT ''"},
		{table: "series", name: "name_initial", definition: "TEXT NOT NULL DEFAULT '#'"},
		{table: "series", name: "is_favorite", definition: "BOOLEAN NOT NULL DEFAULT FALSE"},
		{table: "series", name: "volume_count", definition: "INTEGER NOT NULL DEFAULT 0"},
		{table: "series", name: "book_count", definition: "INTEGER NOT NULL DEFAULT 0"},
		{table: "series", name: "total_pages", definition: "INTEGER NOT NULL DEFAULT 0"},
		{table: "collections", name: "source_type", definition: "TEXT NOT NULL DEFAULT 'manual'"},
		{table: "collections", name: "source_review_id", definition: "INTEGER"},
		{table: "smart_filters", name: "read_state", definition: "TEXT"},
		{table: "smart_filters", name: "min_rating", definition: "REAL"},
		{table: "smart_filters", name: "max_rating", definition: "REAL"},
		{table: "smart_filters", name: "min_progress", definition: "REAL"},
		{table: "smart_filters", name: "max_progress", definition: "REAL"},
		{table: "smart_filters", name: "added_within_days", definition: "INTEGER"},
		{table: "tags", name: "series_count", definition: "INTEGER NOT NULL DEFAULT 0"},
		{table: "koreader_accounts", name: "user_id", definition: "INTEGER NOT NULL DEFAULT 0"},
		// source_key 让系统生成的合集有稳定自然键，从而按键 upsert 而不是整体重建。
		// 存量行留空串：下一次重建会按新键建出正确的行，旧的无键行由
		// DeleteStaleFranchiseCollections 一并清掉（它把 source_key='' 也算作过期）。
		{table: "collections", name: "source_key", definition: "TEXT NOT NULL DEFAULT ''"},
	} {
		if err := ensureColumn(db, column.table, column.name, column.definition); err != nil {
			return err
		}
	}

	// metadata_review_fields.status 是死数据：唯一写入点硬编码 'pending'，全仓没有任何
	// UPDATE/DELETE，且两条把 fields 送到前端的读路径都只查 pending review——就算写进
	// applied/skipped 也永远不可能被读到。字段级的应用结果本就该由 series_metadata_provenance
	// 承担，留着这一列只会变成第二真相源。schema.sql 用的是 CREATE TABLE IF NOT EXISTS，
	// 存量库不会因为 schema 改了就重建，只有这条 ALTER 才能真正落地。
	if err := dropColumnIfExists(db, "metadata_review_fields", "status"); err != nil {
		return err
	}

	if err := execMigrationStatements(db, []string{
		// 移除 series 上的嵌套前缀冗余索引：(library_id, updated_at) 与 (library_id, updated_at, name)
		// 都是 (library_id, updated_at, name, id) 的严格前缀，created_* 同理。按 B-tree 前缀性质，保留的
		// 超集覆盖索引可服务前者的全部查询（无计划回归，cmd/queryplan --strict 守护），而每次系列写入
		// 少维护这 4 个索引，降低大库扫描的写放大。DROP IF EXISTS 幂等：既有库首启即清理，新库为无操作。
		`DROP INDEX IF EXISTS idx_series_library_updated`,
		`DROP INDEX IF EXISTS idx_series_library_created`,
		`DROP INDEX IF EXISTS idx_series_library_updated_name`,
		`DROP INDEX IF EXISTS idx_series_library_created_name`,
		`CREATE INDEX IF NOT EXISTS idx_books_file_hash ON books(file_hash)`,
		`CREATE INDEX IF NOT EXISTS idx_books_quick_hash ON books(quick_hash)`,
		`CREATE INDEX IF NOT EXISTS idx_books_path_fingerprint ON books(path_fingerprint)`,
		`CREATE INDEX IF NOT EXISTS idx_books_path_fingerprint_no_ext ON books(path_fingerprint_no_ext)`,
		`CREATE INDEX IF NOT EXISTS idx_books_library_size ON books(library_id, size)`,
		`CREATE INDEX IF NOT EXISTS idx_reading_bookmarks_book_id ON reading_bookmarks(book_id)`,
		`CREATE INDEX IF NOT EXISTS idx_series_name_initial ON series(name_initial)`,
		`CREATE INDEX IF NOT EXISTS idx_series_library_initial ON series(library_id, name_initial)`,
		`CREATE INDEX IF NOT EXISTS idx_series_library_status ON series(library_id, status)`,
		`CREATE INDEX IF NOT EXISTS idx_series_library_name ON series(library_id, name)`,
		`CREATE INDEX IF NOT EXISTS idx_series_library_initial_name ON series(library_id, name_initial, name)`,
		`CREATE INDEX IF NOT EXISTS idx_series_library_status_name ON series(library_id, status, name)`,
		`CREATE INDEX IF NOT EXISTS idx_series_library_updated_name_id ON series(library_id, updated_at, name, id)`,
		`CREATE INDEX IF NOT EXISTS idx_series_library_created_name_id ON series(library_id, created_at, name, id)`,
		`CREATE INDEX IF NOT EXISTS idx_series_library_name_id ON series(library_id, name, id)`,
		`CREATE INDEX IF NOT EXISTS idx_series_library_rating ON series(library_id, rating, name)`,
		`CREATE INDEX IF NOT EXISTS idx_series_library_books ON series(library_id, book_count, name)`,
		`CREATE INDEX IF NOT EXISTS idx_series_library_volumes ON series(library_id, volume_count, name)`,
		`CREATE INDEX IF NOT EXISTS idx_series_library_pages ON series(library_id, total_pages, name)`,
		`CREATE INDEX IF NOT EXISTS idx_series_library_favorite ON series(library_id, is_favorite, name)`,
		`CREATE INDEX IF NOT EXISTS idx_series_library_favorite_name_id ON series(library_id, is_favorite, name, id)`,
		`CREATE INDEX IF NOT EXISTS idx_series_library_status_books ON series(library_id, status, book_count, name)`,
		`CREATE INDEX IF NOT EXISTS idx_series_library_updated_desc ON series(library_id, updated_at DESC, name, id)`,
		`CREATE INDEX IF NOT EXISTS idx_series_library_created_desc ON series(library_id, created_at DESC, name, id)`,
		`CREATE INDEX IF NOT EXISTS idx_series_library_rating_desc ON series(library_id, rating DESC, name, id)`,
		`CREATE INDEX IF NOT EXISTS idx_series_library_books_desc ON series(library_id, book_count DESC, name, id)`,
		`CREATE INDEX IF NOT EXISTS idx_series_library_volumes_desc ON series(library_id, volume_count DESC, name, id)`,
		`CREATE INDEX IF NOT EXISTS idx_series_library_pages_desc ON series(library_id, total_pages DESC, name, id)`,
		`CREATE INDEX IF NOT EXISTS idx_series_library_favorite_desc ON series(library_id, is_favorite DESC, name, id)`,
		`CREATE INDEX IF NOT EXISTS idx_books_series_sort ON books(series_id, volume, sort_number, name)`,
		`CREATE INDEX IF NOT EXISTS idx_books_series_read ON books(series_id, last_read_page)`,
		`CREATE INDEX IF NOT EXISTS idx_books_read_progress_series ON books(last_read_page, series_id) WHERE last_read_page > 0`,
		`CREATE INDEX IF NOT EXISTS idx_books_cover_pick ON books(series_id, sort_number, name) WHERE cover_path IS NOT NULL AND cover_path != ''`,
		`CREATE INDEX IF NOT EXISTS idx_books_library_modified ON books(library_id, file_modified_at)`,
		`CREATE INDEX IF NOT EXISTS idx_books_library_last_read ON books(library_id, last_read_at) WHERE last_read_at IS NOT NULL`,
		// 健康报告的两个高频谓词都很稀疏（健康的库里绝大多数书不满足），用部分索引可以
		// 把整表扫描换成只扫命中行。没有它们时 /api/health/report 每次请求都要对 books
		// 做约 10 次全表扫描，几十万行的库上单次请求即达秒级。
		`CREATE INDEX IF NOT EXISTS idx_books_health_empty_pages ON books(library_id) WHERE page_count <= 0`,
		`CREATE INDEX IF NOT EXISTS idx_books_health_missing_cover ON books(library_id) WHERE cover_path IS NULL OR cover_path = ''`,
		// 账户列表与设备诊断按 username 过滤 + 按时间倒序取最近事件；无索引时每个账号
		// 都要扫一遍 koreader_sync_events（该表带保留上限，仍可达万行量级）。
		`CREATE INDEX IF NOT EXISTS idx_koreader_sync_events_user_created ON koreader_sync_events(username, created_at DESC, id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_koreader_sync_events_user_doc ON koreader_sync_events(username, document)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_updated_at ON tasks(updated_at)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_scope ON tasks(scope, scope_id)`,
		`CREATE INDEX IF NOT EXISTS idx_smart_filters_library_id ON smart_filters(library_id, updated_at)`,
		`CREATE INDEX IF NOT EXISTS idx_series_relations_target ON series_relations(target_series_id)`,
		`CREATE TRIGGER IF NOT EXISTS trg_series_tags_ai AFTER INSERT ON series_tags BEGIN UPDATE tags SET series_count = series_count + 1 WHERE id = NEW.tag_id; END`,
		`CREATE TRIGGER IF NOT EXISTS trg_series_tags_ad AFTER DELETE ON series_tags BEGIN UPDATE tags SET series_count = series_count - 1 WHERE id = OLD.tag_id; END`,
		`CREATE TRIGGER IF NOT EXISTS trg_series_search_fts_ai AFTER INSERT ON series BEGIN
			INSERT INTO series_search_fts(rowid, library_id, name, title)
			VALUES (NEW.id, NEW.library_id, NEW.name, COALESCE(NEW.title, ''));
		END`,
		`CREATE TRIGGER IF NOT EXISTS trg_series_search_fts_ad AFTER DELETE ON series BEGIN
			DELETE FROM series_search_fts WHERE rowid = OLD.id;
		END`,
		`CREATE TRIGGER IF NOT EXISTS trg_series_search_fts_au AFTER UPDATE OF library_id, name, title ON series BEGIN
			DELETE FROM series_search_fts WHERE rowid = OLD.id;
			INSERT INTO series_search_fts(rowid, library_id, name, title)
			VALUES (NEW.id, NEW.library_id, NEW.name, COALESCE(NEW.title, ''));
		END`,
		`CREATE TRIGGER IF NOT EXISTS trg_book_search_fts_ai AFTER INSERT ON books BEGIN
			INSERT INTO book_search_fts(rowid, series_id, library_id, name, title)
			VALUES (NEW.id, NEW.series_id, NEW.library_id, NEW.name, COALESCE(NEW.title, ''));
		END`,
		`CREATE TRIGGER IF NOT EXISTS trg_book_search_fts_ad AFTER DELETE ON books BEGIN
			DELETE FROM book_search_fts WHERE rowid = OLD.id;
		END`,
		`CREATE TRIGGER IF NOT EXISTS trg_book_search_fts_au AFTER UPDATE OF series_id, library_id, name, title ON books BEGIN
			DELETE FROM book_search_fts WHERE rowid = OLD.id;
			INSERT INTO book_search_fts(rowid, series_id, library_id, name, title)
			VALUES (NEW.id, NEW.series_id, NEW.library_id, NEW.name, COALESCE(NEW.title, ''));
		END`,
		// ---- 短关键字的 2-gram 辅助索引 ----
		//
		// 触发列表与上面 6 条 trg_*_search_fts_* **完全一致**：两张索引必须同生同灭，
		// 否则短关键字与长关键字会看到两份不同的数据。
		//
		// gram 文本用 SQL 的 lower() 而不是 Go 侧的 strings.ToLower：SQLite 无 ICU 时 lower()
		// 只折叠 ASCII A-Z，而 Go 按 Unicode 折叠——索引侧与查询侧必须用同一套折叠规则，
		// 用 Go 拼 MATCH 会和索引对不上。这也与既有 instr(lower(a), lower(b)) 的语义逐字对齐。
		//
		// length()/substr() 在 TEXT 上按字符计数；i 走到 length(t) 让最后一个单字也留一个 gram，
		// 1 字关键字才能靠前缀 MATCH 命中。
		//
		// 写放大实测约 17.6µs/行。扫描侧有 mtime+size 增量拦截，未变更的文件根本走不到这里；
		// 新增/变更的书每本都要开归档读数页（毫秒级起步），这点开销是噪声。
		`CREATE TRIGGER IF NOT EXISTS trg_series_gram_fts_ai AFTER INSERT ON series BEGIN
			INSERT INTO series_gram_fts(rowid, library_id, name, title)
			VALUES (NEW.id, NEW.library_id, (SELECT group_concat(hex(substr(x.t, x.i, 2)), ' ') FROM (WITH RECURSIVE g(t,i) AS (SELECT lower(NEW.name), 1 UNION ALL SELECT t, i+1 FROM g WHERE i < length(t)) SELECT t,i FROM g) x), (SELECT group_concat(hex(substr(x.t, x.i, 2)), ' ') FROM (WITH RECURSIVE g(t,i) AS (SELECT lower(COALESCE(NEW.title, '')), 1 UNION ALL SELECT t, i+1 FROM g WHERE i < length(t)) SELECT t,i FROM g) x));
		END`,
		`CREATE TRIGGER IF NOT EXISTS trg_series_gram_fts_ad AFTER DELETE ON series BEGIN
			DELETE FROM series_gram_fts WHERE rowid = OLD.id;
		END`,
		`CREATE TRIGGER IF NOT EXISTS trg_series_gram_fts_au AFTER UPDATE OF library_id, name, title ON series BEGIN
			DELETE FROM series_gram_fts WHERE rowid = OLD.id;
			INSERT INTO series_gram_fts(rowid, library_id, name, title)
			VALUES (NEW.id, NEW.library_id, (SELECT group_concat(hex(substr(x.t, x.i, 2)), ' ') FROM (WITH RECURSIVE g(t,i) AS (SELECT lower(NEW.name), 1 UNION ALL SELECT t, i+1 FROM g WHERE i < length(t)) SELECT t,i FROM g) x), (SELECT group_concat(hex(substr(x.t, x.i, 2)), ' ') FROM (WITH RECURSIVE g(t,i) AS (SELECT lower(COALESCE(NEW.title, '')), 1 UNION ALL SELECT t, i+1 FROM g WHERE i < length(t)) SELECT t,i FROM g) x));
		END`,
		`CREATE TRIGGER IF NOT EXISTS trg_book_gram_fts_ai AFTER INSERT ON books BEGIN
			INSERT INTO book_gram_fts(rowid, series_id, library_id, name, title)
			VALUES (NEW.id, NEW.series_id, NEW.library_id, (SELECT group_concat(hex(substr(x.t, x.i, 2)), ' ') FROM (WITH RECURSIVE g(t,i) AS (SELECT lower(NEW.name), 1 UNION ALL SELECT t, i+1 FROM g WHERE i < length(t)) SELECT t,i FROM g) x), (SELECT group_concat(hex(substr(x.t, x.i, 2)), ' ') FROM (WITH RECURSIVE g(t,i) AS (SELECT lower(COALESCE(NEW.title, '')), 1 UNION ALL SELECT t, i+1 FROM g WHERE i < length(t)) SELECT t,i FROM g) x));
		END`,
		`CREATE TRIGGER IF NOT EXISTS trg_book_gram_fts_ad AFTER DELETE ON books BEGIN
			DELETE FROM book_gram_fts WHERE rowid = OLD.id;
		END`,
		`CREATE TRIGGER IF NOT EXISTS trg_book_gram_fts_au AFTER UPDATE OF series_id, library_id, name, title ON books BEGIN
			DELETE FROM book_gram_fts WHERE rowid = OLD.id;
			INSERT INTO book_gram_fts(rowid, series_id, library_id, name, title)
			VALUES (NEW.id, NEW.series_id, NEW.library_id, (SELECT group_concat(hex(substr(x.t, x.i, 2)), ' ') FROM (WITH RECURSIVE g(t,i) AS (SELECT lower(NEW.name), 1 UNION ALL SELECT t, i+1 FROM g WHERE i < length(t)) SELECT t,i FROM g) x), (SELECT group_concat(hex(substr(x.t, x.i, 2)), ' ') FROM (WITH RECURSIVE g(t,i) AS (SELECT lower(COALESCE(NEW.title, '')), 1 UNION ALL SELECT t, i+1 FROM g WHERE i < length(t)) SELECT t,i FROM g) x));
		END`,
	}); err != nil {
		return err
	}

	if err = execSchemaStatements(db, true); err != nil {
		return err
	}

	// 一次性全量回填只在 schema 版本升级时执行，避免每次启动都做一遍随库规模线性增长的全量重算。
	needFullBackfill := userVersion < currentSchemaVersion
	if needFullBackfill {
		if err := backfillSeriesInitials(db); err != nil {
			return err
		}
		if err := backfillSeriesStats(db); err != nil {
			return err
		}
		// tags.series_count 触发器只维护增量，历史数据需在升级时一次性回填，
		// 否则老库的标签 facet 计数/排序会一直不准。
		if err := backfillTagSeriesCount(db); err != nil {
			return err
		}
		if err := backfillSeriesMetadataProvenance(db); err != nil {
			return err
		}
	}

	// FTS 索引重建：版本升级、或 migrateFTSTables 刚 DROP 重建了空表时，都必须回填一次。
	if needFullBackfill || ftsRebuilt {
		if err := rebuildSeriesSearchIndex(db); err != nil {
			return err
		}
		if err := rebuildBookSearchIndex(db); err != nil {
			return err
		}
	}

	if err := migrateLegacyKOReaderAccounts(db); err != nil {
		return err
	}

	// 迁移旧的 auto_scan 字段到新的 scan_mode
	// 尝试执行，忽略错误因为有些数据库可能原本就没有 auto_scan
	_, _ = db.Exec(`UPDATE libraries SET scan_mode = 'interval' WHERE auto_scan = 1 AND scan_mode = 'none'`)

	// 记录本次已迁移到的 schema 版本，使下次启动跳过昂贵的全量回填。
	if needFullBackfill {
		if _, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, currentSchemaVersion)); err != nil {
			return err
		}
	}

	// 给查询规划器提供选择性统计（sqlite_stat1）。series 上有约 20 个 library_id 前缀重叠的复合索引，
	// 有统计信息时规划器更能在等价候选中挑对索引。analysis_limit=400 限制每个索引的采样行数，
	// 即便百万级表也能秒级完成；PRAGMA optimize 只会分析确有变化的表，且失败无害（忽略错误）。
	_, _ = db.Exec(`PRAGMA analysis_limit=400`)
	_, _ = db.Exec(`PRAGMA optimize`)

	return nil
}

func sqliteDSN(dbPath string) string {
	// _txlock=immediate 让每个 BeginTx 发出 BEGIN IMMEDIATE，开始即取写锁。
	// 连接池有多条可写连接（MaxOpenConns>=8），若用默认 deferred 事务，两条连接各自
	// BEGIN→读→升级为写会撞上 SQLITE_BUSY_SNAPSHOT 死锁，busy_timeout 对这种“快照升级”
	// 冲突无法重试，会立刻抛 database is locked。改为 immediate 后 busy_timeout 能干净地
	// 串行化写者。store 内 BeginTx 仅用于写事务（ExecTx 等），只读查询走 sqlc 直查 +
	// WAL 并发，不受影响。
	return dbPath + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)" +
		"&_pragma=mmap_size=268435456&_pragma=cache_size=-128000&_pragma=busy_timeout=15000&_pragma=temp_store=2" +
		"&_txlock=immediate"
}

func execSchemaStatements(db *sql.DB, indexStatements bool) error {
	statements := make([]string, 0)
	for _, raw := range strings.Split(schemaSQL, ";") {
		stmt := strings.TrimSpace(raw)
		if stmt == "" {
			continue
		}
		if isSchemaIndexStatement(stmt) != indexStatements {
			continue
		}
		statements = append(statements, stmt)
	}
	return execMigrationStatements(db, statements)
}

func execMigrationStatements(db *sql.DB, statements []string) error {
	if len(statements) == 0 {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	for _, stmt := range statements {
		if _, err := tx.Exec(stmt); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func isSchemaIndexStatement(stmt string) bool {
	normalized := normalizeSchemaStatement(stmt)
	return strings.HasPrefix(normalized, "CREATE INDEX") || strings.HasPrefix(normalized, "CREATE UNIQUE INDEX")
}

func normalizeSchemaStatement(stmt string) string {
	lines := strings.Split(strings.TrimSpace(stmt), "\n")
	for len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[0]), "--") {
		lines = lines[1:]
	}
	return strings.ToUpper(strings.TrimSpace(strings.Join(lines, "\n")))
}

func refreshSeriesStatsStatement(whereClause string) string {
	if strings.TrimSpace(whereClause) == "" {
		whereClause = "1 = 1"
	}
	return `
		INSERT INTO series_stats (
			series_id,
			cover_path,
			cover_book_id,
			read_pages,
			read_book_count,
			completed_book_count,
			last_read_at,
			last_read_book_id,
			tag_names_cache,
			author_names_cache,
			updated_at
		)
		SELECT
			s.id,
			COALESCE((
				SELECT b.cover_path
				FROM books b
				WHERE b.series_id = s.id AND b.cover_path IS NOT NULL AND b.cover_path != ''
				ORDER BY b.sort_number, b.name
				LIMIT 1
			), '') AS cover_path,
			COALESCE((
				SELECT b.id
				FROM books b
				WHERE b.series_id = s.id AND b.cover_path IS NOT NULL AND b.cover_path != ''
				ORDER BY b.sort_number, b.name
				LIMIT 1
			), 0) AS cover_book_id,
			COALESCE((
				SELECT SUM(
					CASE
						WHEN b.last_read_page IS NULL OR b.last_read_page <= 0 THEN 0
						WHEN b.page_count > 0 AND b.last_read_page > b.page_count THEN b.page_count
						ELSE b.last_read_page
					END
				)
				FROM books b
				WHERE b.series_id = s.id
			), 0) AS read_pages,
			COALESCE((
				SELECT COUNT(*)
				FROM books b
				WHERE b.series_id = s.id AND b.last_read_page IS NOT NULL AND b.last_read_page > 0
			), 0) AS read_book_count,
			COALESCE((
				SELECT COUNT(*)
				FROM books b
				WHERE b.series_id = s.id AND b.page_count > 0 AND b.last_read_page >= b.page_count
			), 0) AS completed_book_count,
			(
				SELECT b.last_read_at
				FROM books b
				WHERE b.series_id = s.id AND b.last_read_at IS NOT NULL
				ORDER BY b.last_read_at DESC, b.id DESC
				LIMIT 1
			) AS last_read_at,
			COALESCE((
				SELECT b.id
				FROM books b
				WHERE b.series_id = s.id AND b.last_read_at IS NOT NULL
				ORDER BY b.last_read_at DESC, b.id DESC
				LIMIT 1
			), 0) AS last_read_book_id,
			COALESCE((
				SELECT GROUP_CONCAT(name)
				FROM (
					SELECT DISTINCT t.name AS name
					FROM tags t
					JOIN series_tags st ON st.tag_id = t.id
					WHERE st.series_id = s.id
					ORDER BY t.name
				)
			), '') AS tag_names_cache,
			COALESCE((
				SELECT GROUP_CONCAT(name)
				FROM (
					SELECT DISTINCT a.name AS name
					FROM authors a
					JOIN series_authors sa ON sa.author_id = a.id
					WHERE sa.series_id = s.id
					ORDER BY a.name
				)
			), '') AS author_names_cache,
			CURRENT_TIMESTAMP
		FROM series s
		WHERE ` + whereClause + `
		ON CONFLICT(series_id) DO UPDATE SET
			cover_path = excluded.cover_path,
			cover_book_id = excluded.cover_book_id,
			read_pages = excluded.read_pages,
			read_book_count = excluded.read_book_count,
			completed_book_count = excluded.completed_book_count,
			last_read_at = excluded.last_read_at,
			last_read_book_id = excluded.last_read_book_id,
			tag_names_cache = excluded.tag_names_cache,
			author_names_cache = excluded.author_names_cache,
			updated_at = CURRENT_TIMESTAMP
	`
}

func backfillSeriesStats(db *sql.DB) error {
	_, err := db.Exec(refreshSeriesStatsStatement("1 = 1"))
	return err
}

// backfillTagSeriesCount 全量重算 tags.series_count，供迁移/重建时初始化既有数据。
// 触发器只维护增量，历史数据需此处一次性回填。
func backfillTagSeriesCount(db *sql.DB) error {
	_, err := db.Exec(`UPDATE tags SET series_count = (
		SELECT COUNT(*) FROM series_tags WHERE series_tags.tag_id = tags.id
	)`)
	return err
}

func backfillSeriesMetadataProvenance(db *sql.DB) error {
	for _, stmt := range []string{
		`INSERT OR IGNORE INTO series_metadata_provenance (series_id, field_name, value, source, source_url, confidence, review_id)
		SELECT id, 'title', title, 'manual', '', 1.0, NULL
		FROM series
		WHERE title IS NOT NULL AND title != ''`,
		`INSERT OR IGNORE INTO series_metadata_provenance (series_id, field_name, value, source, source_url, confidence, review_id)
		SELECT id, 'summary', summary, 'manual', '', 1.0, NULL
		FROM series
		WHERE summary IS NOT NULL AND summary != ''`,
		`INSERT OR IGNORE INTO series_metadata_provenance (series_id, field_name, value, source, source_url, confidence, review_id)
		SELECT id, 'publisher', publisher, 'manual', '', 1.0, NULL
		FROM series
		WHERE publisher IS NOT NULL AND publisher != ''`,
		`INSERT OR IGNORE INTO series_metadata_provenance (series_id, field_name, value, source, source_url, confidence, review_id)
		SELECT id, 'status', status, 'manual', '', 1.0, NULL
		FROM series
		WHERE status IS NOT NULL AND status != ''`,
		`INSERT OR IGNORE INTO series_metadata_provenance (series_id, field_name, value, source, source_url, confidence, review_id)
		SELECT id, 'rating', CAST(rating AS TEXT), 'manual', '', 1.0, NULL
		FROM series
		WHERE rating IS NOT NULL`,
		`INSERT OR IGNORE INTO series_metadata_provenance (series_id, field_name, value, source, source_url, confidence, review_id)
		SELECT id, 'language', language, 'manual', '', 1.0, NULL
		FROM series
		WHERE language IS NOT NULL AND language != ''`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

// migrateFTSTables 检测旧 FTS 表结构，若仍含冗余列则 DROP 重建为 contentless 模式。
// 旧结构特征：series_search_fts 含 path 列，book_search_fts 含 series_name 列。
// migrateFTSTables 检测旧版 FTS 结构并在需要时 DROP 重建为 contentless 模式。
// 返回值 rebuilt 表示是否 DROP 了旧表——调用方据此得知“FTS 表刚被清空、必须重新回填”，
// 即使 schema 版本未升级也要强制执行一次 rebuildSearchIndex。
func migrateFTSTables(db *sql.DB) (rebuilt bool, err error) {
	needRebuild := false

	// 检测 series_search_fts 是否含旧列（path）
	rows, err := db.Query(`SELECT * FROM series_search_fts LIMIT 0`)
	if err == nil {
		cols, _ := rows.Columns()
		rows.Close()
		for _, c := range cols {
			if c == "path" {
				needRebuild = true
				break
			}
		}
	}

	if !needRebuild {
		// 检测 book_search_fts 是否含旧列（series_name）
		rows, err = db.Query(`SELECT * FROM book_search_fts LIMIT 0`)
		if err == nil {
			cols, _ := rows.Columns()
			rows.Close()
			for _, c := range cols {
				if c == "series_name" {
					needRebuild = true
					break
				}
			}
		}
	}

	if !needRebuild {
		return false, nil
	}

	// DROP 所有旧触发器和旧 FTS 表，让后续 execSchemaStatements + CREATE TRIGGER IF NOT EXISTS 重建
	for _, stmt := range []string{
		`DROP TRIGGER IF EXISTS trg_series_search_fts_ai`,
		`DROP TRIGGER IF EXISTS trg_series_search_fts_ad`,
		`DROP TRIGGER IF EXISTS trg_series_search_fts_au`,
		`DROP TRIGGER IF EXISTS trg_book_search_fts_ai`,
		`DROP TRIGGER IF EXISTS trg_book_search_fts_ad`,
		`DROP TRIGGER IF EXISTS trg_book_search_fts_au`,
		`DROP TRIGGER IF EXISTS trg_book_search_fts_series_au`,
		`DROP TRIGGER IF EXISTS trg_series_gram_fts_ai`,
		`DROP TRIGGER IF EXISTS trg_series_gram_fts_ad`,
		`DROP TRIGGER IF EXISTS trg_series_gram_fts_au`,
		`DROP TRIGGER IF EXISTS trg_book_gram_fts_ai`,
		`DROP TRIGGER IF EXISTS trg_book_gram_fts_ad`,
		`DROP TRIGGER IF EXISTS trg_book_gram_fts_au`,
		`DROP TABLE IF EXISTS series_gram_fts`,
		`DROP TABLE IF EXISTS book_gram_fts`,
		`DROP TABLE IF EXISTS series_search_fts`,
		`DROP TABLE IF EXISTS book_search_fts`,
	} {
		if _, execErr := db.Exec(stmt); execErr != nil {
			return false, execErr
		}
	}
	return true, nil
}

func migrateLegacyKOReaderAccounts(db *sql.DB) error {
	var accountCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM koreader_accounts`).Scan(&accountCount); err != nil {
		return err
	}
	if accountCount > 0 {
		return nil
	}

	var (
		username string
		syncKey  string
	)
	err := db.QueryRow(`
		SELECT username, password_hash
		FROM koreader_settings
		WHERE id = 1
		  AND username != ''
		  AND password_hash != ''
	`).Scan(&username, &syncKey)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}

	_, err = db.Exec(`
		INSERT INTO koreader_accounts (username, sync_key, enabled, created_at, updated_at)
		VALUES (?, ?, TRUE, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, username, syncKey)
	return err
}

func (s *SqlStore) BackfillSeriesInitials(ctx context.Context) error {
	return backfillSeriesInitials(s.db)
}

func (s *SqlStore) RebuildSeriesSearchIndex(ctx context.Context) error {
	return rebuildSeriesSearchIndexContext(ctx, s.db)
}

func (s *SqlStore) RebuildBookSearchIndex(ctx context.Context) error {
	return rebuildBookSearchIndexContext(ctx, s.db)
}

func rebuildSeriesSearchIndex(db *sql.DB) error {
	return rebuildSeriesSearchIndexContext(context.Background(), db)
}

func rebuildBookSearchIndex(db *sql.DB) error {
	return rebuildBookSearchIndexContext(context.Background(), db)
}

// rebuildSeriesSearchIndexContext 重建系列的两张搜索索引。
//
// 两张一起重建而不是各给一个函数：它们的数据必须始终同源，分开就迟早会有调用方只重建一张。
// Migrate 与维护任务 rebuild_index 都走这里，因而都自动覆盖。
func rebuildSeriesSearchIndexContext(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM series_search_fts`); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO series_search_fts(rowid, library_id, name, title)
		SELECT id, library_id, name, COALESCE(title, '')
		FROM series
	`); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM series_gram_fts`); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO series_gram_fts(rowid, library_id, name, title)
		SELECT s.id, s.library_id, (SELECT group_concat(hex(substr(x.t, x.i, 2)), ' ') FROM (WITH RECURSIVE g(t,i) AS (SELECT lower(s.name), 1 UNION ALL SELECT t, i+1 FROM g WHERE i < length(t)) SELECT t,i FROM g) x), (SELECT group_concat(hex(substr(x.t, x.i, 2)), ' ') FROM (WITH RECURSIVE g(t,i) AS (SELECT lower(COALESCE(s.title, '')), 1 UNION ALL SELECT t, i+1 FROM g WHERE i < length(t)) SELECT t,i FROM g) x)
		FROM series s
	`)
	return err
}

func rebuildBookSearchIndexContext(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM book_search_fts`); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO book_search_fts(rowid, series_id, library_id, name, title)
		SELECT id, series_id, library_id, name, COALESCE(title, '')
		FROM books
	`); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM book_gram_fts`); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO book_gram_fts(rowid, series_id, library_id, name, title)
		SELECT b.id, b.series_id, b.library_id, (SELECT group_concat(hex(substr(x.t, x.i, 2)), ' ') FROM (WITH RECURSIVE g(t,i) AS (SELECT lower(b.name), 1 UNION ALL SELECT t, i+1 FROM g WHERE i < length(t)) SELECT t,i FROM g) x), (SELECT group_concat(hex(substr(x.t, x.i, 2)), ' ') FROM (WITH RECURSIVE g(t,i) AS (SELECT lower(COALESCE(b.title, '')), 1 UNION ALL SELECT t, i+1 FROM g WHERE i < length(t)) SELECT t,i FROM g) x)
		FROM books b
	`)
	return err
}

// backfillSeriesInitials 回填 series.name_initial。
//
// 按 id keyset 分批读取 + 每批一个事务：旧实现先把整张 series 载入内存、再在单个事务里
// 逐行 UPDATE。大库上这意味着回填期间整表驻留内存，且那一个长事务会一直持有写锁，
// 期间任何写操作（扫描入库、进度更新）都要排队等它。
//
// 分批后单批内存与锁持有时间都是常数级；中途失败也只丢当前批，已提交的批次不必重做。
// listSeriesInitialBackfillBatch 按 id keyset 取一批待回填的系列。
//
// 手写而非走 sqlc：其 SQLite 解析器无法处理这条 `WHERE id > ? ORDER BY id LIMIT ?`
// （报 extraneous input），而仓库里 user_progress.go / user_stats.go / koreader_queries.go
// 已有同样的「sqlc 表达不了就手写」先例。
func listSeriesInitialBackfillBatch(ctx context.Context, db *sql.DB, afterID int64, limit int) ([]ListSeriesInitialBackfillCandidatesRow, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, name, title, name_initial FROM series WHERE id > ? ORDER BY id LIMIT ?`,
		afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []ListSeriesInitialBackfillCandidatesRow
	for rows.Next() {
		var i ListSeriesInitialBackfillCandidatesRow
		if err := rows.Scan(&i.ID, &i.Name, &i.Title, &i.NameInitial); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}

func backfillSeriesInitials(db *sql.DB) error {
	ctx := context.Background()
	q := New(db)

	const batchSize = 2000
	var lastID int64

	for {
		candidates, err := listSeriesInitialBackfillBatch(ctx, db, lastID, batchSize)
		if err != nil {
			return err
		}
		if len(candidates) == 0 {
			return nil
		}

		type update struct {
			id      int64
			initial string
		}
		updates := make([]update, 0, len(candidates))
		for _, candidate := range candidates {
			lastID = candidate.ID
			nextInitial := SeriesInitialFromNullTitle(candidate.Title, candidate.Name)
			if candidate.NameInitial != nextInitial {
				updates = append(updates, update{id: candidate.ID, initial: nextInitial})
			}
		}

		if len(updates) > 0 {
			tx, err := db.Begin()
			if err != nil {
				return err
			}
			tq := q.WithTx(tx)
			for _, item := range updates {
				if err := tq.UpdateSeriesInitial(ctx, UpdateSeriesInitialParams{
					NameInitial: item.initial,
					ID:          item.id,
				}); err != nil {
					_ = tx.Rollback()
					return err
				}
			}
			if err := tx.Commit(); err != nil {
				return err
			}
		}

		if len(candidates) < batchSize {
			return nil
		}
	}
}

// migrateReadingBookmarksUserScope 把书签表从「全局共享」迁到「按用户隔离」。
//
// 单纯 ALTER TABLE ADD COLUMN user_id 不够：旧表带 UNIQUE(book_id, page)，
// 而 SQLite 不支持修改已存在的约束，那个隐式唯一索引会一直生效，导致第二个用户
// 给同一本书的同一页加书签时报约束冲突。因此必须走「建新表 → 拷数据 → 换名」。
//
// 存量书签一律归到 user_id=0（历史全局数据），与 user_book_progress 的迁移口径一致：
// 单用户部署下 currentUserID 返回 0，行为完全不变；多用户部署下老书签对所有人可见，
// 但新写入按用户隔离——这比把老数据武断塞给某一个账号更安全。
// 全新库上 schema.sql 已建出正确结构，本函数探测到 user_id 存在即为无操作。
func migrateReadingBookmarksUserScope(db *sql.DB) error {
	hasUserID, err := tableHasColumn(db, "reading_bookmarks", "user_id")
	if err != nil {
		return err
	}
	if hasUserID {
		return nil
	}

	return execMigrationStatements(db, []string{
		`CREATE TABLE reading_bookmarks_migrated (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL DEFAULT 0,
			book_id INTEGER NOT NULL,
			page INTEGER NOT NULL,
			note TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(user_id, book_id, page),
			FOREIGN KEY(book_id) REFERENCES books(id) ON DELETE CASCADE
		)`,
		`INSERT INTO reading_bookmarks_migrated (id, user_id, book_id, page, note, created_at, updated_at)
			SELECT id, 0, book_id, page, note, created_at, updated_at FROM reading_bookmarks`,
		`DROP TABLE reading_bookmarks`,
		`ALTER TABLE reading_bookmarks_migrated RENAME TO reading_bookmarks`,
		`CREATE INDEX IF NOT EXISTS idx_reading_bookmarks_book_id ON reading_bookmarks(book_id)`,
		`CREATE INDEX IF NOT EXISTS idx_reading_bookmarks_user_book ON reading_bookmarks(user_id, book_id, page)`,
	})
}

// tableHasColumn 报告某表是否已有指定列；表不存在时返回 false。
func tableHasColumn(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, err
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
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func ensureColumn(db *sql.DB, table, column, definition string) error {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return err
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
			return err
		}
		if name == column {
			return nil
		}
	}

	_, err = db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition))
	return err
}

// dropColumnIfExists 幂等地删掉一列。
//
// ALTER TABLE ... DROP COLUMN 本身**不**幂等（列不在时直接报 no such column），
// 而迁移每次启动都会重放，所以必须先探测。PRAGMA table_info 对不存在的表返回 0 行且不报错，
// 因此表还没建出来时这里会安全地跳过。
func dropColumnIfExists(db *sql.DB, table, column string) error {
	exists, err := tableHasColumn(db, table, column)
	if err != nil || !exists {
		return err
	}
	_, err = db.Exec(fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", table, column))
	return err
}
