// 候选合集的应用只能成功一次，且成功时必须报出新建合集的 id。
// 破了前者，两个标签页各点一次就会建出两个同名合集，先建的那个从审核记录里彻底失联；
// 破了后者，提示会退化成「已创建合集 #」，用户点不到刚建出来的东西。

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	"manga-manager/internal/database"
	"manga-manager/internal/metadata"
)

// preemptingAIGroupingStore 在开事务之前抢先把候选合集应用掉，精确复现「事务外读到待应用、
// 真正写入时已被别人应用掉」的那个窗口——两个标签页、双击穿透 actingKey、整条审核应用与
// 单条候选合集应用撞车，都是这个形态。底下仍是同一个真库，它只负责在正确的时刻插入一次真实的并发写。
type preemptingAIGroupingStore struct {
	database.Store
	once    sync.Once
	preempt func()
}

func (s *preemptingAIGroupingStore) ExecTx(ctx context.Context, fn func(*database.Queries) error) error {
	s.once.Do(s.preempt)
	return s.Store.ExecTx(ctx, fn)
}

type applyAIGroupingCollectionResponse struct {
	Success             bool  `json:"success"`
	ReviewID            int64 `json:"review_id"`
	CollectionID        int64 `json:"collection_id"`
	CreatedCollectionID int64 `json:"created_collection_id"`
}

// seedAIGroupingReviewWithCollections 建一条待应用的审核，候选合集按 names 顺序各挂一个系列。
func seedAIGroupingReviewWithCollections(t *testing.T, names ...string) (*Controller, database.Store, database.AiGroupingReview, []database.AiGroupingReviewCollection) {
	t.Helper()

	controller, store, lib, seriesA, seriesB := seedAIGroupingReviewFixture(t)
	groups := make([]metadata.AIGroupCollection, 0, len(names))
	for i, name := range names {
		member := seriesA.ID
		if i%2 == 1 {
			member = seriesB.ID
		}
		groups = append(groups, metadata.AIGroupCollection{Name: name, SeriesIDs: []int64{member}})
	}
	review, created, err := controller.createAIGroupingReview(context.Background(), lib.ID, "ollama", []metadata.CandidateSeries{
		{ID: seriesA.ID, Title: seriesA.Name},
		{ID: seriesB.ID, Title: "Beta Title"},
	}, groups)
	if err != nil {
		t.Fatalf("createAIGroupingReview: %v", err)
	}
	if created != len(names) {
		t.Fatalf("入库候选合集数 = %d，期望 %d", created, len(names))
	}
	collections, err := store.ListAIGroupingReviewCollections(context.Background(), review.ID)
	if err != nil {
		t.Fatalf("ListAIGroupingReviewCollections: %v", err)
	}
	return controller, store, review, collections
}

