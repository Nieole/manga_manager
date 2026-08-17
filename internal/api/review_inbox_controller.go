package api

import (
	"log/slog"
	"net/http"
	"strings"

	"manga-manager/internal/database"
)

type reviewInboxCounts struct {
	Metadata   int64 `json:"metadata"`
	AIGrouping int64 `json:"ai_grouping"`
	// KOReaderUnmatched 是 KOReader 同步进度里匹配不到本地书籍的条数。
	//
	// 它是**诊断类**待处理项，不是审核项：没有 pending -> applied/rejected 的逐条状态机，
	// listReviewInbox 也没有对应分支，处理入口在设置页的重建索引/重关联与 Organize 健康报告。
	// 因此**刻意不计入 Total**——Dashboard 的「有 N 条待审核」横幅只读 Total，
	// 混进一个不会随审核动作下降的数会让横幅常亮，点进去还无事可做。
	KOReaderUnmatched int64 `json:"koreader_unmatched"`
	// Total 只累加「能逐条 apply/reject」的两类，是待审核横幅的唯一来源。
	Total int64 `json:"total"`
}

type reviewInboxSummaryResponse struct {
	Counts reviewInboxCounts `json:"counts"`
}

// getReviewInboxSummary returns pending counts across all review categories,
// allowing the unified review center to render badges in a single fetch.
func (c *Controller) getReviewInboxSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	metadataCount, err := c.proposals.PendingCount(ctx)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to count metadata reviews")
		return
	}

	aiCount, err := c.store.CountAIGroupingReviews(ctx, database.CountAIGroupingReviewsParams{Status: "pending"})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to count AI grouping reviews")
		return
	}

	// 只按 KOReader 是否启用门控，与 health_controller.go 的 SkipKOReader 完全同口径
	// ——同一个数字（koreader_progress.book_id IS NULL 的全局计数）经 /api/health/report
	// 本来就对所有登录用户可见，这里再加角色门控不增加任何安全性，只会让 /organize 与
	// /reviews 显示互相矛盾的数。若确实认为它不该对普通用户可见，那是健康报告与
	// /organize 入口的独立问题，得在那两处收口，不能在这里单点「假装收口」。
	var koreaderCount int64
	if c.currentConfig().KOReader.Enabled {
		if n, err := c.store.CountUnmatchedKOReaderProgress(ctx); err != nil {
			// 旁路指标失败不该毁掉主线角标：metadata / ai_grouping 才是审核中心的主体。
			slog.Warn("Failed to count unmatched KOReader progress for review inbox summary", "error", err)
		} else {
			koreaderCount = n
		}
	}

	counts := reviewInboxCounts{
		Metadata:          metadataCount,
		AIGrouping:        aiCount,
		KOReaderUnmatched: koreaderCount,
		Total:             metadataCount + aiCount,
	}
	jsonResponse(w, http.StatusOK, reviewInboxSummaryResponse{Counts: counts})
}

// listReviewInbox dispatches by ?type= to the underlying inbox listing for
// the requested review category, providing a single front-end entrypoint.
func (c *Controller) listReviewInbox(w http.ResponseWriter, r *http.Request) {
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("type"))) {
	case "", "metadata":
		c.listMetadataReviewInbox(w, r)
	case "ai-grouping", "ai_grouping":
		c.listAIGroupingReviews(w, r)
	default:
		jsonError(w, http.StatusBadRequest, "Unsupported review inbox type")
	}
}
