package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"manga-manager/internal/organize"
)

// ============================================
// [#2] 自定义合集 (Collections) 控制器
// ============================================

type Collection struct {
	ID             int64     `json:"id"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	CoverUrl       string    `json:"cover_url"`
	SortOrder      int       `json:"sort_order"`
	SeriesCount    int       `json:"series_count"`
	SourceType     string    `json:"source_type"`
	SourceReviewID *int64    `json:"source_review_id,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type CollectionSeriesItem struct {
	SeriesID   int64          `json:"series_id"`
	SeriesName string         `json:"series_name"`
	CoverPath  sql.NullString `json:"cover_path"`
	BookCount  int64          `json:"book_count"`
	AddedAt    time.Time      `json:"added_at"`
}

var errCollectionNameTaken = organize.ErrCollectionNameTaken

func collectionUseCaseError(w http.ResponseWriter, err error, fallback string) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, organize.ErrCollectionNameTaken):
		status = http.StatusConflict
	case errors.Is(err, organize.ErrCollectionNotFound), errors.Is(err, organize.ErrCollectionMemberNotFound):
		status = http.StatusNotFound
	case errors.Is(err, organize.ErrCollectionReadOnly):
		status = http.StatusForbidden
	case errors.Is(err, organize.ErrCollectionNameRequired), errors.Is(err, organize.ErrCollectionMembersRequired), errors.Is(err, organize.ErrCollectionTooManyMembers):
		status = http.StatusBadRequest
	}
	if status != http.StatusInternalServerError {
		fallback = err.Error()
	}
	if errors.Is(err, organize.ErrCollectionNameTaken) {
		fallback = "A collection with this name already exists"
	}
	jsonError(w, status, fallback)
}

// listCollections 返回可人工编辑的合集。作品群不在其中——见 ListCollectionsWithSeriesCount。
func (c *Controller) listCollections(w http.ResponseWriter, r *http.Request) {
	rows, err := organize.NewCollections(c.store).List(r.Context())
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to list collections")
		return
	}

	items := make([]Collection, 0, len(rows))
	for _, row := range rows {
		item := Collection{
			ID:          row.ID,
			Name:        row.Name,
			Description: row.Description.String,
			CoverUrl:    row.CoverUrl.String,
			SortOrder:   int(row.SortOrder),
			SeriesCount: int(row.SeriesCount),
			SourceType:  row.SourceType,
			CreatedAt:   row.CreatedAt.Time,
			UpdatedAt:   row.UpdatedAt.Time,
		}
		if row.SourceReviewID.Valid {
			value := row.SourceReviewID.Int64
			item.SourceReviewID = &value
		}
		items = append(items, item)
	}
	jsonResponse(w, http.StatusOK, items)
}

type CreateCollectionRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// createCollection 创建一个新合集。名称与重命名、快照固化、AI 分组应用同口径：先去首尾空白
// 再判空并查重，否则 "   " 会建出一个在列表里看不出名字的合集，" 科幻" 与 "科幻" 会并存成两条。
func (c *Controller) createCollection(w http.ResponseWriter, r *http.Request) {
	var req CreateCollectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	id, err := organize.NewCollections(c.store).Create(r.Context(), req.Name, req.Description)
	if err != nil {
		collectionUseCaseError(w, err, "Failed to create collection")
		return
	}
	jsonResponse(w, http.StatusCreated, map[string]interface{}{"id": id, "name": strings.TrimSpace(req.Name)})
}

// deleteCollection 删除合集
func (c *Controller) deleteCollection(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "collectionId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid collection ID")
		return
	}
	if err := organize.NewCollections(c.store).Delete(r.Context(), id); err != nil {
		collectionUseCaseError(w, err, "Failed to delete collection")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "deleted"})
}

type UpdateCollectionRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// updateCollection 更新合集名称和描述
func (c *Controller) updateCollection(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "collectionId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid collection ID")
		return
	}
	var req UpdateCollectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if err := organize.NewCollections(c.store).Update(r.Context(), id, req.Name, req.Description); err != nil {
		collectionUseCaseError(w, err, "Failed to update collection")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "updated"})
}

