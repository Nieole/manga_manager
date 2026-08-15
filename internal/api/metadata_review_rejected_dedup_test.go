// 本文件守卫「被拒绝的提案不会被反复塞回队列」。
//
// 约束：入队去重必须同时覆盖 pending 与近期已拒绝的记录，逐字相同的提案不得在下一轮
// 刮削中重新入队。同时不能把用户堵死——必须留一个强制入队的出口（IgnoreRejected），
// 否则误拒就成了不可撤销的黑名单。

package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"manga-manager/internal/database"
	"manga-manager/internal/metadata"
)

func rejectedDedupResult() *metadata.SeriesMetadata {
	return &metadata.SeriesMetadata{
		Provider:   "bangumi",
		Title:      "External Title",
		Summary:    "External summary",
		SourceID:   99,
		SourceURL:  "https://bgm.tv/subject/99",
		Confidence: 0.8,
	}
}

// rejectReview 走 HTTP handler 把 review 拒掉（不依赖会被改名的 store 方法）。
func rejectReview(t *testing.T, controller *Controller, reviewID int64) {
	t.Helper()
	rec := httptest.NewRecorder()
	controller.rejectMetadataReview(rec, requestWithRouteParam(http.MethodPost, "/x", nil,
		"reviewId", strconv.FormatInt(reviewID, 10)))
	if rec.Code != http.StatusOK {
		t.Fatalf("reject 失败：%d %s", rec.Code, rec.Body.String())
	}
}

func TestRejectedProposalIsNotRequeued(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	ctx := context.Background()
	_, series, _ := seedBookFixture(t, store, t.TempDir(), "Lib", "Series Alpha", "a.cbz", 10)

	review, _, isNew, err := controller.queueMetadataReview(ctx, series, rejectedDedupResult(), "bangumi", "Alpha")
	if err != nil || !isNew {
		t.Fatalf("首次入队失败：isNew=%v err=%v", isNew, err)
	}
	rejectReview(t, controller, review.ID)

	// 同一份内容再刮一次：必须被识别为「此前已拒绝」。
	_, _, _, err = controller.queueMetadataReview(ctx, series, rejectedDedupResult(), "bangumi", "Alpha")
	if !errors.Is(err, errMetadataReviewRejectedBefore) {
		t.Fatalf("二次入队返回 %v，期望 errMetadataReviewRejectedBefore —— "+
			"拒绝过的提案又被塞回队列，用户得反复拒同一条", err)
	}

	pending, err := store.ListPendingMetadataReviewsBySeries(ctx, series.ID)
	if err != nil {
		t.Fatalf("ListPendingMetadataReviewsBySeries: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("待审队列里又出现了 %d 条 —— 「拒绝」这个动作没有任何持久效果", len(pending))
	}
}

// TestForcedQueueBypassesRejectedDedup：误拒之后必须还有路把数据重新加回来。
// 没有这条，「拒绝」就变成了不可撤销的黑名单。
func TestForcedQueueBypassesRejectedDedup(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	ctx := context.Background()
	_, series, _ := seedBookFixture(t, store, t.TempDir(), "Lib", "Series Alpha", "a.cbz", 10)

	review, _, _, err := controller.queueMetadataReview(ctx, series, rejectedDedupResult(), "bangumi", "Alpha")
	if err != nil {
		t.Fatalf("首次入队: %v", err)
	}
	rejectReview(t, controller, review.ID)

	_, _, isNew, err := controller.queueMetadataReviewWithOptions(ctx, series, rejectedDedupResult(),
		"bangumi", "Alpha", queueReviewOptions{IgnoreRejected: true})
	if err != nil {
		t.Fatalf("强制入队失败：%v —— 误拒之后就没有回头路了", err)
	}
	if !isNew {
		t.Fatal("强制入队没有产出新记录")
	}
	pending, _ := store.ListPendingMetadataReviewsBySeries(ctx, series.ID)
	if len(pending) != 1 {
		t.Fatalf("强制入队后待审队列有 %d 条，期望 1 条", len(pending))
	}
}

// TestDifferentContentStillQueuesAfterRejection 是反向护栏：
// 去重只该挡住**逐字相同**的提案，内容变了就必须照常入队。
func TestDifferentContentStillQueuesAfterRejection(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	ctx := context.Background()
	_, series, _ := seedBookFixture(t, store, t.TempDir(), "Lib", "Series Alpha", "a.cbz", 10)

	review, _, _, err := controller.queueMetadataReview(ctx, series, rejectedDedupResult(), "bangumi", "Alpha")
	if err != nil {
		t.Fatalf("首次入队: %v", err)
	}
	rejectReview(t, controller, review.ID)

	changed := rejectedDedupResult()
	changed.Summary = "A completely different summary"
	_, _, isNew, err := controller.queueMetadataReview(ctx, series, changed, "bangumi", "Alpha")
	if err != nil {
		t.Fatalf("内容不同却被挡下：%v —— 去重挡过头，数据源更新后用户再也收不到提案", err)
	}
	if !isNew {
		t.Fatal("内容不同却被判为重复")
	}
}

// TestScrapeApplyReportsRejectedBeforeOutcome：HTTP 层要给出可区分的 outcome，
// 而不是笼统的「队列里已有相同记录」——那对「你自己拒过」是错的说法。
func TestScrapeApplyReportsRejectedBeforeOutcome(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	ctx := context.Background()
	_, series, _ := seedBookFixture(t, store, t.TempDir(), "Lib", "Series Alpha", "a.cbz", 10)

	review, _, _, err := controller.queueMetadataReview(ctx, series, rejectedDedupResult(), "bangumi", "Alpha")
	if err != nil {
		t.Fatalf("首次入队: %v", err)
	}
	rejectReview(t, controller, review.ID)

	body, err := json.Marshal(rejectedDedupResult())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	rec := httptest.NewRecorder()
	controller.applyScrapedMetadata(rec, requestWithRouteParam(http.MethodPost,
		"/api/series/1/scrape-apply?provider=bangumi", body, "seriesId", strconv.FormatInt(series.ID, 10)))
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s —— 良性结果不该报服务端错误", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应: %v", err)
	}
	if outcome, _ := resp["outcome"].(string); outcome != "rejected_before" {
		t.Errorf("outcome = %q，期望 rejected_before", outcome)
	}
	if queued, _ := resp["queued"].(bool); queued {
		t.Error("响应称已入队，实际被去重挡下了")
	}
}

