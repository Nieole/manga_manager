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
	// UserID>0 时进度来源为该用户的 user_series_progress（多用户）；0 表示全局 series_stats。
	UserID int64
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
			sc.cover_path,
			GROUP_CONCAT(DISTINCT t.name) as tags_string,
			COALESCE(ss.read_pages, 0) as read_count
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

// smartCollectionProgressExpr 复用 series_stats 缓存计算阅读进度百分比，口径与全站 series 列表一致：
// 已读页数（clamp 到 page_count 后的 read_pages）占系列总页数（s.total_pages）的比例。
const smartCollectionProgressExpr = `CASE WHEN s.total_pages > 0 THEN COALESCE(ss.read_pages, 0) * 100.0 / s.total_pages ELSE 0 END`

func buildSmartCollectionBaseQuery(filter SmartCollectionFilter) (string, []any) {
	// 改用预计算的 series_stats 缓存（每系列一行、按 series_id 主键关联），并配合 series 表的
	// 冗余统计列（book_count / total_pages），取代此前对整个 books 表做的三重全表聚合。
	// 由于不再有绕过 library 过滤的派生表，WHERE s.library_id = ? 能真正把查询限定在本库范围内。
	// sc = 全局封面/标签缓存；ss = 进度来源（UserID>0 时按用户拆分）。progressJoin 的 user_id 占位符
	// 位于 WHERE 之前，故其实参须先于 filter.LibraryID。
	args := []any{}
	progressJoin := "LEFT JOIN series_stats ss ON ss.series_id = s.id"
	if filter.UserID > 0 {
		progressJoin = "LEFT JOIN user_series_progress ss ON ss.series_id = s.id AND ss.user_id = ?"
		args = append(args, filter.UserID)
	}
	query := `
		FROM series s
		LEFT JOIN series_stats sc ON sc.series_id = s.id
		` + progressJoin + `
		LEFT JOIN series_tags st ON s.id = st.series_id
		LEFT JOIN tags t ON st.tag_id = t.id
		LEFT JOIN series_authors sa ON s.id = sa.series_id
		LEFT JOIN authors a ON sa.author_id = a.id
		WHERE s.library_id = ?
	`
	args = append(args, filter.LibraryID)

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
		query += " AND (" + smartCollectionProgressExpr + ") >= ?"
		args = append(args, *filter.MinProgress)
	}
	if filter.MaxProgress != nil {
		query += " AND (" + smartCollectionProgressExpr + ") <= ?"
		args = append(args, *filter.MaxProgress)
	}
	if filter.AddedWithinDays != nil {
		query += " AND s.created_at >= datetime('now', ?)"
		args = append(args, fmt.Sprintf("-%d days", *filter.AddedWithinDays))
	}
	switch strings.TrimSpace(filter.ReadState) {
	case "unread":
		query += " AND COALESCE(ss.read_book_count, 0) = 0"
	case "reading":
		query += " AND COALESCE(ss.read_book_count, 0) > 0 AND COALESCE(ss.completed_book_count, 0) < s.book_count"
	case "completed":
		query += " AND s.book_count > 0 AND COALESCE(ss.completed_book_count, 0) = s.book_count"
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
