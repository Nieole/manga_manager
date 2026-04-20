package database

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"runtime"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

type Store interface {
	Querier
	Close() error
	ListExternalLibraryBooksByLibrary(ctx context.Context, libraryID int64) ([]ExternalLibraryBookRow, error)
	UpdateSeriesMetadata(ctx context.Context, arg UpdateSeriesMetadataParams) (Series, error)
	ExecTx(ctx context.Context, fn func(*Queries) error) error
	SearchSeriesPaged(ctx context.Context, libraryID int64, letter, status string, tags, authors []string, limit, offset int32, sortBy string) ([]SearchSeriesPagedRow, int, error)
	GetDashboardStats(ctx context.Context) (*DashboardStats, error)
	GetActivityHeatmap(ctx context.Context, weeks int) ([]ActivityDay, error)
	LogReadingActivity(ctx context.Context, bookID int64, pagesRead int) error
	GetRecentReadAll(ctx context.Context, limit int64) ([]RecentReadAllRow, error)
	GetRecommendations(ctx context.Context, limit int) ([]RecommendedSeries, error)
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
	CountBooksMissingIdentity(ctx context.Context, matchMode string) (int64, error)
	CountUnmatchedKOReaderProgress(ctx context.Context) (int64, error)
	FindBookByDocumentFingerprint(ctx context.Context, documentKey, matchMode string, pathIgnoreExtension bool) (KOReaderBookMatch, error)
	UpsertKOReaderProgress(ctx context.Context, arg UpsertKOReaderProgressParams) (KOReaderProgress, error)
	GetKOReaderProgress(ctx context.Context, username, document string) (KOReaderProgress, error)
	ListBooksMissingIdentityBatch(ctx context.Context, matchMode string, afterID int64, limit int) ([]BookIdentityCandidate, error)
	UpdateBookIdentity(ctx context.Context, arg UpdateBookIdentityParams) error
	ListUnmatchedKOReaderProgress(ctx context.Context, limit int) ([]KOReaderProgress, error)
	ListUnmatchedKOReaderProgressBatch(ctx context.Context, afterID int64, limit int) ([]KOReaderProgress, error)
	LinkKOReaderProgressToBook(ctx context.Context, progressID, bookID int64, matchedBy string) error
	CreateKOReaderSyncEvent(ctx context.Context, arg CreateKOReaderSyncEventParams) error
}

type DashboardStats struct {
	TotalSeries int `json:"total_series"`
	TotalBooks  int `json:"total_books"`
	ReadBooks   int `json:"read_books"`
	TotalPages  int `json:"total_pages"`
	ActiveDays7 int `json:"active_days_7"` // 最近7天有阅读行为的天数
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
	dsn := dbPath + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)" +
		"&_pragma=mmap_size=268435456&_pragma=cache_size=-128000&_pragma=busy_timeout=15000&_pragma=temp_store=2"

	db, err := sql.Open("sqlite", dsn)
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

