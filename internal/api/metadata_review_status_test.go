// 本文件守四条终态推进路径的**传输语义**：单条/批量的应用与拒绝，撞上已进终态的提案
// 一律回 409，正常路径回 200。前端按状态码分支——把冲突报成 500 会让用户看到「服务器错误」，
// 报成 200 则会让界面以为处理成功了。
//
// 终态本身的不变量（时间戳不得同时非空、冲突时整体回滚）归 internal/proposal 的裁决用例。

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

// seedPendingMetadataReview 造一个系列 + 一条待裁决的提案。
func seedPendingMetadataReview(t *testing.T, controller *Controller, store database.Store, name string) (database.Series, database.MetadataReview) {
	t.Helper()
	_, series, _ := seedBookFixture(t, store, t.TempDir(), "Lib-"+name, name, "book-"+name+".cbz", 10)

	queued, err := controller.proposals.Queue(context.Background(), series, &metadata.SeriesMetadata{
		Title:      "External " + name,
		Publisher:  "External Publisher",
		Summary:    "External summary",
		SourceID:   1,
		SourceURL:  "https://example.test/" + name,
		Provider:   "bangumi",
		Confidence: 0.7,
	}, "bangumi", name, proposal.QueueOptions{})
	if err != nil {
		t.Fatalf("入队: %v", err)
	}
	if queued.Status != proposal.QueueQueued {
		t.Fatalf("入队得到 status=%q，用例需要一条新建的提案", queued.Status)
	}
	return series, queued.Proposal
}

// TestMetadataReviewTerminalStatusIsGuarded 钉住单条入口的 409。
//
// 播种一律走 HTTP handler 而不是 store 方法：这样把修复回滚做缺陷还原时，用例是以
// **断言失败**报红，而不是因为 store 方法改了名而编译不过——后者证明不了任何事。
func TestMetadataReviewTerminalStatusIsGuarded(t *testing.T) {
	cases := []struct {
		name     string
		seed     string // 先把提案推到的终态：applied / rejected / ""（保持待裁决）
		action   string // 随后执行的动作：apply / reject
		wantCode int
	}{
		{"已应用后再拒绝", "applied", "reject", http.StatusConflict},
		{"已拒绝后再应用", "rejected", "apply", http.StatusConflict},
		{"已拒绝后再拒绝", "rejected", "reject", http.StatusConflict},
		{"已应用后再应用", "applied", "apply", http.StatusConflict},
		{"待裁决时正常拒绝", "", "reject", http.StatusOK},
		{"待裁决时正常应用", "", "apply", http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			controller, store, _, _ := newTestController(t)
			_, review := seedPendingMetadataReview(t, controller, store, "Series-"+tc.name)

			invoke := func(action string) *httptest.ResponseRecorder {
				rec := httptest.NewRecorder()
				req := requestWithRouteParam(http.MethodPost, "/api/metadata/reviews/x", nil,
					"reviewId", strconv.FormatInt(review.ID, 10))
				if action == "apply" {
					controller.applyMetadataReview(rec, req)
				} else {
					controller.rejectMetadataReview(rec, req)
				}
				return rec
			}

			if tc.seed != "" {
				seedAction := "apply"
				if tc.seed == "rejected" {
					seedAction = "reject"
				}
				if rec := invoke(seedAction); rec.Code != http.StatusOK {
					t.Fatalf("播种 %s 失败：HTTP %d %s", tc.seed, rec.Code, rec.Body.String())
				}
			}

			rec := invoke(tc.action)
			if rec.Code != tc.wantCode {
				t.Fatalf("%s 返回 %d，期望 %d（body=%s）", tc.action, rec.Code, tc.wantCode, rec.Body.String())
			}
		})
	}
}

// TestBulkMetadataReviewPutsPreemptedInFailed 钉住批量入口对已进终态提案的归桶：
// 落 failed，与「查不到」同桶——批量响应是前端既有契约，被抢先不另开桶。
func TestBulkMetadataReviewPutsPreemptedInFailed(t *testing.T) {
	for _, action := range []string{"apply", "reject"} {
		t.Run(action, func(t *testing.T) {
			controller, store, _, _ := newTestController(t)
			_, review := seedPendingMetadataReview(t, controller, store, "Bulk-"+action)

			// 先把它拒了，等价于另一个标签页抢先处理。
			rec := httptest.NewRecorder()
			controller.rejectMetadataReview(rec, requestWithRouteParam(http.MethodPost, "/x", nil,
				"reviewId", strconv.FormatInt(review.ID, 10)))
			if rec.Code != http.StatusOK {
				t.Fatalf("抢先拒绝失败：%d %s", rec.Code, rec.Body.String())
			}

			body := []byte(`{"review_ids":[` + strconv.FormatInt(review.ID, 10) + `],"mode":"all"}`)
			rec = httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/metadata/reviews/bulk", bytes.NewReader(body))
			if action == "apply" {
				controller.bulkApplyMetadataReviews(rec, req)
			} else {
				controller.bulkRejectMetadataReviews(rec, req)
			}
			if rec.Code != http.StatusOK {
				t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String())
			}
			var resp metadataReviewBulkResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("解析响应: %v", err)
			}
			if len(resp.Failed) != 1 || resp.Success {
				t.Fatalf("批量%s 的响应 = %+v，期望被抢先的那条落 failed 且 success=false", action, resp)
			}
		})
	}
}
