// 本文件由 store.go 拆分而来，属于 SQLite 数据访问层的「标签/作者等分面查询」子域。

package database

import (
	"context"
	"strings"
)

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
