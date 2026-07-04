// 业务说明：本文件由 controller.go 拆分而来，属于后端 API 层的推荐与 AI 分组子域，负责首页推荐的计算/缓存、AI 分组任务编排、系列首字母重建等接口。

package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"manga-manager/internal/database"
	"manga-manager/internal/metadata"
	"manga-manager/internal/taskcontrol"
	"net/http"
	"time"
)

type AIRecommendationResponse struct {
	SeriesID  int64  `json:"series_id"`
	Reason    string `json:"reason"`
	Title     string `json:"title"`
	CoverPath string `json:"cover_path"`
}

// getRecommendations 基于本地阅读历史的综合 LLM 推荐
func (c *Controller) getRecommendations(w http.ResponseWriter, r *http.Request) {
	locale := requestLocale(r)
	forceRefresh := r.URL.Query().Get("refresh") == "true"

	if !forceRefresh && c.cachedRecommendations(locale) != nil {
		jsonResponse(w, http.StatusOK, c.cachedRecommendations(locale))
		return
	}

	// 合并同一 locale 的并发冷缓存/刷新请求，只触发一次 LLM 推理。用 context.WithoutCancel 解绑
	// leader 的请求取消，避免 leader 客户端断开波及所有搭车的 follower（超时仍由 LLM Timeout 控制）。
	flightCtx := metadata.WithLocale(context.WithoutCancel(r.Context()), locale)
	v, err, _ := c.recommendationsGroup.Do(locale, func() (any, error) {
		if !forceRefresh {
			if cached := c.cachedRecommendations(locale); cached != nil {
				return cached, nil // 等待期间已被其他 leader 填充
			}
		}
		return c.computeRecommendations(flightCtx, locale)
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "AI inference failed: "+err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, v.([]AIRecommendationResponse))
}

// cachedRecommendations 返回未过期的缓存推荐（无有效缓存时返回 nil）。
func (c *Controller) cachedRecommendations(locale string) []AIRecommendationResponse {
	c.recommendationsMutex.RLock()
	defer c.recommendationsMutex.RUnlock()
	cache := c.recommendationsCache[locale]
	if time.Since(c.recommendationsCacheTime[locale]) < 24*time.Hour && len(cache) > 0 {
		return cache
	}
	return nil
}

// computeRecommendations 拉候选、调 LLM 生成推荐并回填缓存。由 getRecommendations 经 singleflight 调用，
// 保证同一 locale 的并发请求只执行一次。
func (c *Controller) computeRecommendations(ctx context.Context, locale string) ([]AIRecommendationResponse, error) {
	// 1. 获取用户最常看的 10 个标签
	tagRows, err := c.store.GetTopReadingTags(ctx, 10)
	var userTags []string
	if err == nil {
		for _, tr := range tagRows {
			userTags = append(userTags, tr.Name)
		}
	}

	// 2. 随机获取 20 本可能有兴趣的候选漫画
	candidateRows, err := c.store.GetCandidateSeriesForAI(ctx, 20)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch candidates from database: %w", err)
	}

	var candidates []metadata.CandidateSeries
	var candidatesMap = make(map[int64]database.GetCandidateSeriesForAIRow)
	for _, cr := range candidateRows {
		title := cr.Title.String
		if title == "" {
			title = cr.Name
		}
		summary := cr.Summary.String
		candidatesMap[cr.ID] = cr
		candidates = append(candidates, metadata.CandidateSeries{
			ID:      cr.ID,
			Title:   title,
			Summary: summary,
		})
	}

	if len(candidates) == 0 {
		return []AIRecommendationResponse{}, nil // 没有候选则不推荐，空结果不缓存
	}

	// 3. 构建 Provider
	cfg := c.currentConfig()
	provider := metadata.NewAIProvider(cfg.LLM.Provider, cfg.LLM.APIMode, cfg.LLM.BaseURL, cfg.LLM.RequestPath, cfg.LLM.Model, cfg.LLM.APIKey, cfg.LLM.Timeout)

	// 4. 交给 LLM 甄选并产出理
	recList, err := provider.GenerateRecommendations(ctx, userTags, candidates, 3)
	if err != nil {
		slog.Error("LLM failed to generate recommendations", "error", err)
		return nil, err
	}

	// 5. 组合最终回包数据
	var finalRecs []AIRecommendationResponse
	for _, rec := range recList {
		cRow, ok := candidatesMap[rec.SeriesID]
		if !ok {
			continue // AI幻觉
		}
		title := cRow.Title.String
		if title == "" {
			title = cRow.Name
		}
		coverPath := ""
		if cRow.CoverPath.Valid {
			coverPath = cRow.CoverPath.String
		}
		finalRecs = append(finalRecs, AIRecommendationResponse{
			SeriesID:  rec.SeriesID,
			Reason:    rec.Reason,
			Title:     title,
			CoverPath: coverPath,
		})
	}

	// Update cache
	c.recommendationsMutex.Lock()
	c.recommendationsCache[locale] = finalRecs
	c.recommendationsCacheTime[locale] = time.Now()
	c.recommendationsMutex.Unlock()

	return finalRecs, nil
}

