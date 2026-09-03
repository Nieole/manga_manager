// 候选合集点名的系列可能在入队之后被删掉（重扫清理）。series_ids 是一串裸 ID、没有外键，
// 悬空 ID 直插 collection_series 会撞 series 外键，把应用炸成 500，整批应用还连累同一审核里
// 其他本来没问题的候选。本文件把「读写两侧一律只认还在的系列」钉死。

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"manga-manager/internal/database"
	"manga-manager/internal/metadata"
)

type applyAIGroupingReviewResponse struct {
	Success     bool  `json:"success"`
	ReviewID    int64 `json:"review_id"`
	Collections int64 `json:"collections"`
	Skipped     int64 `json:"skipped"`
}

// seedAIGroupingReviewWithGroups 按给定分组建一条待应用的审核，两个系列都算候选。
func seedAIGroupingReviewWithGroups(t *testing.T, controller *Controller, store database.Store, lib database.Library, seriesA, seriesB database.Series, groups ...metadata.AIGroupCollection) (database.AiGroupingReview, []database.AiGroupingReviewCollection) {
	t.Helper()
	review, created, err := controller.createAIGroupingReview(context.Background(), lib.ID, "ollama", []metadata.CandidateSeries{
		{ID: seriesA.ID, Title: seriesA.Name},
		{ID: seriesB.ID, Title: "Beta Title"},
	}, groups)
	if err != nil {
		t.Fatalf("createAIGroupingReview: %v", err)
	}
	if created != len(groups) {
		t.Fatalf("入库候选合集数 = %d，期望 %d", created, len(groups))
	}
	collections, err := store.ListAIGroupingReviewCollections(context.Background(), review.ID)
	if err != nil {
		t.Fatalf("ListAIGroupingReviewCollections: %v", err)
	}
	return review, collections
}

// collectionMemberIDs 读出一个合集实际关联到的系列。
func collectionMemberIDs(t *testing.T, store database.Store, collectionID int64) []int64 {
	t.Helper()
	rows, err := store.(*database.SqlStore).DB().QueryContext(context.Background(),
		`SELECT series_id FROM collection_series WHERE collection_id = ? ORDER BY series_id`, collectionID)
	if err != nil {
		t.Fatalf("查合集成员: %v", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("扫描合集成员: %v", err)
		}
		ids = append(ids, id)
	}
	return ids
}

func applyCollectionRequest(controller *Controller, review database.AiGroupingReview, collection database.AiGroupingReviewCollection) *httptest.ResponseRecorder {
	req := requestWithRouteParam(http.MethodPost, "/api/ai-grouping/reviews/1/collections/1/apply", nil, "reviewId", strconv.FormatInt(review.ID, 10))
	req = withRouteParam(req, "collectionId", strconv.FormatInt(collection.ID, 10))
	rec := httptest.NewRecorder()
	controller.applyAIGroupingReviewCollection(rec, req)
	return rec
}

func applyReviewRequest(controller *Controller, review database.AiGroupingReview) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	controller.applyAIGroupingReview(rec, requestWithRouteParam(http.MethodPost, "/api/ai-grouping/reviews/1/apply", nil, "reviewId", strconv.FormatInt(review.ID, 10)))
	return rec
}

// TestApplyAIGroupingReviewSkipsDeletedSeries 守单条候选合集的应用：还剩系列就照建，
// 只把已删除的那些丢掉——撞外键回滚成 500 时用户除了驳回没有任何出路。
func TestApplyAIGroupingReviewSkipsDeletedSeries(t *testing.T) {
	controller, store, lib, seriesA, seriesB := seedAIGroupingReviewFixture(t)
	ctx := context.Background()
	review, collections := seedAIGroupingReviewWithGroups(t, controller, store, lib, seriesA, seriesB,
		metadata.AIGroupCollection{Name: "Mixed", SeriesIDs: []int64{seriesA.ID, seriesB.ID}})
	if err := store.DeleteSeries(ctx, seriesB.ID); err != nil {
		t.Fatalf("DeleteSeries: %v", err)
	}

	rec := applyCollectionRequest(controller, review, collections[0])
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200 body=%s", rec.Code, rec.Body.String())
	}
	var res applyAIGroupingCollectionResponse
	if err := json.NewDecoder(rec.Body).Decode(&res); err != nil {
		t.Fatalf("解析响应: %v", err)
	}
	if got := collectionMemberIDs(t, store, res.CreatedCollectionID); len(got) != 1 || got[0] != seriesA.ID {
		t.Errorf("新建合集的成员 = %v，期望只剩 %d —— 已删除的系列不该进合集", got, seriesA.ID)
	}
}

// TestApplyAIGroupingReviewCollectionAllSeriesDeleted 守「成员全没了」：不建空合集，
// 也不替用户裁决——这条候选留在待处理，由用户驳回。
func TestApplyAIGroupingReviewCollectionAllSeriesDeleted(t *testing.T) {
	controller, store, lib, seriesA, seriesB := seedAIGroupingReviewFixture(t)
	ctx := context.Background()
	review, collections := seedAIGroupingReviewWithGroups(t, controller, store, lib, seriesA, seriesB,
		metadata.AIGroupCollection{Name: "GoneOnly", SeriesIDs: []int64{seriesB.ID}})
	if err := store.DeleteSeries(ctx, seriesB.ID); err != nil {
		t.Fatalf("DeleteSeries: %v", err)
	}

	rec := applyCollectionRequest(controller, review, collections[0])
	if rec.Code != http.StatusConflict {
		t.Fatalf("状态码 = %d，期望 409 body=%s", rec.Code, rec.Body.String())
	}
	if got := countCollectionsNamed(t, store, "GoneOnly"); got != 0 {
		t.Errorf("名为 GoneOnly 的合集数 = %d，期望 0 —— 不该建出一个没有成员的合集", got)
	}
	stored, err := store.GetAIGroupingReviewCollection(ctx, collections[0].ID)
	if err != nil {
		t.Fatalf("GetAIGroupingReviewCollection: %v", err)
	}
	if stored.Status != "pending" {
		t.Errorf("候选合集状态 = %q，期望仍是 pending —— 系统不替用户裁决", stored.Status)
	}
}

