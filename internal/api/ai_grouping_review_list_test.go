// 本文件守 AI 分组审核列表接口的查询次数与错误传播：预取必须把候选合集与系列名
// 批量查询而不是逐条查询，否则页大小与每条合集数会拖出两层 N+1；两步查询的错误
// 也不能被吞掉——吞掉后 DB 故障时接口仍返回 200，候选合集会静默显示为空。

package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"manga-manager/internal/database"
	"manga-manager/internal/metadata"
)

// aiGroupingCountingStore 统计两条会退化成 N+1 的查询各被调用了几次。
type aiGroupingCountingStore struct {
	database.Store
	perReviewCollectionCalls atomic.Int64
	seriesNameCalls          atomic.Int64
	// failSeriesNames 让 GetSeriesNamesByIDs 报错，用来验证错误不被吞掉。
	failSeriesNames bool
}

var errFakeSeriesNames = errors.New("boom")

func (s *aiGroupingCountingStore) ListAIGroupingReviewCollections(ctx context.Context, reviewID int64) ([]database.AiGroupingReviewCollection, error) {
	s.perReviewCollectionCalls.Add(1)
	return s.Store.ListAIGroupingReviewCollections(ctx, reviewID)
}

func (s *aiGroupingCountingStore) GetSeriesNamesByIDs(ctx context.Context, ids []int64) ([]database.GetSeriesNamesByIDsRow, error) {
	s.seriesNameCalls.Add(1)
	if s.failSeriesNames {
		return nil, errFakeSeriesNames
	}
	return s.Store.GetSeriesNamesByIDs(ctx, ids)
}

// seedManyAIGroupingReviews 造 n 条各带 2 个候选合集的待审记录。
func seedManyAIGroupingReviews(t *testing.T, controller *Controller, libraryID int64, seriesA, seriesB database.Series, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if _, _, err := controller.createAIGroupingReview(context.Background(), libraryID, "ollama",
			[]metadata.CandidateSeries{
				{ID: seriesA.ID, Title: seriesA.Name},
				{ID: seriesB.ID, Title: "Beta Title"},
			}, []metadata.AIGroupCollection{
				{Name: "First", SeriesIDs: []int64{seriesA.ID, seriesB.ID}},
				{Name: "Second", SeriesIDs: []int64{seriesB.ID}},
			}); err != nil {
			t.Fatalf("createAIGroupingReview %d: %v", i, err)
		}
	}
}

func TestListAIGroupingReviewsBatchesDetailQueries(t *testing.T) {
	controller, store, lib, seriesA, seriesB := seedAIGroupingReviewFixture(t)
	const reviewCount = 10
	seedManyAIGroupingReviews(t, controller, lib.ID, seriesA, seriesB, reviewCount)

	counting := &aiGroupingCountingStore{Store: store}
	controller.store = counting

	rec := httptest.NewRecorder()
	controller.listAIGroupingReviews(rec, httptest.NewRequest(http.MethodGet, "/api/ai-grouping/reviews", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String())
	}

	var payload aiGroupingReviewsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("解析响应: %v", err)
	}
	if len(payload.Items) != reviewCount {
		t.Fatalf("返回 %d 条审核，期望 %d", len(payload.Items), reviewCount)
	}
	// 数据仍要完整：批量化不能以少给数据为代价。
	for _, item := range payload.Items {
		if len(item.Collections) != 2 {
			t.Fatalf("审核 %d 的候选合集数 = %d，期望 2", item.ID, len(item.Collections))
		}
		if len(item.Collections[0].Series) != 2 {
			t.Fatalf("候选合集里的系列数 = %d，期望 2", len(item.Collections[0].Series))
		}
	}

	if got := counting.perReviewCollectionCalls.Load(); got != 0 {
		t.Errorf("逐条查候选合集 %d 次 —— 每页 %d 条审核就是 %d 次单行查询，应当批量取",
			got, reviewCount, got)
	}
	// 全部审核涉及的系列名只该查一次。
	if got := counting.seriesNameCalls.Load(); got > 1 {
		t.Errorf("查系列名 %d 次 —— 应当把所有候选合集的系列 ID 合并成一次查询", got)
	}
}

// TestListAIGroupingReviewsPropagatesDetailErrors：明细加载失败必须报错，
// 不能返回 200 + 空候选合集——那会让用户以为 AI 什么都没分出来。
func TestListAIGroupingReviewsPropagatesDetailErrors(t *testing.T) {
	controller, store, lib, seriesA, seriesB := seedAIGroupingReviewFixture(t)
	seedManyAIGroupingReviews(t, controller, lib.ID, seriesA, seriesB, 2)

	controller.store = &aiGroupingCountingStore{Store: store, failSeriesNames: true}

	rec := httptest.NewRecorder()
	controller.listAIGroupingReviews(rec, httptest.NewRequest(http.MethodGet, "/api/ai-grouping/reviews", nil))
	if rec.Code == http.StatusOK {
		t.Fatalf("明细查询失败却返回了 200：%s —— 候选合集静默显示为空，"+
			"用户会以为 AI 一个分组都没分出来，而不是加载出错", rec.Body.String())
	}
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("HTTP %d，期望 500", rec.Code)
	}
}
