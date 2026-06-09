// 业务说明：本文件是业务实现，属于 SQLite 数据访问层，负责把漫画库、系列、阅读进度、任务和元数据状态持久化为稳定数据模型。
// 它连接 sqlc 生成查询与上层领域服务，是资料库筛选、搜索同步和关系图谱的数据基础。
// 维护时应保持 schema、查询定义、事务边界和迁移兼容，避免破坏既有用户数据。

package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

type SmartCollectionFilter struct {
	LibraryID       int64
	ActiveLetter    string
	ActiveStatus    string
	ActiveTag       string
	ActiveAuthor    string
	MinRating       *float64
	MaxRating       *float64
	MinProgress     *float64
	MaxProgress     *float64
	AddedWithinDays *int
	ReadState       string
	SortByField     string
	SortDir         string
}

func (s *SqlStore) SearchSmartCollectionSeries(ctx context.Context, filter SmartCollectionFilter, limit, offset int) ([]SearchSeriesPagedRow, int, error) {
	baseQuery, args := buildSmartCollectionBaseQuery(filter)

	countQuery := "SELECT COUNT(DISTINCT s.id) " + baseQuery
	var total int
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	selectQuery := `
		SELECT
			s.id, s.library_id, s.name, s.title, s.summary, s.publisher, s.status, s.rating, s.language, s.locked_fields, s.name_initial, s.path, s.created_at, s.updated_at, s.is_favorite, s.volume_count, s.book_count, s.total_pages,
			bc.cover_path,
			GROUP_CONCAT(DISTINCT t.name) as tags_string,
			COALESCE(rc.read_count, 0) as read_count
	` + baseQuery + fmt.Sprintf(` GROUP BY s.id ORDER BY %s LIMIT ? OFFSET ?`, smartCollectionOrderClause(filter))
	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, limit, offset)

	rows, err := s.db.QueryContext(ctx, selectQuery, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]SearchSeriesPagedRow, 0)
	for rows.Next() {
		var item SearchSeriesPagedRow
		if err := rows.Scan(
			&item.ID,
			&item.LibraryID,
			&item.Name,
			&item.Title,
			&item.Summary,
			&item.Publisher,
			&item.Status,
			&item.Rating,
			&item.Language,
			&item.LockedFields,
			&item.NameInitial,
			&item.Path,
			&item.CreatedAt,
			&item.UpdatedAt,
			&item.IsFavorite,
			&item.VolumeCount,
			&item.BookCount,
			&item.TotalPages,
			&item.CoverPath,
			&item.TagsString,
			&item.ReadCount,
		); err != nil {
			return nil, 0, err
		}
		item.ActualBookCount = int(item.BookCount)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func buildSmartCollectionBaseQuery(filter SmartCollectionFilter) (string, []any) {
	query := `
		FROM series s
		LEFT JOIN (
			SELECT series_id, cover_path,
				ROW_NUMBER() OVER (PARTITION BY series_id ORDER BY sort_number, name) as rn
			FROM books WHERE cover_path IS NOT NULL AND cover_path != ''
		) bc ON bc.series_id = s.id AND bc.rn = 1
		LEFT JOIN (
			SELECT series_id, COALESCE(SUM(last_read_page), 0) as read_count
			FROM books
			WHERE last_read_page > 0
			GROUP BY series_id
		) rc ON rc.series_id = s.id
		LEFT JOIN (
			SELECT
				series_id,
				COUNT(*) as book_count,
				SUM(CASE WHEN last_read_page IS NOT NULL AND last_read_page > 0 THEN 1 ELSE 0 END) as read_books,
				SUM(CASE WHEN page_count > 0 AND last_read_page >= page_count THEN 1 ELSE 0 END) as completed_books,
				CASE
					WHEN SUM(CASE WHEN page_count > 0 THEN page_count ELSE 0 END) > 0
					THEN SUM(CASE WHEN last_read_page IS NOT NULL AND last_read_page > 0 THEN MIN(last_read_page, page_count) ELSE 0 END) * 100.0 / SUM(CASE WHEN page_count > 0 THEN page_count ELSE 0 END)
					ELSE 0
				END as progress_percent
			FROM books
			GROUP BY series_id
		) rp ON rp.series_id = s.id
		LEFT JOIN series_tags st ON s.id = st.series_id
		LEFT JOIN tags t ON st.tag_id = t.id
		LEFT JOIN series_authors sa ON s.id = sa.series_id
		LEFT JOIN authors a ON sa.author_id = a.id
		WHERE s.library_id = ?
	`
	args := []any{filter.LibraryID}

	if value := strings.TrimSpace(filter.ActiveLetter); value != "" {
		query += " AND s.name_initial = ?"
		args = append(args, strings.ToUpper(value))
	}
	if value := strings.TrimSpace(filter.ActiveStatus); value != "" {
		query += " AND s.status = ?"
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.ActiveTag); value != "" {
		query += " AND t.name = ?"
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.ActiveAuthor); value != "" {
		query += " AND a.name = ?"
		args = append(args, value)
	}
	if filter.MinRating != nil {
		query += " AND s.rating >= ?"
		args = append(args, *filter.MinRating)
	}
	if filter.MaxRating != nil {
		query += " AND s.rating <= ?"
		args = append(args, *filter.MaxRating)
	}
	if filter.MinProgress != nil {
		query += " AND COALESCE(rp.progress_percent, 0) >= ?"
		args = append(args, *filter.MinProgress)
	}
	if filter.MaxProgress != nil {
		query += " AND COALESCE(rp.progress_percent, 0) <= ?"
		args = append(args, *filter.MaxProgress)
	}
	if filter.AddedWithinDays != nil {
		query += " AND s.created_at >= datetime('now', ?)"
		args = append(args, fmt.Sprintf("-%d days", *filter.AddedWithinDays))
	}
	switch strings.TrimSpace(filter.ReadState) {
	case "unread":
		query += " AND COALESCE(rp.read_books, 0) = 0"
	case "reading":
		query += " AND COALESCE(rp.read_books, 0) > 0 AND COALESCE(rp.completed_books, 0) < COALESCE(rp.book_count, 0)"
	case "completed":
		query += " AND COALESCE(rp.book_count, 0) > 0 AND COALESCE(rp.completed_books, 0) = COALESCE(rp.book_count, 0)"
	}
	return query, args
}

func smartCollectionOrderClause(filter SmartCollectionFilter) string {
	field := strings.TrimSpace(filter.SortByField)
	dir := strings.ToUpper(strings.TrimSpace(filter.SortDir))
	if dir != "ASC" && dir != "DESC" {
		dir = "ASC"
	}
	switch field {
	case "rating":
		return fmt.Sprintf("s.rating %s, s.name ASC", dir)
	case "books":
		return fmt.Sprintf("s.book_count %s, s.name ASC", dir)
	case "volumes":
		return fmt.Sprintf("s.volume_count %s, s.name ASC", dir)
	case "pages":
		return fmt.Sprintf("s.total_pages %s, s.name ASC", dir)
	case "read":
		return fmt.Sprintf("read_count %s, s.name ASC", dir)
	case "created":
		return fmt.Sprintf("s.created_at %s, s.name ASC", dir)
	case "updated":
		return fmt.Sprintf("s.updated_at %s, s.name ASC", dir)
	case "favorite":
		return fmt.Sprintf("s.is_favorite %s, s.name ASC", dir)
	default:
		return fmt.Sprintf("s.name %s", dir)
	}
}

var _ = sql.ErrNoRows
