// 业务说明：本文件是业务实现，属于 SQLite 数据访问层，负责把漫画库、系列、阅读进度、任务和元数据状态持久化为稳定数据模型。
// 它连接 sqlc 生成查询与上层领域服务，是资料库筛选、搜索同步和关系图谱的数据基础。
// 维护时应保持 schema、查询定义、事务边界和迁移兼容，避免破坏既有用户数据。

package database

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

type Store interface {
	Querier
	Close() error
	// Store 是 sqlc 生成查询之上的领域边界：Controller 和 Scanner 只依赖这里暴露的业务操作。
	// 新增方法时应优先判断它是“通用 SQL 查询”还是“跨表业务动作”，前者放 query.sql，后者在 Store 中封装事务。
	ListExternalLibraryBooksByLibrary(ctx context.Context, libraryID int64) ([]ExternalLibraryBookRow, error)
	UpdateSeriesMetadata(ctx context.Context, arg UpdateSeriesMetadataParams) (Series, error)
	ExecTx(ctx context.Context, fn func(*Queries) error) error
	SearchSeriesPaged(ctx context.Context, libraryID int64, keyword, letter, status string, tags, authors []string, limit, offset int32, sortBy string) ([]SearchSeriesPagedRow, int, error)
	SearchSeriesCursor(ctx context.Context, libraryID int64, keyword, letter, status string, tags, authors []string, limit int32, sortBy, cursor string) ([]SearchSeriesPagedRow, string, bool, error)
	SearchGlobalSeries(ctx context.Context, keyword string, limit int32) ([]SeriesSearchHit, error)
	SearchGlobalBooks(ctx context.Context, keyword string, limit int32) ([]BookSearchHit, error)
	SearchProtocolSeries(ctx context.Context, keyword string, limit, offset int32) ([]ProtocolSeriesRow, int, error)
	RebuildSeriesSearchIndex(ctx context.Context) error
	RebuildBookSearchIndex(ctx context.Context) error
	GetDashboardStats(ctx context.Context) (*DashboardStats, error)
	GetDashboardStructuralStats(ctx context.Context) (*DashboardStructuralStats, error)
	GetDashboardVolatileStats(ctx context.Context) (*DashboardVolatileStats, error)
	ListProtocolSeriesByIDs(ctx context.Context, ids []int64) ([]ProtocolSeriesRow, error)
	SearchTags(ctx context.Context, query string, limit int) ([]Tag, error)
	SearchAuthors(ctx context.Context, query string, limit int) ([]Author, error)
	UpsertTask(ctx context.Context, task TaskRecord) error
	ListTasks(ctx context.Context, filters TaskFilters) ([]TaskRecord, error)
	DeleteTasks(ctx context.Context, filters TaskFilters) (int64, error)
	GetHealthReport(ctx context.Context, filters HealthIssueFilters) (HealthReport, error)
	GetKOReaderSettings(ctx context.Context) (KOReaderSettings, error)
	UpsertKOReaderSettings(ctx context.Context, arg UpsertKOReaderSettingsParams) (KOReaderSettings, error)
	ListKOReaderAccounts(ctx context.Context) ([]KOReaderAccount, error)
	CreateKOReaderAccount(ctx context.Context, arg CreateKOReaderAccountParams) (KOReaderAccount, error)
	GetKOReaderAccountByUsername(ctx context.Context, username string) (KOReaderAccount, error)
	GetKOReaderAccountByID(ctx context.Context, id int64) (KOReaderAccount, error)
	RotateKOReaderAccountKey(ctx context.Context, id int64, syncKey string) (KOReaderAccount, error)
	SetKOReaderAccountEnabled(ctx context.Context, id int64, enabled bool) (KOReaderAccount, error)
	DeleteKOReaderAccount(ctx context.Context, id int64) error
	GetKOReaderStats(ctx context.Context) (KOReaderStats, error)
	GetLatestKOReaderFailure(ctx context.Context) (KOReaderSyncEvent, error)
	BackfillSeriesInitials(ctx context.Context) error
	ListKOReaderDeviceDiagnostics(ctx context.Context) ([]KOReaderDeviceDiagnostic, error)
	ListKOReaderDeviceMatchMethods(ctx context.Context) ([]KOReaderDeviceMatchMethod, error)
	ListKOReaderDeviceConflicts(ctx context.Context, limit int) ([]KOReaderDeviceConflict, error)
	CountBooksMissingIdentity(ctx context.Context, matchMode string) (int64, error)
	CountUnmatchedKOReaderProgress(ctx context.Context) (int64, error)
	FindBookByDocumentFingerprint(ctx context.Context, documentKey, matchMode string, pathIgnoreExtension bool) (KOReaderBookMatch, error)
	UpsertKOReaderProgress(ctx context.Context, arg UpsertKOReaderProgressParams) (KOReaderProgress, error)
	GetKOReaderProgress(ctx context.Context, username, document string) (KOReaderProgress, error)
	ListBooksMissingIdentityBatch(ctx context.Context, matchMode string, afterID int64, limit int) ([]BookIdentityCandidate, error)
	CountBooksMissingQuickHash(ctx context.Context) (int64, error)
	ListBooksMissingQuickHashBatch(ctx context.Context, afterID int64, limit int) ([]BookIdentityCandidate, error)
	UpdateBookIdentity(ctx context.Context, arg UpdateBookIdentityParams) error
	ListUnmatchedKOReaderProgress(ctx context.Context, limit int) ([]KOReaderProgress, error)
	ListUnmatchedKOReaderProgressBatch(ctx context.Context, afterID int64, limit int) ([]KOReaderProgress, error)
	LinkKOReaderProgressToBook(ctx context.Context, progressID, bookID int64, matchedBy string) error
	CreateKOReaderSyncEvent(ctx context.Context, arg CreateKOReaderSyncEventParams) error
	GetReadingListItemProgress(ctx context.Context, readingListID int64) (map[int64]ReadingListSeriesProgress, error)
	SearchSmartCollectionSeries(ctx context.Context, filter SmartCollectionFilter, limit, offset int) ([]SearchSeriesPagedRow, int, error)
}

type ReadingListSeriesProgress struct {
	SeriesID       int64 `json:"series_id"`
	ReadBooks      int64 `json:"read_books"`
	CompletedBooks int64 `json:"completed_books"`
	TotalBooks     int64 `json:"total_books"`
}

type LibrarySize struct {
	LibraryID   int64  `json:"library_id"`
	LibraryName string `json:"library_name"`
	TotalSize   int64  `json:"total_size"`
}

type DashboardStats struct {
	TotalSeries  int           `json:"total_series"`
	TotalBooks   int           `json:"total_books"`
	ReadBooks    int           `json:"read_books"`
	TotalPages   int           `json:"total_pages"`
	ActiveDays7  int           `json:"active_days_7"` // 最近7天有阅读行为的天数
	LibrarySizes []LibrarySize `json:"library_sizes"`
}

// DashboardStructuralStats 是仅在扫描增删书/库时才变化的结构性统计。
// 其中 TotalBooks/TotalPages 是对 books 表的全表扫描（70w 行级别），代价高，
// 故与高频变化的阅读类统计分开缓存，避免阅读进度更新触发全表重算。
type DashboardStructuralStats struct {
	TotalSeries  int
	TotalBooks   int
	TotalPages   int
	LibrarySizes []LibrarySize
}

// DashboardVolatileStats 是随阅读进度高频变化的统计，查询均走索引/时间窗口，代价低。
type DashboardVolatileStats struct {
	ReadBooks   int
	ActiveDays7 int
}

