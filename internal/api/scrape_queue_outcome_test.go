// 本文件守刮削入口把一次入队的结局翻成 outcome 码与文案这张表。这些结局全是良性的，
// 必须回 200 并给出可行动的说法：翻错一个码，前端会用另一种原因解释这次刮削（对「你自己拒过」
// 说成「队列里已有相同记录」尤其误导）；漏掉一个，用户只剩一条与实情无关的兜底提示。
// 去重与锁定规则本身归 internal/proposal 的用例。

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

// scrapeApplyResult 是 scrape-apply 响应里用例关心的那几个字段。
type scrapeApplyResult struct {
	Success  bool   `json:"success"`
	Queued   bool   `json:"queued"`
	Outcome  string `json:"outcome"`
	Message  string `json:"message"`
	ReviewID int64  `json:"review_id"`
}

// postScrapeApply 把 result 交给 scrape-apply 入口；extraQuery 是附加查询串（如 "&force=1"）。
func postScrapeApply(t *testing.T, controller *Controller, seriesID int64, result *metadata.SeriesMetadata, extraQuery string) scrapeApplyResult {
	t.Helper()
	body, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	rec := httptest.NewRecorder()
	controller.applyScrapedMetadata(rec, requestWithRouteParam(http.MethodPost,
		"/api/series/1/scrape-apply?provider=bangumi"+extraQuery, body,
		"seriesId", strconv.FormatInt(seriesID, 10)))
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s —— 良性结果不该报服务端错误", rec.Code, rec.Body.String())
	}
	var out scrapeApplyResult
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("解析响应: %v", err)
	}
	return out
}

// rejectReview 走 HTTP handler 把提案拒掉。
func rejectReview(t *testing.T, controller *Controller, reviewID int64) {
	t.Helper()
	rec := httptest.NewRecorder()
	controller.rejectMetadataReview(rec, requestWithRouteParam(http.MethodPost, "/x", nil,
		"reviewId", strconv.FormatInt(reviewID, 10)))
	if rec.Code != http.StatusOK {
		t.Fatalf("reject 失败：%d %s", rec.Code, rec.Body.String())
	}
}

func TestScrapeApplyTranslatesQueueOutcomes(t *testing.T) {
	tests := []struct {
		name string
		want string
		// arrange 摆好库里的状态，并返回本次要提交的刮削结果。
		arrange func(t *testing.T, controller *Controller, store database.Store, series database.Series) *metadata.SeriesMetadata
	}{
		{
			name: "队列里已有逐字相同的提案",
			want: "duplicate_ignored",
			arrange: func(t *testing.T, controller *Controller, _ database.Store, series database.Series) *metadata.SeriesMetadata {
				if first := postScrapeApply(t, controller, series.ID, rejectedDedupResult(), ""); !first.Queued {
					t.Fatalf("铺垫的首次入队没成功：%+v", first)
				}
				return rejectedDedupResult()
			},
		},
		{
			name: "与当前值毫无差异",
			want: "no_changes",
			arrange: func(_ *testing.T, _ *Controller, _ database.Store, series database.Series) *metadata.SeriesMetadata {
				// 系列没有 title 时当前值就是它的名字，提交同一个值即毫无差异。
				return &metadata.SeriesMetadata{Provider: "bangumi", Title: series.Name, SourceID: 99}
			},
		},
		{
			name: "有差异但差异字段全被锁",
			want: "all_locked",
			arrange: func(t *testing.T, _ *Controller, store database.Store, series database.Series) *metadata.SeriesMetadata {
				lockSeriesFields(t, store, series.ID, "title")
				return &metadata.SeriesMetadata{Provider: "bangumi", Title: "A Different Title", SourceID: 99}
			},
		},
		{
			name: "此前已被用户拒绝",
			want: "rejected_before",
			arrange: func(t *testing.T, controller *Controller, _ database.Store, series database.Series) *metadata.SeriesMetadata {
				first := postScrapeApply(t, controller, series.ID, rejectedDedupResult(), "")
				if !first.Queued {
					t.Fatalf("铺垫的首次入队没成功：%+v", first)
				}
				rejectReview(t, controller, first.ReviewID)
				return rejectedDedupResult()
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			controller, store, _, _ := newTestController(t)
			_, series, _ := seedBookFixture(t, store, t.TempDir(), "Lib", "Series Alpha", "a.cbz", 10)

			result := tc.arrange(t, controller, store, series)
			got := postScrapeApply(t, controller, series.ID, result, "")

			if !got.Success || got.Queued {
				t.Errorf("success=%v queued=%v，期望 true/false —— 良性结局不是失败，也没有入队",
					got.Success, got.Queued)
			}
			if got.Outcome != tc.want {
				t.Errorf("outcome = %q，期望 %q —— 前端会用另一种原因解释这次刮削", got.Outcome, tc.want)
			}
			if got.Message == "" {
				t.Error("message 为空 —— 未映射 outcome 的客户端拿不到任何说法")
			}
		})
	}
}

// TestScrapeApplyForceParamRequeues：force=1 是 UI 上「仍要加入队列」的出口。
// 没有它，误拒之后每次刮削都会被去重挡下，同一份数据再也进不来。
func TestScrapeApplyForceParamRequeues(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	_, series, _ := seedBookFixture(t, store, t.TempDir(), "Lib", "Series Alpha", "a.cbz", 10)

	first := postScrapeApply(t, controller, series.ID, rejectedDedupResult(), "")
	if !first.Queued {
		t.Fatalf("首次入队没成功：%+v", first)
	}
	rejectReview(t, controller, first.ReviewID)

	forced := postScrapeApply(t, controller, series.ID, rejectedDedupResult(), "&force=1")
	if !forced.Queued {
		t.Fatalf("force=1 仍未入队：%+v —— UI 上「仍要加入队列」这条路是断的", forced)
	}
	pending, err := store.ListPendingMetadataReviewsBySeries(context.Background(), series.ID)
	if err != nil {
		t.Fatalf("ListPendingMetadataReviewsBySeries: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("强制入队后待裁决队列有 %d 条，期望 1 条", len(pending))
	}
}
