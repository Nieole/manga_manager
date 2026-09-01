// 候选合集名由 AI 给出，但落地的仍是普通合集，同样受「哪里都不许出现两个同名合集」约束。
// 破了这条，一次应用就能在列表里放进两个名字一模一样、内容却不同的合集。

package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"manga-manager/internal/database"
)

func TestApplyAIGroupingReviewRejectsDuplicateCollectionName(t *testing.T) {
	t.Run("撞上已有合集名整批回滚报 409", func(t *testing.T) {
		controller, store, review, _ := seedAIGroupingReviewWithCollections(t, "G1")
		ctx := context.Background()
		db := store.(*database.SqlStore).DB()
		// 大小写不同：与 CollectionNameExists 的 COLLATE NOCASE 同口径，照样算撞名。
		if _, err := db.ExecContext(ctx, `INSERT INTO collections (name) VALUES (?)`, "g1"); err != nil {
			t.Fatalf("预置同名合集失败: %v", err)
		}

		req := requestWithRouteParam(http.MethodPost, "/api/ai-grouping/reviews/1/apply", nil, "reviewId", strconv.FormatInt(review.ID, 10))
		rec := httptest.NewRecorder()
		controller.applyAIGroupingReview(rec, req)
		if rec.Code != http.StatusConflict {
			t.Fatalf("状态码 = %d，期望 %d body=%s", rec.Code, http.StatusConflict, rec.Body.String())
		}
		if got := countCollectionsNamed(t, store, "G1"); got != 0 {
			t.Errorf("名为 G1 的合集数 = %d，期望 0 —— 撞名的候选合集仍然落库了", got)
		}
		updated, err := store.GetAIGroupingReview(ctx, review.ID)
		if err != nil {
			t.Fatalf("GetAIGroupingReview: %v", err)
		}
		if updated.Status != "pending" {
			t.Errorf("审核状态 = %q，期望仍是 pending —— 整批应当回滚", updated.Status)
		}
	})

	t.Run("同一批里两条同名候选也被挡下", func(t *testing.T) {
		controller, store, review, _ := seedAIGroupingReviewWithCollections(t, "Dup", "Dup")

		req := requestWithRouteParam(http.MethodPost, "/api/ai-grouping/reviews/1/apply", nil, "reviewId", strconv.FormatInt(review.ID, 10))
		rec := httptest.NewRecorder()
		controller.applyAIGroupingReview(rec, req)
		if rec.Code != http.StatusConflict {
			t.Fatalf("状态码 = %d，期望 %d body=%s", rec.Code, http.StatusConflict, rec.Body.String())
		}
		if got := countCollectionsNamed(t, store, "Dup"); got != 0 {
			t.Errorf("名为 Dup 的合集数 = %d，期望 0 —— 整批应当回滚", got)
		}
	})

	t.Run("单条应用撞名报 409 且不影响别的候选", func(t *testing.T) {
		controller, store, review, collections := seedAIGroupingReviewWithCollections(t, "G1", "G2")
		ctx := context.Background()
		db := store.(*database.SqlStore).DB()
		if _, err := db.ExecContext(ctx, `INSERT INTO collections (name) VALUES (?)`, "G1"); err != nil {
			t.Fatalf("预置同名合集失败: %v", err)
		}

		conflictReq := requestWithRouteParam(http.MethodPost, "/api/ai-grouping/reviews/1/collections/1/apply", nil, "reviewId", strconv.FormatInt(review.ID, 10))
		conflictReq = withRouteParam(conflictReq, "collectionId", strconv.FormatInt(collections[0].ID, 10))
		conflictRec := httptest.NewRecorder()
		controller.applyAIGroupingReviewCollection(conflictRec, conflictReq)
		if conflictRec.Code != http.StatusConflict {
			t.Fatalf("状态码 = %d，期望 %d body=%s", conflictRec.Code, http.StatusConflict, conflictRec.Body.String())
		}
		if got := countCollectionsNamed(t, store, "G1"); got != 1 {
			t.Errorf("名为 G1 的合集数 = %d，期望仍是预置的那 1 条", got)
		}

		// 反向判据：不撞名的那条照样应用得了，撞名不该把整条审核卡死。
		okReq := requestWithRouteParam(http.MethodPost, "/api/ai-grouping/reviews/1/collections/2/apply", nil, "reviewId", strconv.FormatInt(review.ID, 10))
		okReq = withRouteParam(okReq, "collectionId", strconv.FormatInt(collections[1].ID, 10))
		okRec := httptest.NewRecorder()
		controller.applyAIGroupingReviewCollection(okRec, okReq)
		if okRec.Code != http.StatusOK {
			t.Fatalf("不撞名的候选状态码 = %d，期望 200 body=%s", okRec.Code, okRec.Body.String())
		}
		if got := countCollectionsNamed(t, store, "G2"); got != 1 {
			t.Errorf("名为 G2 的合集数 = %d，期望 1", got)
		}
	})
}