type TaskRecord struct {
	Key        string            `json:"key"`
	Type       string            `json:"type"`
	Scope      string            `json:"scope"`
	ScopeID    *int64            `json:"scope_id,omitempty"`
	ScopeName  string            `json:"scope_name,omitempty"`
	Status     string            `json:"status"`
	Message    string            `json:"message"`
	Error      string            `json:"error,omitempty"`
	Current    int               `json:"current"`
	Total      int               `json:"total"`
	CanCancel  bool              `json:"can_cancel"`
	Retryable  bool              `json:"retryable"`
	Params     map[string]string `json:"params,omitempty"`
	StartedAt  time.Time         `json:"started_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
	FinishedAt *time.Time        `json:"finished_at,omitempty"`
	Sequence   int64             `json:"-"`
}

type TaskFilters struct {
	Status  string
	Scope   string
	Type    string
	ScopeID *int64
	Query   string
	Limit   int
}

type SearchSeriesPagedRow struct {
	Series
	CoverPath       sql.NullString  `json:"cover_path"`
	TagsString      *string         `json:"tags_string"`
	VolumeCount     int             `json:"volume_count"`
	ActualBookCount int             `json:"actual_book_count"`
	ReadCount       int             `json:"read_count"`
	TotalPages      sql.NullFloat64 `json:"total_pages"`
	IsFavorite      bool            `json:"is_favorite"`
	LastReadAt      sql.NullTime    `json:"last_read_at"`
	LastReadBookID  sql.NullInt64   `json:"last_read_book_id"`
	LastReadPage    sql.NullInt64   `json:"last_read_page"`
}

type SeriesSearchHit struct {
	SearchSeriesPagedRow
	Score float64
}

type BookSearchHit struct {
	Book
	SeriesName  string
	SeriesTitle sql.NullString
	Score       float64
}

type seriesSearchSort struct {
	Field string
	Dir   string
	Expr  string
}

type seriesCursorPayload struct {
	SortBy string `json:"sort_by"`
	Value  string `json:"value"`
	Name   string `json:"name"`
	ID     int64  `json:"id"`
}

type ProtocolSeriesRow struct {
	ID          int64     `json:"id"`
	LibraryID   int64     `json:"library_id"`
	Name        string    `json:"name"`
	Title       string    `json:"title"`
	Summary     string    `json:"summary"`
	Status      string    `json:"status"`
	BookCount   int64     `json:"book_count"`
	TotalPages  int64     `json:"total_pages"`
	CoverPath   string    `json:"cover_path"`
	CoverBookID int64     `json:"cover_book_id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type SqlStore struct {
	*Queries
	db *sql.DB
}

// DB 返回底层数据库连接（供自定义查询使用）
func (s *SqlStore) DB() *sql.DB {
	return s.db
}

func NewStore(dbPath string) (Store, error) {
	// 针对 100MB 级别的数据库进行精简优化：
	// mmap_size=268435456 (256MB，足以将百兆级数据库整个隐射进内存，消除系统的换页压力)
	// cache_size=-128000  (128MB，页缓存亦完全够用，不需要分配夸张的 500MB 防御性冗余)
	// busy_timeout=15000  (保持长超时，预防因高并发读写引发 sqlite3 busy lock)
	// temp_store=2        (MEMORY：百兆规模下，内存聚合 ORDER 操作极快，保持使用内存)
	db, err := sql.Open("sqlite", sqliteDSN(dbPath))
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// 开启连接池支持
	// 对于现代无并发 CGO 限制的 purely go sqlite，我们设置并行度
	maxConns := runtime.NumCPU() * 2
	if maxConns < 8 {
		maxConns = 8
	}
	db.SetMaxOpenConns(maxConns)
	db.SetMaxIdleConns(maxConns / 2)

	return &SqlStore{
		Queries: New(db),
		db:      db,
	}, nil
}

func (s *SqlStore) Close() error {
	return s.db.Close()
}

func (s *SqlStore) DeleteKOReaderAccount(ctx context.Context, id int64) error {
	return s.ExecTx(ctx, func(q *Queries) error {
		account, err := q.GetKOReaderAccountByID(ctx, id)
		if err != nil {
			return err
		}
		if _, err := q.db.ExecContext(ctx, `DELETE FROM koreader_progress WHERE username = ?`, account.Username); err != nil {
			return err
		}
		if _, err := q.db.ExecContext(ctx, `DELETE FROM koreader_sync_events WHERE username = ?`, account.Username); err != nil {
			return err
		}
		_, err = q.db.ExecContext(ctx, `DELETE FROM koreader_accounts WHERE id = ?`, id)
		return err
	})
}

// ExecTx 提供一个事务包裹器以进行批量执行，这对防止 SQLite 并发锁极为关键
func (s *SqlStore) ExecTx(ctx context.Context, fn func(*Queries) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	q := s.Queries.WithTx(tx)
	if err := fn(q); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("tx err: %v, rb err: %v", err, rbErr)
		}
		return err
	}
	return tx.Commit()
}

func (s *SqlStore) RefreshSeriesStats(ctx context.Context, seriesID int64) error {
	_, err := s.db.ExecContext(ctx, refreshSeriesStatsStatement("s.id = ?"), seriesID)
	return err
}

func (s *SqlStore) GetReadingListItemProgress(ctx context.Context, readingListID int64) (map[int64]ReadingListSeriesProgress, error) {
	rows, err := s.Queries.GetReadingListItemProgressByList(ctx, readingListID)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]ReadingListSeriesProgress, len(rows))
	for _, r := range rows {
		out[r.SeriesID] = ReadingListSeriesProgress{
			SeriesID:       r.SeriesID,
			ReadBooks:      r.ReadBooks,
			CompletedBooks: r.CompletedBooks,
			TotalBooks:     r.TotalBooks,
		}
	}
	return out, nil
}

