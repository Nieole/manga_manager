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
	"manga-manager/internal/organize"
)

type aiGroupingReviewSeriesView struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Title string `json:"title"`
}

type aiGroupingReviewCollectionView struct {
	ID                  int64                        `json:"id"`
	ReviewID            int64                        `json:"review_id"`
	Name                string                       `json:"name"`
	Description         string                       `json:"description"`
	SeriesIDs           []int64                      `json:"series_ids"`
	Series              []aiGroupingReviewSeriesView `json:"series"`
	SeriesCount         int64                        `json:"series_count"`
	Status              string                       `json:"status"`
	CreatedCollectionID *int64                       `json:"created_collection_id,omitempty"`
}

type aiGroupingReviewView struct {
	ID              int64                            `json:"id"`
	LibraryID       int64                            `json:"library_id"`
	LibraryName     string                           `json:"library_name"`
	Provider        string                           `json:"provider"`
	Status          string                           `json:"status"`
	Summary         string                           `json:"summary"`
	CandidateCount  int64                            `json:"candidate_count"`
	CollectionCount int64                            `json:"collection_count"`
	CreatedAt       time.Time                        `json:"created_at"`
	UpdatedAt       time.Time                        `json:"updated_at"`
	AppliedAt       *time.Time                       `json:"applied_at,omitempty"`
	RejectedAt      *time.Time                       `json:"rejected_at,omitempty"`
	Collections     []aiGroupingReviewCollectionView `json:"collections"`
}

type aiGroupingReviewsResponse struct {
	Items  []aiGroupingReviewView `json:"items"`
	Total  int64                  `json:"total"`
	Limit  int64                  `json:"limit"`
	Offset int64                  `json:"offset"`
}

type updateAIGroupingReviewCollectionRequest struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	SeriesIDs   []int64 `json:"series_ids"`
}

func aiGroupingParseSeriesIDs(raw string) []int64 { return organize.AIGroupingSeriesIDs(raw) }

// aiGroupingViewData 是渲染审核视图所需的预取数据，避免按审核、按候选合集逐条查询
// 拖出两层 N+1（页大小 × 每条的候选合集数），也避免两步查询各自的错误被吞掉——
// 吞掉后 DB 故障时接口仍返回 200，候选合集会静默显示为空。
type aiGroupingViewData struct {
	collectionsByReview map[int64][]database.AiGroupingReviewCollection
	seriesByID          map[int64]database.GetSeriesNamesByIDsRow
}

// loadAIGroupingViewData 一次性取齐这批审核的候选合集与涉及的全部系列名。
func loadAIGroupingViewData(ctx context.Context, q database.Querier, reviewIDs []int64) (aiGroupingViewData, error) {
	data := aiGroupingViewData{
		collectionsByReview: map[int64][]database.AiGroupingReviewCollection{},
		seriesByID:          map[int64]database.GetSeriesNamesByIDsRow{},
	}
	if len(reviewIDs) == 0 {
		return data, nil
	}

	collections, err := q.ListAIGroupingReviewCollectionsByReviews(ctx, reviewIDs)
	if err != nil {
		return aiGroupingViewData{}, err
	}
	seriesIDSet := map[int64]struct{}{}
	for _, collection := range collections {
		data.collectionsByReview[collection.ReviewID] = append(data.collectionsByReview[collection.ReviewID], collection)
		for _, id := range aiGroupingParseSeriesIDs(collection.SeriesIds) {
			seriesIDSet[id] = struct{}{}
		}
	}
	if len(seriesIDSet) == 0 {
		return data, nil
	}

	seriesIDs := make([]int64, 0, len(seriesIDSet))
	for id := range seriesIDSet {
		seriesIDs = append(seriesIDs, id)
	}
	rows, err := q.GetSeriesNamesByIDs(ctx, seriesIDs)
	if err != nil {
		return aiGroupingViewData{}, err
	}
	for _, row := range rows {
		data.seriesByID[row.ID] = row
	}
	return data, nil
}

// loadAIGroupingViewDataForCollection 给「只渲染单个候选合集」的路径用。
func loadAIGroupingViewDataForCollection(ctx context.Context, q database.Querier, collection database.AiGroupingReviewCollection) (aiGroupingViewData, error) {
	data := aiGroupingViewData{
		collectionsByReview: map[int64][]database.AiGroupingReviewCollection{},
		seriesByID:          map[int64]database.GetSeriesNamesByIDsRow{},
	}
	ids := aiGroupingParseSeriesIDs(collection.SeriesIds)
	if len(ids) == 0 {
		return data, nil
	}
	rows, err := q.GetSeriesNamesByIDs(ctx, ids)
	if err != nil {
		return aiGroupingViewData{}, err
	}
	for _, row := range rows {
		data.seriesByID[row.ID] = row
	}
	return data, nil
}

