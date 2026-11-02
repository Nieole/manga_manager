package api

import (
	"manga-manager/internal/metadata"
	"manga-manager/internal/task"
	"net/http"
)

type AIRecommendationResponse struct {
	SeriesID  int64  `json:"series_id"`
	Reason    string `json:"reason"`
	Title     string `json:"title"`
	CoverPath string `json:"cover_path"`
}

func (c *Controller) getRecommendations(w http.ResponseWriter, r *http.Request) {
	subject, err := c.currentReadingSubject(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid reading subject")
		return
	}
	items, err := c.recommendations.Load(r.Context(), c.store, subject, requestLocale(r), r.URL.Query().Get("refresh") == "true", func() metadata.Recommender {
		cfg := c.currentConfig()
		return metadata.NewAIProvider(cfg.LLM.Provider, cfg.LLM.APIMode, cfg.LLM.BaseURL, cfg.LLM.RequestPath, cfg.LLM.Model, cfg.LLM.APIKey, cfg.LLM.Timeout)
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "AI inference failed: "+err.Error())
		return
	}
	var out []AIRecommendationResponse
	if items != nil {
		out = make([]AIRecommendationResponse, 0, len(items))
	}
	for _, item := range items {
		out = append(out, AIRecommendationResponse{SeriesID: item.SeriesID, Reason: item.Reason, Title: item.Title, CoverPath: item.CoverPath})
	}
	jsonResponse(w, http.StatusOK, out)
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

func (c *maintenanceHTTP) rebuildInitials(w http.ResponseWriter, r *http.Request) {
	if err := c.initials.BackfillSeriesInitials(r.Context()); err != nil {
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