func (s *SqlStore) refreshSeriesStatsForBook(ctx context.Context, bookID int64) error {
	seriesID, err := s.Queries.GetSeriesIDByBookID(ctx, bookID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	return s.RefreshSeriesStats(ctx, seriesID)
}

func (s *SqlStore) CreateBook(ctx context.Context, arg CreateBookParams) (Book, error) {
	book, err := s.Queries.CreateBook(ctx, arg)
	if err != nil {
		return Book{}, err
	}
	if err := s.RefreshSeriesStats(ctx, book.SeriesID); err != nil {
		return Book{}, err
	}
	return book, nil
}

func (s *SqlStore) UpsertBookByPath(ctx context.Context, arg UpsertBookByPathParams) (Book, error) {
	book, err := s.Queries.UpsertBookByPath(ctx, arg)
	if err != nil {
		return Book{}, err
	}
	if err := s.RefreshSeriesStats(ctx, book.SeriesID); err != nil {
		return Book{}, err
	}
	return book, nil
}

func (s *SqlStore) DeleteBook(ctx context.Context, id int64) error {
	seriesID, err := s.Queries.GetSeriesIDByBookID(ctx, id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err := s.Queries.DeleteBook(ctx, id); err != nil {
		return err
	}
	if err == nil {
		return s.RefreshSeriesStats(ctx, seriesID)
	}
	return nil
}

func (s *SqlStore) DeleteBookByPath(ctx context.Context, path string) error {
	seriesID, err := s.Queries.GetSeriesIDByBookPath(ctx, path)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err := s.Queries.DeleteBookByPath(ctx, path); err != nil {
		return err
	}
	if err == nil {
		return s.RefreshSeriesStats(ctx, seriesID)
	}
	return nil
}

func (s *SqlStore) UpdateBookProgress(ctx context.Context, arg UpdateBookProgressParams) error {
	if err := s.Queries.UpdateBookProgress(ctx, arg); err != nil {
		return err
	}
	return s.refreshSeriesStatsForBook(ctx, arg.ID)
}

func (s *SqlStore) UpdateSeriesStatistics(ctx context.Context, arg UpdateSeriesStatisticsParams) error {
	if err := s.Queries.UpdateSeriesStatistics(ctx, arg); err != nil {
		return err
	}
	return s.RefreshSeriesStats(ctx, arg.ID)
}

func (s *SqlStore) LinkSeriesTag(ctx context.Context, arg LinkSeriesTagParams) error {
	if err := s.Queries.LinkSeriesTag(ctx, arg); err != nil {
		return err
	}
	return s.RefreshSeriesStats(ctx, arg.SeriesID)
}

func (s *SqlStore) ClearSeriesTags(ctx context.Context, seriesID int64) error {
	if err := s.Queries.ClearSeriesTags(ctx, seriesID); err != nil {
		return err
	}
	return s.RefreshSeriesStats(ctx, seriesID)
}

func (s *SqlStore) LinkSeriesAuthor(ctx context.Context, arg LinkSeriesAuthorParams) error {
	if err := s.Queries.LinkSeriesAuthor(ctx, arg); err != nil {
		return err
	}
	return s.RefreshSeriesStats(ctx, arg.SeriesID)
}

func (s *SqlStore) ClearSeriesAuthors(ctx context.Context, seriesID int64) error {
	if err := s.Queries.ClearSeriesAuthors(ctx, seriesID); err != nil {
		return err
	}
	return s.RefreshSeriesStats(ctx, seriesID)
}

// Migrate 供启动时执行迁移
func Migrate(dbPath string) error {
	db, err := sql.Open("sqlite", sqliteDSN(dbPath))
	if err != nil {
		return err
	}
	defer db.Close()

	// 必须在建表/建触发器之前迁移旧版 FTS 结构：migrateFTSTables 会 DROP 掉带冗余列的旧
	// series_search_fts / book_search_fts 及其触发器，随后的 execSchemaStatements(false)
	// 以新 schema 重建虚拟表、CREATE TRIGGER IF NOT EXISTS 重建触发器。若放在建表之后，
	// DROP 完却没有任何语句重建虚拟表，rebuildSeriesSearchIndex 的 DELETE 会因表不存在而
	// 让整个 Migrate 失败（老库升级首启崩溃）。全新库上该 SELECT 探测直接报错、needRebuild 保持 false，为无操作。
	if err := migrateFTSTables(db); err != nil {
		return err
	}

	if err = execSchemaStatements(db, false); err != nil {
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
	} {
		if err := ensureColumn(db, column.table, column.name, column.definition); err != nil {
			return err
		}
	}

	if err := execMigrationStatements(db, []string{
		`CREATE INDEX IF NOT EXISTS idx_books_file_hash ON books(file_hash)`,
		`CREATE INDEX IF NOT EXISTS idx_books_quick_hash ON books(quick_hash)`,
		`CREATE INDEX IF NOT EXISTS idx_books_path_fingerprint ON books(path_fingerprint)`,
		`CREATE INDEX IF NOT EXISTS idx_books_path_fingerprint_no_ext ON books(path_fingerprint_no_ext)`,
		`CREATE INDEX IF NOT EXISTS idx_books_library_size ON books(library_id, size)`,
		`CREATE INDEX IF NOT EXISTS idx_reading_bookmarks_book_id ON reading_bookmarks(book_id)`,
		`CREATE INDEX IF NOT EXISTS idx_series_name_initial ON series(name_initial)`,
		`CREATE INDEX IF NOT EXISTS idx_series_library_initial ON series(library_id, name_initial)`,
		`CREATE INDEX IF NOT EXISTS idx_series_library_status ON series(library_id, status)`,
		`CREATE INDEX IF NOT EXISTS idx_series_library_updated ON series(library_id, updated_at)`,
		`CREATE INDEX IF NOT EXISTS idx_series_library_created ON series(library_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_series_library_name ON series(library_id, name)`,
		`CREATE INDEX IF NOT EXISTS idx_series_library_initial_name ON series(library_id, name_initial, name)`,
		`CREATE INDEX IF NOT EXISTS idx_series_library_status_name ON series(library_id, status, name)`,
		`CREATE INDEX IF NOT EXISTS idx_series_library_updated_name ON series(library_id, updated_at, name)`,
		`CREATE INDEX IF NOT EXISTS idx_series_library_created_name ON series(library_id, created_at, name)`,
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
		`CREATE INDEX IF NOT EXISTS idx_books_series_sort ON books(series_id, volume, sort_number, name)`,
		`CREATE INDEX IF NOT EXISTS idx_books_series_read ON books(series_id, last_read_page)`,
		`CREATE INDEX IF NOT EXISTS idx_books_read_progress_series ON books(last_read_page, series_id) WHERE last_read_page > 0`,
		`CREATE INDEX IF NOT EXISTS idx_books_cover_pick ON books(series_id, sort_number, name) WHERE cover_path IS NOT NULL AND cover_path != ''`,
		`CREATE INDEX IF NOT EXISTS idx_books_library_modified ON books(library_id, file_modified_at)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_updated_at ON tasks(updated_at)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_scope ON tasks(scope, scope_id)`,
		`CREATE INDEX IF NOT EXISTS idx_smart_filters_library_id ON smart_filters(library_id, updated_at)`,
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
	}); err != nil {
		return err
	}

	if err = execSchemaStatements(db, true); err != nil {
		return err
	}

	if err := backfillSeriesInitials(db); err != nil {
		return err
	}

	if err := backfillSeriesStats(db); err != nil {
		return err
	}

	if err := rebuildSeriesSearchIndex(db); err != nil {
		return err
	}
	if err := rebuildBookSearchIndex(db); err != nil {
		return err
	}

	if err := backfillSeriesMetadataProvenance(db); err != nil {
		return err
	}

	if err := migrateLegacyKOReaderAccounts(db); err != nil {
		return err
	}

	// 迁移旧的 auto_scan 字段到新的 scan_mode
	// 尝试执行，忽略错误因为有些数据库可能原本就没有 auto_scan
	_, _ = db.Exec(`UPDATE libraries SET scan_mode = 'interval' WHERE auto_scan = 1 AND scan_mode = 'none'`)

	return nil
}

func sqliteDSN(dbPath string) string {
	return dbPath + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)" +
		"&_pragma=mmap_size=268435456&_pragma=cache_size=-128000&_pragma=busy_timeout=15000&_pragma=temp_store=2"
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
func migrateFTSTables(db *sql.DB) error {
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
		return nil
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
		`DROP TABLE IF EXISTS series_search_fts`,
		`DROP TABLE IF EXISTS book_search_fts`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
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

func rebuildSeriesSearchIndexContext(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM series_search_fts`); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO series_search_fts(rowid, library_id, name, title)
		SELECT id, library_id, name, COALESCE(title, '')
		FROM series
	`)
	return err
}

func rebuildBookSearchIndexContext(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM book_search_fts`); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO book_search_fts(rowid, series_id, library_id, name, title)
		SELECT id, series_id, library_id, name, COALESCE(title, '')
		FROM books
	`)
	return err
}

func backfillSeriesInitials(db *sql.DB) error {
	ctx := context.Background()
	q := New(db)

	type update struct {
		id      int64
		initial string
	}
	updates := make([]update, 0)
	candidates, err := q.ListSeriesInitialBackfillCandidates(ctx)
	if err != nil {
		return err
	}
	for _, candidate := range candidates {
		nextInitial := SeriesInitialFromNullTitle(candidate.Title, candidate.Name)
		if candidate.NameInitial != nextInitial {
			updates = append(updates, update{
				id:      candidate.ID,
				initial: nextInitial,
			})
		}
	}
	if len(updates) == 0 {
		return nil
	}

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
	return tx.Commit()
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

func (s *SqlStore) UpsertTask(ctx context.Context, task TaskRecord) error {
	paramsJSON := ""
	if len(task.Params) > 0 {
		data, err := json.Marshal(task.Params)
		if err != nil {
			return err
		}
		paramsJSON = string(data)
	}

	var scopeID sql.NullInt64
	if task.ScopeID != nil {
		scopeID = sql.NullInt64{Int64: *task.ScopeID, Valid: true}
	}
	var finishedAt sql.NullTime
	if task.FinishedAt != nil {
		finishedAt = sql.NullTime{Time: *task.FinishedAt, Valid: true}
	}

	return s.Queries.UpsertTaskRecord(ctx, UpsertTaskRecordParams{
		Key:        task.Key,
		Type:       task.Type,
		Scope:      task.Scope,
		ScopeID:    scopeID,
		ScopeName:  task.ScopeName,
		Status:     task.Status,
		Message:    task.Message,
		Error:      task.Error,
		Current:    int64(task.Current),
		Total:      int64(task.Total),
		CanCancel:  task.CanCancel,
		Retryable:  task.Retryable,
		Params:     paramsJSON,
		StartedAt:  task.StartedAt,
		UpdatedAt:  task.UpdatedAt,
		FinishedAt: finishedAt,
		Sequence:   task.Sequence,
	})
}

func (s *SqlStore) ListTasks(ctx context.Context, filters TaskFilters) ([]TaskRecord, error) {
	query := `
		SELECT key, type, scope, scope_id, scope_name, status, message, error,
		       current, total, can_cancel, retryable, params,
		       started_at, updated_at, finished_at, sequence
		FROM tasks
		WHERE 1 = 1
	`
	args := make([]any, 0)
	if filters.Status != "" {
		query += ` AND status = ?`
		args = append(args, filters.Status)
	}
	if filters.Scope != "" {
		query += ` AND scope = ?`
		args = append(args, filters.Scope)
	}
	if filters.Type != "" {
		query += ` AND type = ?`
		args = append(args, filters.Type)
	}
	if filters.ScopeID != nil {
		query += ` AND scope_id = ?`
		args = append(args, *filters.ScopeID)
	}
	if filters.Query != "" {
		query += ` AND lower(key || ' ' || message || ' ' || error) LIKE ?`
		args = append(args, "%"+strings.ToLower(filters.Query)+"%")
	}
	query += ` ORDER BY sequence DESC, updated_at DESC, started_at DESC, key DESC`
	if filters.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, filters.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tasks := make([]TaskRecord, 0)
	for rows.Next() {
		task, err := scanTaskRecord(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return tasks, nil
}

func (s *SqlStore) DeleteTasks(ctx context.Context, filters TaskFilters) (int64, error) {
	query := `DELETE FROM tasks WHERE status NOT IN ('running', 'paused', 'cancelling')`
	args := make([]any, 0)
	if filters.Status != "" {
		query += ` AND status = ?`
		args = append(args, filters.Status)
	}
	if filters.Scope != "" {
		query += ` AND scope = ?`
		args = append(args, filters.Scope)
	}
	if filters.Type != "" {
		query += ` AND type = ?`
		args = append(args, filters.Type)
	}
	if filters.ScopeID != nil {
		query += ` AND scope_id = ?`
		args = append(args, *filters.ScopeID)
	}
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

type taskScanner interface {
	Scan(dest ...any) error
}

func scanTaskRecord(row taskScanner) (TaskRecord, error) {
	var (
		task       TaskRecord
		scopeID    sql.NullInt64
		finishedAt sql.NullTime
		paramsJSON string
	)
	err := row.Scan(
		&task.Key,
		&task.Type,
		&task.Scope,
		&scopeID,
		&task.ScopeName,
		&task.Status,
		&task.Message,
		&task.Error,
		&task.Current,
		&task.Total,
		&task.CanCancel,
		&task.Retryable,
		&paramsJSON,
		&task.StartedAt,
		&task.UpdatedAt,
		&finishedAt,
		&task.Sequence,
	)
	if err != nil {
		return TaskRecord{}, err
	}
	if scopeID.Valid {
		task.ScopeID = &scopeID.Int64
	}
	if finishedAt.Valid {
		task.FinishedAt = &finishedAt.Time
	}
	if strings.TrimSpace(paramsJSON) != "" {
		var params map[string]string
		if err := json.Unmarshal([]byte(paramsJSON), &params); err != nil {
			return TaskRecord{}, err
		}
		task.Params = params
	}
	return task, nil
}

// SearchSeriesPaged 供主页根据标签和作者查询并分页。
// 默认列表只走 series + series_stats，只有标签/作者筛选时才进入关联表。
func (s *SqlStore) SearchSeriesPaged(ctx context.Context, libraryID int64, keyword, letter, status string, tags, authors []string, limit, offset int32, sortBy string) ([]SearchSeriesPagedRow, int, error) {
	baseQuery, whereClause, args := buildSeriesSearchQuery(libraryID, keyword, letter, status, tags, authors)

	var total int
	if keyword == "" && status == "" && letter == "" && len(tags) == 0 && len(authors) == 0 {
		if libraryID == 0 {
			if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM series`).Scan(&total); err != nil {
				return nil, 0, err
			}
		} else if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM series WHERE library_id = ?`, libraryID).Scan(&total); err != nil {
			return nil, 0, err
		}
	} else {
		countQuery := `SELECT COUNT(*) FROM series s` + whereClause
		if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
			return nil, 0, err
		}
	}

	sortSpec := parseSeriesSearchSort(sortBy)
	orderClause := seriesSearchOffsetOrderClause(sortSpec)

	queryArgs := append([]interface{}{}, args...)
	baseQuery += whereClause + fmt.Sprintf(` ORDER BY %s LIMIT ? OFFSET ?`, orderClause)
	queryArgs = append(queryArgs, limit, offset)

	rows, err := s.db.QueryContext(ctx, baseQuery, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	items, err := scanSearchSeriesPagedRows(rows)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *SqlStore) SearchSeriesCursor(ctx context.Context, libraryID int64, keyword, letter, status string, tags, authors []string, limit int32, sortBy, cursor string) ([]SearchSeriesPagedRow, string, bool, error) {
	sortSpec := parseSeriesSearchSort(sortBy)
	if !sortSpec.supportsCursor() {
		return nil, "", false, fmt.Errorf("sort %q does not support cursor pagination", sortBy)
	}
	if limit <= 0 {
		limit = 50
	}

	baseQuery, whereClause, args := buildSeriesSearchQuery(libraryID, keyword, letter, status, tags, authors)
	filters := strings.TrimPrefix(whereClause, " WHERE ")
	queryArgs := append([]interface{}{}, args...)

	if cursor != "" {
		payload, err := decodeSeriesCursor(cursor)
		if err != nil {
			return nil, "", false, err
		}
		if payload.SortBy != seriesSearchSortKey(sortSpec) {
			return nil, "", false, fmt.Errorf("cursor sort %q does not match request sort %q", payload.SortBy, seriesSearchSortKey(sortSpec))
		}
		seekClause, seekArgs := seriesSearchSeekClause(sortSpec, payload)
		if seekClause != "" {
			if filters != "" {
				filters += " AND "
			}
			filters += seekClause
			queryArgs = append(queryArgs, seekArgs...)
		}
	}

	if filters != "" {
		baseQuery += " WHERE " + filters
	}
	orderClause := seriesSearchCursorOrderClause(sortSpec)
	queryArgs = append(queryArgs, int(limit)+1)
	baseQuery += fmt.Sprintf(` ORDER BY %s LIMIT ?`, orderClause)

	rows, err := s.db.QueryContext(ctx, baseQuery, queryArgs...)
	if err != nil {
		return nil, "", false, err
	}
	items, err := scanSearchSeriesPagedRows(rows)
	if err != nil {
		return nil, "", false, err
	}

	hasMore := len(items) > int(limit)
	if hasMore {
		items = items[:int(limit)]
	}
	nextCursor := ""
	if hasMore && len(items) > 0 {
		nextCursor = encodeSeriesCursor(sortSpec, items[len(items)-1])
	}
	return items, nextCursor, hasMore, nil
}

func (s *SqlStore) SearchGlobalSeries(ctx context.Context, keyword string, limit int32) ([]SeriesSearchHit, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return []SeriesSearchHit{}, nil
	}
	if limit <= 0 {
		limit = 20
	}

	if seriesSearchFTSEligible(keyword) {
		items, err := s.searchGlobalSeriesFTS(ctx, keyword, limit)
		if err == nil {
			return items, nil
		}
	}
	return s.searchGlobalSeriesSubstring(ctx, keyword, limit)
}

func (s *SqlStore) SearchGlobalBooks(ctx context.Context, keyword string, limit int32) ([]BookSearchHit, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return []BookSearchHit{}, nil
	}
	if limit <= 0 {
		limit = 20
	}

	if seriesSearchFTSEligible(keyword) {
		items, err := s.searchGlobalBooksFTS(ctx, keyword, limit)
		if err == nil {
			return items, nil
		}
	}
	return s.searchGlobalBooksSubstring(ctx, keyword, limit)
}

func (s *SqlStore) SearchProtocolSeries(ctx context.Context, keyword string, limit, offset int32) ([]ProtocolSeriesRow, int, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return []ProtocolSeriesRow{}, 0, nil
	}
	if limit <= 0 {
		limit = 30
	}
	if offset < 0 {
		offset = 0
	}
	if seriesSearchFTSEligible(keyword) {
		rows, total, err := s.searchProtocolSeriesFTS(ctx, keyword, limit, offset)
		if err == nil {
			return rows, total, nil
		}
	}
	return s.searchProtocolSeriesSubstring(ctx, keyword, limit, offset)
}

func (s *SqlStore) searchProtocolSeriesFTS(ctx context.Context, keyword string, limit, offset int32) ([]ProtocolSeriesRow, int, error) {
	match := fts5PhraseQuery(keyword)
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM series_search_fts WHERE series_search_fts MATCH ?`, match).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			s.id,
			s.library_id,
			s.name,
			COALESCE(s.title, '') AS title,
			COALESCE(s.summary, '') AS summary,
			COALESCE(s.status, '') AS status,
			s.book_count,
			s.total_pages,
			COALESCE(ss.cover_path, '') AS cover_path,
			COALESCE(ss.cover_book_id, 0) AS cover_book_id,
			s.created_at,
			s.updated_at
		FROM (
			SELECT rowid, bm25(series_search_fts, 2.0, 3.0) AS rank
			FROM series_search_fts
			WHERE series_search_fts MATCH ?
		) m
		JOIN series s ON s.id = m.rowid
		LEFT JOIN series_stats ss ON ss.series_id = s.id
		ORDER BY
			CASE
				WHEN lower(s.name) = lower(?) OR lower(COALESCE(s.title, '')) = lower(?) THEN 0
				WHEN instr(lower(s.name), lower(?)) > 0 OR instr(lower(COALESCE(s.title, '')), lower(?)) > 0 THEN 1
				ELSE 2
			END,
			m.rank ASC,
			COALESCE(NULLIF(s.title, ''), s.name) COLLATE NOCASE ASC
		LIMIT ? OFFSET ?
	`, match, keyword, keyword, keyword, keyword, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	items, err := scanProtocolSeriesRows(rows)
	return items, total, err
}

func (s *SqlStore) searchProtocolSeriesSubstring(ctx context.Context, keyword string, limit, offset int32) ([]ProtocolSeriesRow, int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM series s
		WHERE instr(lower(s.name), lower(?)) > 0
		   OR instr(lower(COALESCE(s.title, '')), lower(?)) > 0
		   OR instr(lower(s.path), lower(?)) > 0
	`, keyword, keyword, keyword).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			s.id,
			s.library_id,
			s.name,
			COALESCE(s.title, '') AS title,
			COALESCE(s.summary, '') AS summary,
			COALESCE(s.status, '') AS status,
			s.book_count,
			s.total_pages,
			COALESCE(ss.cover_path, '') AS cover_path,
			COALESCE(ss.cover_book_id, 0) AS cover_book_id,
			s.created_at,
			s.updated_at
		FROM series s
		LEFT JOIN series_stats ss ON ss.series_id = s.id
		WHERE instr(lower(s.name), lower(?)) > 0
		   OR instr(lower(COALESCE(s.title, '')), lower(?)) > 0
		   OR instr(lower(s.path), lower(?)) > 0
		ORDER BY
			CASE
				WHEN lower(s.name) = lower(?) OR lower(COALESCE(s.title, '')) = lower(?) THEN 0
				WHEN instr(lower(s.name), lower(?)) > 0 OR instr(lower(COALESCE(s.title, '')), lower(?)) > 0 THEN 1
				ELSE 2
			END,
			COALESCE(NULLIF(s.title, ''), s.name) COLLATE NOCASE ASC
		LIMIT ? OFFSET ?
	`, keyword, keyword, keyword, keyword, keyword, keyword, keyword, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	items, err := scanProtocolSeriesRows(rows)
	return items, total, err
}

func (s *SqlStore) searchGlobalBooksFTS(ctx context.Context, keyword string, limit int32) ([]BookSearchHit, error) {
	match := fts5PhraseQuery(keyword)
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			b.id, b.series_id, b.library_id, b.name, b.path, b.size, b.file_modified_at, b.volume, b.title, b.summary, b.number, b.sort_number, b.page_count, b.cover_path, b.last_read_page, b.last_read_at, b.file_hash, b.quick_hash, b.path_fingerprint, b.path_fingerprint_no_ext, b.filename_fingerprint, b.created_at, b.updated_at,
			s.name AS series_name,
			s.title AS series_title,
			(
				CASE
					WHEN lower(b.name) = lower(?) OR lower(COALESCE(b.title, '')) = lower(?) THEN 3.5
					WHEN lower(s.name) = lower(?) OR lower(COALESCE(s.title, '')) = lower(?) THEN 3.0
					WHEN instr(lower(b.name), lower(?)) > 0 OR instr(lower(COALESCE(b.title, '')), lower(?)) > 0 THEN 2.5
					WHEN instr(lower(s.name), lower(?)) > 0 OR instr(lower(COALESCE(s.title, '')), lower(?)) > 0 THEN 2.0
					ELSE 1.0
				END
				+ (1.0 / (1.0 + ABS(m.rank)))
			) AS score
		FROM (
			SELECT rowid, bm25(book_search_fts, 2.5, 3.0) AS rank
			FROM book_search_fts
			WHERE book_search_fts MATCH ?
			UNION
			SELECT b2.id AS rowid, 1.5 AS rank
			FROM series_search_fts sf
			JOIN books b2 ON b2.series_id = sf.rowid
			WHERE series_search_fts MATCH ?
		) m
		JOIN books b ON b.id = m.rowid
		JOIN series s ON s.id = b.series_id
		GROUP BY b.id
		ORDER BY
			CASE
				WHEN lower(b.name) = lower(?) OR lower(COALESCE(b.title, '')) = lower(?) THEN 0
				WHEN lower(s.name) = lower(?) OR lower(COALESCE(s.title, '')) = lower(?) THEN 1
				WHEN instr(lower(b.name), lower(?)) > 0 OR instr(lower(COALESCE(b.title, '')), lower(?)) > 0 THEN 2
				WHEN instr(lower(s.name), lower(?)) > 0 OR instr(lower(COALESCE(s.title, '')), lower(?)) > 0 THEN 3
				ELSE 4
			END,
			MIN(m.rank) ASC,
			s.name COLLATE NOCASE ASC,
			b.volume ASC,
			b.sort_number ASC,
			b.name COLLATE NOCASE ASC
		LIMIT ?
	`, keyword, keyword, keyword, keyword, keyword, keyword, keyword, keyword, match, match, keyword, keyword, keyword, keyword, keyword, keyword, keyword, keyword, limit)
	if err != nil {
		return nil, err
	}
	return scanBookSearchHits(rows)
}

func (s *SqlStore) searchGlobalBooksSubstring(ctx context.Context, keyword string, limit int32) ([]BookSearchHit, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			b.id, b.series_id, b.library_id, b.name, b.path, b.size, b.file_modified_at, b.volume, b.title, b.summary, b.number, b.sort_number, b.page_count, b.cover_path, b.last_read_page, b.last_read_at, b.file_hash, b.quick_hash, b.path_fingerprint, b.path_fingerprint_no_ext, b.filename_fingerprint, b.created_at, b.updated_at,
			s.name AS series_name,
			s.title AS series_title,
			CASE
				WHEN lower(b.name) = lower(?) OR lower(COALESCE(b.title, '')) = lower(?) THEN 4.0
				WHEN lower(s.name) = lower(?) OR lower(COALESCE(s.title, '')) = lower(?) THEN 3.5
				WHEN instr(lower(b.name), lower(?)) > 0 OR instr(lower(COALESCE(b.title, '')), lower(?)) > 0 THEN 3.0
				WHEN instr(lower(s.name), lower(?)) > 0 OR instr(lower(COALESCE(s.title, '')), lower(?)) > 0 THEN 2.5
				ELSE 1.0
			END AS score
		FROM books b
		JOIN series s ON s.id = b.series_id
		WHERE instr(lower(b.name), lower(?)) > 0
		   OR instr(lower(COALESCE(b.title, '')), lower(?)) > 0
		   OR instr(lower(s.name), lower(?)) > 0
		   OR instr(lower(COALESCE(s.title, '')), lower(?)) > 0
		   OR instr(lower(b.path), lower(?)) > 0
		ORDER BY
			CASE
				WHEN lower(b.name) = lower(?) OR lower(COALESCE(b.title, '')) = lower(?) THEN 0
				WHEN lower(s.name) = lower(?) OR lower(COALESCE(s.title, '')) = lower(?) THEN 1
				WHEN instr(lower(b.name), lower(?)) > 0 OR instr(lower(COALESCE(b.title, '')), lower(?)) > 0 THEN 2
				WHEN instr(lower(s.name), lower(?)) > 0 OR instr(lower(COALESCE(s.title, '')), lower(?)) > 0 THEN 3
				ELSE 4
			END,
			s.name COLLATE NOCASE ASC,
			b.volume ASC,
			b.sort_number ASC,
			b.name COLLATE NOCASE ASC
		LIMIT ?
	`, keyword, keyword, keyword, keyword, keyword, keyword, keyword, keyword, keyword, keyword, keyword, keyword, keyword, keyword, keyword, keyword, keyword, keyword, keyword, keyword, keyword, limit)
	if err != nil {
		return nil, err
	}
	return scanBookSearchHits(rows)
}

func (s *SqlStore) searchGlobalSeriesFTS(ctx context.Context, keyword string, limit int32) ([]SeriesSearchHit, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			s.id, s.library_id, s.name, s.title, s.summary, s.publisher, s.status, s.rating, s.language, s.locked_fields, s.name_initial, s.path, s.created_at, s.updated_at, s.is_favorite, s.volume_count, s.book_count, s.total_pages,
			ss.cover_path,
			COALESCE(ss.tag_names_cache, '') as tags_string,
			COALESCE(ss.read_pages, 0) as read_count,
			ss.last_read_at,
			NULLIF(ss.last_read_book_id, 0) AS last_read_book_id,
			(SELECT b2.last_read_page FROM books b2 WHERE b2.id = ss.last_read_book_id) AS last_read_page,
			(
				CASE
					WHEN lower(s.name) = lower(?) OR lower(COALESCE(s.title, '')) = lower(?) THEN 3.0
					WHEN instr(lower(s.name), lower(?)) > 0 OR instr(lower(COALESCE(s.title, '')), lower(?)) > 0 THEN 2.0
					ELSE 1.0
				END
				+ (1.0 / (1.0 + ABS(m.rank)))
			) AS score
		FROM (
			SELECT rowid, bm25(series_search_fts, 2.0, 3.0) AS rank
			FROM series_search_fts
			WHERE series_search_fts MATCH ?
		) m
		JOIN series s ON s.id = m.rowid
		LEFT JOIN series_stats ss ON ss.series_id = s.id
		ORDER BY
			CASE
				WHEN lower(s.name) = lower(?) OR lower(COALESCE(s.title, '')) = lower(?) THEN 0
				WHEN instr(lower(s.name), lower(?)) > 0 OR instr(lower(COALESCE(s.title, '')), lower(?)) > 0 THEN 1
				ELSE 2
			END,
			m.rank ASC,
			COALESCE(NULLIF(s.title, ''), s.name) COLLATE NOCASE ASC
		LIMIT ?
	`, keyword, keyword, keyword, keyword, fts5PhraseQuery(keyword), keyword, keyword, keyword, keyword, limit)
	if err != nil {
		return nil, err
	}
	return scanSeriesSearchHits(rows)
}

func (s *SqlStore) searchGlobalSeriesSubstring(ctx context.Context, keyword string, limit int32) ([]SeriesSearchHit, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			s.id, s.library_id, s.name, s.title, s.summary, s.publisher, s.status, s.rating, s.language, s.locked_fields, s.name_initial, s.path, s.created_at, s.updated_at, s.is_favorite, s.volume_count, s.book_count, s.total_pages,
			ss.cover_path,
			COALESCE(ss.tag_names_cache, '') as tags_string,
			COALESCE(ss.read_pages, 0) as read_count,
			ss.last_read_at,
			NULLIF(ss.last_read_book_id, 0) AS last_read_book_id,
			(SELECT b2.last_read_page FROM books b2 WHERE b2.id = ss.last_read_book_id) AS last_read_page,
			CASE
				WHEN lower(s.name) = lower(?) OR lower(COALESCE(s.title, '')) = lower(?) THEN 4.0
				WHEN instr(lower(s.name), lower(?)) > 0 OR instr(lower(COALESCE(s.title, '')), lower(?)) > 0 THEN 3.0
				ELSE 1.0
			END AS score
		FROM series s
		LEFT JOIN series_stats ss ON ss.series_id = s.id
		WHERE instr(lower(s.name), lower(?)) > 0
		   OR instr(lower(COALESCE(s.title, '')), lower(?)) > 0
		   OR instr(lower(s.path), lower(?)) > 0
		ORDER BY
			CASE
				WHEN lower(s.name) = lower(?) OR lower(COALESCE(s.title, '')) = lower(?) THEN 0
				WHEN instr(lower(s.name), lower(?)) > 0 OR instr(lower(COALESCE(s.title, '')), lower(?)) > 0 THEN 1
				ELSE 2
			END,
			COALESCE(NULLIF(s.title, ''), s.name) COLLATE NOCASE ASC
		LIMIT ?
	`, keyword, keyword, keyword, keyword, keyword, keyword, keyword, keyword, keyword, keyword, keyword, limit)
	if err != nil {
		return nil, err
	}
	return scanSeriesSearchHits(rows)
}

