package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
	rows, err := c.store.ListCollectionViews(ctx)
	if err != nil {
		return nil, err
	}

	items := make([]CollectionView, 0, len(rows))
	for _, row := range rows {
		viewID, _ := row.ViewID.(string)
		item := CollectionView{
			ID:          viewID,
			NumericID:   row.ID,
			Kind:        row.Kind,
			Name:        row.Name,
			Description: row.Description,
			LibraryName: row.LibraryName,
			SeriesCount: int(row.SeriesCount),
			SourceType:  row.SourceType,
			SortOrder:   int(row.SortOrder),
			CreatedAt:   row.CreatedAt.Time,
			UpdatedAt:   row.UpdatedAt.Time,
		}
		if row.LibraryID.Valid {
			value := row.LibraryID.Int64
			item.LibraryID = &value
		}
		if row.SourceReviewID.Valid {
			value := row.SourceReviewID.Int64
			item.SourceReviewID = &value
		}
		items = append(items, item)
	}
	return items, nil
}

func (c *Controller) loadStaticCollectionSeries(ctx context.Context, collectionID int64, limit, offset int) (CollectionView, []collectionSeriesListItem, int, error) {
	row, err := c.store.GetStaticCollectionView(ctx, collectionID)
	if err != nil {
		return CollectionView{}, nil, 0, err
	}
	view := CollectionView{
		Kind:        row.Kind,
		NumericID:   row.ID,
		Name:        row.Name,
		Description: row.Description.String,
		SeriesCount: int(row.SeriesCount),
		SourceType:  row.SourceType,
		SortOrder:   int(row.SortOrder),
		CreatedAt:   row.CreatedAt.Time,
		UpdatedAt:   row.UpdatedAt.Time,
	}
	if id, ok := row.ViewID.(string); ok {
		view.ID = id
	}
	if row.SourceReviewID.Valid {
		value := row.SourceReviewID.Int64
		view.SourceReviewID = &value
	}

	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	rawRows, err := c.store.ListStaticCollectionSeriesPaged(ctx, database.ListStaticCollectionSeriesPagedParams{
		CollectionID: collectionID,
		LimitCount:   int64(limit),
		OffsetValue:  int64(offset),
	})
	if err != nil {
		return CollectionView{}, nil, 0, err
	}
	items := make([]collectionSeriesListItem, 0, len(rawRows))
	for _, r := range rawRows {
		items = append(items, collectionSeriesListItem{
			ID:         r.ID,
			LibraryID:  r.LibraryID,
			Name:       r.Name,
			Title:      r.Title,
			Summary:    r.Summary,
			Status:     r.Status,
			UpdatedAt:  r.UpdatedAt,
			BookCount:  r.BookCount,
			TotalPages: r.TotalPages,
			CoverPath:  r.CoverPath,
		})
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
	return c.store.SearchSmartCollectionSeries(ctx, database.SmartCollectionFilter{
		LibraryID:       filter.LibraryID,
		ActiveLetter:    stringValue(filter.ActiveLetter),
		ActiveStatus:    stringValue(filter.ActiveStatus),
		ActiveTag:       stringValue(filter.ActiveTag),
		ActiveAuthor:    stringValue(filter.ActiveAuthor),
		MinRating:       filter.MinRating,
		MaxRating:       filter.MaxRating,
		MinProgress:     filter.MinProgress,
		MaxProgress:     filter.MaxProgress,
		AddedWithinDays: filter.AddedWithinDays,
		ReadState:       stringValue(filter.ReadState),
		SortByField:     filter.SortByField,
		SortDir:         filter.SortDir,
	}, limit, offset)
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
	if _, err := c.store.CollectionNameExists(ctx, strings.TrimSpace(name)); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (c *Controller) getSmartFilterByID(r *http.Request, filterID int64) (SmartFilter, error) {
	row, err := c.store.GetSmartFilterByID(r.Context(), filterID)
	if err != nil {
		return SmartFilter{}, err
	}
	return smartFilterFromDB(row), nil
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