// aiGroupingLibrary 扫描资料库中没有集合的系列，利用 LLM 进行智能分组
func (c *Controller) launchAIGroupingTask(libID int64, locale string) bool {
	taskKey := fmt.Sprintf("ai_grouping_library_%d", libID)
	if !c.startPausableCancelableTask(taskKey, "ai_grouping", "AI 智能分组开始...", 1) {
		return false
	}
	scopeName := ""
	if lib, err := c.store.GetLibrary(context.Background(), libID); err == nil {
		scopeName = lib.Name
	}
	c.setTaskMetadata(taskKey, nil, scopeName)
	taskCtx, cleanupCancel := c.newTaskContext(taskKey)

	c.runBackground(func() {
		defer cleanupCancel()
		libraryID, taskLocale := libID, locale
		ctx := metadata.WithLocale(taskCtx, taskLocale)

		c.updateTaskDetails(taskKey, 0, 1, "正在读取待分组系列", "collecting_series", "", nil, nil)
		seriesRows, err := c.store.GetSeriesWithoutCollection(ctx, libraryID)
		if errors.Is(err, context.Canceled) {
			c.completeTask(taskKey, "cancelled", "AI 智能分组已取消")
			return
		}
		if err != nil {
			slog.Error("Failed to fetch series for grouping", "error", err)
			c.failTaskWithError(taskKey, "AI 分组失败 (数据库获取异常)", err.Error())
			return
		}

		slog.Info("AI grouping: fetched candidate series", "library_id", libraryID, "count", len(seriesRows))

		if len(seriesRows) == 0 {
			c.finishTask(taskKey, "此库中所有作品已分组完成")
			return
		}
		if err := taskcontrol.Wait(ctx); errors.Is(err, context.Canceled) {
			c.completeTask(taskKey, "cancelled", "AI 智能分组已取消")
			return
		}

		chunkSize := 50
		if len(seriesRows) > chunkSize {
			seriesRows = seriesRows[:chunkSize]
		}

		var candidates []metadata.CandidateSeries
		for _, row := range seriesRows {
			title := row.Title.String
			if title == "" {
				title = row.Name
			}
			candidates = append(candidates, metadata.CandidateSeries{
				ID:      row.ID,
				Title:   title,
				Summary: row.Summary.String,
			})
		}

		cfg := c.currentConfig()
		provider := metadata.NewAIProvider(cfg.LLM.Provider, cfg.LLM.APIMode, cfg.LLM.BaseURL, cfg.LLM.RequestPath, cfg.LLM.Model, cfg.LLM.APIKey, cfg.LLM.Timeout)
		c.updateTaskDetails(taskKey, 0, 1, "正在请求 AI 分组", "requesting_provider", "", map[string]int64{
			"candidate_series": int64(len(candidates)),
		}, map[string]string{
			"provider": provider.Name(),
		})
		collections, err := provider.GenerateGrouping(ctx, candidates)
		if errors.Is(err, context.Canceled) {
			c.completeTask(taskKey, "cancelled", "AI 智能分组已取消")
			return
		}
		if err != nil {
			slog.Error("Failed to generate grouping", "error", err)
			c.failTaskWithError(taskKey, fmt.Sprintf("AI 分组失败: %s", err.Error()), err.Error())
			return
		}

		c.updateTaskDetails(taskKey, 1, 1, "正在写入 AI 分组审阅", "queueing_review", "", nil, nil)
		review, reviewCollections, err := c.createAIGroupingReview(ctx, libraryID, provider.Name(), candidates, collections)
		if errors.Is(err, context.Canceled) {
			c.completeTask(taskKey, "cancelled", "AI 智能分组已取消")
			return
		}
		if err != nil {
			slog.Error("Failed to create AI grouping review", "library_id", libraryID, "error", err)
			c.failTaskWithError(taskKey, "AI 分组审核生成失败", err.Error())
			return
		}
		if reviewCollections == 0 {
			c.finishTask(taskKey, "AI 智能分组未生成可审核的合集")
			return
		}

		c.finishTask(taskKey, fmt.Sprintf("AI 智能分组审核已生成 (审核单 #%d，%d 个候选合集)", review.ID, reviewCollections))
		c.PublishEvent("refresh")
	})

	return true
}

func (c *Controller) aiGroupingLibrary(w http.ResponseWriter, r *http.Request) {
	libID, err := parseID(r, "libraryId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid library ID")
		return
	}
	if !c.launchAIGroupingTask(libID, requestLocale(r)) {
		jsonResponse(w, http.StatusConflict, map[string]string{"error": "An AI grouping task is already running for this library"})
		return
	}

	jsonResponse(w, http.StatusAccepted, map[string]string{"message": "AI 分组审核任务已提交至后台"})
}

func (c *Controller) rebuildInitials(w http.ResponseWriter, r *http.Request) {
	if err := c.store.BackfillSeriesInitials(r.Context()); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "success"})
}
