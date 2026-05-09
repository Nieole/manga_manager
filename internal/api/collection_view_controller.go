package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"manga-manager/internal/database"
)

type CollectionView struct {
	ID             string    `json:"id"`
	NumericID      int64     `json:"numeric_id"`
	Kind           string    `json:"kind"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	LibraryID      *int64    `json:"library_id,omitempty"`
	LibraryName    string    `json:"library_name,omitempty"`
	SeriesCount    int       `json:"series_count"`
	SourceType     string    `json:"source_type"`
	SourceReviewID *int64    `json:"source_review_id,omitempty"`
	SortOrder      int       `json:"sort_order"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type snapshotSmartCollectionRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Limit       int    `json:"limit"`
}

type SmartCollectionSnapshotPreviewResponse struct {
	FilterID      int64                           `json:"filter_id"`
	Name          string                          `json:"name"`
	Description   string                          `json:"description"`
	Items         []database.SearchSeriesPagedRow `json:"items"`
	Total         int                             `json:"total"`
	PreviewLimit  int                             `json:"preview_limit"`
	SnapshotLimit int                             `json:"snapshot_limit"`
	SnapshotCount int                             `json:"snapshot_count"`
	Truncated     bool                            `json:"truncated"`
	NameConflict  bool                            `json:"name_conflict"`
	Filter        SmartFilter                     `json:"filter"`
}

type SmartCollectionSeriesResponse struct {
	Items    []database.SearchSeriesPagedRow `json:"items"`
	Total    int                             `json:"total"`
	Limit    int                             `json:"limit"`
	Offset   int                             `json:"offset"`
	Filter   SmartFilter                     `json:"filter"`
	Kind     string                          `json:"kind"`
	ViewID   string                          `json:"view_id"`
	ViewName string                          `json:"view_name"`
}

type collectionSeriesListItem struct {
	ID         int64
	LibraryID  int64
	Name       string
	Title      string
	Summary    string
	Status     string
	UpdatedAt  time.Time
	BookCount  int64
	TotalPages int64
	CoverPath  string
}

const (
	defaultSmartSnapshotLimit        = 1000
	maxSmartSnapshotLimit            = 1000
	defaultSmartSnapshotPreviewLimit = 12
	maxSmartSnapshotPreviewLimit     = 50
)

func (c *Controller) listCollectionViews(w http.ResponseWriter, r *http.Request) {
	items, err := c.loadCollectionViews(r.Context())
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to list collection views")
		return
	}
	jsonResponse(w, http.StatusOK, items)
}

