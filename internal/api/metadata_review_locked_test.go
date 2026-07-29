// 业务说明：本文件守卫「锁定字段」在整条审核链路上的一致行为。
//
// 字段锁的语义是「这个值我手动定过，别让刮削覆盖」。但此前锁只在 apply 的最后一步生效：
// 锁定字段照样入队为待审提案，用户在收件箱里看到它、点「应用」，写入时又被静默跳过
// ——一次什么都没发生的操作，整条 review 却被标成已应用，被丢弃的提案再也找不回来
//（收件箱只查 pending）。全部字段都被锁时更糟：接口返回「成功」，实际零写入。
//
// 另一半是展示口径：行上的 locked 是**入队瞬间**的快照。「先入队、后加锁」的行 locked=false，
// UI 上没有任何锁徽章，用户点了应用照样被丢弃。锁定是系列的当前属性，展示时就该按当前值算。

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

// TestLockedFieldsAreNotQueued：锁定字段不再进待审队列。
func TestLockedFieldsAreNotQueued(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	ctx := context.Background()
	_, series, _ := seedBookFixture(t, store, t.TempDir(), "Lib", "Series Alpha", "a.cbz", 10)

	lockSeriesFields(t, store, series.ID, "title,summary")
	series, err := store.GetSeries(ctx, series.ID)
	if err != nil {
		t.Fatalf("GetSeries: %v", err)
	}

	_, fields, _, err := controller.queueMetadataReview(ctx, series, fullScrapeResult(), "bangumi", "Alpha")
	if err != nil {
		t.Fatalf("queueMetadataReview: %v", err)
	}

	queued := map[string]bool{}
	for _, f := range fields {
		queued[f.FieldName] = true
	}
	for _, locked := range []string{"title", "summary"} {
		if queued[locked] {
			t.Errorf("锁定字段 %q 仍被入队 —— 用户点应用后它会被静默丢弃，整条 review 却算已应用", locked)
		}
	}
	// 精确断言字段名集合，而不是「都没被锁」这种恒真判断。
	for _, want := range []string{"publisher", "status", "rating", "tags"} {
		if !queued[want] {
			t.Errorf("未锁定字段 %q 没有入队 —— 过滤过头了", want)
		}
	}
}

// TestAllFieldsLockedReportsDistinctOutcome：差异全被锁住时要与「本来就没差异」区分开。
//
// 两者对用户意味着完全不同的动作（解锁 vs 无需处理），此前都走同一条路径，
// 而且 apply 侧还会返回 HTTP 500，看起来像服务器坏了。
func TestAllFieldsLockedReportsDistinctOutcome(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	ctx := context.Background()
	_, series, _ := seedBookFixture(t, store, t.TempDir(), "Lib", "Series Alpha", "a.cbz", 10)

	lockSeriesFields(t, store, series.ID, "title,summary,publisher,status,rating,tags,authors")
	series, err := store.GetSeries(ctx, series.ID)
	if err != nil {
		t.Fatalf("GetSeries: %v", err)
	}

	_, _, _, err = controller.queueMetadataReview(ctx, series, fullScrapeResult(), "bangumi", "Alpha")
	if !errors.Is(err, errAllFieldsLocked) {
		t.Fatalf("入队返回 %v，期望 errAllFieldsLocked —— 「全被锁」被当成了「没差异」，"+
			"用户看到「数据已是最新」，实际是该去解锁", err)
	}
}

// TestApplySkipsFieldsLockedAfterQueueing：入队后才加的锁也要生效，且报告要诚实。
//
// 这是原缺陷里最隐蔽的一半：入队时未锁 → 行上 locked=false → UI 无徽章 →
// 用户点应用 → 写入时被跳过，而整条 review 被标成 applied。
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

	// review 必须**保持 pending**：被跳过的提案还留在收件箱里，用户解锁后还能再应用。
	// 此前它会被标成 applied，而收件箱只查 pending，这些提案就此永久消失。
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

// TestQueueDedupIgnoresLockedFieldsInSignature：入队后加锁不该制造重复待审记录。
//
// 新一轮刮削产出的 changes 已经把当前锁定字段筛掉了，若比对签名时不同口径地筛，
// 两边永远对不上，每次刮削都会为同一个系列再堆一条几乎相同的待审记录。
func TestQueueDedupIgnoresLockedFieldsInSignature(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	ctx := context.Background()
	_, series, _ := seedBookFixture(t, store, t.TempDir(), "Lib", "Series Alpha", "a.cbz", 10)

	first, _, isNew, err := controller.queueMetadataReview(ctx, series, fullScrapeResult(), "bangumi", "Alpha")
	if err != nil {
		t.Fatalf("首次入队: %v", err)
	}
	if !isNew {
		t.Fatal("首次入队应当是新建")
	}

	lockSeriesFields(t, store, series.ID, "title")
	relocked, err := store.GetSeries(ctx, series.ID)
	if err != nil {
		t.Fatalf("GetSeries: %v", err)
	}

	second, _, isNew, err := controller.queueMetadataReview(ctx, relocked, fullScrapeResult(), "bangumi", "Alpha")
	if err != nil {
		t.Fatalf("二次入队: %v", err)
	}
	if isNew || second.ID != first.ID {
		t.Fatalf("加锁后再刮削又建了一条待审记录（%d vs %d）—— 收件箱里会堆同一个系列的重复条目",
			second.ID, first.ID)
	}
}
