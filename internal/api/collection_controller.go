package api

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"manga-manager/internal/database"
)

// ============================================
// [#2] 自定义合集 (Collections) 控制器
// ============================================

type Collection struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CoverUrl    string    `json:"cover_url"`
	SortOrder   int       `json:"sort_order"`
	SeriesCount int       `json:"series_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
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
	rows, err := c.store.(*database.SqlStore).DB().QueryContext(r.Context(), `
		SELECT c.id, c.name, c.description, c.cover_url, c.sort_order, c.created_at, c.updated_at,
			(SELECT COUNT(*) FROM collection_series cs WHERE cs.collection_id = c.id) as series_count
		FROM collections c ORDER BY c.sort_order, c.name
	`)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to list collections")
		return
	}
	defer rows.Close()

	var items []Collection
	for rows.Next() {
		var item Collection
		if err := rows.Scan(&item.ID, &item.Name, &item.Description, &item.CoverUrl, &item.SortOrder, &item.CreatedAt, &item.UpdatedAt, &item.SeriesCount); err != nil {
			slog.Error("Failed to scan collection", "error", err)
			continue
		}
		items = append(items, item)
	}
	if items == nil {
		items = []Collection{}
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

	db := c.store.(*database.SqlStore).DB()
	result, err := db.ExecContext(r.Context(),
		`INSERT INTO collections (name, description) VALUES (?, ?)`, req.Name, req.Description)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to create collection")
		return
	}

	id, _ := result.LastInsertId()
	jsonResponse(w, http.StatusCreated, map[string]interface{}{"id": id, "name": req.Name})
}

// deleteCollection 删除合集
func (c *Controller) deleteCollection(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "collectionId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid collection ID")
		return
	}
	db := c.store.(*database.SqlStore).DB()
	_, err = db.ExecContext(r.Context(), `DELETE FROM collections WHERE id = ?`, id)
	if err != nil {
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
	db := c.store.(*database.SqlStore).DB()
	_, err = db.ExecContext(r.Context(),
		`UPDATE collections SET name = ?, description = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		req.Name, req.Description, id)
	if err != nil {
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

	db := c.store.(*database.SqlStore).DB()
	rows, err := db.QueryContext(r.Context(), `
		SELECT s.id, s.name,
			(SELECT b.cover_path FROM books b WHERE b.series_id = s.id AND b.cover_path IS NOT NULL AND b.cover_path != '' ORDER BY b.sort_number, b.name LIMIT 1) as cover_path,
			s.book_count, cs.added_at
		FROM collection_series cs
		JOIN series s ON s.id = cs.series_id
		WHERE cs.collection_id = ?
		ORDER BY cs.sort_order, s.name
	`, id)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to get collection series")
		return
	}
	defer rows.Close()

	var items []CollectionSeriesItem
	for rows.Next() {
		var item CollectionSeriesItem
		if err := rows.Scan(&item.SeriesID, &item.SeriesName, &item.CoverPath, &item.BookCount, &item.AddedAt); err != nil {
			continue
		}
		items = append(items, item)
	}
	if items == nil {
		items = []CollectionSeriesItem{}
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

	db := c.store.(*database.SqlStore).DB()
	ctx := r.Context()
	added := 0
	for _, sid := range req.SeriesIDs {
		_, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO collection_series (collection_id, series_id) VALUES (?, ?)`, id, sid)
		if err == nil {
			added++
		}
	}

	// 更新合集 updated_at
	_, _ = db.ExecContext(ctx, `UPDATE collections SET updated_at = CURRENT_TIMESTAMP WHERE id = ?`, id)

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

	db := c.store.(*database.SqlStore).DB()
	_, err = db.ExecContext(r.Context(), `DELETE FROM collection_series WHERE collection_id = ? AND series_id = ?`, collectionID, seriesID)
	if err != nil {
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

// getSeriesRelations 获取系列的所有关联
func (c *Controller) getSeriesRelations(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}

	db := c.store.(*database.SqlStore).DB()
	rows, err := db.QueryContext(r.Context(), `
		SELECT sr.id, sr.target_series_id, s.name, sr.relation_type
		FROM series_relations sr
		JOIN series s ON s.id = sr.target_series_id
		WHERE sr.source_series_id = ?
		UNION ALL
		SELECT sr.id, sr.source_series_id, s.name, sr.relation_type
		FROM series_relations sr
		JOIN series s ON s.id = sr.source_series_id
		WHERE sr.target_series_id = ?
	`, seriesID, seriesID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to get relations")
		return
	}
	defer rows.Close()

	var items []SeriesRelation
	for rows.Next() {
		var item SeriesRelation
		if err := rows.Scan(&item.ID, &item.TargetSeriesID, &item.TargetSeriesName, &item.RelationType); err != nil {
			continue
		}
		items = append(items, item)
	}
	if items == nil {
		items = []SeriesRelation{}
	}
	jsonResponse(w, http.StatusOK, items)
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

	db := c.store.(*database.SqlStore).DB()
	if err := db.QueryRowContext(r.Context(), `SELECT id FROM series WHERE id = ?`, req.TargetSeriesID).Scan(new(int64)); err != nil {
		jsonError(w, http.StatusNotFound, "Target series not found")
		return
	}
	var existingID int64
	err = db.QueryRowContext(r.Context(), `
		SELECT id FROM series_relations
		WHERE (source_series_id = ? AND target_series_id = ?)
		   OR (source_series_id = ? AND target_series_id = ?)
		LIMIT 1
	`, seriesID, req.TargetSeriesID, req.TargetSeriesID, seriesID).Scan(&existingID)
	if err == nil {
		jsonResponse(w, http.StatusOK, map[string]interface{}{"status": "exists", "id": existingID})
		return
	}
	if err != sql.ErrNoRows {
		jsonError(w, http.StatusInternalServerError, "Failed to check relation")
		return
	}

	_, err = db.ExecContext(r.Context(),
		`INSERT OR IGNORE INTO series_relations (source_series_id, target_series_id, relation_type) VALUES (?, ?, ?)`,
		seriesID, req.TargetSeriesID, req.RelationType)
	if err != nil {
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
	db := c.store.(*database.SqlStore).DB()
	_, _ = db.ExecContext(r.Context(), `DELETE FROM series_relations WHERE id = ?`, relationID)
	jsonResponse(w, http.StatusOK, map[string]string{"status": "deleted"})
}
