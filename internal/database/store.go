package database

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"runtime"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

type Store interface {
	Querier
	Close() error
	UpdateSeriesMetadata(ctx context.Context, arg UpdateSeriesMetadataParams) (Series, error)
	ExecTx(ctx context.Context, fn func(*Queries) error) error
	SearchSeriesPaged(ctx context.Context, libraryID int64, limit, offset int, tags, authors []string, status string) ([]SearchSeriesPagedRow, int, error)
}

type SearchSeriesPagedRow struct {
	Series
	CoverPath       sql.NullString  `json:"cover_path"`
	TagsString      *string         `json:"tags_string"`
	VolumeCount     int             `json:"volume_count"`
	ActualBookCount int             `json:"actual_book_count"`
	ReadCount       int             `json:"read_count"`
	TotalPages      sql.NullFloat64 `json:"total_pages"`
}

type sqlStore struct {
	*Queries
	db *sql.DB
}

func NewStore(dbPath string) (Store, error) {
	// 加载现代 SQLite 对于千兆以上规模及大量随机读取极其友好的调教参数。
	// mmap_size=30000000000 (允许超过系统内存约30GB的超大内存隐射加快搜索页的读取，极大地减轻冷启动延迟)
	// cache_size=-500000  (单独为SQLite划定高达并超过 500MB 的专用查询热缓存页)
	// busy_timeout=15000  (防止在长列表遍历且伴随有并发写入时的死锁退出报错)
	// temp_store=2        (MEMORY：所有临时聚合、ORDER BY 操作与临时表将完全使用内存而非耗损SSD)
	dsn := dbPath + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)" +
		"&_pragma=mmap_size=30000000000&_pragma=cache_size=-500000&_pragma=busy_timeout=15000&_pragma=temp_store=2"

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

	return &sqlStore{
		Queries: New(db),
		db:      db,
	}, nil
}

func (s *sqlStore) Close() error {
	return s.db.Close()
}

// ExecTx 提供一个事务包裹器以进行批量执行，这对防止 SQLite 并发锁极为关键
func (s *sqlStore) ExecTx(ctx context.Context, fn func(*Queries) error) error {
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
func (s *sqlStore) SearchSeriesPaged(ctx context.Context, libraryID int64, limit, offset int, tags, authors []string, status string) ([]SearchSeriesPagedRow, int, error) {
	// 构建动态 SQL
	baseQuery := `
		SELECT 
            s.*,
            (SELECT b.cover_path FROM books b WHERE b.series_id = s.id AND b.cover_path IS NOT NULL AND b.cover_path != '' ORDER BY b.sort_number, b.name LIMIT 1) as cover_path,
            GROUP_CONCAT(DISTINCT t.name) as tags_string,
            COUNT(DISTINCT NULLIF(b.volume, '')) as volume_count,
            COUNT(DISTINCT b.id) as actual_book_count,
            COALESCE(SUM(CASE WHEN b.last_read_page > 1 THEN b.last_read_page ELSE 0 END), 0) as read_count,
            SUM(b.page_count) as total_pages
		FROM series s
		LEFT JOIN series_tags st ON s.id = st.series_id
		LEFT JOIN tags t ON st.tag_id = t.id
		LEFT JOIN series_authors sa ON s.id = sa.series_id
		LEFT JOIN authors a ON sa.author_id = a.id
        LEFT JOIN books b ON s.id = b.series_id
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
	err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	// Finish Paginated Query
	baseQuery += ` GROUP BY s.id ORDER BY s.name LIMIT ? OFFSET ?`
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
			&i.CoverPath,
			&i.TagsString,
			&i.VolumeCount,
			&i.ActualBookCount,
			&i.ReadCount,
			&i.TotalPages,
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
