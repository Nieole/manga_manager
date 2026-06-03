package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"manga-manager/internal/database"
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

// listCollections 返回所有合集
func (c *Controller) listCollections(w http.ResponseWriter, r *http.Request) {
	rows, err := c.store.ListCollectionsWithSeriesCount(r.Context())
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

// createCollection 创建一个新合集
func (c *Controller) createCollection(w http.ResponseWriter, r *http.Request) {
	var req CreateCollectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		jsonError(w, http.StatusBadRequest, "Name is required")
		return
	}

	id, err := c.store.CreateSimpleCollection(r.Context(), database.CreateSimpleCollectionParams{
		Name:        req.Name,
		Description: nullStringFromString(req.Description),
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to create collection")
		return
	}
	jsonResponse(w, http.StatusCreated, map[string]interface{}{"id": id, "name": req.Name})
}

// deleteCollection 删除合集
func (c *Controller) deleteCollection(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "collectionId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid collection ID")
		return
	}
	if err := c.store.DeleteCollection(r.Context(), id); err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to delete collection")
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
	if err := c.store.UpdateCollectionDetails(r.Context(), database.UpdateCollectionDetailsParams{
		Name:        req.Name,
		Description: nullStringFromString(req.Description),
		ID:          id,
	}); err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to update collection")
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

	rows, err := c.store.ListCollectionSeries(r.Context(), id)
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.SeriesIDs) == 0 {
		jsonError(w, http.StatusBadRequest, "series_ids is required")
		return
	}

	ctx := r.Context()
	added := 0
	for _, sid := range req.SeriesIDs {
		if err := c.store.AddSeriesToCollection(ctx, database.AddSeriesToCollectionParams{
			CollectionID: id,
			SeriesID:     sid,
		}); err == nil {
			added++
		}
	}

	_ = c.store.TouchCollection(ctx, id)

	jsonResponse(w, http.StatusOK, map[string]interface{}{"added": added})
}

// removeSeriesFromCollection 从合集中移除系列
func (c *Controller) removeSeriesFromCollection(w http.ResponseWriter, r *http.Request) {
	collectionID, err := parseID(r, "collectionId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid collection ID")
		return
	}
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}

	if err := c.store.RemoveSeriesFromCollection(r.Context(), database.RemoveSeriesFromCollectionParams{
		CollectionID: collectionID,
		SeriesID:     seriesID,
	}); err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to remove series")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "removed"})
}

// ============================================
// [#5] 系列关联 (Series Relations)
// ============================================

type SeriesRelation struct {
	ID               int64  `json:"id"`
	TargetSeriesID   int64  `json:"target_series_id"`
	TargetSeriesName string `json:"target_series_name"`
	RelationType     string `json:"relation_type"`
}

// inverseRelationType returns the inverse relation type for display
// when viewing a relation from the target's perspective.
// e.g. A → B (spinoff) means B sees A as "parent"
func inverseRelationType(rt string) string {
	switch rt {
	case "sequel":
		return "prequel"
	case "prequel":
		return "sequel"
	case "spinoff":
		return "parent_story"
	case "side_story":
		return "parent_story"
	case "adaptation":
		return "source"
	case "remake":
		return "original"
	case "same_universe":
		return "same_universe"
	case "parent_story":
		return "spinoff"
	case "alternative_version":
		return "alternative_version"
	case "alternate_story":
		return "alternate_story"
	case "crossover":
		return "crossover"
	case "one_shot":
		return "serialization"
	case "anthology":
		return "original"
	case "doujinshi":
		return "original"
	default:
		return rt
	}
}

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

func (c *Controller) loadSeriesRelations(ctx context.Context, seriesID int64) ([]SeriesRelation, error) {
	forward, err := c.store.ListForwardSeriesRelations(ctx, seriesID)
	if err != nil {
		return nil, err
	}

	items := make([]SeriesRelation, 0, len(forward))
	for _, row := range forward {
		items = append(items, SeriesRelation{
			ID:               row.ID,
			TargetSeriesID:   row.TargetSeriesID,
			TargetSeriesName: row.TargetSeriesName,
			RelationType:     row.RelationType,
		})
	}

	reverse, err := c.store.ListReverseSeriesRelations(ctx, seriesID)
	if err != nil {
		return nil, err
	}
	for _, row := range reverse {
		items = append(items, SeriesRelation{
			ID:               row.ID,
			TargetSeriesID:   row.TargetSeriesID,
			TargetSeriesName: row.TargetSeriesName,
			RelationType:     inverseRelationType(row.RelationType),
		})
	}

	return items, nil
}

type CreateRelationRequest struct {
	TargetSeriesID int64  `json:"target_series_id"`
	RelationType   string `json:"relation_type"`
}

// createSeriesRelation 创建系列关联
func (c *Controller) createSeriesRelation(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}
	var req CreateRelationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TargetSeriesID == 0 {
		jsonError(w, http.StatusBadRequest, "target_series_id is required")
		return
	}
	if req.RelationType == "" {
		req.RelationType = "sequel"
	}
	req.RelationType = strings.TrimSpace(req.RelationType)
	if req.RelationType == "" {
		req.RelationType = "sequel"
	}
	if req.TargetSeriesID == seriesID {
		jsonError(w, http.StatusUnprocessableEntity, "A series cannot relate to itself")
		return
	}

	if _, err := c.store.SeriesExistsByID(r.Context(), req.TargetSeriesID); err != nil {
		jsonError(w, http.StatusNotFound, "Target series not found")
		return
	}

	existingID, err := c.store.FindExistingSeriesRelation(r.Context(), database.FindExistingSeriesRelationParams{
		LeftID:  seriesID,
		RightID: req.TargetSeriesID,
	})
	if err == nil {
		jsonResponse(w, http.StatusOK, map[string]interface{}{"status": "exists", "id": existingID})
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		jsonError(w, http.StatusInternalServerError, "Failed to check relation")
		return
	}

	if err := c.store.CreateSeriesRelation(r.Context(), database.CreateSeriesRelationParams{
		SourceSeriesID: seriesID,
		TargetSeriesID: req.TargetSeriesID,
		RelationType:   req.RelationType,
	}); err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to create relation")
		return
	}
	jsonResponse(w, http.StatusCreated, map[string]string{"status": "created"})
}

// deleteSeriesRelation 删除系列关联
func (c *Controller) deleteSeriesRelation(w http.ResponseWriter, r *http.Request) {
	relationID, err := parseID(r, "relationId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid relation ID")
		return
	}
	_ = c.store.DeleteSeriesRelation(r.Context(), relationID)
	jsonResponse(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func nullStringFromString(value string) sql.NullString {
	if value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}
