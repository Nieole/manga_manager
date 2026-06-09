// 业务说明：本文件是业务实现，属于后端 HTTP API 层，负责把前端请求转换为数据库、扫描器、图片处理和元数据服务调用。
// 它承载资料库浏览、阅读器取页、系列维护、任务进度、系统设置和静态资源缓存等对外业务契约。
// 维护时应重点关注请求参数校验、错误语义、缓存头、并发任务状态和前后端字段兼容性。

package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/metadata"
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

func aiGroupingParseSeriesIDs(raw string) []int64 {
	var ids []int64
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return []int64{}
	}
	clean := ids[:0]
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		clean = append(clean, id)
	}
	return clean
}

func aiGroupingSeriesViews(ctx context.Context, q database.Querier, ids []int64) []aiGroupingReviewSeriesView {
	if len(ids) == 0 {
		return []aiGroupingReviewSeriesView{}
	}
	rows, err := q.GetSeriesNamesByIDs(ctx, ids)
	if err != nil {
		return []aiGroupingReviewSeriesView{}
	}
	byID := make(map[int64]database.GetSeriesNamesByIDsRow, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	views := make([]aiGroupingReviewSeriesView, 0, len(ids))
	for _, id := range ids {
		row, ok := byID[id]
		if !ok {
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

func aiGroupingReviewCollectionToView(ctx context.Context, q database.Querier, row database.AiGroupingReviewCollection) aiGroupingReviewCollectionView {
	ids := aiGroupingParseSeriesIDs(row.SeriesIds)
	view := aiGroupingReviewCollectionView{
		ID:          row.ID,
		ReviewID:    row.ReviewID,
		Name:        row.Name,
		Description: strings.TrimSpace(row.Description),
		SeriesIDs:   ids,
		Series:      aiGroupingSeriesViews(ctx, q, ids),
		SeriesCount: row.SeriesCount,
		Status:      row.Status,
	}
	if row.CreatedCollectionID.Valid {
		value := row.CreatedCollectionID.Int64
		view.CreatedCollectionID = &value
	}
	return view
}

func aiGroupingReviewToView(ctx context.Context, q database.Querier, review database.AiGroupingReview, libraryName string) aiGroupingReviewView {
	collections, _ := q.ListAIGroupingReviewCollections(ctx, review.ID)
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
		view.Collections = append(view.Collections, aiGroupingReviewCollectionToView(ctx, q, collection))
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

type aiGroupingReviewProposal struct {
	Name        string
	Description string
	SeriesIDs   []int64
}

func normalizeAIGroupingReviewProposals(candidates []metadata.CandidateSeries, groups []metadata.AIGroupCollection) []aiGroupingReviewProposal {
	candidateIDs := make(map[int64]struct{}, len(candidates))
	for _, candidate := range candidates {
		candidateIDs[candidate.ID] = struct{}{}
	}

	proposals := make([]aiGroupingReviewProposal, 0, len(groups))
	for _, group := range groups {
		name := strings.TrimSpace(group.Name)
		if name == "" {
			continue
		}
		seriesIDs := make([]int64, 0, len(group.SeriesIDs))
		seen := make(map[int64]struct{}, len(group.SeriesIDs))
		for _, id := range group.SeriesIDs {
			if _, ok := candidateIDs[id]; !ok {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			seriesIDs = append(seriesIDs, id)
		}
		sort.Slice(seriesIDs, func(i, j int) bool { return seriesIDs[i] < seriesIDs[j] })
		if len(seriesIDs) == 0 {
			continue
		}
		proposals = append(proposals, aiGroupingReviewProposal{
			Name:        name,
			Description: strings.TrimSpace(group.Description),
			SeriesIDs:   seriesIDs,
		})
	}
	return proposals
}

func (c *Controller) createAIGroupingReview(ctx context.Context, libraryID int64, providerName string, candidates []metadata.CandidateSeries, groups []metadata.AIGroupCollection) (database.AiGroupingReview, int, error) {
	var created database.AiGroupingReview
	proposals := normalizeAIGroupingReviewProposals(candidates, groups)
	if len(proposals) == 0 {
		return created, 0, nil
	}
	payload, _ := json.Marshal(map[string]any{
		"candidates":  candidates,
		"collections": groups,
		"proposals":   proposals,
	})

	err := c.store.ExecTx(ctx, func(q *database.Queries) error {
		review, err := q.CreateAIGroupingReview(ctx, database.CreateAIGroupingReviewParams{
			LibraryID:       libraryID,
			Provider:        strings.TrimSpace(providerName),
			Status:          "pending",
			Summary:         "AI grouping review queued",
			RawPayload:      string(payload),
			CandidateCount:  int64(len(candidates)),
			CollectionCount: int64(len(proposals)),
		})
		if err != nil {
			return err
		}

		for _, proposal := range proposals {
			rawIDs, _ := json.Marshal(proposal.SeriesIDs)
			if _, err := q.CreateAIGroupingReviewCollection(ctx, database.CreateAIGroupingReviewCollectionParams{
				ReviewID:    review.ID,
				Name:        proposal.Name,
				Description: proposal.Description,
				SeriesIds:   string(rawIDs),
				SeriesCount: int64(len(proposal.SeriesIDs)),
				Status:      "pending",
			}); err != nil {
				return err
			}
		}

		created = review
		return nil
	})
	return created, len(proposals), err
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
	for _, row := range rows {
		review := aiGroupingReviewFromListRow(row)
		payload.Items = append(payload.Items, aiGroupingReviewToView(r.Context(), c.store, review, row.LibraryName))
	}
	jsonResponse(w, http.StatusOK, payload)
}

func (c *Controller) getAIGroupingReviewCollectionForAction(r *http.Request) (database.AiGroupingReview, database.AiGroupingReviewCollection, bool) {
	reviewID, err := parseID(r, "reviewId")
	if err != nil {
		return database.AiGroupingReview{}, database.AiGroupingReviewCollection{}, false
	}
	collectionID, err := parseID(r, "collectionId")
	if err != nil {
		return database.AiGroupingReview{}, database.AiGroupingReviewCollection{}, false
	}
	review, err := c.store.GetAIGroupingReview(r.Context(), reviewID)
	if err != nil {
		return database.AiGroupingReview{}, database.AiGroupingReviewCollection{}, false
	}
	collection, err := c.store.GetAIGroupingReviewCollection(r.Context(), collectionID)
	if err != nil || collection.ReviewID != review.ID {
		return database.AiGroupingReview{}, database.AiGroupingReviewCollection{}, false
	}
	return review, collection, true
}

func (c *Controller) updateAIGroupingReviewCollection(w http.ResponseWriter, r *http.Request) {
	review, collection, ok := c.getAIGroupingReviewCollectionForAction(r)
	if !ok {
		jsonError(w, http.StatusNotFound, "AI grouping review collection not found")
		return
	}
	if review.Status != "pending" || collection.Status != "pending" {
		jsonError(w, http.StatusConflict, "AI grouping review collection is not editable")
		return
	}

	var req updateAIGroupingReviewCollectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		jsonError(w, http.StatusBadRequest, "Name is required")
		return
	}
	cleanIDs := aiGroupingParseSeriesIDs(mustJSON(req.SeriesIDs))
	if len(cleanIDs) == 0 {
		jsonError(w, http.StatusBadRequest, "series_ids is required")
		return
	}
	if !c.aiGroupingSeriesIDsBelongToReview(r.Context(), review.ID, collection.ID, cleanIDs) {
		jsonError(w, http.StatusBadRequest, "series_ids must come from the same review")
		return
	}
	rawIDs, _ := json.Marshal(cleanIDs)
	updated, err := c.store.UpdateAIGroupingReviewCollection(r.Context(), database.UpdateAIGroupingReviewCollectionParams{
		Name:        name,
		Description: strings.TrimSpace(req.Description),
		SeriesIds:   string(rawIDs),
		SeriesCount: int64(len(cleanIDs)),
		ID:          collection.ID,
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to update AI grouping review collection")
		return
	}
	jsonResponse(w, http.StatusOK, aiGroupingReviewCollectionToView(r.Context(), c.store, updated))
}

func mustJSON(ids []int64) string {
	raw, _ := json.Marshal(ids)
	return string(raw)
}

func (c *Controller) aiGroupingSeriesIDsBelongToReview(ctx context.Context, reviewID, currentCollectionID int64, ids []int64) bool {
	collections, err := c.store.ListAIGroupingReviewCollections(ctx, reviewID)
	if err != nil {
		return false
	}
	allowed := make(map[int64]struct{})
	for _, collection := range collections {
		if collection.Status != "pending" && collection.ID != currentCollectionID {
			continue
		}
		for _, id := range aiGroupingParseSeriesIDs(collection.SeriesIds) {
			allowed[id] = struct{}{}
		}
	}
	for _, id := range ids {
		if _, ok := allowed[id]; !ok {
			return false
		}
	}
	return true
}

func (c *Controller) applyAIGroupingReviewCollection(w http.ResponseWriter, r *http.Request) {
	review, collection, ok := c.getAIGroupingReviewCollectionForAction(r)
	if !ok {
		jsonError(w, http.StatusNotFound, "AI grouping review collection not found")
		return
	}
	if review.Status != "pending" || collection.Status != "pending" {
		jsonError(w, http.StatusConflict, "AI grouping review collection is not pending")
		return
	}
	createdID, err := c.applyAIGroupingReviewCollectionTx(r.Context(), review, collection)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to apply AI grouping review collection")
		return
	}
	c.PublishEvent("refresh")
	jsonResponse(w, http.StatusOK, map[string]any{
		"success":               true,
		"review_id":             review.ID,
		"collection_id":         collection.ID,
		"created_collection_id": createdID,
	})
}

func (c *Controller) applyAIGroupingReview(w http.ResponseWriter, r *http.Request) {
	reviewID, err := parseID(r, "reviewId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid AI grouping review ID")
		return
	}
	review, err := c.store.GetAIGroupingReview(r.Context(), reviewID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "AI grouping review not found")
		return
	}
	if review.Status != "pending" {
		jsonError(w, http.StatusConflict, "AI grouping review is not pending")
		return
	}
	collections, err := c.store.ListAIGroupingReviewCollections(r.Context(), reviewID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to load AI grouping review")
		return
	}
	applied := 0
	err = c.store.ExecTx(r.Context(), func(q *database.Queries) error {
		for _, item := range collections {
			if item.Status != "pending" {
				continue
			}
			if _, err := applyAIGroupingReviewCollectionWithQueries(r.Context(), q, review, item); err != nil {
				return err
			}
			applied++
		}
		return finalizeAIGroupingReviewStatus(r.Context(), q, review.ID)
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to apply AI grouping review")
		return
	}
	c.PublishEvent("refresh")
	jsonResponse(w, http.StatusOK, map[string]any{
		"success":     true,
		"review_id":   reviewID,
		"collections": applied,
	})
}

func (c *Controller) applyAIGroupingReviewCollectionTx(ctx context.Context, review database.AiGroupingReview, collection database.AiGroupingReviewCollection) (int64, error) {
	var createdID int64
	err := c.store.ExecTx(ctx, func(q *database.Queries) error {
		id, err := applyAIGroupingReviewCollectionWithQueries(ctx, q, review, collection)
		if err != nil {
			return err
		}
		pending, err := q.CountPendingAIGroupingReviewCollections(ctx, review.ID)
		if err != nil {
			return err
		}
		if pending == 0 {
			return finalizeAIGroupingReviewStatus(ctx, q, review.ID)
		}
		createdID = id
		return nil
	})
	return createdID, err
}

func applyAIGroupingReviewCollectionWithQueries(ctx context.Context, q *database.Queries, review database.AiGroupingReview, collection database.AiGroupingReviewCollection) (int64, error) {
	seriesIDs := aiGroupingParseSeriesIDs(collection.SeriesIds)
	if strings.TrimSpace(collection.Name) == "" || len(seriesIDs) == 0 {
		return 0, sql.ErrNoRows
	}
	created, err := q.CreateCollection(ctx, database.CreateCollectionParams{
		Name:           strings.TrimSpace(collection.Name),
		Description:    sql.NullString{String: strings.TrimSpace(collection.Description), Valid: strings.TrimSpace(collection.Description) != ""},
		SourceType:     "ai_grouping",
		SourceReviewID: sql.NullInt64{Int64: review.ID, Valid: true},
	})
	if err != nil {
		return 0, err
	}
	for _, seriesID := range seriesIDs {
		if err := q.AddSeriesToCollection(ctx, database.AddSeriesToCollectionParams{
			CollectionID: created.ID,
			SeriesID:     seriesID,
		}); err != nil {
			return 0, err
		}
	}
	if err := q.TouchCollection(ctx, created.ID); err != nil {
		return 0, err
	}
	if err := q.MarkAIGroupingReviewCollectionApplied(ctx, database.MarkAIGroupingReviewCollectionAppliedParams{
		CreatedCollectionID: sql.NullInt64{Int64: created.ID, Valid: true},
		ID:                  collection.ID,
	}); err != nil {
		return 0, err
	}
	return created.ID, nil
}

func (c *Controller) rejectAIGroupingReviewCollection(w http.ResponseWriter, r *http.Request) {
	review, collection, ok := c.getAIGroupingReviewCollectionForAction(r)
	if !ok {
		jsonError(w, http.StatusNotFound, "AI grouping review collection not found")
		return
	}
	if review.Status != "pending" || collection.Status != "pending" {
		jsonError(w, http.StatusConflict, "AI grouping review collection is not pending")
		return
	}
	if err := c.store.ExecTx(r.Context(), func(q *database.Queries) error {
		if err := q.MarkAIGroupingReviewCollectionRejected(r.Context(), collection.ID); err != nil {
			return err
		}
		pending, err := q.CountPendingAIGroupingReviewCollections(r.Context(), review.ID)
		if err != nil {
			return err
		}
		if pending == 0 {
			return finalizeAIGroupingReviewStatus(r.Context(), q, review.ID)
		}
		return nil
	}); err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to reject AI grouping review collection")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{
		"success":       true,
		"review_id":     review.ID,
		"collection_id": collection.ID,
	})
}

func finalizeAIGroupingReviewStatus(ctx context.Context, q *database.Queries, reviewID int64) error {
	applied, err := q.CountAppliedAIGroupingReviewCollections(ctx, reviewID)
	if err != nil {
		return err
	}
	status := "rejected"
	if applied > 0 {
		status = "applied"
	}
	_, err = q.UpdateAIGroupingReviewStatus(ctx, database.UpdateAIGroupingReviewStatusParams{
		Status: status,
		ID:     reviewID,
	})
	return err
}

func (c *Controller) rejectAIGroupingReview(w http.ResponseWriter, r *http.Request) {
	reviewID, err := parseID(r, "reviewId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid AI grouping review ID")
		return
	}
	review, err := c.store.GetAIGroupingReview(r.Context(), reviewID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "AI grouping review not found")
		return
	}
	if review.Status != "pending" {
		jsonError(w, http.StatusConflict, "AI grouping review is not pending")
		return
	}
	if err := c.store.ExecTx(r.Context(), func(q *database.Queries) error {
		if err := q.MarkAIGroupingReviewCollectionsRejected(r.Context(), review.ID); err != nil {
			return err
		}
		_, err := q.UpdateAIGroupingReviewStatus(r.Context(), database.UpdateAIGroupingReviewStatusParams{
			Status: "rejected",
			ID:     review.ID,
		})
		return err
	}); err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to reject AI grouping review")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{
		"success":   true,
		"review_id": reviewID,
	})
}