func countCollectionsNamed(t *testing.T, store database.Store, name string) int {
	t.Helper()
	var count int
	row := store.(*database.SqlStore).DB().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM collections WHERE name = ?`, name)
	if err := row.Scan(&count); err != nil {
		t.Fatalf("count collections named %q: %v", name, err)
	}
	return count
}

// TestApplyAIGroupingReviewRejectsPreemptedApply 把「已经被别人应用过了」在两个入口上钉成同一个结局：
// 409 + 整体回滚，而不是再建一个同名合集。
func TestApplyAIGroupingReviewRejectsPreemptedApply(t *testing.T) {
	cases := []struct {
		name string
		// act 用被抢先过的 store 发出一次应用请求，返回响应。
		act func(t *testing.T, controller *Controller, review database.AiGroupingReview, collection database.AiGroupingReviewCollection) *httptest.ResponseRecorder
	}{
		{
			name: "单条候选合集应用",
			act: func(t *testing.T, controller *Controller, review database.AiGroupingReview, collection database.AiGroupingReviewCollection) *httptest.ResponseRecorder {
				req := requestWithRouteParam(http.MethodPost, "/api/ai-grouping/reviews/1/collections/1/apply", nil, "reviewId", strconv.FormatInt(review.ID, 10))
				req = withRouteParam(req, "collectionId", strconv.FormatInt(collection.ID, 10))
				rec := httptest.NewRecorder()
				controller.applyAIGroupingReviewCollection(rec, req)
				return rec
			},
		},
		{
			name: "整条审核应用",
			act: func(t *testing.T, controller *Controller, review database.AiGroupingReview, _ database.AiGroupingReviewCollection) *httptest.ResponseRecorder {
				req := requestWithRouteParam(http.MethodPost, "/api/ai-grouping/reviews/1/apply", nil, "reviewId", strconv.FormatInt(review.ID, 10))
				rec := httptest.NewRecorder()
				controller.applyAIGroupingReview(rec, req)
				return rec
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			controller, store, review, collections := seedAIGroupingReviewWithCollections(t, "G1")
			ctx := context.Background()
			target := collections[0]

			// 抢先者走的是同一条生产通路，只是提前一步提交。
			preempted := &preemptingAIGroupingStore{Store: store, preempt: func() {
				if err := store.ExecTx(ctx, func(q *database.Queries) error {
					_, err := applyAIGroupingReviewCollectionWithQueries(ctx, q, review, target)
					return err
				}); err != nil {
					t.Errorf("抢先应用失败: %v", err)
				}
			}}
			controller.store = preempted

			rec := tc.act(t, controller, review, target)
			if rec.Code != http.StatusConflict {
				t.Errorf("状态码 = %d，期望 %d —— 被抢先与前置检查发现的是同一件事", rec.Code, http.StatusConflict)
			}

			if got := countCollectionsNamed(t, store, "G1"); got != 1 {
				t.Errorf("名为 G1 的合集数 = %d，期望 1 —— 同一条候选合集被应用了两次", got)
			}

			applied, err := store.GetAIGroupingReviewCollection(ctx, target.ID)
			if err != nil {
				t.Fatalf("GetAIGroupingReviewCollection: %v", err)
			}
			if !applied.CreatedCollectionID.Valid {
				t.Fatalf("created_collection_id 丢了: %+v", applied)
			}
			var survivor int64
			row := store.(*database.SqlStore).DB().QueryRowContext(ctx, `SELECT id FROM collections WHERE name = ?`, "G1")
			if err := row.Scan(&survivor); err != nil {
				t.Fatalf("查 G1 合集 id: %v", err)
			}
			if applied.CreatedCollectionID.Int64 != survivor {
				t.Errorf("created_collection_id = %d，实际留存的合集是 %d —— 抢先者建的那个已无迹可寻",
					applied.CreatedCollectionID.Int64, survivor)
			}
		})
	}
}

// TestApplyAIGroupingReviewCollectionReportsCreatedID 守住「应用成功必带 id」，
// 尤其是收尾那一条——只有一条候选合集的审核是最常见的形态。
func TestApplyAIGroupingReviewCollectionReportsCreatedID(t *testing.T) {
	cases := []struct {
		name  string
		names []string
	}{
		{name: "只有一条候选合集的审核", names: []string{"G1"}},
		{name: "逐条应用两条候选合集", names: []string{"G1", "G2"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			controller, store, review, collections := seedAIGroupingReviewWithCollections(t, tc.names...)
			ctx := context.Background()

			for i, collection := range collections {
				req := requestWithRouteParam(http.MethodPost, "/api/ai-grouping/reviews/1/collections/1/apply", nil, "reviewId", strconv.FormatInt(review.ID, 10))
				req = withRouteParam(req, "collectionId", strconv.FormatInt(collection.ID, 10))
				rec := httptest.NewRecorder()
				controller.applyAIGroupingReviewCollection(rec, req)
				if rec.Code != http.StatusOK {
					t.Fatalf("第 %d 条应用状态码 = %d body=%s", i+1, rec.Code, rec.Body.String())
				}
				var res applyAIGroupingCollectionResponse
				if err := json.NewDecoder(rec.Body).Decode(&res); err != nil {
					t.Fatalf("解析响应: %v", err)
				}

				stored, err := store.GetAIGroupingReviewCollection(ctx, collection.ID)
				if err != nil {
					t.Fatalf("GetAIGroupingReviewCollection: %v", err)
				}
				if !stored.CreatedCollectionID.Valid {
					t.Fatalf("第 %d 条没记下 created_collection_id", i+1)
				}
				if res.CreatedCollectionID != stored.CreatedCollectionID.Int64 {
					t.Errorf("第 %d 条（共 %d 条）回传 created_collection_id = %d，库里是 %d —— 前端会提示「已创建合集 #」",
						i+1, len(collections), res.CreatedCollectionID, stored.CreatedCollectionID.Int64)
				}
			}

			// 反向判据：逐条应用完仍要正常收尾，合集一个不少。
			updated, err := store.GetAIGroupingReview(ctx, review.ID)
			if err != nil {
				t.Fatalf("GetAIGroupingReview: %v", err)
			}
			if updated.Status != "applied" || !updated.AppliedAt.Valid {
				t.Errorf("审核终态 = %q applied_at=%v，期望 applied", updated.Status, updated.AppliedAt.Valid)
			}
			for _, name := range tc.names {
				if got := countCollectionsNamed(t, store, name); got != 1 {
					t.Errorf("名为 %s 的合集数 = %d，期望 1", name, got)
				}
			}
		})
	}
}
