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

	countQuery := "SELECT COUNT(*) " + baseQuery
	var total int
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	selectQuery := `
		SELECT
			s.id, s.library_id, s.name, s.title, s.summary, s.publisher, s.status, s.rating, s.language, s.locked_fields, s.name_initial, s.path, s.created_at, s.updated_at, s.is_favorite, s.volume_count, s.book_count, s.total_pages,
			sc.cover_path,
			COALESCE(sc.tag_names_cache, '') as tags_string,
			COALESCE(ss.read_pages, 0) as read_count
	` + baseQuery + fmt.Sprintf(` ORDER BY %s LIMIT ? OFFSET ?`, smartCollectionOrderClause(filter))
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
	// 聚合由预计算的 series_stats 缓存（每系列一行、按 series_id 主键关联）与 series 表的
	// 冗余统计列（book_count / total_pages）承担；查询不得引入绕过 library 过滤的派生表，
	// 否则 WHERE s.library_id = ? 就管不到内层。
	// sc = 全局封面/标签缓存；ss = 进度来源（UserID>0 时按用户拆分）。progressJoin 的 user_id 占位符
	// 位于 WHERE 之前，故其实参须先于 filter.LibraryID。
	args := []any{}
	progressJoin := "LEFT JOIN series_stats ss ON ss.series_id = s.id"
	if filter.UserID > 0 {
		progressJoin = "LEFT JOIN user_series_progress ss ON ss.series_id = s.id AND ss.user_id = ?"
		args = append(args, filter.UserID)
	}
	// 标签/作者不得用 LEFT JOIN + GROUP BY 收敛：那会让每系列产出 tags×authors 行的中间集、
	// 需物化去重 filesort，且 GROUP BY 使 series 复合排序索引失效。这里与主列表
	// buildSeriesSearchQuery 一致，改走 EXISTS 子查询表达 ActiveTag/ActiveAuthor、标签串直接读
	// 预计算的 sc.tag_names_cache，使每系列恒为单行、走排序索引、COUNT 用 COUNT(*)。
	query := `
		FROM series s
		LEFT JOIN series_stats sc ON sc.series_id = s.id
		` + progressJoin + `
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
		query += " AND EXISTS (SELECT 1 FROM series_tags st JOIN tags t ON st.tag_id = t.id WHERE st.series_id = s.id AND t.name = ?)"
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.ActiveAuthor); value != "" {
		query += " AND EXISTS (SELECT 1 FROM series_authors sa JOIN authors a ON sa.author_id = a.id WHERE sa.series_id = s.id AND a.name = ?)"
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
		// 用 >= 与常规库列表(buildSeriesSearchQuery)、续读续集查询保持一致——completed_book_count 可能因
		// 统计口径短暂 > book_count，用 = 会漏掉这些其实已读完的系列。
		query += " AND s.book_count > 0 AND COALESCE(ss.completed_book_count, 0) >= s.book_count"
	}
	return query, args
}

func smartCollectionOrderClause(filter SmartCollectionFilter) string {
	field := strings.TrimSpace(filter.SortByField)
	dir := strings.ToUpper(strings.TrimSpace(filter.SortDir))
	if dir != "ASC" && dir != "DESC" {
		dir = "ASC"
	}
	// 末位的 s.id 让排序成为全序：同名系列在 name 之后再无区分键时，LIMIT/OFFSET 翻页的
	// 行序不再由查询计划决定，页与页之间不会重复或漏掉条目。与主列表
	// seriesSearchOffsetOrderClause 同一惯例——tie-break 跟随最后一个内容键的方向。
	switch field {
	case "rating":
		return fmt.Sprintf("s.rating %s, s.name ASC, s.id ASC", dir)
	case "books":
		return fmt.Sprintf("s.book_count %s, s.name ASC, s.id ASC", dir)
	case "volumes":
		return fmt.Sprintf("s.volume_count %s, s.name ASC, s.id ASC", dir)
	case "pages":
		return fmt.Sprintf("s.total_pages %s, s.name ASC, s.id ASC", dir)
	case "read":
		return fmt.Sprintf("read_count %s, s.name ASC, s.id ASC", dir)
	case "created":
		return fmt.Sprintf("s.created_at %s, s.name ASC, s.id ASC", dir)
	case "updated":
		return fmt.Sprintf("s.updated_at %s, s.name ASC, s.id ASC", dir)
	case "favorite":
		return fmt.Sprintf("s.is_favorite %s, s.name ASC, s.id ASC", dir)
	default:
		return fmt.Sprintf("s.name %s, s.id %s", dir, dir)
	}
}

var _ = sql.ErrNoRows

// CountSmartCollectionSeries 返回智能书架当前命中的系列数。
//
// 必须复用 buildSmartCollectionBaseQuery，只把 SELECT 换成 COUNT(*)：与列表用另一套 SQL
// 口径分裂，会出现「书架显示 12 个系列，点进去只有 9 个」这类计数与列表对不上的问题。
func (s *SqlStore) CountSmartCollectionSeries(ctx context.Context, filter SmartCollectionFilter) (int, error) {
	baseQuery, args := buildSmartCollectionBaseQuery(filter)
	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) "+baseQuery, args...).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}