// getCollectionSeries 获取合集中的系列列表
func (c *Controller) getCollectionSeries(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "collectionId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid collection ID")
		return
	}

	rows, err := organize.NewCollections(c.store).Members(r.Context(), id)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to get collection series")
		return
	}

	items := make([]CollectionSeriesItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, CollectionSeriesItem{
			SeriesID:   row.SeriesID,
			SeriesName: row.SeriesName,
			CoverPath:  row.CoverPath,
			BookCount:  row.BookCount,
			AddedAt:    row.AddedAt.Time,
		})
	}
	jsonResponse(w, http.StatusOK, items)
}

type AddSeriesToCollectionRequest struct {
	SeriesIDs []int64 `json:"series_ids"`
}

// addSeriesToCollection 批量添加系列到合集
func (c *Controller) addSeriesToCollection(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "collectionId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid collection ID")
		return
	}
	var req AddSeriesToCollectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "series_ids is required")
		return
	}
	added, err := organize.NewCollections(c.store).AddMembers(r.Context(), id, req.SeriesIDs)
	if err != nil {
		collectionUseCaseError(w, err, "Failed to add series to collection")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{"added": added})
}

// removeSeriesFromCollection 从合集中移除系列
func (c *Controller) removeSeriesFromCollection(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "collectionId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid collection ID")
		return
	}
	sid, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}
	if err := organize.NewCollections(c.store).RemoveMember(r.Context(), id, sid); err != nil {
		collectionUseCaseError(w, err, "Failed to remove series")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "removed"})
}

// ============================================
// [#5] 系列关联 (Series Relations)
// ============================================

type SeriesRelation = organize.SeriesRelation

// getSeriesRelations 获取系列的所有关联
func (c *Controller) getSeriesRelations(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}

	items, err := c.loadSeriesRelations(r.Context(), seriesID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to get relations")
		return
	}
	jsonResponse(w, http.StatusOK, items)
}

// getSeriesFranchise 获取系列所在宇宙的整个关联关系连通图
func (c *Controller) getSeriesFranchise(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}

	items, err := c.relations.Graph(r.Context(), seriesID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to get franchise relations")
		return
	}

	// 转换为前端可用的格式
	type FranchiseRelation struct {
		ID               int64  `json:"id"`
		SourceSeriesID   int64  `json:"source_series_id"`
		TargetSeriesID   int64  `json:"target_series_id"`
		RelationType     string `json:"relation_type"`
		SourceSeriesName string `json:"source_series_name"`
		TargetSeriesName string `json:"target_series_name"`
		SourceCoverPath  string `json:"source_cover_path"`
		TargetCoverPath  string `json:"target_cover_path"`
	}

	result := make([]FranchiseRelation, 0, len(items))
	for _, item := range items {
		sourceCover := ""
		if item.SourceCoverPath.Valid {
			sourceCover = item.SourceCoverPath.String
		}
		targetCover := ""
		if item.TargetCoverPath.Valid {
			targetCover = item.TargetCoverPath.String
		}

		result = append(result, FranchiseRelation{
			ID:               item.ID,
			SourceSeriesID:   item.SourceSeriesID,
			TargetSeriesID:   item.TargetSeriesID,
			RelationType:     item.RelationType,
			SourceSeriesName: item.SourceSeriesName,
			TargetSeriesName: item.TargetSeriesName,
			SourceCoverPath:  sourceCover,
			TargetCoverPath:  targetCover,
		})
	}

	jsonResponse(w, http.StatusOK, result)
}

