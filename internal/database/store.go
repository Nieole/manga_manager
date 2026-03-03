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
	UpdateSeriesMetadata(ctx context.Context, arg UpdateSeriesMetadataParams) (Series, error)
	ExecTx(ctx context.Context, fn func(*Queries) error) error
	SearchSeriesPaged(ctx context.Context, libraryID int64, letter, status string, tags, authors []string, limit, offset int32, sortBy string) ([]SearchSeriesPagedRow, int, error)
	GetDashboardStats(ctx context.Context) (*DashboardStats, error)
	GetRecentReadAll(ctx context.Context, limit int64) ([]RecentReadAllRow, error)
	GetRecommendations(ctx context.Context, limit int) ([]RecommendedSeries, error)
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

	_, err = db.Exec(schemaSQL)
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
			SELECT series_id, COUNT(*) as read_count FROM books WHERE last_read_page > 0 GROUP BY series_id
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
			orderClause = fmt.Sprintf("actual_book_count %s, s.name ASC", dir)
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
			(SELECT COUNT(DISTINCT DATE(last_read_at)) FROM books WHERE last_read_at >= DATE('now', '-7 days') AND last_read_page > 0) as active_days_7
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
