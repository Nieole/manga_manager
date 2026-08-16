// 本文件守卫**当前**锁定集在应用与展示两处的效力：入队时不锁、之后才加的锁同样算数。
//
// 约束：apply 遇到锁定字段必须跳过写入，且不得把 review 标成 applied（否则被跳过的提案
// 在只查 pending 的收件箱里永久消失）；展示的锁徽章要按当前锁定集渲染，不是入队快照。
// 入队侧对锁的处置归 internal/proposal 的用例。

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

// TestApplySkipsFieldsLockedAfterQueueing：入队后才加的锁也要生效，且报告要诚实——
// 不能一边跳过写入一边把 review 标成 applied。
func TestApplySkipsFieldsLockedAfterQueueing(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	ctx := context.Background()
	_, series, _ := seedBookFixture(t, store, t.TempDir(), "Lib", "Series Alpha", "a.cbz", 10)

	review, _, _, err := controller.queueMetadataReview(ctx, series, fullScrapeResult(), "bangumi", "Alpha")
	if err != nil {
		t.Fatalf("queueMetadataReview: %v", err)
	}

	// 入队之后用户才锁住全部字段。
	lockSeriesFields(t, store, series.ID, "title,summary,publisher,status,rating,tags,authors")

	before, err := store.GetSeries(ctx, series.ID)
	if err != nil {
		t.Fatalf("GetSeries: %v", err)
	}

	rec := httptest.NewRecorder()
	controller.applyMetadataReview(rec, requestWithRouteParam(http.MethodPost, "/x", nil,
		"reviewId", strconv.FormatInt(review.ID, 10)))
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

	after, err := store.GetSeries(ctx, series.ID)
	if err != nil {
		t.Fatalf("GetSeries: %v", err)
	}
	if after.Title != before.Title || after.Publisher != before.Publisher {
		t.Error("锁定字段仍被写入了")
	}

	// review 必须**保持 pending**：标成 applied 会让被跳过的提案在只查 pending 的
	// 收件箱里永久消失，用户解锁后也找不回来。
	reloaded, err := store.GetMetadataReview(ctx, review.ID)
	if err != nil {
		t.Fatalf("GetMetadataReview: %v", err)
	}
	if reloaded.Status != "pending" {
		t.Errorf("review 状态变成 %q —— 零写入却把它消费掉了，被跳过的提案再也找不回来",
			reloaded.Status)
	}
}

// TestPartialLockApplyWritesUnlockedFields 是反向护栏：部分锁定时未锁字段必须照常写入。
// 没有这一条，新加的过滤器过度过滤时不会报红。
func TestPartialLockApplyWritesUnlockedFields(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	ctx := context.Background()
	_, series, _ := seedBookFixture(t, store, t.TempDir(), "Lib", "Series Alpha", "a.cbz", 10)

	review, _, _, err := controller.queueMetadataReview(ctx, series, fullScrapeResult(), "bangumi", "Alpha")
	if err != nil {
		t.Fatalf("queueMetadataReview: %v", err)
	}
	before, err := store.GetSeries(ctx, series.ID)
	if err != nil {
		t.Fatalf("GetSeries: %v", err)
	}
	lockSeriesFields(t, store, series.ID, "title")

	rec := httptest.NewRecorder()
	controller.applyMetadataReview(rec, requestWithRouteParam(http.MethodPost, "/x", nil,
		"reviewId", strconv.FormatInt(review.ID, 10)))
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String())
	}

	after, err := store.GetSeries(ctx, series.ID)
	if err != nil {
		t.Fatalf("GetSeries: %v", err)
	}
	if after.Title != before.Title {
		t.Errorf("锁定的 title 被覆盖了：%v -> %v", before.Title.String, after.Title.String)
	}
	if after.Publisher.String != "Scraped Publisher" {
		t.Errorf("未锁定的 publisher 没有写入（got %q）—— 过滤过头了", after.Publisher.String)
	}
	if after.Summary.String != "Scraped summary" {
		t.Errorf("未锁定的 summary 没有写入（got %q）", after.Summary.String)
	}
}

// TestReviewViewReflectsCurrentLockState：徽章按**当前**锁定集渲染，不是入队快照。
func TestReviewViewReflectsCurrentLockState(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	ctx := context.Background()
	_, series, _ := seedBookFixture(t, store, t.TempDir(), "Lib", "Series Alpha", "a.cbz", 10)

	if _, _, _, err := controller.queueMetadataReview(ctx, series, fullScrapeResult(), "bangumi", "Alpha"); err != nil {
		t.Fatalf("queueMetadataReview: %v", err)
	}
	// 入队之后才加锁：行上的 Locked 快照仍是 false。
	lockSeriesFields(t, store, series.ID, "publisher")

	payload, err := controller.loadSeriesMetadataReview(ctx, series.ID)
	if err != nil {
		t.Fatalf("loadSeriesMetadataReview: %v", err)
	}
	if len(payload.Reviews) != 1 {
		t.Fatalf("期望 1 条待审记录，实际 %d", len(payload.Reviews))
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
		t.Fatal("待审字段里找不到 publisher")
	}
}
