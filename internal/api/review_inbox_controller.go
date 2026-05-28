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
