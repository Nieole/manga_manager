// 本文件由 controller.go 拆分而来，属于后端 API 层的推荐与 AI 分组子域，负责首页推荐的计算/缓存、AI 分组任务编排、系列首字母重建等接口。

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

	// 2. 随机获取 20 本可能有兴趣的候选漫画。
	//    传 userID 是必需的：「排除已读过半」的过滤要看**这个用户**的进度，
	//    读全局 books.last_read_page 在多用户下恒为 0，等于没有过滤。
	candidateRows, err := c.store.SampleCandidateSeriesForAI(ctx, userID, 20)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch candidates from database: %w", err)
	}

	var candidates []metadata.CandidateSeries
	var candidatesMap = make(map[int64]database.CandidateSeriesForAI)
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

// launchAIGroupingTask 把资料库里尚未归入合集的系列交给 LLM 智能分组，走引擎的启动入口。
//
// 它的**完成**分支有三个（生成了审阅单 / 全都已分组 / 没产出可审阅的合集），失败分支有三个，
// 取消分支只有一个：都由任务体经 TaskResult 覆盖任务声明里的默认码表达。
func (c *Controller) launchAIGroupingTask(libID int64, locale string) error {
	scopeName := ""
	if lib, err := c.store.GetLibrary(context.Background(), libID); err == nil {
		scopeName = lib.Name
	}

	spec := TaskSpec{
		Key:       fmt.Sprintf("ai_grouping_library_%d", libID),
		Type:      "ai_grouping",
		StartCode: "task.msg.ai_grouping.start",
		Total:     1,
		CanCancel: true,
		CanPause:  true,
		ScopeName: scopeName,
		// locale 必须落进任务参数（而不是起始文案的占位参数）：**重启函数**靠它恢复原始语言，
		// 缺失时只能回退成硬编码 zh-CN，重试出来的分组理由就换了语种。
		Metadata:     map[string]string{"locale": locale},
		CompleteCode: "task.msg.ai_grouping.review_generated",
		CancelCode:   "task.msg.ai_grouping.cancelled",
		FailCode:     "task.msg.ai_grouping.fail_generate",
	}

	return c.taskEngine.Run(spec, func(taskCtx context.Context, tp *TaskProgress) (TaskResult, error) {
		ctx := metadata.WithLocale(taskCtx, locale)

		tp.Phase("collecting_series", "task.msg.ai_grouping.collecting_series", nil)
		seriesRows, err := c.store.GetSeriesWithoutCollection(ctx, libID)
		if err != nil {
			// 取消是用户按的，不是故障：无条件记 ERROR 的话，每按一次取消日志里就多出
			// 一串看着像故障的 "context canceled"。下面两处同理。
			if !errors.Is(err, context.Canceled) {
				slog.Error("Failed to fetch series for grouping", "library_id", libID, "error", err)
			}
			return taskFailure("task.msg.ai_grouping.fail_db_fetch", err), err
		}

		slog.Info("AI grouping: fetched candidate series", "library_id", libID, "count", len(seriesRows))

		if len(seriesRows) == 0 {
			return TaskResult{Code: "task.msg.ai_grouping.all_already_grouped"}, nil
		}
		if err := taskcontrol.Wait(ctx); err != nil {
			return TaskResult{}, err
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
		tp.Report(TaskFrame{
			Phase:   "requesting_provider",
			Code:    "task.msg.ai_grouping.requesting_provider",
			Metrics: map[string]int64{"candidate_series": int64(len(candidates))},
			Labels:  map[string]string{"provider": provider.Name()},
		})
		collections, err := provider.GenerateGrouping(ctx, candidates)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				slog.Error("Failed to generate grouping", "library_id", libID, "error", err)
			}
			return TaskResult{}, err
		}

		done := 1
		tp.Report(TaskFrame{
			Current: &done,
			Phase:   "queueing_review",
			Code:    "task.msg.ai_grouping.queueing_review",
		})
		review, reviewCollections, err := c.createAIGroupingReview(ctx, libID, provider.Name(), candidates, collections)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				slog.Error("Failed to create AI grouping review", "library_id", libID, "error", err)
			}
			return taskFailure("task.msg.ai_grouping.fail_create_review", err), err
		}
		if reviewCollections == 0 {
			return TaskResult{Code: "task.msg.ai_grouping.no_review_collections"}, nil
		}

		c.PublishEvent("refresh")
		return TaskResult{Params: map[string]string{
			"reviewId": strconv.FormatInt(review.ID, 10),
			"count":    strconv.Itoa(reviewCollections),
		}}, nil
	})
}

func (c *Controller) aiGroupingLibrary(w http.ResponseWriter, r *http.Request) {
	libID, err := parseID(r, "libraryId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid library ID")
		return
	}
	if err := c.launchAIGroupingTask(libID, requestLocale(r)); err != nil {
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
	c.recommendations.purge()
}
