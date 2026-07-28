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
	"strconv"
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
	userID := c.currentUserID(r)
	forceRefresh := r.URL.Query().Get("refresh") == "true"
	// 缓存与 singleflight 都必须按 (locale, user) 分区：推荐是基于该用户的阅读历史算出来的，
	// 只按 locale 分区会让所有人共用同一份结果——既没有个人化，也把彼此的阅读偏好互相泄露。
	cacheKey := recommendationCacheKey(locale, userID)

	if !forceRefresh && c.cachedRecommendations(cacheKey) != nil {
		jsonResponse(w, http.StatusOK, c.cachedRecommendations(cacheKey))
		return
	}

	// 合并同一 (locale, user) 的并发冷缓存/刷新请求，只触发一次 LLM 推理。用 context.WithoutCancel
	// 解绑 leader 的请求取消，避免 leader 客户端断开波及所有搭车的 follower（超时仍由 LLM Timeout 控制）。
	flightCtx := metadata.WithLocale(context.WithoutCancel(r.Context()), locale)
	v, err := c.recommendations.do(cacheKey, func() (any, error) {
		if !forceRefresh {
			if cached := c.cachedRecommendations(cacheKey); cached != nil {
				return cached, nil // 等待期间已被其他 leader 填充
			}
		}
		return c.computeRecommendations(flightCtx, locale, userID)
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "AI inference failed: "+err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, v.([]AIRecommendationResponse))
}

// recommendationCacheKey 把 locale 与用户 id 组合成缓存分区键。
// 未登录/单用户部署下 userID 为 0，键退化为纯 locale 语义，行为与改造前一致。
func recommendationCacheKey(locale string, userID int64) string {
	return locale + "|" + strconv.FormatInt(userID, 10)
}

// cachedRecommendations 返回未过期的缓存推荐（无有效缓存时返回 nil）；委托给 recommendationCache。
func (c *Controller) cachedRecommendations(key string) []AIRecommendationResponse {
	return c.recommendations.cached(key)
}

// computeRecommendations 拉候选、调 LLM 生成推荐并回填缓存。由 getRecommendations 经 singleflight 调用，
// 保证同一 locale 的并发请求只执行一次。
func (c *Controller) computeRecommendations(ctx context.Context, locale string, userID int64) ([]AIRecommendationResponse, error) {
	// 先记下缓存世代：LLM 推理要几秒到几十秒，期间可能发生删库并 purge 缓存。
	// 回填时若世代已变，说明这份结果基于已不存在的数据，storeAt 会丢弃它。
	cacheGen := c.recommendations.snapshotGen()

	// 1. 获取该用户最常看的 10 个标签。多用户下必须走 user_reading_activity，
	//    否则每个人拿到的都是全站合并出来的偏好。
	var (
		tagRows []database.GetTopReadingTagsRow
		err     error
	)
	if userID > 0 {
		tagRows, err = c.store.GetUserTopReadingTags(ctx, userID, 10)
	} else {
		tagRows, err = c.store.GetTopReadingTags(ctx, 10)
	}
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

	// 回填缓存。键必须与 getRecommendations 读取时用的完全一致，否则写进去的分区
	// 永远读不到，每次首页请求都会重新调一次 LLM。
	c.recommendations.storeAt(cacheGen, recommendationCacheKey(locale, userID), finalRecs)

	return finalRecs, nil
}

// aiGroupingLibrary 扫描资料库中没有集合的系列，利用 LLM 进行智能分组
func (c *Controller) launchAIGroupingTask(libID int64, locale string) bool {
	taskKey := fmt.Sprintf("ai_grouping_library_%d", libID)
	if !c.taskEngine.startPausableCancelableTaskMsg(taskKey, "ai_grouping", "task.msg.ai_grouping.start", nil, 1) {
		return false
	}
	scopeName := ""
	if lib, err := c.store.GetLibrary(context.Background(), libID); err == nil {
		scopeName = lib.Name
	}
	// 持久化 locale 到任务参数，使重试能恢复原始语言（此前 locale 从未落库、重试只能硬编码 zh-CN）。
	c.taskEngine.setTaskMetadata(taskKey, map[string]string{"locale": locale}, scopeName)
	taskCtx, cleanupCancel := c.taskEngine.newTaskContext(taskKey)

	c.runBackgroundTask(taskKey, func() {
		defer cleanupCancel()
		libraryID, taskLocale := libID, locale
		ctx := metadata.WithLocale(taskCtx, taskLocale)

		c.taskEngine.updateTaskDetailsMsg(taskKey, 0, 1, "task.msg.ai_grouping.collecting_series", nil, "collecting_series", "", nil, nil)
		seriesRows, err := c.store.GetSeriesWithoutCollection(ctx, libraryID)
		if errors.Is(err, context.Canceled) {
			c.taskEngine.completeTaskMsg(taskKey, "cancelled", "task.msg.ai_grouping.cancelled", nil)
			return
		}
		if err != nil {
			slog.Error("Failed to fetch series for grouping", "error", err)
			c.taskEngine.failTaskErrMsg(taskKey, "task.msg.ai_grouping.fail_db_fetch", nil, err.Error())
			return
		}

		slog.Info("AI grouping: fetched candidate series", "library_id", libraryID, "count", len(seriesRows))

		if len(seriesRows) == 0 {
			c.taskEngine.finishTaskMsg(taskKey, "task.msg.ai_grouping.all_already_grouped", nil)
			return
		}
		if err := taskcontrol.Wait(ctx); errors.Is(err, context.Canceled) {
			c.taskEngine.completeTaskMsg(taskKey, "cancelled", "task.msg.ai_grouping.cancelled", nil)
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
		c.taskEngine.updateTaskDetailsMsg(taskKey, 0, 1, "task.msg.ai_grouping.requesting_provider", nil, "requesting_provider", "", map[string]int64{
			"candidate_series": int64(len(candidates)),
		}, map[string]string{
			"provider": provider.Name(),
		})
		collections, err := provider.GenerateGrouping(ctx, candidates)
		if errors.Is(err, context.Canceled) {
			c.taskEngine.completeTaskMsg(taskKey, "cancelled", "task.msg.ai_grouping.cancelled", nil)
			return
		}
		if err != nil {
			slog.Error("Failed to generate grouping", "error", err)
			c.taskEngine.failTaskErrMsg(taskKey, "task.msg.ai_grouping.fail_generate", nil, err.Error())
			return
		}

		c.taskEngine.updateTaskDetailsMsg(taskKey, 1, 1, "task.msg.ai_grouping.queueing_review", nil, "queueing_review", "", nil, nil)
		review, reviewCollections, err := c.createAIGroupingReview(ctx, libraryID, provider.Name(), candidates, collections)
		if errors.Is(err, context.Canceled) {
			c.taskEngine.completeTaskMsg(taskKey, "cancelled", "task.msg.ai_grouping.cancelled", nil)
			return
		}
		if err != nil {
			slog.Error("Failed to create AI grouping review", "library_id", libraryID, "error", err)
			c.taskEngine.failTaskErrMsg(taskKey, "task.msg.ai_grouping.fail_create_review", nil, err.Error())
			return
		}
		if reviewCollections == 0 {
			c.taskEngine.finishTaskMsg(taskKey, "task.msg.ai_grouping.no_review_collections", nil)
			return
		}

		c.taskEngine.finishTaskMsg(taskKey, "task.msg.ai_grouping.review_generated", map[string]string{"reviewId": strconv.FormatInt(review.ID, 10), "count": strconv.Itoa(reviewCollections)})
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
	c.recommendations.purge()
}
