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

// franchiseCollectionSourceType 是作品群合集在 collections.source_type 里的取值。
const franchiseCollectionSourceType = "system_franchise"

// errCollectionNameTaken 表示事务内查到了同名合集。它只在事务里产生——事务外的查重直接用
// collectionNameExists——由建号与 AI 分组应用两条写入路径共用，一律转成 409。
var errCollectionNameTaken = errors.New("collection name already exists")

// derivedCollectionSourceType 判断这个来源的合集是否「推导出来、由重建流程持有」。
// 只有作品群属于这一类：它由系列关系的连通分量推导，每次重建都按稳定键 upsert 并把
// 不属于连通分量的成员删掉，人工改动一律被静默覆盖。smart_snapshot 与 ai_grouping
// 是一次性固化的产物，落地后归用户所有、没有任何流程会回头覆盖，因此不在此列。
func derivedCollectionSourceType(sourceType string) bool {
	return sourceType == franchiseCollectionSourceType
}

// writableCollection 载入合集并挡下对推导型合集的人工写入，是这条不变量的唯一执行点
// ——把合集从列表里过滤掉只是收走了入口，直接调 API 仍然改得到。
// 第二个返回值为 false 时响应已写出，调用方直接 return。
func (c *Controller) writableCollection(w http.ResponseWriter, r *http.Request, id int64) (database.GetStaticCollectionViewRow, bool) {
	view, err := c.store.GetStaticCollectionView(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, http.StatusNotFound, "Collection not found")
			return view, false
		}
		jsonError(w, http.StatusInternalServerError, "Failed to load collection")
		return view, false
	}
	if derivedCollectionSourceType(view.SourceType) {
		// 403 而非 409：这不是可以重试的状态冲突，这个合集对人工写入永远关闭。
		jsonError(w, http.StatusForbidden, "Franchise collections are derived from series relations and cannot be edited")
		return view, false
	}
	return view, true
}

// listCollections 返回可人工编辑的合集。作品群不在其中——见 ListCollectionsWithSeriesCount。
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

// createCollection 创建一个新合集。名称与重命名、快照固化、AI 分组应用同口径：先去首尾空白
// 再判空并查重，否则 "   " 会建出一个在列表里看不出名字的合集，" 科幻" 与 "科幻" 会并存成两条。
func (c *Controller) createCollection(w http.ResponseWriter, r *http.Request) {
	var req CreateCollectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		jsonError(w, http.StatusBadRequest, "Name is required")
		return
	}

	// 查重与插入放进同一个事务：store 的 DSN 带 _txlock=immediate，BeginTx 即取写锁，
	// 因此两个并发建号请求会被串行化，不会各自读到「不存在」再各插一条同名合集。
	// collections.name 上没有唯一约束兜底，这道串行化就是这条不变量在建号入口的全部保障。
	var id int64
	err := c.store.ExecTx(r.Context(), func(q *database.Queries) error {
		switch _, err := q.CollectionNameExists(r.Context(), name); {
		case err == nil:
			return errCollectionNameTaken
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}
		created, err := q.CreateSimpleCollection(r.Context(), database.CreateSimpleCollectionParams{
			Name:        name,
			Description: nullStringFromString(strings.TrimSpace(req.Description)),
		})
		if err != nil {
			return err
		}
		id = created
		return nil
	})
	if errors.Is(err, errCollectionNameTaken) {
		jsonError(w, http.StatusConflict, "A collection with this name already exists")
		return
	}
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to create collection")
		return
	}
	jsonResponse(w, http.StatusCreated, map[string]interface{}{"id": id, "name": name})
}

