// 本文件守锁定字段在 api 侧的传输语义：单条应用全被锁时的响应形状、锁定标志落到系列详情
// 的响应里、收件箱的徽章数与同一响应里的锁标记数一致——三处错的后果一样，用户看不出某个
// 字段写不进去，点了应用才被静默丢弃。
//
// 规则本身、应用时按锁过滤、入队侧对锁的处置，都归 internal/proposal 的用例。

package api

import (
	"bytes"
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

// TestReviewViewCarriesLockBadgeToResponse：模块算好的锁定标志要一路落到响应里。
//
// 规则本身归 internal/proposal 的用例；这里守的是视图映射没把它搬丢。
func TestReviewViewCarriesLockBadgeToResponse(t *testing.T) {
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

// inboxPage 取一页收件箱，并按真实响应体解码——徽章数与锁标记必须出自同一份 JSON。
func inboxPage(t *testing.T, controller *Controller) metadataReviewInboxResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	controller.listMetadataReviewInbox(rec, httptest.NewRequest(http.MethodGet, "/inbox", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("收件箱 HTTP %d：%s", rec.Code, rec.Body.String())
	}
	var page metadataReviewInboxResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("解析收件箱响应: %v", err)
	}
	return page
}

// lockedBadges 数 diff 面板上会画出锁标记的字段。
func lockedBadges(fields []metadataReviewFieldView) int64 {
	var count int64
	for _, field := range fields {
		if field.Locked {
			count++
		}
	}
	return count
}

// seedInboxProposal 建一个系列、入队一条七字段提案，随后按 lock 加锁（空串即不加锁）。
func seedInboxProposal(t *testing.T, controller *Controller, store database.Store, lock string) database.MetadataReview {
	t.Helper()
	_, series, _ := seedBookFixture(t, store, t.TempDir(), "Lib", "Series Alpha", "a.cbz", 10)
	queued, err := controller.proposals.Queue(context.Background(), series, fullScrapeResult(), "bangumi", "Alpha", proposal.QueueOptions{})
	if err != nil || queued.Status != proposal.QueueQueued {
		t.Fatalf("入队：status=%q err=%v", queued.Status, err)
	}
	if lock != "" {
		// 入队之后才加锁：字段行上的快照仍是 false。
		lockSeriesFields(t, store, series.ID, lock)
	}
	return queued.Proposal
}

// TestInboxLockedFieldCountMatchesLockBadges：收件箱徽章的数与同一响应里的锁标记数一致。
//
// 两个数出自同一个响应体却各算各的，用户在列表上看不出哪些提案有锁，批量应用后收到一串
// locked_skipped，事前完全无从预判。
func TestInboxLockedFieldCountMatchesLockBadges(t *testing.T) {
	t.Run("入队后新增的锁也算进徽章", func(t *testing.T) {
		controller, store, _, _ := newTestController(t)
		seedInboxProposal(t, controller, store, "publisher,summary")

		page := inboxPage(t, controller)
		if len(page.Items) != 1 {
			t.Fatalf("收件箱有 %d 条，期望 1 条", len(page.Items))
		}
		item := page.Items[0]
		if badges := lockedBadges(item.Fields); item.LockedFieldCount != badges {
			t.Errorf("徽章数 locked_field_count=%d，同一响应里的锁标记有 %d 个 —— "+
				"列表看不出这条提案里有被锁的字段", item.LockedFieldCount, badges)
		}
		if item.LockedFieldCount != 2 {
			t.Errorf("locked_field_count=%d，期望 2（publisher 与 summary）", item.LockedFieldCount)
		}
	})

	t.Run("无锁定字段时徽章不出现", func(t *testing.T) {
		controller, store, _, _ := newTestController(t)
		seedInboxProposal(t, controller, store, "")

		item := inboxPage(t, controller).Items[0]
		if item.LockedFieldCount != 0 {
			t.Errorf("locked_field_count=%d，期望 0 —— 没有锁的提案不该挂琥珀色徽章", item.LockedFieldCount)
		}
		if badges := lockedBadges(item.Fields); badges != 0 {
			t.Errorf("diff 面板画了 %d 个锁标记，期望 0", badges)
		}
	})

	t.Run("批量应用的 locked_skipped 与事前显示的数对得上", func(t *testing.T) {
		controller, store, _, _ := newTestController(t)
		review := seedInboxProposal(t, controller, store, "title,summary,publisher,status,rating,tags,authors")

		item := inboxPage(t, controller).Items[0]
		if item.LockedFieldCount != item.FieldCount {
			t.Fatalf("locked_field_count=%d、field_count=%d，全锁的提案两者应当相等 —— "+
				"用户事前看不出这条提案一个字段也写不进去", item.LockedFieldCount, item.FieldCount)
		}

		body, _ := json.Marshal(metadataReviewBulkRequest{ReviewIDs: []int64{review.ID}, Mode: "all"})
		rec := httptest.NewRecorder()
		controller.bulkApplyMetadataReviews(rec, httptest.NewRequest(http.MethodPost, "/bulk-apply", bytes.NewReader(body)))
		if rec.Code != http.StatusOK {
			t.Fatalf("批量应用 HTTP %d：%s", rec.Code, rec.Body.String())
		}
		var resp metadataReviewBulkResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("解析批量应用响应: %v", err)
		}
		if len(resp.Skipped) != 1 || resp.Skipped[0] != review.ID {
			t.Fatalf("skipped=%v，期望只有提案 %d —— 事前的徽章数预示的正是这个结果",
				resp.Skipped, review.ID)
		}
	})
}