func (d aiGroupingViewData) seriesViews(ids []int64) []aiGroupingReviewSeriesView {
	views := make([]aiGroupingReviewSeriesView, 0, len(ids))
	for _, id := range ids {
		row, ok := d.seriesByID[id]
		if !ok {
			// 系列已被删除：跳过而不是留空壳。ai_grouping_review_collections.series_ids
			// 是一串裸 ID，没有外键，所以这种情况是正常的。
			continue
		}
		views = append(views, aiGroupingReviewSeriesView{
			ID:    row.ID,
			Name:  row.Name,
			Title: row.Title,
		})
	}
	return views
}

func aiGroupingReviewCollectionToView(data aiGroupingViewData, row database.AiGroupingReviewCollection) aiGroupingReviewCollectionView {
	// series_ids、series 与 series_count 三者必须讲同一件事：只讲还在库里的系列。
	// 存储里的裸 ID 与 series_count 记的是 AI 提的那一份，其中已被删除的系列用户无从操作：
	// 照抄出去，界面会写着「2 个系列」却只列出 1 个，编辑草稿还会把那个既看不见
	// 也点不掉的悬空 ID 原样写回去。
	series := data.seriesViews(aiGroupingParseSeriesIDs(row.SeriesIds))
	ids := make([]int64, 0, len(series))
	for _, item := range series {
		ids = append(ids, item.ID)
	}
	view := aiGroupingReviewCollectionView{
		ID:          row.ID,
		ReviewID:    row.ReviewID,
		Name:        row.Name,
		Description: strings.TrimSpace(row.Description),
		SeriesIDs:   ids,
		Series:      series,
		SeriesCount: int64(len(series)),
		Status:      row.Status,
	}
	if row.CreatedCollectionID.Valid {
		value := row.CreatedCollectionID.Int64
		view.CreatedCollectionID = &value
	}
	return view
}

func aiGroupingReviewToView(data aiGroupingViewData, review database.AiGroupingReview, libraryName string) aiGroupingReviewView {
	collections := data.collectionsByReview[review.ID]
	view := aiGroupingReviewView{
		ID:              review.ID,
		LibraryID:       review.LibraryID,
		LibraryName:     libraryName,
		Provider:        review.Provider,
		Status:          review.Status,
		Summary:         review.Summary,
		CandidateCount:  review.CandidateCount,
		CollectionCount: review.CollectionCount,
		CreatedAt:       review.CreatedAt,
		UpdatedAt:       review.UpdatedAt,
		Collections:     make([]aiGroupingReviewCollectionView, 0, len(collections)),
	}
	if review.AppliedAt.Valid {
		value := review.AppliedAt.Time
		view.AppliedAt = &value
	}
	if review.RejectedAt.Valid {
		value := review.RejectedAt.Time
		view.RejectedAt = &value
	}
	for _, collection := range collections {
		view.Collections = append(view.Collections, aiGroupingReviewCollectionToView(data, collection))
	}
	return view
}

func aiGroupingReviewFromListRow(row database.ListAIGroupingReviewsRow) database.AiGroupingReview {
	return database.AiGroupingReview{
		ID:              row.ID,
		LibraryID:       row.LibraryID,
		Provider:        row.Provider,
		Status:          row.Status,
		Summary:         row.Summary,
		RawPayload:      row.RawPayload,
		CandidateCount:  row.CandidateCount,
		CollectionCount: row.CollectionCount,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
		AppliedAt:       row.AppliedAt,
		RejectedAt:      row.RejectedAt,
	}
}

func (c *Controller) listAIGroupingReviews(w http.ResponseWriter, r *http.Request) {
	libraryID, _ := strconv.ParseInt(r.URL.Query().Get("library_id"), 10, 64)
	if libraryID < 0 {
		libraryID = 0
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	limit, _ := strconv.ParseInt(r.URL.Query().Get("limit"), 10, 64)
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	offset, _ := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
	if offset < 0 {
		offset = 0
	}

	params := database.ListAIGroupingReviewsParams{
		LibraryID: libraryID,
		Status:    status,
		Offset:    offset,
		Limit:     limit,
	}
	total, err := c.store.CountAIGroupingReviews(r.Context(), database.CountAIGroupingReviewsParams{
		LibraryID: params.LibraryID,
		Status:    params.Status,
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to count AI grouping reviews")
		return
	}
	rows, err := c.store.ListAIGroupingReviews(r.Context(), params)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to list AI grouping reviews")
		return
	}
	if rows == nil {
		rows = []database.ListAIGroupingReviewsRow{}
	}

	payload := aiGroupingReviewsResponse{
		Items:  make([]aiGroupingReviewView, 0, len(rows)),
		Total:  total,
		Limit:  limit,
		Offset: offset,
	}
	reviewIDs := make([]int64, 0, len(rows))
	for _, row := range rows {
		reviewIDs = append(reviewIDs, row.ID)
	}
	// 预取要么全成功要么整体报错：两步查询的错误都不能被 `_` 吞掉，否则 DB 故障时
	// 接口会返回 200 且候选合集全为空，用户会以为 AI 一个分组都没分出来。
	data, err := loadAIGroupingViewData(r.Context(), c.store, reviewIDs)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to load AI grouping review details")
		return
	}
	for _, row := range rows {
		review := aiGroupingReviewFromListRow(row)
		payload.Items = append(payload.Items, aiGroupingReviewToView(data, review, row.LibraryName))
	}
	jsonResponse(w, http.StatusOK, payload)
}

