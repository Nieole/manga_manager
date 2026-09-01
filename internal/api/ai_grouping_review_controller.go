package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/metadata"
)

// errAIGroupingCollectionPreempted 表示这条候选合集在本次加载与写入之间已被别人处理掉了。
// 它只在包内用于触发整体回滚，对外一律表达为 409——与前置检查发现的是同一件事。
var errAIGroupingCollectionPreempted = errors.New("ai grouping review collection is no longer pending")

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
	ids := aiGroupingParseSeriesIDs(row.SeriesIds)
	view := aiGroupingReviewCollectionView{
		ID:          row.ID,
		ReviewID:    row.ReviewID,
		Name:        row.Name,
		Description: strings.TrimSpace(row.Description),
		SeriesIDs:   ids,
		Series:      data.seriesViews(ids),
		SeriesCount: row.SeriesCount,
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
	collectionData, err := loadAIGroupingViewDataForCollection(r.Context(), c.store, updated)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to load AI grouping collection details")
		return
	}
	jsonResponse(w, http.StatusOK, aiGroupingReviewCollectionToView(collectionData, updated))
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
	if errors.Is(err, errAIGroupingCollectionPreempted) {
		jsonError(w, http.StatusConflict, "AI grouping review collection is not pending")
		return
	}
	if errors.Is(err, errCollectionNameTaken) {
		jsonError(w, http.StatusConflict, "A collection with this name already exists")
		return
	}
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
	// 批量入口与单条入口共用同一道 CAS，因此也共用同一个结局：只要有一条候选合集在加载之后
	// 被别人处理掉，整批回滚报 409，用户刷新后重来即可——绝不容忍「部分提交 + 重复合集」。
	if errors.Is(err, errAIGroupingCollectionPreempted) {
		jsonError(w, http.StatusConflict, "AI grouping review is not pending")
		return
	}
	if errors.Is(err, errCollectionNameTaken) {
		jsonError(w, http.StatusConflict, "A collection with this name already exists")
		return
	}
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
		// 先记下 id 再判收尾：应用最后一条候选合集会走进下面的提前 return，
		// 而那恰恰是最常见的形态（只有一条候选合集的审核），漏掉就永远报不出新建合集。
		createdID = id
		pending, err := q.CountPendingAIGroupingReviewCollections(ctx, review.ID)
		if err != nil {
			return err
		}
		if pending == 0 {
			return finalizeAIGroupingReviewStatus(ctx, q, review.ID)
		}
		return nil
	})
	if err != nil {
		// 事务已整体回滚，那个 id 指向的合集并不存在。
		return 0, err
	}
	return createdID, nil
}

func applyAIGroupingReviewCollectionWithQueries(ctx context.Context, q *database.Queries, review database.AiGroupingReview, collection database.AiGroupingReviewCollection) (int64, error) {
	seriesIDs := aiGroupingParseSeriesIDs(collection.SeriesIds)
	name := strings.TrimSpace(collection.Name)
	if name == "" || len(seriesIDs) == 0 {
		return 0, sql.ErrNoRows
	}
	// 候选合集名由 AI 给出，同样受「哪里都不许出现两个同名合集」约束：查重落在事务内的 SQL 上，
	// 因此整批应用时先建的那几条也算数——同一批里两条同名候选，第二条就在这里被挡下。
	// 撞名不是能重试的瞬时状态，用户得先改名或驳回这条候选，因此整批回滚。
	switch _, err := q.CollectionNameExists(ctx, name); {
	case err == nil:
		return 0, errCollectionNameTaken
	case !errors.Is(err, sql.ErrNoRows):
		return 0, err
	}
	created, err := q.CreateCollection(ctx, database.CreateCollectionParams{
		Name:           name,
		Description:    sql.NullString{String: strings.TrimSpace(collection.Description), Valid: strings.TrimSpace(collection.Description) != ""},
		SourceType:     "ai_grouping",
		SourceReviewID: sql.NullInt64{Int64: review.ID, Valid: true},
	})
	if err != nil {
		return 0, err
	}
	for _, seriesID := range seriesIDs {
		if _, err := q.AddSeriesToCollection(ctx, database.AddSeriesToCollectionParams{
			CollectionID: created.ID,
			SeriesID:     seriesID,
		}); err != nil {
			return 0, err
		}
	}
	if err := q.TouchCollection(ctx, created.ID); err != nil {
		return 0, err
	}
	// 同一事务内把候选合集从待应用 CAS 到已应用：建合集与状态推进原子，否则两个都读到
	// 待应用旧快照的请求会各建一个同名合集，先建的那个还会被后写的 created_collection_id 顶掉。
	// 守卫必须落在事务内的 SQL 上——调用方的前置检查读到的是事务外的快照，随时可能过期。
	rows, err := q.MarkAIGroupingReviewCollectionApplied(ctx, database.MarkAIGroupingReviewCollectionAppliedParams{
		CreatedCollectionID: sql.NullInt64{Int64: created.ID, Valid: true},
		ID:                  collection.ID,
	})
	if err != nil {
		return 0, err
	}
	if rows == 0 {
		// 刚建的合集与成员随事务整体撤销。
		return 0, errAIGroupingCollectionPreempted
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
