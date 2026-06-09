// 业务说明：本文件是业务实现，属于数据库查询计划辅助工具，用于观察 SQL 索引命中和复杂查询执行路径。
// 它支撑搜索、资料库筛选和统计接口的性能分析。
// 维护时应确保输出能对应到 sqlc 查询和实际业务筛选条件。

package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	_ "modernc.org/sqlite"
)

type planCase struct {
	name     string
	query    string
	args     []any
	expected []string
}

func main() {
	dbPath := flag.String("db", "data/sample.db", "SQLite database path")
	libraryID := flag.Int64("library", 1, "library id used by library-scoped plans")
	seriesID := flag.Int64("series", 1, "series id used by book-scoped plans")
	strict := flag.Bool("strict", false, "exit non-zero when a plan does not mention the expected index")
	flag.Parse()

	db, err := sql.Open("sqlite", *dbPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		log.Fatalf("ping database: %v", err)
	}

	cases := []planCase{
		{
			name:     "series/library/name",
			query:    `SELECT id, name FROM series WHERE library_id = ? ORDER BY name ASC LIMIT 50`,
			args:     []any{*libraryID},
			expected: []string{"idx_series_library_name"},
		},
		{
			name:     "series/library/initial/name",
			query:    `SELECT id, name FROM series WHERE library_id = ? AND name_initial = ? ORDER BY name ASC LIMIT 50`,
			args:     []any{*libraryID, "A"},
			expected: []string{"idx_series_library_initial_name", "idx_series_library_initial"},
		},
		{
			name:     "series/library/status/books",
			query:    `SELECT id, name FROM series WHERE library_id = ? AND status = ? ORDER BY book_count DESC, name ASC LIMIT 50`,
			args:     []any{*libraryID, "COMPLETED"},
			expected: []string{"idx_series_library_status_books", "idx_series_library_status"},
		},
		{
			name:     "series/library/updated",
			query:    `SELECT id, name FROM series WHERE library_id = ? ORDER BY updated_at DESC, name ASC LIMIT 50`,
			args:     []any{*libraryID},
			expected: []string{"idx_series_library_updated_name", "idx_series_library_updated"},
		},
		{
			name:     "series/library/rating",
			query:    `SELECT id, name FROM series WHERE library_id = ? ORDER BY rating DESC, name ASC LIMIT 50`,
			args:     []any{*libraryID},
			expected: []string{"idx_series_library_rating"},
		},
		{
			name:     "series/library/pages",
			query:    `SELECT id, name FROM series WHERE library_id = ? ORDER BY total_pages DESC, name ASC LIMIT 50`,
			args:     []any{*libraryID},
			expected: []string{"idx_series_library_pages"},
		},
		{
			name:     "series/library/favorite",
			query:    `SELECT id, name FROM series WHERE library_id = ? ORDER BY is_favorite DESC, name ASC LIMIT 50`,
			args:     []any{*libraryID},
			expected: []string{"idx_series_library_favorite"},
		},
		{
			name:     "home/default-updated",
			query:    `SELECT s.id, s.name, ss.cover_path, ss.tag_names_cache, ss.read_pages FROM series s LEFT JOIN series_stats ss ON ss.series_id = s.id WHERE s.library_id = ? ORDER BY s.updated_at DESC, s.name ASC LIMIT 50`,
			args:     []any{*libraryID},
			expected: []string{"idx_series_library_updated_name", "idx_series_library_updated"},
		},
		{
			name:     "home/name",
			query:    `SELECT s.id, s.name, ss.cover_path, ss.tag_names_cache, ss.read_pages FROM series s LEFT JOIN series_stats ss ON ss.series_id = s.id WHERE s.library_id = ? ORDER BY s.name ASC LIMIT 50 OFFSET 0`,
			args:     []any{*libraryID},
			expected: []string{"idx_series_library_name"},
		},
		{
			name:     "home/created",
			query:    `SELECT s.id, s.name, ss.cover_path, ss.tag_names_cache, ss.read_pages FROM series s LEFT JOIN series_stats ss ON ss.series_id = s.id WHERE s.library_id = ? ORDER BY s.created_at DESC, s.name ASC LIMIT 50 OFFSET 0`,
			args:     []any{*libraryID},
			expected: []string{"idx_series_library_created_name", "idx_series_library_created"},
		},
		{
			name:     "home/favorite",
			query:    `SELECT s.id, s.name, ss.cover_path, ss.tag_names_cache, ss.read_pages FROM series s LEFT JOIN series_stats ss ON ss.series_id = s.id WHERE s.library_id = ? ORDER BY s.is_favorite DESC, s.name ASC LIMIT 50 OFFSET 0`,
			args:     []any{*libraryID},
			expected: []string{"idx_series_library_favorite"},
		},
		{
			name:     "series-detail/books",
			query:    `SELECT id, name FROM books WHERE series_id = ? ORDER BY volume ASC, sort_number ASC, name ASC`,
			args:     []any{*seriesID},
			expected: []string{"idx_books_series_sort"},
		},
		{
			name:     "books/series/cover-pick",
			query:    `SELECT cover_path FROM books WHERE series_id = ? AND cover_path IS NOT NULL AND cover_path != '' ORDER BY sort_number ASC, name ASC LIMIT 1`,
			args:     []any{*seriesID},
			expected: []string{"idx_books_cover_pick"},
		},
		{
			name:     "books/read-progress",
			query:    `SELECT series_id, SUM(last_read_page) FROM books WHERE last_read_page > 0 GROUP BY series_id LIMIT 50`,
			expected: []string{"idx_books_series_read", "idx_books_read_progress_series"},
		},
		{
			name:     "recent-read/all",
			query:    `SELECT s.name, s.id, b.id, b.name, b.title, ss.last_read_at, b.last_read_page, b.page_count, COALESCE(ss.cover_path, '') FROM series_stats ss INDEXED BY idx_series_stats_last_read JOIN series s ON s.id = ss.series_id JOIN books b ON b.id = ss.last_read_book_id WHERE ss.last_read_at IS NOT NULL AND ss.last_read_book_id > 0 AND b.last_read_page IS NOT NULL AND b.last_read_page > 0 ORDER BY ss.last_read_at DESC, s.name ASC LIMIT 10`,
			expected: []string{"idx_series_stats_last_read"},
		},
		{
			name:     "dashboard/stats-counts",
			query:    `SELECT (SELECT COUNT(*) FROM series), (SELECT COUNT(*) FROM books), (SELECT COUNT(*) FROM books WHERE last_read_page > 0), (SELECT COALESCE(SUM(page_count), 0) FROM books), (SELECT COUNT(DISTINCT date) FROM reading_activity WHERE date >= DATE('now', '-7 days'))`,
			expected: []string{"idx_books_read_progress_series", "idx_reading_activity_date"},
		},
		{
			name:     "dashboard/library-sizes",
			query:    `SELECT l.id, l.name, COALESCE(bs.total_size, 0) FROM libraries l LEFT JOIN (SELECT library_id, SUM(size) as total_size FROM books INDEXED BY idx_books_library_size GROUP BY library_id) bs ON bs.library_id = l.id ORDER BY bs.total_size DESC`,
			expected: []string{"idx_books_library_size"},
		},
	}

	ctx := context.Background()
	failures := 0
	for _, item := range cases {
		details, err := explain(ctx, db, item)
		if err != nil {
			log.Fatalf("explain %s: %v", item.name, err)
		}
		matched := matchesExpected(details, item.expected)
		if !matched {
			failures++
		}
		printPlan(item, details, matched)
	}

	if *strict && failures > 0 {
		fmt.Fprintf(os.Stderr, "query plans missing expected indexes: %d\n", failures)
		os.Exit(1)
	}
}

func explain(ctx context.Context, db *sql.DB, item planCase) ([]string, error) {
	rows, err := db.QueryContext(ctx, "EXPLAIN QUERY PLAN "+item.query, item.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var details []string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			return nil, err
		}
		details = append(details, detail)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return details, nil
}

func matchesExpected(details, expected []string) bool {
	if len(expected) == 0 {
		return true
	}
	joined := strings.Join(details, "\n")
	for _, indexName := range expected {
		if strings.Contains(joined, indexName) {
			return true
		}
	}
	return false
}

func printPlan(item planCase, details []string, matched bool) {
	status := "ok"
	if !matched {
		status = "warn"
	}
	fmt.Printf("[%s] %s\n", status, item.name)
	if len(item.expected) > 0 {
		fmt.Printf("  expected: %s\n", strings.Join(item.expected, " or "))
	}
	for _, detail := range details {
		fmt.Printf("  - %s\n", detail)
	}
}