// getLibraryFranchiseGraph 获取资源库全景关联关系连通图
func (c *Controller) getLibraryFranchiseGraph(w http.ResponseWriter, r *http.Request) {
	libraryID, err := parseID(r, "libraryId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid library ID")
		return
	}

	items, err := c.relations.LibraryGraph(r.Context(), libraryID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to get library franchise relations")
		return
	}

	// 硬性上限：库级图谱必须截断，否则超大库会产生巨型 payload 且前端 O(N^2) 力导向布局冻结主线程。
	// 截断到安全边界（前端另有更小的节点级上限做可读性截断，见 franchise-graph 页）；非静默，记录被丢弃的数量。
	const maxLibraryFranchiseRelations = 4000
	if len(items) > maxLibraryFranchiseRelations {
		slog.Warn("library franchise graph truncated",
			"library_id", libraryID, "total_relations", len(items), "cap", maxLibraryFranchiseRelations)
		items = items[:maxLibraryFranchiseRelations]
	}

	type FranchiseRelation struct {
		ID               int64  `json:"id"`
		SourceSeriesID   int64  `json:"source_series_id"`
		TargetSeriesID   int64  `json:"target_series_id"`
		RelationType     string `json:"relation_type"`
		SourceSeriesName string `json:"source_series_name"`
		TargetSeriesName string `json:"target_series_name"`
		SourceCoverPath  string `json:"source_cover_path"`
		TargetCoverPath  string `json:"target_cover_path"`
	}

	result := make([]FranchiseRelation, 0, len(items))
	for _, item := range items {
		sourceCover := ""
		if item.SourceCoverPath.Valid {
			sourceCover = item.SourceCoverPath.String
		}
		targetCover := ""
		if item.TargetCoverPath.Valid {
			targetCover = item.TargetCoverPath.String
		}

		result = append(result, FranchiseRelation{
			ID:               item.ID,
			SourceSeriesID:   item.SourceSeriesID,
			TargetSeriesID:   item.TargetSeriesID,
			RelationType:     item.RelationType,
			SourceSeriesName: item.SourceSeriesName,
			TargetSeriesName: item.TargetSeriesName,
			SourceCoverPath:  sourceCover,
			TargetCoverPath:  targetCover,
		})
	}

	jsonResponse(w, http.StatusOK, result)
}

func (c *Controller) loadSeriesRelations(ctx context.Context, id int64) ([]SeriesRelation, error) {
	return c.relations.List(ctx, id)
}

type CreateRelationRequest struct {
	TargetSeriesID int64  `json:"target_series_id"`
	RelationType   string `json:"relation_type"`
}

// createSeriesRelation 创建系列关联
func relationUseCaseError(w http.ResponseWriter, err error, fallback string) {
	switch {
	case errors.Is(err, organize.ErrRelationSelf):
		jsonError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, organize.ErrRelationTargetMissing), errors.Is(err, organize.ErrRelationMissing):
		jsonError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, organize.ErrRelationTypeRequired):
		jsonError(w, http.StatusBadRequest, err.Error())
	default:
		jsonError(w, http.StatusInternalServerError, fallback)
	}
}
func (c *Controller) createSeriesRelation(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}
	var req CreateRelationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TargetSeriesID == 0 {
		jsonError(w, http.StatusBadRequest, "target_series_id is required")
		return
	}
	result, err := c.relations.Create(r.Context(), id, req.TargetSeriesID, req.RelationType)
	if err != nil {
		relationUseCaseError(w, err, "Failed to create relation")
		return
	}
	if result.Existing {
		jsonResponse(w, http.StatusOK, map[string]interface{}{"status": "exists", "id": result.ID})
		return
	}
	jsonResponse(w, http.StatusCreated, map[string]string{"status": "created"})
}
func (c *Controller) deleteSeriesRelation(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "relationId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid relation ID")
		return
	}
	if err := c.relations.Delete(r.Context(), id); err != nil {
		relationUseCaseError(w, err, "Failed to delete relation")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "deleted"})
}

type UpdateRelationRequest struct {
	RelationType string `json:"relation_type"`
}

func (c *Controller) updateSeriesRelation(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "relationId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid relation ID")
		return
	}
	var req UpdateRelationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "relation_type is required")
		return
	}
	if err := c.relations.Update(r.Context(), id, req.RelationType); err != nil {
		relationUseCaseError(w, err, "Failed to update relation")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "updated"})
}