// TestScrapeApplyForceParamRequeues：force=1 是 UI 上「仍要加入队列」的出口。
func TestScrapeApplyForceParamRequeues(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	ctx := context.Background()
	_, series, _ := seedBookFixture(t, store, t.TempDir(), "Lib", "Series Alpha", "a.cbz", 10)

	review, _, _, err := controller.queueMetadataReview(ctx, series, rejectedDedupResult(), "bangumi", "Alpha")
	if err != nil {
		t.Fatalf("首次入队: %v", err)
	}
	rejectReview(t, controller, review.ID)

	body, err := json.Marshal(rejectedDedupResult())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	rec := httptest.NewRecorder()
	controller.applyScrapedMetadata(rec, requestWithRouteParam(http.MethodPost,
		"/api/series/1/scrape-apply?provider=bangumi&force=1", body, "seriesId", strconv.FormatInt(series.ID, 10)))
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应: %v", err)
	}
	if queued, _ := resp["queued"].(bool); !queued {
		t.Fatalf("force=1 仍未入队：%v —— UI 上「仍要加入队列」这条路是断的", resp)
	}
}

// TestRejectedDedupIsScopedToSeries：一个系列的拒绝记录不该影响另一个系列。
func TestRejectedDedupIsScopedToSeries(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	ctx := context.Background()
	_, seriesA, _ := seedBookFixture(t, store, t.TempDir(), "LibA", "Series Alpha", "a.cbz", 10)
	_, seriesB, _ := seedBookFixture(t, store, t.TempDir(), "LibB", "Series Beta", "b.cbz", 10)

	review, _, _, err := controller.queueMetadataReview(ctx, seriesA, rejectedDedupResult(), "bangumi", "Alpha")
	if err != nil {
		t.Fatalf("A 入队: %v", err)
	}
	rejectReview(t, controller, review.ID)

	if _, _, isNew, err := controller.queueMetadataReview(ctx, seriesB, rejectedDedupResult(), "bangumi", "Beta"); err != nil || !isNew {
		t.Fatalf("B 的入队被 A 的拒绝记录挡下了：isNew=%v err=%v —— 去重必须按系列隔离", isNew, err)
	}

	// database 侧同样确认查询是按 series_id 过滤的（别指望上层记得传对参数）。
	rejected, err := store.ListRecentRejectedMetadataReviewsBySeries(ctx,
		database.ListRecentRejectedMetadataReviewsBySeriesParams{SeriesID: seriesB.ID, Limit: rejectedDedupWindow})
	if err != nil {
		t.Fatalf("ListRecentRejectedMetadataReviewsBySeries: %v", err)
	}
	if len(rejected) != 0 {
		t.Errorf("系列 B 查到了 %d 条拒绝记录，实际只有系列 A 拒过", len(rejected))
	}
}