func (c *Controller) aiGroupingActions() *organize.AIGrouping {
	return organize.NewAIGrouping(c.store, func() { c.PublishEvent("refresh") })
}
func aiGroupingActionIDs(r *http.Request) (int64, int64, bool) {
	reviewID, err := parseID(r, "reviewId")
	if err != nil {
		return 0, 0, false
	}
	id, err := parseID(r, "collectionId")
	return reviewID, id, err == nil
}
func aiGroupingActionError(w http.ResponseWriter, r *http.Request, err error, fallback string) {
	var invalid *organize.InvalidAIGroupingEdit
	switch {
	case errors.Is(err, organize.ErrAIGroupingNotFound), errors.Is(err, sql.ErrNoRows):
		jsonError(w, http.StatusNotFound, "AI grouping review collection not found")
	case errors.Is(err, organize.ErrAIGroupingNotPending):
		jsonError(w, http.StatusConflict, err.Error())
	case errors.Is(err, organize.ErrAIGroupingSeriesGone):
		jsonError(w, http.StatusConflict, apiText(requestLocale(r), "ai_grouping.collection.series_gone"))
	case errors.Is(err, organize.ErrCollectionNameTaken):
		jsonError(w, http.StatusConflict, "A collection with this name already exists")
	case errors.As(err, &invalid):
		jsonError(w, http.StatusBadRequest, err.Error())
	default:
		jsonError(w, http.StatusInternalServerError, fallback)
	}
}
func (c *Controller) updateAIGroupingReviewCollection(w http.ResponseWriter, r *http.Request) {
	reviewID, id, ok := aiGroupingActionIDs(r)
	if !ok {
		jsonError(w, http.StatusNotFound, "AI grouping review collection not found")
		return
	}
	var req updateAIGroupingReviewCollectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	updated, err := c.aiGroupingActions().EditCollection(r.Context(), reviewID, id, organize.EditAIGroupingCollection{Name: req.Name, Description: req.Description, SeriesIDs: req.SeriesIDs})
	if err != nil {
		aiGroupingActionError(w, r, err, "Failed to update AI grouping review collection")
		return
	}
	data, err := loadAIGroupingViewDataForCollection(r.Context(), c.store, updated)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to load AI grouping collection details")
		return
	}
	jsonResponse(w, http.StatusOK, aiGroupingReviewCollectionToView(data, updated))
}
func (c *Controller) applyAIGroupingReviewCollection(w http.ResponseWriter, r *http.Request) {
	reviewID, id, ok := aiGroupingActionIDs(r)
	if !ok {
		jsonError(w, http.StatusNotFound, "AI grouping review collection not found")
		return
	}
	created, err := c.aiGroupingActions().ApplyCollection(r.Context(), reviewID, id)
	if err != nil {
		aiGroupingActionError(w, r, err, "Failed to apply AI grouping review collection")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"success": true, "review_id": reviewID, "collection_id": id, "created_collection_id": created})
}
func (c *Controller) applyAIGroupingReview(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "reviewId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid AI grouping review ID")
		return
	}
	result, err := c.aiGroupingActions().ApplyReview(r.Context(), id)
	if err != nil {
		aiGroupingActionError(w, r, err, "Failed to apply AI grouping review")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"success": true, "review_id": id, "collections": result.Applied, "skipped": result.Skipped})
}
func (c *Controller) rejectAIGroupingReviewCollection(w http.ResponseWriter, r *http.Request) {
	reviewID, id, ok := aiGroupingActionIDs(r)
	if !ok {
		jsonError(w, http.StatusNotFound, "AI grouping review collection not found")
		return
	}
	if err := c.aiGroupingActions().RejectCollection(r.Context(), reviewID, id); err != nil {
		aiGroupingActionError(w, r, err, "Failed to reject AI grouping review collection")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"success": true, "review_id": reviewID, "collection_id": id})
}
func (c *Controller) rejectAIGroupingReview(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "reviewId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid AI grouping review ID")
		return
	}
	if err := c.aiGroupingActions().RejectReview(r.Context(), id); err != nil {
		aiGroupingActionError(w, r, err, "Failed to reject AI grouping review")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"success": true, "review_id": id})
}