// TestApplyAIGroupingReviewDoesNotPunishSiblings 守整批应用的事务边界：一条候选的系列全没了
// 只跳过它自己，同一审核里其他候选照建；还有待处理候选时审核不收尾。
func TestApplyAIGroupingReviewDoesNotPunishSiblings(t *testing.T) {
	controller, store, lib, seriesA, seriesB := seedAIGroupingReviewFixture(t)
	ctx := context.Background()
	review, _ := seedAIGroupingReviewWithGroups(t, controller, store, lib, seriesA, seriesB,
		metadata.AIGroupCollection{Name: "Healthy", SeriesIDs: []int64{seriesA.ID}},
		metadata.AIGroupCollection{Name: "GoneOnly", SeriesIDs: []int64{seriesB.ID}})
	if err := store.DeleteSeries(ctx, seriesB.ID); err != nil {
		t.Fatalf("DeleteSeries: %v", err)
	}

	rec := applyReviewRequest(controller, review)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200 body=%s", rec.Code, rec.Body.String())
	}
	var res applyAIGroupingReviewResponse
	if err := json.NewDecoder(rec.Body).Decode(&res); err != nil {
		t.Fatalf("解析响应: %v", err)
	}
	if res.Collections != 1 || res.Skipped != 1 {
		t.Errorf("应用 %d 条、跳过 %d 条，期望 1/1", res.Collections, res.Skipped)
	}
	if got := countCollectionsNamed(t, store, "Healthy"); got != 1 {
		t.Errorf("名为 Healthy 的合集数 = %d，期望 1 —— 好的候选被同批的坏候选连累了", got)
	}
	updated, err := store.GetAIGroupingReview(ctx, review.ID)
	if err != nil {
		t.Fatalf("GetAIGroupingReview: %v", err)
	}
	if updated.Status != "pending" {
		t.Errorf("审核状态 = %q，期望仍是 pending —— 还有一条候选没裁决", updated.Status)
	}
}

// TestApplyAIGroupingReviewAllCandidatesGone 守「整批一条都建不出来」：报 409 而不是
// 假装成功——响应里 collections=0 前端读不到，用户会以为合集已经建好了。
func TestApplyAIGroupingReviewAllCandidatesGone(t *testing.T) {
	controller, store, lib, seriesA, seriesB := seedAIGroupingReviewFixture(t)
	ctx := context.Background()
	review, _ := seedAIGroupingReviewWithGroups(t, controller, store, lib, seriesA, seriesB,
		metadata.AIGroupCollection{Name: "GoneOnly", SeriesIDs: []int64{seriesB.ID}})
	if err := store.DeleteSeries(ctx, seriesB.ID); err != nil {
		t.Fatalf("DeleteSeries: %v", err)
	}

	rec := applyReviewRequest(controller, review)
	if rec.Code != http.StatusConflict {
		t.Fatalf("状态码 = %d，期望 409 body=%s", rec.Code, rec.Body.String())
	}
	updated, err := store.GetAIGroupingReview(ctx, review.ID)
	if err != nil {
		t.Fatalf("GetAIGroupingReview: %v", err)
	}
	if updated.Status != "pending" {
		t.Errorf("审核状态 = %q，期望仍是 pending", updated.Status)
	}
}

// TestAIGroupingReviewViewHidesDeletedSeries 守读侧口径：series_ids、series 与 series_count
// 三者一律只讲还在的系列。不一致时用户看到「2 个系列」却只列出 1 个，编辑草稿还会把那个
// 点不到的悬空 ID 原样写回去。
func TestAIGroupingReviewViewHidesDeletedSeries(t *testing.T) {
	controller, store, lib, seriesA, seriesB := seedAIGroupingReviewFixture(t)
	ctx := context.Background()
	seedAIGroupingReviewWithGroups(t, controller, store, lib, seriesA, seriesB,
		metadata.AIGroupCollection{Name: "Mixed", SeriesIDs: []int64{seriesA.ID, seriesB.ID}})
	if err := store.DeleteSeries(ctx, seriesB.ID); err != nil {
		t.Fatalf("DeleteSeries: %v", err)
	}

	rec := httptest.NewRecorder()
	controller.listAIGroupingReviews(rec, httptest.NewRequest(http.MethodGet, "/api/ai-grouping/reviews?status=pending", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload aiGroupingReviewsResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("解析响应: %v", err)
	}
	if len(payload.Items) != 1 || len(payload.Items[0].Collections) != 1 {
		t.Fatalf("响应形状不对: %+v", payload)
	}
	view := payload.Items[0].Collections[0]
	if len(view.Series) != 1 || view.Series[0].ID != seriesA.ID {
		t.Fatalf("series = %+v，期望只剩 %d", view.Series, seriesA.ID)
	}
	if len(view.SeriesIDs) != 1 || view.SeriesIDs[0] != seriesA.ID {
		t.Errorf("series_ids = %v，期望只剩 %d —— 编辑草稿会把悬空 ID 原样写回去", view.SeriesIDs, seriesA.ID)
	}
	if view.SeriesCount != 1 {
		t.Errorf("series_count = %d，期望 1 —— 界面会写着「2 个系列」却只列出 1 个", view.SeriesCount)
	}
}
