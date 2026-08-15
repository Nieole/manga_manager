// 本文件守卫审核中心角标的计数口径。
//
// koreader_unmatched 必须如实上报，且不能并入 Total：metadata / ai_grouping 有
// pending→applied/rejected 的逐条状态机，未匹配的 KOReader 进度没有（只有全局 reconcile
// 任务）——混进 Total 会让「有 N 条待审核」横幅常亮却无事可做。

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"manga-manager/internal/database"
)

func TestReviewInboxSummaryCountsUnmatchedKOReaderProgress(t *testing.T) {
	cases := []struct {
		name          string
		koreaderOn    bool
		wantUnmatched int64
	}{
		{"启用 KOReader 时如实上报", true, 1},
		// 关闭同步后遗留的历史记录不该继续报数——与健康报告的 SkipKOReader 同口径。
		{"关闭 KOReader 时不报数", false, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			controller, store, _, _ := newTestController(t)
			ctx := context.Background()

			cfg := controller.currentConfig()
			cfg.KOReader.Enabled = tc.koreaderOn
			controller.config.Replace(&cfg)

			sqlStore, ok := store.(*database.SqlStore)
			if !ok {
				t.Fatalf("需要 *SqlStore 播种，得到 %T", store)
			}
			// 一条匹配不到本地书籍的同步进度（book_id 为 NULL）。
			if _, err := sqlStore.DB().ExecContext(ctx, `
				INSERT INTO koreader_progress (username, document, progress, percentage, device, device_id, book_id)
				VALUES ('dev', 'unknown-doc', '1', 0.1, 'kindle', 'd1', NULL)
			`); err != nil {
				t.Fatalf("播种未匹配进度失败: %v", err)
			}

			rec := httptest.NewRecorder()
			controller.getReviewInboxSummary(rec, httptest.NewRequest(http.MethodGet, "/api/reviews/inbox/summary", nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String())
			}

			var resp reviewInboxSummaryResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("解析响应失败: %v", err)
			}
			if resp.Counts.KOReaderUnmatched != tc.wantUnmatched {
				t.Errorf("koreader_unmatched = %d, want %d —— 设置页与审核中心会显示互相矛盾的数字",
					resp.Counts.KOReaderUnmatched, tc.wantUnmatched)
			}
			// 契约不变量：Total 只累加可逐条 apply/reject 的两类。
			// 把诊断类计数混进去会让 Dashboard 的待审核横幅常亮，而点进去无事可做。
			if want := resp.Counts.Metadata + resp.Counts.AIGrouping; resp.Counts.Total != want {
				t.Errorf("total = %d, want %d（= metadata + ai_grouping）—— koreader_unmatched 不该并入 Total",
					resp.Counts.Total, want)
			}
		})
	}
}