func seriesSearchFTSEligible(keyword string) bool {
	return len([]rune(strings.TrimSpace(keyword))) >= 3
}

func fts5PhraseQuery(keyword string) string {
	return `"` + strings.ReplaceAll(strings.TrimSpace(keyword), `"`, `""`) + `"`
}

func buildSeriesSearchQuery(libraryID int64, keyword, letter, status string, tags, authors []string) (string, string, []interface{}) {
	baseQuery := `
		SELECT
            s.id, s.library_id, s.name, s.title, s.summary, s.publisher, s.status, s.rating, s.language, s.locked_fields, s.name_initial, s.path, s.created_at, s.updated_at, s.is_favorite, s.volume_count, s.book_count, s.total_pages,
            ss.cover_path,
            COALESCE(ss.tag_names_cache, '') as tags_string,
            COALESCE(ss.read_pages, 0) as read_count,
            ss.last_read_at,
            NULLIF(ss.last_read_book_id, 0) AS last_read_book_id,
            (SELECT b2.last_read_page FROM books b2 WHERE b2.id = ss.last_read_book_id) AS last_read_page
		FROM series s
		LEFT JOIN series_stats ss ON ss.series_id = s.id
	`

	filters := make([]string, 0, 5)
	args := make([]interface{}, 0, 8)
	if libraryID != 0 {
		filters = append(filters, `s.library_id = ?`)
		args = append(args, libraryID)
	}

	if keyword != "" {
		filters = append(filters, `(instr(lower(s.name), lower(?)) > 0 OR instr(lower(COALESCE(s.title, '')), lower(?)) > 0)`)
		args = append(args, keyword, keyword)
	}

	if status != "" {
		filters = append(filters, `s.status = ?`)
		args = append(args, status)
	}

	if letter != "" {
		normalizedLetter := strings.ToUpper(strings.TrimSpace(letter))
		if normalizedLetter != "" {
			if normalizedLetter == "#" {
				filters = append(filters, `s.name_initial = '#'`)
			} else {
				filters = append(filters, `s.name_initial = ?`)
				args = append(args, normalizedLetter)
			}
		}
	}

	if len(tags) > 0 {
		filter := `EXISTS (
			SELECT 1
			FROM series_tags st
			JOIN tags t ON st.tag_id = t.id
			WHERE st.series_id = s.id AND t.name IN (`
		for i, tag := range tags {
			if i > 0 {
				filter += `, `
			}
			filter += `?`
			args = append(args, tag)
		}
		filter += `))`
		filters = append(filters, filter)
	}

	if len(authors) > 0 {
		filter := `EXISTS (
			SELECT 1
			FROM series_authors sa
			JOIN authors a ON sa.author_id = a.id
			WHERE sa.series_id = s.id AND a.name IN (`
		for i, author := range authors {
			if i > 0 {
				filter += `, `
			}
			filter += `?`
			args = append(args, author)
		}
		filter += `))`
		filters = append(filters, filter)
	}

	whereClause := ""
	if len(filters) > 0 {
		whereClause = " WHERE " + strings.Join(filters, " AND ")
	}
	return baseQuery, whereClause, args
}

