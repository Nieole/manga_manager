// 本文件守**当前**锁定集在 api 侧的两处效力：单条应用全被锁时的响应形状，
// 以及系列详情页上锁徽章按当前锁定集渲染（不是入队瞬间的快照）。
//
// 锁徽章渲染错，用户会以为某个字段能被应用，点下去却被静默丢弃。
// 应用时按锁过滤、以及入队侧对锁的处置，都归 internal/proposal 的用例。

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
	"manga-manager/internal/proposal"
)

// lockSeriesFields 直接写 locked_fields（绕开 HTTP 层，用例要精确控制锁的范围）。
func lockSeriesFields(t *testing.T, store database.Store, seriesID int64, fields string) {
	t.Helper()
	sqlStore, ok := store.(*database.SqlStore)
	if !ok {
		t.Fatalf("需要 *SqlStore 才能直改行，得到 %T", store)
	}
	if _, err := sqlStore.DB().ExecContext(context.Background(),
		`UPDATE series SET locked_fields = ? WHERE id = ?`, fields, seriesID); err != nil {
		t.Fatalf("锁定字段失败: %v", err)
	}
}

// fullScrapeResult 是一份会命中全部七个字段的刮削结果。
func fullScrapeResult() *metadata.SeriesMetadata {
	return &metadata.SeriesMetadata{
		Provider:   "bangumi",
		Title:      "Scraped Title",
		Summary:    "Scraped summary",
		Publisher:  "Scraped Publisher",
		Status:     "ongoing",
		Rating:     4.5,
		Tags:       []string{"action", "drama"},
		SourceID:   42,
		SourceURL:  "https://bgm.tv/subject/42",
		Confidence: 0.9,
	}
}

// TestApplyReportsLockedSkippedWithoutServerError：一个字段都没写不是服务端故障。
//
// 落 500 时前端只能提示「服务器错误」，给不出「先去解锁」这条可行动的建议。
func TestApplyReportsLockedSkippedWithoutServerError(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	ctx := context.Background()
	_, series, _ := seedBookFixture(t, store, t.TempDir(), "Lib", "Series Alpha", "a.cbz", 10)

	queued, err := controller.proposals.Queue(ctx, series, fullScrapeResult(), "bangumi", "Alpha", proposal.QueueOptions{})
	if err != nil || queued.Status != proposal.QueueQueued {
		t.Fatalf("入队：status=%q err=%v", queued.Status, err)
	}
	// 入队之后用户才锁住全部字段。
	lockSeriesFields(t, store, series.ID, "title,summary,publisher,status,rating,tags,authors")

	rec := httptest.NewRecorder()
	controller.applyMetadataReview(rec, requestWithRouteParam(http.MethodPost, "/x", nil,
		"reviewId", strconv.FormatInt(queued.Proposal.ID, 10)))
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP %d，期望 200（良性结果不该报服务端错误）：%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应: %v", err)
	}
	if applied, _ := resp["applied"].(bool); applied {
		t.Error("响应称 applied=true，但一个字段也没写进去")
	}
	if outcome, _ := resp["outcome"].(string); outcome != "locked_skipped" {
		t.Errorf("outcome = %q，期望 locked_skipped —— 用户需要知道该去解锁", outcome)
	}
}

// TestReviewViewReflectsCurrentLockState：徽章按**当前**锁定集渲染，不是入队快照。
func TestReviewViewReflectsCurrentLockState(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	ctx := context.Background()
	_, series, _ := seedBookFixture(t, store, t.TempDir(), "Lib", "Series Alpha", "a.cbz", 10)

	if _, err := controller.proposals.Queue(ctx, series, fullScrapeResult(), "bangumi", "Alpha", proposal.QueueOptions{}); err != nil {
		t.Fatalf("入队: %v", err)
	}
	// 入队之后才加锁：行上的 Locked 快照仍是 false。
	lockSeriesFields(t, store, series.ID, "publisher")

	payload, err := controller.loadSeriesMetadataReview(ctx, series.ID)
	if err != nil {
		t.Fatalf("loadSeriesMetadataReview: %v", err)
	}
	if len(payload.Reviews) != 1 {
		t.Fatalf("期望 1 条待裁决提案，实际 %d", len(payload.Reviews))
	}
	var found bool
	for _, f := range payload.Reviews[0].Fields {
		if f.Name != "publisher" {
			continue
		}
		found = true
		if !f.Locked {
			t.Error("publisher 在系列详情里没有锁徽章 —— 用户会以为它能被应用，" +
				"点下去却被静默丢弃")
		}
	}
	if !found {
		t.Fatal("待裁决字段里找不到 publisher")
	}
}