// deleteCollection 删除合集
func (c *Controller) deleteCollection(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "collectionId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid collection ID")
		return
	}
	if _, ok := c.writableCollection(w, r, id); !ok {
		return
	}
	// 检查影响行数：不存在的资源返回 200 会让前端把「这个合集早就没了」当成删除成功，
	// 列表刷新后它却还在（因为压根不是同一个 id），用户只会觉得功能时灵时不灵。
	affected, err := c.store.DeleteCollection(r.Context(), id)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to delete collection")
		return
	}
	if affected == 0 {
		jsonError(w, http.StatusNotFound, "Collection not found")
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
	// collections.name 上没有数据库级唯一约束（重名由应用层判定），所以必须显式查一次；
	// 同一次载入顺带挡下作品群这类推导型合集——它的名字会被重建的 upsert 覆盖回去。
	// 与快照固化路径共用同一判定，保证「哪里都不许出现两个同名合集」。
	current, ok := c.writableCollection(w, r, id)
	if !ok {
		return
	}
	var req UpdateCollectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	// 名称必填：PUT 请求缺 name 字段时不能直接写成空串（前端只想改描述时很自然就会这么发），
	// 否则合集会在列表里变成一个没有名字的条目。
	name := strings.TrimSpace(req.Name)
	if name == "" {
		jsonError(w, http.StatusBadRequest, "Name is required")
		return
	}
	if !strings.EqualFold(strings.TrimSpace(current.Name), name) {
		conflict, err := c.collectionNameExists(r.Context(), name)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "Failed to check collection name")
			return
		}
		if conflict {
			jsonError(w, http.StatusConflict, "A collection with this name already exists")
			return
		}
	}

	if err := c.store.UpdateCollectionDetails(r.Context(), database.UpdateCollectionDetailsParams{
		Name:        name,
		Description: nullStringFromString(strings.TrimSpace(req.Description)),
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
	if _, ok := c.writableCollection(w, r, id); !ok {
		return
	}
	var req AddSeriesToCollectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.SeriesIDs) == 0 {
		jsonError(w, http.StatusBadRequest, "series_ids is required")
		return
	}

	// 数量上限：与智能合集快照口径一致，防止单个请求塞入任意多条。
	if len(req.SeriesIDs) > maxCollectionBatchSize {
		jsonError(w, http.StatusBadRequest, "Too many series in one request")
		return
	}

	ctx := r.Context()
	added := 0
	// 整批放进一个事务：逐条独立写入时，中途失败会留下「加了一半」的合集，
	// 而 added 计数又只统计「没报错」的次数——AddSeriesToCollection 用的是
	// INSERT OR IGNORE，重复添加不报错也不插入，于是 added 把空操作也算成了成功。
	err = c.store.ExecTx(ctx, func(q *database.Queries) error {
		for _, sid := range req.SeriesIDs {
			affected, err := q.AddSeriesToCollection(ctx, database.AddSeriesToCollectionParams{
				CollectionID: id,
				SeriesID:     sid,
			})
			if err != nil {
				return err
			}
			added += int(affected)
		}
		return nil
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to add series to collection")
		return
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
	if _, ok := c.writableCollection(w, r, collectionID); !ok {
		return
	}

	affected, err := c.store.RemoveSeriesFromCollection(r.Context(), database.RemoveSeriesFromCollectionParams{
		CollectionID: collectionID,
		SeriesID:     seriesID,
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to remove series")
		return
	}
	if affected == 0 {
		jsonError(w, http.StatusNotFound, "Series is not in this collection")
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

// getSeriesFranchise 获取系列所在宇宙的整个关联关系连通图
func (c *Controller) getSeriesFranchise(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}

	items, err := c.store.GetConnectedSeriesRelations(r.Context(), seriesID)
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

	items, err := c.store.GetAllSeriesRelationsForLibrary(r.Context(), database.GetAllSeriesRelationsForLibraryParams{
		LibraryID:   libraryID,
		LibraryID_2: libraryID,
	})
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

	// 合并式调度 franchise 重建（去抖 + 登记到 backgroundWG）
	c.scheduleFranchiseRebuild()

	jsonResponse(w, http.StatusCreated, map[string]string{"status": "created"})
}

// deleteSeriesRelation 删除系列关联
func (c *Controller) deleteSeriesRelation(w http.ResponseWriter, r *http.Request) {
	relationID, err := parseID(r, "relationId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid relation ID")
		return
	}
	affected, err := c.store.DeleteSeriesRelation(r.Context(), relationID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to delete relation")
		return
	}
	if affected == 0 {
		jsonError(w, http.StatusNotFound, "Relation not found")
		return
	}

	// 合并式调度 franchise 重建（去抖 + 登记到 backgroundWG）
	c.scheduleFranchiseRebuild()

	jsonResponse(w, http.StatusOK, map[string]string{"status": "deleted"})
}

type UpdateRelationRequest struct {
	RelationType string `json:"relation_type"`
}

// updateSeriesRelation 更新系列关联
func (c *Controller) updateSeriesRelation(w http.ResponseWriter, r *http.Request) {
	relationID, err := parseID(r, "relationId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid relation ID")
		return
	}
	var req UpdateRelationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "relation_type is required")
		return
	}
	req.RelationType = strings.TrimSpace(req.RelationType)
	if req.RelationType == "" {
		jsonError(w, http.StatusBadRequest, "relation_type cannot be empty")
		return
	}

	if err := c.store.UpdateSeriesRelation(r.Context(), database.UpdateSeriesRelationParams{
		RelationType: req.RelationType,
		ID:           relationID,
	}); err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to update relation")
		return
	}

	// 合并式调度 franchise 重建（去抖 + 登记到 backgroundWG）
	c.scheduleFranchiseRebuild()

	jsonResponse(w, http.StatusOK, map[string]string{"status": "updated"})
}

func nullStringFromString(value string) sql.NullString {
	if value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}