func (c *Controller) loadCollectionViews(ctx context.Context) ([]CollectionView, error) {
	db := c.store.(*database.SqlStore).DB()
	rows, err := db.QueryContext(ctx, `
		SELECT
			'collection:' || c.id as view_id,
			c.id,
			'collection' as kind,
			c.name,
			COALESCE(c.description, '') as description,
			NULL as library_id,
			'' as library_name,
			(SELECT COUNT(*) FROM collection_series cs WHERE cs.collection_id = c.id) as series_count,
			c.source_type,
			c.source_review_id,
			c.sort_order,
			c.created_at,
			c.updated_at
		FROM collections c
		UNION ALL
		SELECT
			'smart:' || sf.id as view_id,
			sf.id,
			'smart' as kind,
			sf.name,
			TRIM(
				COALESCE('tag=' || sf.active_tag, '') || ' ' ||
				COALESCE('author=' || sf.active_author, '') || ' ' ||
				COALESCE('status=' || sf.active_status, '') || ' ' ||
				COALESCE('letter=' || sf.active_letter, '') || ' ' ||
				COALESCE('read=' || sf.read_state, '') || ' ' ||
				COALESCE('rating>=' || sf.min_rating, '') || ' ' ||
				COALESCE('rating<=' || sf.max_rating, '') || ' ' ||
				COALESCE('progress>=' || sf.min_progress, '') || ' ' ||
				COALESCE('progress<=' || sf.max_progress, '') || ' ' ||
				COALESCE('added<=' || sf.added_within_days || 'd', '')
			) as description,
			sf.library_id,
			l.name as library_name,
			(
				SELECT COUNT(DISTINCT s.id)
				FROM series s
				LEFT JOIN series_tags st ON s.id = st.series_id
				LEFT JOIN tags t ON st.tag_id = t.id
				LEFT JOIN series_authors sa ON s.id = sa.series_id
				LEFT JOIN authors a ON sa.author_id = a.id
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
				WHERE s.library_id = sf.library_id
				  AND (sf.active_status IS NULL OR s.status = sf.active_status)
				  AND (sf.active_letter IS NULL OR s.name_initial = sf.active_letter)
				  AND (sf.active_tag IS NULL OR t.name = sf.active_tag)
				  AND (sf.active_author IS NULL OR a.name = sf.active_author)
				  AND (sf.min_rating IS NULL OR s.rating >= sf.min_rating)
				  AND (sf.max_rating IS NULL OR s.rating <= sf.max_rating)
				  AND (sf.min_progress IS NULL OR COALESCE(rp.progress_percent, 0) >= sf.min_progress)
				  AND (sf.max_progress IS NULL OR COALESCE(rp.progress_percent, 0) <= sf.max_progress)
				  AND (sf.added_within_days IS NULL OR s.created_at >= datetime('now', '-' || sf.added_within_days || ' days'))
				  AND (
					sf.read_state IS NULL
					OR (sf.read_state = 'unread' AND COALESCE(rp.read_books, 0) = 0)
					OR (sf.read_state = 'reading' AND COALESCE(rp.read_books, 0) > 0 AND COALESCE(rp.completed_books, 0) < COALESCE(rp.book_count, 0))
					OR (sf.read_state = 'completed' AND COALESCE(rp.book_count, 0) > 0 AND COALESCE(rp.completed_books, 0) = COALESCE(rp.book_count, 0))
				  )
			) as series_count,
			'smart_filter' as source_type,
			NULL as source_review_id,
			0 as sort_order,
			sf.created_at,
			sf.updated_at
		FROM smart_filters sf
		JOIN libraries l ON l.id = sf.library_id
		ORDER BY kind, sort_order, name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]CollectionView, 0)
	for rows.Next() {
		var item CollectionView
		var libraryID sql.NullInt64
		var sourceReviewID sql.NullInt64
		if err := rows.Scan(
			&item.ID,
			&item.NumericID,
			&item.Kind,
			&item.Name,
			&item.Description,
			&libraryID,
			&item.LibraryName,
			&item.SeriesCount,
			&item.SourceType,
			&sourceReviewID,
			&item.SortOrder,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if libraryID.Valid {
			value := libraryID.Int64
			item.LibraryID = &value
		}
		if sourceReviewID.Valid {
			value := sourceReviewID.Int64
			item.SourceReviewID = &value
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (c *Controller) loadStaticCollectionSeries(ctx context.Context, collectionID int64, limit, offset int) (CollectionView, []collectionSeriesListItem, int, error) {
	db := c.store.(*database.SqlStore).DB()
	var view CollectionView
	var description sql.NullString
	var sourceReviewID sql.NullInt64
	err := db.QueryRowContext(ctx, `
		SELECT
			'collection:' || c.id,
			c.id,
			'collection',
			c.name,
			c.description,
			(SELECT COUNT(*) FROM collection_series cs WHERE cs.collection_id = c.id),
			c.source_type,
			c.source_review_id,
			c.sort_order,
			c.created_at,
			c.updated_at
		FROM collections c
		WHERE c.id = ?
		LIMIT 1
	`, collectionID).Scan(
		&view.ID,
		&view.NumericID,
		&view.Kind,
		&view.Name,
		&description,
		&view.SeriesCount,
		&view.SourceType,
		&sourceReviewID,
		&view.SortOrder,
		&view.CreatedAt,
		&view.UpdatedAt,
	)
	if err != nil {
		return CollectionView{}, nil, 0, err
	}
	if description.Valid {
		view.Description = description.String
	}
	if sourceReviewID.Valid {
		value := sourceReviewID.Int64
		view.SourceReviewID = &value
	}

	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := db.QueryContext(ctx, `
		SELECT
			s.id,
			s.library_id,
			s.name,
			COALESCE(s.title, ''),
			COALESCE(s.summary, ''),
			COALESCE(s.status, ''),
			s.updated_at,
			s.book_count,
			s.total_pages,
			CAST(COALESCE((
				SELECT b.cover_path
				FROM books b
				WHERE b.series_id = s.id AND b.cover_path IS NOT NULL AND b.cover_path != ''
				ORDER BY b.sort_number, b.name
				LIMIT 1
			), '') AS TEXT) as cover_path
		FROM collection_series cs
		JOIN series s ON s.id = cs.series_id
		WHERE cs.collection_id = ?
		ORDER BY cs.sort_order, COALESCE(NULLIF(s.title, ''), s.name) COLLATE NOCASE
		LIMIT ? OFFSET ?
	`, collectionID, limit, offset)
	if err != nil {
		return CollectionView{}, nil, 0, err
	}
	defer rows.Close()

	items := make([]collectionSeriesListItem, 0)
	for rows.Next() {
		var item collectionSeriesListItem
		if err := rows.Scan(
			&item.ID,
			&item.LibraryID,
			&item.Name,
			&item.Title,
			&item.Summary,
			&item.Status,
			&item.UpdatedAt,
			&item.BookCount,
			&item.TotalPages,
			&item.CoverPath,
		); err != nil {
			return CollectionView{}, nil, 0, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return CollectionView{}, nil, 0, err
	}
	return view, items, view.SeriesCount, nil
}

func (c *Controller) loadSmartCollectionSeries(ctx context.Context, filter SmartFilter, limit, offset int) ([]database.SearchSeriesPagedRow, int, error) {
	if limit <= 0 {
		limit = filter.PageSize
	}
	if offset < 0 {
		offset = 0
	}
	db := c.store.(*database.SqlStore).DB()
	baseQuery, args := smartCollectionBaseQuery(filter)

	countQuery := "SELECT COUNT(DISTINCT s.id) " + baseQuery
	var total int
	if err := db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	selectQuery := `
		SELECT
			s.id, s.library_id, s.name, s.title, s.summary, s.publisher, s.status, s.rating, s.language, s.locked_fields, s.name_initial, s.path, s.created_at, s.updated_at, s.is_favorite, s.volume_count, s.book_count, s.total_pages,
			bc.cover_path,
			GROUP_CONCAT(DISTINCT t.name) as tags_string,
			COALESCE(rc.read_count, 0) as read_count
	` + baseQuery + fmt.Sprintf(` GROUP BY s.id ORDER BY %s LIMIT ? OFFSET ?`, smartCollectionOrderClause(filter))
	args = append(args, limit, offset)

	rows, err := db.QueryContext(ctx, selectQuery, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]database.SearchSeriesPagedRow, 0)
	for rows.Next() {
		var item database.SearchSeriesPagedRow
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

func (c *Controller) getSmartCollectionSeries(w http.ResponseWriter, r *http.Request) {
	filterID, err := parseID(r, "filterId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid smart collection ID")
		return
	}
	filter, err := c.getSmartFilterByID(r, filterID)
	if err != nil {
		if err == sql.ErrNoRows {
			jsonError(w, http.StatusNotFound, "Smart collection not found")
			return
		}
		jsonError(w, http.StatusInternalServerError, "Failed to load smart collection")
		return
	}

	limit := filter.PageSize
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			switch parsed {
			case 30, 50, 100:
				limit = parsed
			}
		}
	}
	offset := 0
	if raw := r.URL.Query().Get("offset"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			offset = parsed
		}
	}

	rows, total, err := c.loadSmartCollectionSeries(r.Context(), filter, limit, offset)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to load smart collection series")
		return
	}
	if rows == nil {
		rows = []database.SearchSeriesPagedRow{}
	}
	jsonResponse(w, http.StatusOK, SmartCollectionSeriesResponse{
		Items:    rows,
		Total:    total,
		Limit:    limit,
		Offset:   offset,
		Filter:   filter,
		Kind:     "smart",
		ViewID:   "smart:" + strconv.FormatInt(filter.ID, 10),
		ViewName: filter.Name,
	})
}

func (c *Controller) previewSmartCollectionSnapshot(w http.ResponseWriter, r *http.Request) {
	filterID, err := parseID(r, "filterId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid smart collection ID")
		return
	}
	filter, err := c.getSmartFilterByID(r, filterID)
	if err != nil {
		if err == sql.ErrNoRows {
			jsonError(w, http.StatusNotFound, "Smart collection not found")
			return
		}
		jsonError(w, http.StatusInternalServerError, "Failed to load smart collection")
		return
	}

	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		name = filter.Name
	}
	description := strings.TrimSpace(r.URL.Query().Get("description"))
	if description == "" {
		description = "Snapshot from smart collection: " + filter.Name
	}
	previewLimit := defaultSmartSnapshotPreviewLimit
	if raw := r.URL.Query().Get("preview_limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			previewLimit = parsed
		}
	}
	if previewLimit > maxSmartSnapshotPreviewLimit {
		previewLimit = maxSmartSnapshotPreviewLimit
	}
	snapshotLimit := normalizeSmartSnapshotLimit(0)
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			snapshotLimit = normalizeSmartSnapshotLimit(parsed)
		}
	}

	rows, total, err := c.loadSmartCollectionSeries(r.Context(), filter, previewLimit, 0)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to preview smart collection members")
		return
	}
	if rows == nil {
		rows = []database.SearchSeriesPagedRow{}
	}
	conflict, err := c.collectionNameExists(r.Context(), name)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to check collection name")
		return
	}

	snapshotCount := total
	if snapshotCount > snapshotLimit {
		snapshotCount = snapshotLimit
	}
	jsonResponse(w, http.StatusOK, SmartCollectionSnapshotPreviewResponse{
		FilterID:      filter.ID,
		Name:          name,
		Description:   description,
		Items:         rows,
		Total:         total,
		PreviewLimit:  previewLimit,
		SnapshotLimit: snapshotLimit,
		SnapshotCount: snapshotCount,
		Truncated:     total > snapshotLimit,
		NameConflict:  conflict,
		Filter:        filter,
	})
}

func (c *Controller) snapshotSmartCollection(w http.ResponseWriter, r *http.Request) {
	filterID, err := parseID(r, "filterId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid smart collection ID")
		return
	}
	filter, err := c.getSmartFilterByID(r, filterID)
	if err != nil {
		if err == sql.ErrNoRows {
			jsonError(w, http.StatusNotFound, "Smart collection not found")
			return
		}
		jsonError(w, http.StatusInternalServerError, "Failed to load smart collection")
		return
	}

	var req snapshotSmartCollectionRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = filter.Name
	}
	description := strings.TrimSpace(req.Description)
	if description == "" {
		description = "Snapshot from smart collection: " + filter.Name
	}
	limit := req.Limit
	limit = normalizeSmartSnapshotLimit(limit)

	rows, total, err := c.loadSmartCollectionSeries(r.Context(), filter, limit, 0)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to load smart collection members")
		return
	}
	if len(rows) == 0 {
		jsonError(w, http.StatusConflict, "Smart collection has no members to snapshot")
		return
	}

	var createdID int64
	err = c.store.ExecTx(r.Context(), func(q *database.Queries) error {
		created, err := q.CreateCollection(r.Context(), database.CreateCollectionParams{
			Name:           name,
			Description:    sql.NullString{String: description, Valid: description != ""},
			SourceType:     "smart_snapshot",
			SourceReviewID: sql.NullInt64{},
		})
		if err != nil {
			return err
		}
		for _, row := range rows {
			if err := q.AddSeriesToCollection(r.Context(), database.AddSeriesToCollectionParams{
				CollectionID: created.ID,
				SeriesID:     row.ID,
			}); err != nil {
				return err
			}
		}
		if err := q.TouchCollection(r.Context(), created.ID); err != nil {
			return err
		}
		createdID = created.ID
		return nil
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to snapshot smart collection")
		return
	}

	jsonResponse(w, http.StatusCreated, map[string]any{
		"id":           createdID,
		"name":         name,
		"series_count": len(rows),
		"total":        total,
		"truncated":    total > len(rows),
	})
}

func normalizeSmartSnapshotLimit(limit int) int {
	if limit <= 0 {
		return defaultSmartSnapshotLimit
	}
	if limit > maxSmartSnapshotLimit {
		return maxSmartSnapshotLimit
	}
	return limit
}

func (c *Controller) collectionNameExists(ctx context.Context, name string) (bool, error) {
	if strings.TrimSpace(name) == "" {
		return false, nil
	}
	db := c.store.(*database.SqlStore).DB()
	var exists int
	err := db.QueryRowContext(ctx, `
		SELECT 1
		FROM collections
		WHERE name = ? COLLATE NOCASE
		LIMIT 1
	`, strings.TrimSpace(name)).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return exists == 1, nil
}

func (c *Controller) getSmartFilterByID(r *http.Request, filterID int64) (SmartFilter, error) {
	db := c.store.(*database.SqlStore).DB()
	row := db.QueryRowContext(r.Context(), `
		SELECT id, library_id, name, active_tag, active_author, active_status, active_letter,
		       read_state, min_rating, max_rating, min_progress, max_progress, added_within_days,
		       sort_by_field, sort_dir, page_size, created_at, updated_at
		FROM smart_filters
		WHERE id = ?
		LIMIT 1
	`, filterID)
	return scanSmartFilter(row)
}

func smartCollectionBaseQuery(filter SmartFilter) (string, []any) {
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

	if value := stringValue(filter.ActiveLetter); value != "" {
		query += " AND s.name_initial = ?"
		args = append(args, strings.ToUpper(value))
	}
	if value := stringValue(filter.ActiveStatus); value != "" {
		query += " AND s.status = ?"
		args = append(args, value)
	}
	if value := stringValue(filter.ActiveTag); value != "" {
		query += " AND t.name = ?"
		args = append(args, value)
	}
	if value := stringValue(filter.ActiveAuthor); value != "" {
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
	switch stringValue(filter.ReadState) {
	case "unread":
		query += " AND COALESCE(rp.read_books, 0) = 0"
	case "reading":
		query += " AND COALESCE(rp.read_books, 0) > 0 AND COALESCE(rp.completed_books, 0) < COALESCE(rp.book_count, 0)"
	case "completed":
		query += " AND COALESCE(rp.book_count, 0) > 0 AND COALESCE(rp.completed_books, 0) = COALESCE(rp.book_count, 0)"
	}
	return query, args
}

func smartCollectionOrderClause(filter SmartFilter) string {
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

func smartFilterSortBy(filter SmartFilter) string {
	field := strings.TrimSpace(filter.SortByField)
	if field == "" {
		field = "name"
	}
	dir := strings.TrimSpace(filter.SortDir)
	if dir == "" {
		dir = "asc"
	}
	return field + "_" + dir
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func stringList(value *string) []string {
	text := stringValue(value)
	if text == "" {
		return nil
	}
	return []string{text}
}
