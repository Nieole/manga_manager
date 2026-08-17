// 本文件守部分应用在两个入口上的**响应形状**：批量落 Partial 桶、单条回 outcome=partial。
// 前端据此决定这条提案还留不留在收件箱里——报成 applied 会让用户以为已经处理完了。
//
// 「提案保持待裁决、只删已写入的字段行」这条不变量归 internal/proposal 的裁决用例。

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

// seedPartialApplyFixture 造一个「title 已有值、summary/publisher 为空」的系列，
// 并入队一条同时提案这三个字段的提案。fill_empty 会写后两个、筛掉 title。
func seedPartialApplyFixture(t *testing.T, controller *Controller, store database.Store, name string) (database.Series, database.MetadataReview) {
	t.Helper()
	ctx := context.Background()
	_, series, _ := seedBookFixture(t, store, t.TempDir(), "Lib-"+name, name, "book-"+name+".cbz", 10)

	sqlStore, ok := store.(*database.SqlStore)
	if !ok {
		t.Fatalf("需要 *SqlStore 播种，得到 %T", store)
	}
	if _, err := sqlStore.DB().ExecContext(ctx,
		`UPDATE series SET title = ?, summary = '', publisher = '' WHERE id = ?`,
		"Existing Title", series.ID); err != nil {
		t.Fatalf("播种系列失败: %v", err)
	}
	series, err := store.GetSeries(ctx, series.ID)
	if err != nil {
		t.Fatalf("GetSeries: %v", err)
	}

	queued, err := controller.proposals.Queue(ctx, series, &metadata.SeriesMetadata{
		Provider:   "bangumi",
		Title:      "External Title",
		Summary:    "External summary",
		Publisher:  "External Publisher",
		SourceID:   7,
		SourceURL:  "https://bgm.tv/subject/7",
		Confidence: 0.8,
	}, "bangumi", name, proposal.QueueOptions{})
	if err != nil {
		t.Fatalf("入队: %v", err)
	}
	if queued.Status != proposal.QueueQueued {
		t.Fatalf("入队得到 status=%q，用例需要一条新建的提案", queued.Status)
	}
	return series, queued.Proposal
}

// TestBulkApplyReportsPartialBucket：fill_empty 筛掉了 title，这条提案还留在收件箱里。
func TestBulkApplyReportsPartialBucket(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	_, review := seedPartialApplyFixture(t, controller, store, "Alpha")

	body := []byte(`{"review_ids":[` + strconv.FormatInt(review.ID, 10) + `],"mode":"fill_empty"}`)
	rec := httptest.NewRecorder()
	controller.bulkApplyMetadataReviews(rec,
		httptest.NewRequest(http.MethodPost, "/api/metadata/reviews/bulk-apply", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String())
	}
	var resp metadataReviewBulkResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应: %v", err)
	}
	if len(resp.Partial) != 1 || len(resp.Applied) != 0 || len(resp.Failed) != 0 {
		t.Errorf("响应 = %+v，期望只落 Partial 桶 —— 报成 Applied 会让用户以为已经处理完了", resp)
	}
}

// TestSingleApplyReportsPartialOutcome：单条入口给出同一种分类。
func TestSingleApplyReportsPartialOutcome(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	_, review := seedPartialApplyFixture(t, controller, store, "Gamma")

	// 入队之后锁住 summary：它不会被写入，因此提案不能关单。
	lockSeriesFields(t, store, review.SeriesID, "summary")

	rec := httptest.NewRecorder()
	controller.applyMetadataReview(rec, requestWithRouteParam(http.MethodPost, "/x", nil,
		"reviewId", strconv.FormatInt(review.ID, 10)))
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应: %v", err)
	}
	if outcome, _ := resp["outcome"].(string); outcome != "partial" {
		t.Errorf("outcome = %q，期望 partial", outcome)
	}
	if applied, _ := resp["applied"].(bool); !applied {
		t.Error("响应称 applied=false，但确实写进去了一部分")
	}
	if remaining, _ := resp["remaining_fields"].([]any); len(remaining) == 0 {
		t.Error("remaining_fields 为空 —— 前端据此提示还有几个字段没处理")
	}
}
