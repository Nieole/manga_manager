package api

import (
	"net/http"

	"manga-manager/internal/task"
)

type AIRecommendationResponse struct {
	SeriesID  int64  `json:"series_id"`
	Reason    string `json:"reason"`
	Title     string `json:"title"`
	CoverPath string `json:"cover_path"`
}

func (c *Controller) aiGroupingLibrary(w http.ResponseWriter, r *http.Request) {
	libID, err := parseID(r, "libraryId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid library ID")
		return
	}
	if _, err := c.intelligence.GroupLibrary(r.Context(), libID, requestLocale(r), task.TriggerManual); err != nil {
		writeTaskLaunchError(w, err, "An AI grouping task is already running for this library", "Failed to start AI grouping")
		return
	}

	jsonResponse(w, http.StatusAccepted, map[string]string{"message": apiText(requestLocale(r), "recommendations.ai_grouping_submitted")})
}

func (c *Controller) rebuildInitials(w http.ResponseWriter, r *http.Request) {
	if err := c.store.BackfillSeriesInitials(r.Context()); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "success"})
}

// purgeRecommendationCache 清空 AI 推荐缓存（库结构变化后调用）。
// 与 invalidateDashboardStatsCache / purgeReadingPathCaches 并列，让删库那三行读起来是一件事。
func (c *Controller) purgeRecommendationCache() {
	c.recommendations.Purge()
}
