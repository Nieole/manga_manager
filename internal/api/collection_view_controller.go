package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/organize"
)

// CollectionView 是合集页左栏的一个列表项，手工合集与智能书架共用。
// 两者的差异全部收在 SmartFilter 里：它只在智能书架上出现，手工合集的 JSON 一个筛选字段都不多带。
type CollectionView = organize.CollectionView

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

const defaultSmartCollectionPageLimit = organize.DefaultSmartCollectionPageLimit
const maxSmartCollectionPageLimit = organize.MaxSmartCollectionPageLimit

func (c *Controller) listCollectionViews(w http.ResponseWriter, r *http.Request) {
	items, err := c.loadCollectionViews(r.Context(), c.currentUserID(r))
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to list collection views")
		return
	}
	jsonResponse(w, http.StatusOK, items)
}

func (c *Controller) loadCollectionViews(ctx context.Context, userID int64) ([]CollectionView, error) {
	return organize.NewSmartCollections(c.store).Views(ctx, userID)
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
		view.ViewID = id
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

func (c *Controller) loadSmartCollectionSeries(ctx context.Context, filter SmartFilter, limit, offset int, userID int64) ([]database.SearchSeriesPagedRow, int, error) {
	page, err := organize.NewSmartCollections(c.store).Members(ctx, filter.ID, userID, limit, offset)
	return page.Items, page.Total, err
}
func (c *Controller) getSmartFilterByID(r *http.Request, id int64) (SmartFilter, error) {
	return organize.NewSmartCollections(c.store).Get(r.Context(), id)
}
func queryInt(r *http.Request, key string) int {
	value, _ := strconv.Atoi(r.URL.Query().Get(key))
	return value
}
func smartCollectionError(w http.ResponseWriter, err error, fallback string) {
	if errors.Is(err, sql.ErrNoRows) {
		jsonError(w, http.StatusNotFound, "Smart collection not found")
		return
	}
	if errors.Is(err, organize.ErrSmartSnapshotEmpty) {
		jsonError(w, http.StatusConflict, err.Error())
		return
	}
	collectionUseCaseError(w, err, fallback)
}
func (c *Controller) getSmartCollectionSeries(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "filterId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid smart collection ID")
		return
	}
	limit := queryInt(r, "limit")
	if limit > 0 {
		limit = organize.SmartCollectionPageLimit(limit)
	}
	page, err := organize.NewSmartCollections(c.store).Members(r.Context(), id, c.currentUserID(r), limit, queryInt(r, "offset"))
	if err != nil {
		smartCollectionError(w, err, "Failed to load smart collection series")
		return
	}
	if page.Items == nil {
		page.Items = []database.SearchSeriesPagedRow{}
	}
	jsonResponse(w, http.StatusOK, SmartCollectionSeriesResponse{Items: page.Items, Total: page.Total, Limit: page.Limit, Offset: page.Offset, Filter: page.Filter, Kind: "smart", ViewID: "smart:" + strconv.FormatInt(id, 10), ViewName: page.Filter.Name})
}
func (c *Controller) previewSmartCollectionSnapshot(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "filterId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid smart collection ID")
		return
	}
	req := organize.SmartSnapshotRequest{Name: r.URL.Query().Get("name"), Description: r.URL.Query().Get("description"), Limit: queryInt(r, "limit")}
	preview, err := organize.NewSmartCollections(c.store).PreviewSnapshot(r.Context(), id, c.currentUserID(r), req, queryInt(r, "preview_limit"))
	if err != nil {
		smartCollectionError(w, err, "Failed to preview smart collection members")
		return
	}
	if preview.Items == nil {
		preview.Items = []database.SearchSeriesPagedRow{}
	}
	jsonResponse(w, http.StatusOK, SmartCollectionSnapshotPreviewResponse{FilterID: preview.FilterID, Name: preview.Name, Description: preview.Description, Items: preview.Items, Total: preview.Total, PreviewLimit: preview.PreviewLimit, SnapshotLimit: preview.SnapshotLimit, SnapshotCount: preview.SnapshotCount, Truncated: preview.Truncated, NameConflict: preview.NameConflict, Filter: preview.Filter})
}
func (c *Controller) snapshotSmartCollection(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "filterId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid smart collection ID")
		return
	}
	var req snapshotSmartCollectionRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	result, err := organize.NewSmartCollections(c.store).Snapshot(r.Context(), id, c.currentUserID(r), organize.SmartSnapshotRequest{Name: req.Name, Description: req.Description, Limit: req.Limit})
	if err != nil {
		smartCollectionError(w, err, "Failed to snapshot smart collection")
		return
	}
	jsonResponse(w, http.StatusCreated, map[string]any{"id": result.ID, "name": result.Name, "series_count": result.SeriesCount, "total": result.Total, "truncated": result.Truncated})
}