func scanSearchSeriesPagedRows(rows *sql.Rows) ([]SearchSeriesPagedRow, error) {
	defer rows.Close()

	var items []SearchSeriesPagedRow
	for rows.Next() {
		var i SearchSeriesPagedRow
		if err := rows.Scan(
			&i.ID,
			&i.LibraryID,
			&i.Name,
			&i.Title,
			&i.Summary,
			&i.Publisher,
			&i.Status,
			&i.Rating,
			&i.Language,
			&i.LockedFields,
			&i.NameInitial,
			&i.Path,
			&i.CreatedAt,
			&i.UpdatedAt,
			&i.IsFavorite,
			&i.VolumeCount,
			&i.BookCount,
			&i.TotalPages,
			&i.CoverPath,
			&i.TagsString,
			&i.ReadCount,
			&i.LastReadAt,
			&i.LastReadBookID,
			&i.LastReadPage,
		); err != nil {
			return nil, err
		}
		i.ActualBookCount = int(i.BookCount)
		items = append(items, i)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func scanSeriesSearchHits(rows *sql.Rows) ([]SeriesSearchHit, error) {
	defer rows.Close()

	var items []SeriesSearchHit
	for rows.Next() {
		var i SeriesSearchHit
		if err := rows.Scan(
			&i.ID,
			&i.LibraryID,
			&i.Name,
			&i.Title,
			&i.Summary,
			&i.Publisher,
			&i.Status,
			&i.Rating,
			&i.Language,
			&i.LockedFields,
			&i.NameInitial,
			&i.Path,
			&i.CreatedAt,
			&i.UpdatedAt,
			&i.IsFavorite,
			&i.VolumeCount,
			&i.BookCount,
			&i.TotalPages,
			&i.CoverPath,
			&i.TagsString,
			&i.ReadCount,
			&i.LastReadAt,
			&i.LastReadBookID,
			&i.LastReadPage,
			&i.Score,
		); err != nil {
			return nil, err
		}
		i.ActualBookCount = int(i.BookCount)
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func scanBookSearchHits(rows *sql.Rows) ([]BookSearchHit, error) {
	defer rows.Close()

	var items []BookSearchHit
	for rows.Next() {
		var i BookSearchHit
		if err := rows.Scan(
			&i.ID,
			&i.SeriesID,
			&i.LibraryID,
			&i.Name,
			&i.Path,
			&i.Size,
			&i.FileModifiedAt,
			&i.Volume,
			&i.Title,
			&i.Summary,
			&i.Number,
			&i.SortNumber,
			&i.PageCount,
			&i.CoverPath,
			&i.LastReadPage,
			&i.LastReadAt,
			&i.FileHash,
			&i.QuickHash,
			&i.PathFingerprint,
			&i.PathFingerprintNoExt,
			&i.FilenameFingerprint,
			&i.CreatedAt,
			&i.UpdatedAt,
			&i.SeriesName,
			&i.SeriesTitle,
			&i.Score,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func scanProtocolSeriesRows(rows *sql.Rows) ([]ProtocolSeriesRow, error) {
	defer rows.Close()

	var items []ProtocolSeriesRow
	for rows.Next() {
		var item ProtocolSeriesRow
		if err := rows.Scan(
			&item.ID,
			&item.LibraryID,
			&item.Name,
			&item.Title,
			&item.Summary,
			&item.Status,
			&item.BookCount,
			&item.TotalPages,
			&item.CoverPath,
			&item.CoverBookID,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func parseSeriesSearchSort(sortBy string) seriesSearchSort {
	spec := seriesSearchSort{Field: "name", Dir: "ASC", Expr: "s.name"}
	parts := strings.Split(sortBy, "_")
	if len(parts) != 2 {
		return spec
	}

	field, dir := parts[0], strings.ToUpper(parts[1])
	if dir != "ASC" && dir != "DESC" {
		dir = "ASC"
	}
	spec.Field = field
	spec.Dir = dir
	switch field {
	case "rating":
		spec.Expr = "s.rating"
	case "books":
		spec.Expr = "s.book_count"
	case "volumes":
		spec.Expr = "s.volume_count"
	case "pages":
		spec.Expr = "s.total_pages"
	case "read":
		spec.Expr = "COALESCE(ss.read_pages, 0)"
	case "created":
		spec.Expr = "s.created_at"
	case "updated":
		spec.Expr = "s.updated_at"
	case "favorite":
		spec.Expr = "s.is_favorite"
	case "name":
		spec.Expr = "s.name"
	default:
		spec.Field = "name"
		spec.Expr = "s.name"
	}
	return spec
}

func (s seriesSearchSort) supportsCursor() bool {
	switch s.Field {
	case "name", "updated", "created", "favorite":
		return true
	default:
		return false
	}
}

func SeriesSearchSortSupportsCursor(sortBy string) bool {
	return parseSeriesSearchSort(sortBy).supportsCursor()
}

func NextSeriesSearchCursor(sortBy string, row SearchSeriesPagedRow) string {
	sortSpec := parseSeriesSearchSort(sortBy)
	if !sortSpec.supportsCursor() {
		return ""
	}
	return encodeSeriesCursor(sortSpec, row)
}

func seriesSearchSortKey(s seriesSearchSort) string {
	return s.Field + "_" + strings.ToLower(s.Dir)
}

func seriesSearchOffsetOrderClause(s seriesSearchSort) string {
	switch s.Field {
	case "rating", "books", "volumes", "pages", "read", "created", "updated":
		return fmt.Sprintf("%s %s, s.name ASC, s.id ASC", s.Expr, s.Dir)
	case "favorite":
		return fmt.Sprintf("s.is_favorite %s, s.name ASC, s.id ASC", s.Dir)
	case "name":
		return fmt.Sprintf("s.name %s, s.id %s", s.Dir, s.Dir)
	default:
		return "s.name ASC, s.id ASC"
	}
}

func seriesSearchCursorOrderClause(s seriesSearchSort) string {
	if s.Field == "name" {
		return fmt.Sprintf("s.name %s, s.id %s", s.Dir, s.Dir)
	}
	return fmt.Sprintf("%s %s, s.name ASC, s.id ASC", s.Expr, s.Dir)
}

func seriesSearchSeekClause(s seriesSearchSort, cursor seriesCursorPayload) (string, []interface{}) {
	if s.Field == "name" {
		operator := ">"
		if s.Dir == "DESC" {
			operator = "<"
		}
		return fmt.Sprintf(`(s.name %s ? OR (s.name = ? AND s.id %s ?))`, operator, operator), []interface{}{cursor.Name, cursor.Name, cursor.ID}
	}

	operator := ">"
	if s.Dir == "DESC" {
		operator = "<"
	}
	value := interface{}(cursor.Value)
	switch s.Field {
	case "updated", "created":
		if parsed, err := time.Parse(time.RFC3339Nano, cursor.Value); err == nil {
			value = parsed
		}
	case "favorite":
		if parsed, err := strconv.Atoi(cursor.Value); err == nil {
			value = parsed
		}
	}
	return fmt.Sprintf(`(%s %s ? OR (%s = ? AND (s.name > ? OR (s.name = ? AND s.id > ?))))`, s.Expr, operator, s.Expr),
		[]interface{}{value, value, cursor.Name, cursor.Name, cursor.ID}
}

func encodeSeriesCursor(s seriesSearchSort, row SearchSeriesPagedRow) string {
	payload := seriesCursorPayload{
		SortBy: seriesSearchSortKey(s),
		Name:   row.Name,
		ID:     row.ID,
	}
	switch s.Field {
	case "updated":
		payload.Value = row.UpdatedAt.Format(time.RFC3339Nano)
	case "created":
		payload.Value = row.CreatedAt.Format(time.RFC3339Nano)
	case "favorite":
		if row.IsFavorite {
			payload.Value = "1"
		} else {
			payload.Value = "0"
		}
	default:
		payload.Value = row.Name
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeSeriesCursor(cursor string) (seriesCursorPayload, error) {
	var payload seriesCursorPayload
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return payload, err
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return payload, err
	}
	if payload.SortBy == "" || payload.ID <= 0 {
		return payload, fmt.Errorf("invalid series cursor")
	}
	return payload, nil
}

// GetDashboardStats 一次性拿到全局统计看板所需的聚合数据（组合结构性+阅读类统计）
func (s *SqlStore) GetDashboardStats(ctx context.Context) (*DashboardStats, error) {
	structural, err := s.GetDashboardStructuralStats(ctx)
	if err != nil {
		return nil, err
	}
	volatile, err := s.GetDashboardVolatileStats(ctx)
	if err != nil {
		return nil, err
	}
	return &DashboardStats{
		TotalSeries:  structural.TotalSeries,
		TotalBooks:   structural.TotalBooks,
		TotalPages:   structural.TotalPages,
		LibrarySizes: structural.LibrarySizes,
		ReadBooks:    volatile.ReadBooks,
		ActiveDays7:  volatile.ActiveDays7,
	}, nil
}

// GetDashboardStructuralStats 计算仅随扫描增删书/库变化的结构性统计。
// total_books / total_pages 是对 books 表的全表扫描，代价高，调用方应缓存且仅在扫描后失效。
func (s *SqlStore) GetDashboardStructuralStats(ctx context.Context) (*DashboardStructuralStats, error) {
	var stats DashboardStructuralStats
	var totalPages any
	err := s.db.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM series) AS total_series,
			(SELECT COUNT(*) FROM books) AS total_books,
			(SELECT COALESCE(SUM(page_count), 0) FROM books) AS total_pages
	`).Scan(&stats.TotalSeries, &stats.TotalBooks, &totalPages)
	if err != nil {
		return nil, err
	}
	switch v := totalPages.(type) {
	case int64:
		stats.TotalPages = int(v)
	case float64:
		stats.TotalPages = int(v)
	}

	sizeRows, err := s.Queries.ListLibrarySizes(ctx)
	if err == nil {
		sizes := make([]LibrarySize, 0, len(sizeRows))
		for _, r := range sizeRows {
			sizes = append(sizes, LibrarySize{
				LibraryID:   r.LibraryID,
				LibraryName: r.LibraryName,
				TotalSize:   int64(r.TotalSize),
			})
		}
		stats.LibrarySizes = sizes
	}

	return &stats, nil
}

// GetDashboardVolatileStats 计算随阅读进度高频变化的统计，均走索引/时间窗口，代价低。
func (s *SqlStore) GetDashboardVolatileStats(ctx context.Context) (*DashboardVolatileStats, error) {
	var stats DashboardVolatileStats
	err := s.db.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM books WHERE last_read_page > 0) AS read_books,
			(SELECT COUNT(DISTINCT date) FROM reading_activity WHERE date >= DATE('now', '-7 days')) AS active_days_7
	`).Scan(&stats.ReadBooks, &stats.ActiveDays7)
	if err != nil {
		return nil, err
	}
	return &stats, nil
}

// ActivityDay 代表某一天的阅读活跃数据
type ActivityDay struct {
	Date      string `json:"date"`       // YYYY-MM-DD
	PageCount int    `json:"page_count"` // 当天阅读的总页数
}

func normalizeFacetLimit(limit int) int {
	if limit <= 0 {
		return 30
	}
	if limit > 100 {
		return 100
	}
	return limit
}

func (s *SqlStore) SearchTags(ctx context.Context, query string, limit int) ([]Tag, error) {
	limit = normalizeFacetLimit(limit)
	query = strings.TrimSpace(query)
	args := make([]any, 0, 2)
	where := ""
	if query != "" {
		where = "WHERE lower(name) LIKE ?"
		args = append(args, "%"+strings.ToLower(query)+"%")
	}
	args = append(args, limit)

	// series_count 由 series_tags 触发器维护，按其倒序走 idx_tags_series_count，
	// 避免对 25000+ 标签做 LEFT JOIN + GROUP BY + COUNT 全量聚合。
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, created_at
		FROM tags
		`+where+`
		ORDER BY series_count DESC, name ASC
		LIMIT ?
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Tag, 0)
	for rows.Next() {
		var item Tag
		if err := rows.Scan(&item.ID, &item.Name, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *SqlStore) SearchAuthors(ctx context.Context, query string, limit int) ([]Author, error) {
	limit = normalizeFacetLimit(limit)
	query = strings.TrimSpace(query)
	args := make([]any, 0, 2)
	where := ""
	if query != "" {
		where = "WHERE lower(a.name) LIKE ?"
		args = append(args, "%"+strings.ToLower(query)+"%")
	}
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, `
		WITH ranked_authors AS (
			SELECT
				a.id,
				a.name,
				a.role,
				a.created_at,
				COUNT(sa.series_id) AS usage_count,
				ROW_NUMBER() OVER (
					PARTITION BY lower(a.name)
					ORDER BY COUNT(sa.series_id) DESC, a.id ASC
				) AS rank
			FROM authors a
			LEFT JOIN series_authors sa ON sa.author_id = a.id
			`+where+`
			GROUP BY a.id
		)
		SELECT id, name, role, created_at
		FROM ranked_authors
		WHERE rank = 1
		ORDER BY usage_count DESC, name ASC
		LIMIT ?
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Author, 0)
	for rows.Next() {
		var item Author
		if err := rows.Scan(&item.ID, &item.Name, &item.Role, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *SqlStore) ListProtocolSeriesByIDs(ctx context.Context, ids []int64) ([]ProtocolSeriesRow, error) {
	if len(ids) == 0 {
		return []ProtocolSeriesRow{}, nil
	}

	placeholders := make([]string, 0, len(ids))
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}
	if len(args) == 0 {
		return []ProtocolSeriesRow{}, nil
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			s.id,
			s.library_id,
			s.name,
			COALESCE(s.title, '') AS title,
			COALESCE(s.summary, '') AS summary,
			COALESCE(s.status, '') AS status,
			s.book_count,
			s.total_pages,
			COALESCE(ss.cover_path, '') AS cover_path,
			COALESCE(ss.cover_book_id, 0) AS cover_book_id,
			s.created_at,
			s.updated_at
		FROM series s
		LEFT JOIN series_stats ss ON ss.series_id = s.id
		WHERE s.id IN (`+strings.Join(placeholders, ",")+`)
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byID := make(map[int64]ProtocolSeriesRow, len(args))
	for rows.Next() {
		var item ProtocolSeriesRow
		if err := rows.Scan(
			&item.ID,
			&item.LibraryID,
			&item.Name,
			&item.Title,
			&item.Summary,
			&item.Status,
			&item.BookCount,
			&item.TotalPages,
			&item.CoverPath,
			&item.CoverBookID,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		byID[item.ID] = item
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	items := make([]ProtocolSeriesRow, 0, len(byID))
	for _, id := range ids {
		if item, ok := byID[id]; ok {
			items = append(items, item)
		}
	}
	return items, nil
}