// 供启动时执行迁移
func Migrate(dbPath string) error {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	if _, err = db.Exec(schemaSQL); err != nil {
		return err
	}

	for _, column := range []struct {
		table      string
		name       string
		definition string
	}{
		{table: "libraries", name: "koreader_sync_enabled", definition: "BOOLEAN NOT NULL DEFAULT TRUE"},
		{table: "books", name: "file_hash", definition: "TEXT"},
		{table: "books", name: "path_fingerprint", definition: "TEXT"},
		{table: "books", name: "path_fingerprint_no_ext", definition: "TEXT"},
	} {
		if err := ensureColumn(db, column.table, column.name, column.definition); err != nil {
			return err
		}
	}

	for _, stmt := range []string{
		`CREATE INDEX IF NOT EXISTS idx_books_file_hash ON books(file_hash)`,
		`CREATE INDEX IF NOT EXISTS idx_books_path_fingerprint ON books(path_fingerprint)`,
		`CREATE INDEX IF NOT EXISTS idx_books_path_fingerprint_no_ext ON books(path_fingerprint_no_ext)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}

	if err := migrateLegacyKOReaderAccounts(db); err != nil {
		return err
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
	if err == sql.ErrNoRows {
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

// SearchSeriesPaged 供主页根据标签和作者进行交集查询并分页
func (s *SqlStore) SearchSeriesPaged(ctx context.Context, libraryID int64, letter, status string, tags, authors []string, limit, offset int32, sortBy string) ([]SearchSeriesPagedRow, int, error) {
	// 构建动态 SQL - 使用预聚合子查询替代关联子查询提升查询性能
	baseQuery := `
		SELECT
            s.*,
            bc.cover_path,
            GROUP_CONCAT(DISTINCT t.name) as tags_string,
            COALESCE(rc.read_count, 0) as read_count
		FROM series s
		LEFT JOIN (
			SELECT series_id, cover_path,
				ROW_NUMBER() OVER (PARTITION BY series_id ORDER BY sort_number, name) as rn
			FROM books WHERE cover_path IS NOT NULL AND cover_path != ''
		) bc ON bc.series_id = s.id AND bc.rn = 1
		LEFT JOIN (
			SELECT series_id, COALESCE(SUM(last_read_page), 0) as read_count FROM books WHERE last_read_page > 0 GROUP BY series_id
		) rc ON rc.series_id = s.id
		LEFT JOIN series_tags st ON s.id = st.series_id
		LEFT JOIN tags t ON st.tag_id = t.id
		LEFT JOIN series_authors sa ON s.id = sa.series_id
		LEFT JOIN authors a ON sa.author_id = a.id
		WHERE s.library_id = ?
	`
	// 因为使用了 GROUP BY，所以不能再外层 COUNT(DISTINCT s.id)，我们需要一个单独的包裹写法
	countFilters := `
		FROM series s
		LEFT JOIN series_tags st ON s.id = st.series_id
		LEFT JOIN tags t ON st.tag_id = t.id
		LEFT JOIN series_authors sa ON s.id = sa.series_id
		LEFT JOIN authors a ON sa.author_id = a.id
		WHERE s.library_id = ?
	`

	args := []interface{}{libraryID}

	if status != "" {
		baseQuery += ` AND s.status = ?`
		countFilters += ` AND s.status = ?`
		args = append(args, status)
	}

	if letter != "" {
		if letter == "#" {
			baseQuery += ` AND UPPER(SUBSTR(s.name, 1, 1)) NOT BETWEEN 'A' AND 'Z'`
			countFilters += ` AND UPPER(SUBSTR(s.name, 1, 1)) NOT BETWEEN 'A' AND 'Z'`
		} else {
			baseQuery += ` AND UPPER(SUBSTR(s.name, 1, 1)) = ?`
			countFilters += ` AND UPPER(SUBSTR(s.name, 1, 1)) = ?`
			args = append(args, strings.ToUpper(letter))
		}
	}

	if len(tags) > 0 {
		baseQuery += ` AND t.name IN (`
		countFilters += ` AND t.name IN (`
		for i, tag := range tags {
			if i > 0 {
				baseQuery += `, `
				countFilters += `, `
			}
			baseQuery += `?`
			countFilters += `?`
			args = append(args, tag)
		}
		baseQuery += `)`
		countFilters += `)`
	}

	if len(authors) > 0 {
		baseQuery += ` AND a.name IN (`
		countFilters += ` AND a.name IN (`
		for i, author := range authors {
			if i > 0 {
				baseQuery += `, `
				countFilters += `, `
			}
			baseQuery += `?`
			countFilters += `?`
			args = append(args, author)
		}
		baseQuery += `)`
		countFilters += `)`
	}

	countQuery := `SELECT COUNT(DISTINCT s.id) ` + countFilters

	// Fetch Total Count First
	var total int
	err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total) // Use all args for count query
	if err != nil {
		return nil, 0, err
	}

	// Dynamic Ordering
	orderClause := "s.name ASC" // Default Sort fallback
	parts := strings.Split(sortBy, "_")
	if len(parts) == 2 {
		field, dir := parts[0], strings.ToUpper(parts[1])
		if dir != "ASC" && dir != "DESC" {
			dir = "ASC"
		}
		switch field {
		case "rating":
			orderClause = fmt.Sprintf("s.rating %s, s.name ASC", dir)
		case "books":
			orderClause = fmt.Sprintf("s.book_count %s, s.name ASC", dir)
		case "created":
			orderClause = fmt.Sprintf("s.created_at %s, s.name ASC", dir)
		case "updated":
			orderClause = fmt.Sprintf("s.updated_at %s, s.name ASC", dir)
		case "name":
			orderClause = fmt.Sprintf("s.name %s", dir)
		case "favorite":
			orderClause = fmt.Sprintf("is_favorite %s, s.name ASC", dir)
		}
	}

	// Finish Paginated Query
	baseQuery += fmt.Sprintf(` GROUP BY s.id ORDER BY %s LIMIT ? OFFSET ?`, orderClause)
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, baseQuery, args...)
	if err != nil {
		return nil, 0, err
	}
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
		); err != nil {
			return nil, 0, err
		}
		i.ActualBookCount = int(i.BookCount)
		items = append(items, i)
	}
	if err := rows.Close(); err != nil {
		return nil, 0, err
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return items, total, nil
}

// GetDashboardStats 一次性拿到全局统计看板所需的聚合数据
func (s *SqlStore) GetDashboardStats(ctx context.Context) (*DashboardStats, error) {
	query := `
		SELECT
			(SELECT COUNT(*) FROM series) as total_series,
			(SELECT COUNT(*) FROM books) as total_books,
			(SELECT COUNT(*) FROM books WHERE last_read_page > 0) as read_books,
			(SELECT COALESCE(SUM(page_count), 0) FROM books) as total_pages,
			(SELECT COUNT(DISTINCT date) FROM reading_activity WHERE date >= DATE('now', '-7 days')) as active_days_7
	`
	var stats DashboardStats
	err := s.db.QueryRowContext(ctx, query).Scan(
		&stats.TotalSeries,
		&stats.TotalBooks,
		&stats.ReadBooks,
		&stats.TotalPages,
		&stats.ActiveDays7,
	)
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

// GetActivityHeatmap 返回最近 weeks 周每天的阅读页数（基于 reading_activity 表精确统计）
func (s *SqlStore) GetActivityHeatmap(ctx context.Context, weeks int) ([]ActivityDay, error) {
	days := weeks * 7
	query := `
		SELECT date, SUM(pages_read) as page_count
		FROM reading_activity
		WHERE date >= DATE('now', ? || ' days')
		GROUP BY date
		ORDER BY date ASC
	`
	offset := fmt.Sprintf("-%d", days)
	rows, err := s.db.QueryContext(ctx, query, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []ActivityDay
	for rows.Next() {
		var d ActivityDay
		if err := rows.Scan(&d.Date, &d.PageCount); err != nil {
			return nil, err
		}
		items = append(items, d)
	}
	return items, rows.Err()
}

// LogReadingActivity 记录一次阅读活动到 reading_activity 表（同 book 同日 UPSERT 累加）
func (s *SqlStore) LogReadingActivity(ctx context.Context, bookID int64, pagesRead int) error {
	query := `
		INSERT INTO reading_activity (book_id, date, pages_read)
		VALUES (?, DATE('now'), ?)
		ON CONFLICT(book_id, date) DO UPDATE SET
			pages_read = MAX(reading_activity.pages_read, excluded.pages_read)
	`
	_, err := s.db.ExecContext(ctx, query, bookID, pagesRead)
	return err
}

// RecentReadAllRow Dashboard 继续阅读列表的简化返回结构
type RecentReadAllRow struct {
	SeriesID     int64          `json:"series_id"`
	SeriesName   string         `json:"series_name"`
	BookID       int64          `json:"book_id"`
	BookName     string         `json:"book_name"`
	BookTitle    sql.NullString `json:"book_title"`
	CoverPath    sql.NullString `json:"cover_path"`
	LastReadPage sql.NullInt64  `json:"last_read_page"`
	LastReadAt   sql.NullTime   `json:"last_read_at"`
	PageCount    int64          `json:"page_count"`
}

// GetRecentReadAll 跨库查询最近阅读的书籍（每个系列取最新一本）
func (s *SqlStore) GetRecentReadAll(ctx context.Context, limit int64) ([]RecentReadAllRow, error) {
	query := `
		WITH RankedBooks AS (
			SELECT
				b.series_id, b.id AS book_id, b.name AS book_name, b.title AS book_title,
				b.last_read_at, b.last_read_page, b.page_count,
				ROW_NUMBER() OVER(PARTITION BY b.series_id ORDER BY b.last_read_at DESC) as rn
			FROM books b
			WHERE b.last_read_at IS NOT NULL AND b.last_read_page > 0
		)
		SELECT
			s.name AS series_name,
			rb.series_id, rb.book_id, rb.book_name, rb.book_title,
			rb.last_read_at, rb.last_read_page, rb.page_count,
			(SELECT b2.cover_path FROM books b2 WHERE b2.series_id = s.id AND b2.cover_path IS NOT NULL AND b2.cover_path != '' ORDER BY b2.sort_number, b2.name LIMIT 1) as cover_path
		FROM RankedBooks rb
		JOIN series s ON s.id = rb.series_id
		WHERE rb.rn = 1
		ORDER BY rb.last_read_at DESC
		LIMIT ?
	`

	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []RecentReadAllRow
	for rows.Next() {
		var i RecentReadAllRow
		if err := rows.Scan(
			&i.SeriesName,
			&i.SeriesID, &i.BookID, &i.BookName, &i.BookTitle,
			&i.LastReadAt, &i.LastReadPage, &i.PageCount,
			&i.CoverPath,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}

// RecommendedSeries 推荐系列的返回结构
type RecommendedSeries struct {
	ID        int64          `json:"id"`
	Name      string         `json:"name"`
	Title     sql.NullString `json:"title"`
	CoverPath sql.NullString `json:"cover_path"`
	BookCount int64          `json:"book_count"`
	Score     int            `json:"score"` // 匹配权重
}

// GetRecommendations 基于用户阅读偏好（收藏 + 已读）的 Tag 权重推荐未读系列
func (s *SqlStore) GetRecommendations(ctx context.Context, limit int) ([]RecommendedSeries, error) {
	// 使用一条 SQL 完成全部逻辑：
	// 1. 从用户偏好系列（收藏或已读 2+ 本的系列）提取 Tag
	// 2. 找到拥有这些 Tag 但未被阅读过的系列
	// 3. 按匹配 Tag 数量降序排列
	query := `
		WITH preferred_tags AS (
			SELECT DISTINCT st.tag_id
			FROM series_tags st
			JOIN series s ON s.id = st.series_id
			WHERE s.is_favorite = 1
			   OR (SELECT COUNT(*) FROM books b WHERE b.series_id = s.id AND b.last_read_page > 0) >= 2
		),
		unread_series AS (
			SELECT s.id
			FROM series s
			WHERE NOT EXISTS (SELECT 1 FROM books b WHERE b.series_id = s.id AND b.last_read_page > 0)
		)
		SELECT s.id, s.name, s.title, s.book_count,
			(SELECT b.cover_path FROM books b WHERE b.series_id = s.id AND b.cover_path IS NOT NULL AND b.cover_path != '' ORDER BY b.sort_number, b.name LIMIT 1) as cover_path,
			COUNT(st.tag_id) as score
		FROM unread_series us
		JOIN series s ON s.id = us.id
		JOIN series_tags st ON st.series_id = s.id
		JOIN preferred_tags pt ON pt.tag_id = st.tag_id
		GROUP BY s.id
		ORDER BY score DESC
		LIMIT ?
	`

	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []RecommendedSeries
	for rows.Next() {
		var i RecommendedSeries
		if err := rows.Scan(&i.ID, &i.Name, &i.Title, &i.BookCount, &i.CoverPath, &i.Score); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}
