// 业务说明：本文件是业务实现，属于后端 HTTP API 层，负责把前端请求转换为数据库、扫描器、图片处理和元数据服务调用。
// 它承载资料库浏览、阅读器取页、系列维护、任务进度、系统设置和静态资源缓存等对外业务契约。
// 维护时应重点关注请求参数校验、错误语义、缓存头、并发任务状态和前后端字段兼容性。

package api

import (
	"net/http"
	"strings"

	"manga-manager/internal/database"
)

type reviewInboxCounts struct {
	Metadata          int64 `json:"metadata"`
	AIGrouping        int64 `json:"ai_grouping"`
	KOReaderUnmatched int64 `json:"koreader_unmatched"`
	Total             int64 `json:"total"`
}

type reviewInboxSummaryResponse struct {
	Counts reviewInboxCounts `json:"counts"`
}

// getReviewInboxSummary returns pending counts across all review categories,
// allowing the unified review center to render badges in a single fetch.
func (c *Controller) getReviewInboxSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	metadataCount, err := c.store.CountPendingMetadataReviewInbox(ctx, database.CountPendingMetadataReviewInboxParams{})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to count metadata reviews")
		return
	}

	aiCount, err := c.store.CountAIGroupingReviews(ctx, database.CountAIGroupingReviewsParams{Status: "pending"})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to count AI grouping reviews")
		return
	}

	counts := reviewInboxCounts{
		Metadata:          metadataCount,
		AIGrouping:        aiCount,
		KOReaderUnmatched: 0,
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
