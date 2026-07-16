// 业务说明：本文件是业务实现，属于后端 HTTP API 层，负责把前端请求转换为数据库、扫描器、图片处理和元数据服务调用。
// 它承载资料库浏览、阅读器取页、系列维护、任务进度、系统设置和静态资源缓存等对外业务契约。
// 维护时应重点关注请求参数校验、错误语义、缓存头、并发任务状态和前后端字段兼容性。

package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/metadata"
	"manga-manager/internal/taskcontrol"
)

const scrapeRateLimitDelay = 500 * time.Millisecond

// 以下 helper 用常量格式串按 locale 生成带占位的刮削响应文案（默认中文，en-US 输出英文），
// 满足 go vet 的常量格式检查（若把 %s/%v 格式串入表再 Sprintf，vet 会报 non-constant format string）。
func scrapeNotFoundMsg(locale, providerName string) string {
	if locale == "en-US" {
		return fmt.Sprintf("No matching entry found on %s", providerName)
	}
	return fmt.Sprintf("未在 %s 上找到匹配的条目", providerName)
}

func scrapeSearchFailedMsg(locale, providerName string, err error) string {
	if locale == "en-US" {
		return fmt.Sprintf("%s search failed: %v", providerName, err)
	}
	return fmt.Sprintf("%s 搜索失败: %v", providerName, err)
}

func scrapeFailedMsg(locale, providerName string, err error) string {
	if locale == "en-US" {
		return fmt.Sprintf("%s scrape failed: %v", providerName, err)
	}
	return fmt.Sprintf("%s 刮削失败: %v", providerName, err)
}

// getProvider 根据名称返回对应的 Provider 实例
func (c *Controller) getProvider(name string) metadata.Provider {
	if c.providerFactory != nil {
		return c.providerFactory(name)
	}
	switch strings.ToLower(name) {
	case "ollama", "llm", "openai", "openai-legacy":
		cfg := c.currentConfig()
		provider := cfg.LLM.Provider
		model := cfg.LLM.Model
		apiKey := cfg.LLM.APIKey
		return metadata.NewAIProvider(provider, cfg.LLM.APIMode, cfg.LLM.BaseURL, cfg.LLM.RequestPath, model, apiKey, cfg.LLM.Timeout)
	case "anilist":
		return metadata.NewAniListProvider()
	case "mangadex":
		return metadata.NewMangaDexProvider()
	case "myanimelist", "mal":
		return metadata.NewMyAnimeListProvider(c.currentConfig().Scrapers.MALClientID)
	case "comicvine", "comic-vine", "comic_vine":
		return metadata.NewComicVineProvider(c.currentConfig().Scrapers.ComicVineAPIKey)
	default:
		return metadata.NewBangumiProvider()
	}
}

// availableProviders 返回可用的 provider 列表供前端展示。
// AniList / MangaDex 免密钥恒可用；MyAnimeList / Comic Vine 仅在配置了对应凭据时出现（否则源不可用，避免误选）。
func (c *Controller) listProviders(w http.ResponseWriter, r *http.Request) {
	providers := []map[string]string{
		{"id": "bangumi", "name": "Bangumi", "description": "从 Bangumi 番组计划获取漫画元数据"},
		{"id": "anilist", "name": "AniList", "description": "从 AniList 获取漫画元数据（英文/罗马音/原名、评分、标签、作者）"},
		{"id": "mangadex", "name": "MangaDex", "description": "从 MangaDex 获取漫画元数据（多语言标题、标签、作者、封面）"},
	}
	cfg := c.currentConfig()
	if strings.TrimSpace(cfg.Scrapers.MALClientID) != "" {
		providers = append(providers, map[string]string{"id": "myanimelist", "name": "MyAnimeList", "description": "从 MyAnimeList 获取漫画元数据（需配置 Client ID）"})
	}
	if strings.TrimSpace(cfg.Scrapers.ComicVineAPIKey) != "" {
		providers = append(providers, map[string]string{"id": "comicvine", "name": "Comic Vine", "description": "从 Comic Vine 获取欧美 comics 元数据（需配置 API Key）"})
	}
	providers = append(providers, map[string]string{"id": "llm", "name": "AI/LLM 解析", "description": "通过配置的大语言模型(如 Ollama, LM Studio, OpenAI)推理生成元数据"})
	jsonResponse(w, http.StatusOK, providers)
}

func (c *Controller) searchMetadata(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		jsonError(w, http.StatusBadRequest, "Missing query parameter 'q'")
		return
	}

	providerName := r.URL.Query().Get("provider")
	provider := c.getProvider(providerName)

	result, err := provider.FetchSeriesMetadata(requestContextWithLocale(r), query)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, scrapeSearchFailedMsg(requestLocale(r), provider.Name(), err))
		return
	}

	if result == nil {
		jsonResponse(w, http.StatusOK, map[string]interface{}{"found": false, "message": scrapeNotFoundMsg(requestLocale(r), provider.Name())})
		return
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"found":      true,
		"provider":   provider.Name(),
		"title":      result.Title,
		"summary":    result.Summary,
		"publisher":  result.Publisher,
		"cover_url":  result.CoverURL,
		"rating":     result.Rating,
		"tags":       result.Tags,
		"source_id":  result.SourceID,
		"source_url": metadataSourceURL(provider.Name(), result),
		"confidence": result.Confidence,
	})
}

func (c *Controller) scrapeSearchMetadata(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}

	providerName := r.URL.Query().Get("provider")
	provider := c.getProvider(providerName)

	// 优先从查询参数获取 q，若无则按系列标题搜索
	searchTitle := r.URL.Query().Get("q")
	if searchTitle == "" {
		series, err := c.store.GetSeries(r.Context(), seriesID)
		if err != nil {
			jsonError(w, http.StatusNotFound, "Series not found")
			return
		}

		searchTitle = series.Name
		if series.Title.Valid && series.Title.String != "" {
			searchTitle = series.Title.String
		}
	}

	limitStr := r.URL.Query().Get("limit")
	limit := 20
	if limitStr != "" {
		fmt.Sscanf(limitStr, "%d", &limit)
	}
	offsetStr := r.URL.Query().Get("offset")
	offset := 0
	if offsetStr != "" {
		fmt.Sscanf(offsetStr, "%d", &offset)
	}

	results, total, err := provider.SearchMetadata(requestContextWithLocale(r), searchTitle, limit, offset)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, scrapeSearchFailedMsg(requestLocale(r), provider.Name(), err))
		return
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"results":  results,
		"provider": provider.Name(),
		"limit":    limit,
		"offset":   offset,
		"total":    total,
	})
}

func (c *Controller) applyScrapedMetadata(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}

	var result metadata.SeriesMetadata
	if err := json.NewDecoder(r.Body).Decode(&result); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid metadata payload")
		return
	}

	// 从路径参数或请求体获取 provider 用于记录来源链接
	providerName := r.URL.Query().Get("provider")

	series, err := c.store.GetSeries(r.Context(), seriesID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "Series not found")
		return
	}

	review, fields, isNew, err := c.queueMetadataReview(r.Context(), series, &result, providerName, series.Name)
	if err != nil {
		if errors.Is(err, errNoMetadataChanges) {
			jsonResponse(w, http.StatusOK, map[string]interface{}{
				"success": true,
				"queued":  false,
				"outcome": "no_changes",
				"message": "所有数据与当前信息完全一致，无需更新",
			})
			return
		}
		jsonError(w, http.StatusInternalServerError, "Failed to queue metadata review")
		return
	}

	if !isNew {
		jsonResponse(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"queued":  false,
			"outcome": "duplicate_ignored",
			"message": "待审核队列中已存在完全相同的记录，已为您忽略",
		})
		return
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"success":     true,
		"queued":      true,
		"outcome":     "queued",
		"review_id":   review.ID,
		"field_count": len(fields),
		"series":      series,
	})
}

func (c *Controller) applyMetadataToSeries(ctx context.Context, series database.Series, result *metadata.SeriesMetadata, opts metadataApplyOptions) error {
	return c.applyMetadataToSeriesWithHook(ctx, series, result, opts, nil)
}

// applyMetadataToSeriesWithHook 在同一事务内应用系列元数据，并可选在提交前执行 afterInTx（同事务）。
// 元数据审阅 apply 借此把「写元数据」与「标记 review 已处理」并入同一事务，避免元数据已写但状态仍
// pending 导致同一 review 被重复 apply。
func (c *Controller) applyMetadataToSeriesWithHook(ctx context.Context, series database.Series, result *metadata.SeriesMetadata, opts metadataApplyOptions, afterInTx func(*database.Queries) error) error {
	// 解析已锁定字段
	lockedSet := metadataLockedFieldSet(series)
	providerName := strings.TrimSpace(opts.ProviderName)

	return c.store.ExecTx(ctx, func(q *database.Queries) error {
		updateParams := database.UpdateSeriesMetadataParams{ID: series.ID}
		appliedFields := make(map[string]string)
		confidence := opts.Confidence
		if confidence <= 0 {
			confidence = metadataDefaultConfidence(opts.ProviderName)
		}
		reviewID := sql.NullInt64{}
		if opts.ReviewID != nil {
			reviewID = sql.NullInt64{Int64: *opts.ReviewID, Valid: true}
		}

		if !lockedSet["title"] && result.Title != "" {
			updateParams.Title = sql.NullString{String: result.Title, Valid: true}
			appliedFields["title"] = result.Title
		} else {
			updateParams.Title = series.Title
		}

		if !lockedSet["summary"] && result.Summary != "" {
			updateParams.Summary = sql.NullString{String: result.Summary, Valid: true}
			appliedFields["summary"] = result.Summary
		} else {
			updateParams.Summary = series.Summary
		}

		if !lockedSet["publisher"] && result.Publisher != "" {
			updateParams.Publisher = sql.NullString{String: result.Publisher, Valid: true}
			appliedFields["publisher"] = result.Publisher
		} else {
			updateParams.Publisher = series.Publisher
		}

		if !lockedSet["rating"] && result.Rating > 0 {
			updateParams.Rating = sql.NullFloat64{Float64: result.Rating, Valid: true}
			appliedFields["rating"] = fmt.Sprintf("%.1f", result.Rating)
		} else {
			updateParams.Rating = series.Rating
		}

		if !lockedSet["status"] && result.Status != "" {
			status := metadata.NormalizeStatusCode(result.Status)
			updateParams.Status = sql.NullString{String: status, Valid: true}
			appliedFields["status"] = status
		} else {
			updateParams.Status = series.Status
		}
		updateParams.Language = series.Language
		updateParams.LockedFields = series.LockedFields
		updateParams.NameInitial = database.SeriesInitialFromNullTitle(updateParams.Title, series.Name)

		_, err := q.UpdateSeriesMetadata(ctx, updateParams)
		if err != nil {
			return err
		}

		recordField := func(fieldName, value string) error {
			if strings.TrimSpace(value) == "" {
				return nil
			}
			_, err := q.UpsertSeriesMetadataProvenance(ctx, database.UpsertSeriesMetadataProvenanceParams{
				SeriesID:   series.ID,
				FieldName:  fieldName,
				Value:      value,
				Source:     providerName,
				SourceUrl:  strings.TrimSpace(opts.SourceURL),
				Confidence: confidence,
				ReviewID:   reviewID,
			})
			return err
		}

		for _, fieldName := range []string{"title", "summary", "publisher", "status", "rating"} {
			if err := recordField(fieldName, appliedFields[fieldName]); err != nil {
				return err
			}
		}

		// 标签
		var tagValues []string
		if !lockedSet["tags"] {
			for _, tagName := range result.Tags {
				tagName = strings.TrimSpace(tagName)
				if tagName == "" {
					continue
				}
				if inserted, err := q.UpsertTag(ctx, tagName); err == nil {
					_ = q.LinkSeriesTag(ctx, database.LinkSeriesTagParams{SeriesID: series.ID, TagID: inserted.ID})
				}
				tagValues = append(tagValues, tagName)
			}
		}
		if len(tagValues) > 0 {
			sort.Strings(tagValues)
			if err := recordField("tags", strings.Join(tagValues, " / ")); err != nil {
				return err
			}
		}

		// 作者
		var authorEntries []string
		if !lockedSet["authors"] && len(result.Authors) > 0 {
			seen := make(map[string]struct{}, len(result.Authors))
			for _, a := range result.Authors {
				name := strings.TrimSpace(a.Name)
				role := strings.TrimSpace(a.Role)
				if name == "" {
					continue
				}
				key := strings.ToLower(name + "|" + role)
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				if inserted, err := q.UpsertAuthor(ctx, database.UpsertAuthorParams{Name: name, Role: role}); err == nil {
					_ = q.LinkSeriesAuthor(ctx, database.LinkSeriesAuthorParams{SeriesID: series.ID, AuthorID: inserted.ID})
				}
				authorEntries = append(authorEntries, metadataAuthorEntryString(name, role))
			}
		}
		if len(authorEntries) > 0 {
			sort.Strings(authorEntries)
			if err := recordField("authors", strings.Join(authorEntries, " / ")); err != nil {
				return err
			}
		}

		// 来源链接：仅 Bangumi 提供 bgm.tv 外链。providerName 可能是 key（"bangumi"）
		// 或显示名，统一用包含匹配，避免 LLM 显示名（如 "Ollama LLM"）被误判为可写外链。
		if result.SourceID > 0 && strings.Contains(strings.ToLower(providerName), "bangumi") {
			linkName := "Bangumi"
			linkURL := fmt.Sprintf("https://bgm.tv/subject/%d", result.SourceID)

			existingLinks, _ := q.GetLinksForSeries(ctx, series.ID)
			hasLink := false
			for _, l := range existingLinks {
				if l.Name == linkName {
					hasLink = true
					break
				}
			}
			if !hasLink {
				_, _ = q.LinkSeriesLink(ctx, database.LinkSeriesLinkParams{
					SeriesID: series.ID,
					Name:     linkName,
					Url:      linkURL,
				})
				if err := recordField("source_link", linkURL); err != nil {
					return err
				}
			}
		}

		if err := q.RefreshSeriesStats(ctx, series.ID); err != nil {
			return err
		}
		if afterInTx != nil {
			return afterInTx(q)
		}
		return nil
	})
}

func (c *Controller) scrapeSeriesMetadata(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}

	// 从请求体解析 provider 参数
	var reqBody struct {
		Provider string `json:"provider"`
	}
	_ = json.NewDecoder(r.Body).Decode(&reqBody)

	provider := c.getProvider(reqBody.Provider)

	series, err := c.store.GetSeries(r.Context(), seriesID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "Series not found")
		return
	}

	// 用系列的 title（若有）或 name 作为搜索关键词
	searchTitle := series.Name
	if series.Title.Valid && series.Title.String != "" {
		searchTitle = series.Title.String
	}

	result, err := provider.FetchSeriesMetadata(requestContextWithLocale(r), searchTitle)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, scrapeFailedMsg(requestLocale(r), provider.Name(), err))
		return
	}

	if result == nil {
		// outcome 是与前端约定的稳定结果码：前端据此决定提示级别并本地化文案，不再靠解析中文 message。
		// message 仍返回作为老客户端/未映射时的兜底文本。
		jsonResponse(w, http.StatusOK, map[string]interface{}{
			"scraped": false,
			"outcome": "not_found",
			"message": fmt.Sprintf("在 %s 上未找到与『%s』匹配的条目", provider.Name(), searchTitle),
		})
		return
	}

	review, fields, isNew, err := c.queueMetadataReview(r.Context(), series, result, provider.Name(), searchTitle)
	if err != nil {
		if errors.Is(err, errNoMetadataChanges) {
			jsonResponse(w, http.StatusOK, map[string]interface{}{
				"scraped": false,
				"outcome": "no_changes",
				"message": fmt.Sprintf("从 %s 找到条目，但所有数据与当前信息完全一致，无需加入待审核队列", provider.Name()),
			})
			return
		}
		jsonError(w, http.StatusInternalServerError, "Failed to save scraped metadata")
		return
	}

	if !isNew {
		jsonResponse(w, http.StatusOK, map[string]interface{}{
			"scraped": false,
			"outcome": "duplicate_ignored",
			"message": fmt.Sprintf("从 %s 找到条目，但待审核队列中已存在完全相同的记录，已为您忽略", provider.Name()),
		})
		return
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"scraped":     true,
		"outcome":     "queued",
		"provider":    provider.Name(),
		"message":     fmt.Sprintf("已将 %s 的『%s』加入审阅队列", provider.Name(), result.Title),
		"series":      series,
		"metadata":    result,
		"review_id":   review.ID,
		"field_count": len(fields),
	})
}

// 批量刮削所有系列的元数据
// scrapeSeriesEntry 是刮削任务的最小工作单元（系列 id + 用于检索的名称）。
type scrapeSeriesEntry struct {
	ID   int64
	Name string
}

// scrapeMetrics 聚合刮削任务的实时计数；toMap 生成任务进度指标，消除此前在两个刮削函数里各自
// 重复 4 次的 9 键 map 字面量。
type scrapeMetrics struct {
	total            int
	processed        int
	success          int
	notFound         int
	failed           int
	queuedReview     int
	providerRequests int
	providerErrors   int
	rateLimitedWait  time.Duration
}

func (m scrapeMetrics) toMap() map[string]int64 {
	return map[string]int64{
		"total_series":         int64(m.total),
		"processed_series":     int64(m.processed),
		"success_count":        int64(m.success),
		"not_found_count":      int64(m.notFound),
		"failed_count":         int64(m.failed),
		"queued_review_count":  int64(m.queuedReview),
		"provider_requests":    int64(m.providerRequests),
		"provider_errors":      int64(m.providerErrors),
		"rate_limited_wait_ms": m.rateLimitedWait.Milliseconds(),
	}
}

// runScrapeTask 是全库/单库两种批量刮削的共享执行体：对 entries 逐个请求 provider、写入元数据
// 审阅队列、按速率限制推进，并持续上报进度与指标。cancelMsg/donePrefix/logMsg 承载两个入口的
// 文案差异。bgCtx 必须已注入 locale；调用方负责 start/setTaskMetadata/cleanup 与 goroutine 调度。
// 此前两个函数各有一份约 150 行的近乎逐行拷贝（且日志已发生漂移），此处统一到带完整日志的版本。
func (c *Controller) runScrapeTask(bgCtx context.Context, taskKey, providerKey, providerName, cancelCode, doneCode, logMsg string, provider metadata.Provider, entries []scrapeSeriesEntry) {
	m := scrapeMetrics{total: len(entries)}
	c.updateTaskDetailsMsg(taskKey, 0, m.total, "task.msg.scrape.collecting_series", nil, "collecting_series", "", m.toMap(), nil)

	for i, entry := range entries {
		if err := taskcontrol.Wait(bgCtx); errors.Is(err, context.Canceled) {
			c.completeTaskMsg(taskKey, "cancelled", cancelCode, nil)
			return
		}
		slog.Info(logMsg, "provider", providerName, "progress", fmt.Sprintf("%d/%d", i+1, m.total), "series_name", entry.Name)

		m.providerRequests++
		m.processed = i
		c.updateTaskDetailsMsg(taskKey, i, m.total, "task.msg.scrape.requesting_provider", map[string]string{"name": entry.Name}, "requesting_provider", entry.Name, m.toMap(), map[string]string{
			"provider":            providerKey,
			"provider_name":       providerName,
			"current_series_id":   strconv.FormatInt(entry.ID, 10),
			"current_series_name": entry.Name,
		})

		result, err := provider.FetchSeriesMetadata(bgCtx, entry.Name)
		if err != nil {
			m.failed++
			m.providerErrors++
			slog.Warn("Scraping failed for series", "provider", providerName, "series_name", entry.Name, "error", err)
			continue
		}
		if result == nil {
			m.notFound++
			slog.Info("Entry not found by provider", "provider", providerName, "series_name", entry.Name)
			continue
		}

		series, err := c.store.GetSeries(bgCtx, entry.ID)
		if err != nil {
			continue
		}

		c.updateTaskDetailsMsg(taskKey, i, m.total, "task.msg.scrape.queueing_review", map[string]string{"name": entry.Name}, "queueing_review", entry.Name, m.toMap(), nil)
		if err := taskcontrol.Wait(bgCtx); errors.Is(err, context.Canceled) {
			c.completeTaskMsg(taskKey, "cancelled", cancelCode, nil)
			return
		}
		if _, _, isNew, err := c.queueMetadataReview(bgCtx, series, result, providerName, entry.Name); err == nil {
			m.success++
			if isNew {
				m.queuedReview++
				slog.Info("Queued metadata review", "provider", providerName, "series_title", result.Title)
			}
		} else if !errors.Is(err, errNoMetadataChanges) {
			m.failed++
			slog.Warn("Scraping failed for series", "provider", providerName, "series_name", entry.Name, "error", err)
		}
		m.processed = i + 1
		c.updateTaskDetailsMsg(taskKey, i+1, m.total, "task.msg.scrape.rate_limited_wait", map[string]string{"name": entry.Name}, "rate_limited_wait", entry.Name, m.toMap(), nil)

		// 速率限制
		if err := taskcontrol.Wait(bgCtx); errors.Is(err, context.Canceled) {
			c.completeTaskMsg(taskKey, "cancelled", cancelCode, nil)
			return
		}
		select {
		case <-time.After(scrapeRateLimitDelay):
			m.rateLimitedWait += scrapeRateLimitDelay
		case <-bgCtx.Done():
			c.completeTaskMsg(taskKey, "cancelled", cancelCode, nil)
			return
		}
	}

	slog.Info("Scrape task completed", "provider", providerName, "task_key", taskKey, "success_count", m.success, "total_count", m.total)
	c.finishTaskMsg(taskKey, doneCode, map[string]string{"success": strconv.Itoa(m.success), "total": strconv.Itoa(m.total)})
	c.PublishEvent("refresh")
}

func (c *Controller) launchBatchScrapeAllSeriesTask(ctx context.Context, providerKey string) error {
	provider := c.getProvider(providerKey)
	locale := metadata.LocaleFromContext(ctx)
	libs, err := c.store.ListLibraries(ctx)
	if err != nil {
		return err
	}

	var allSeries []scrapeSeriesEntry

	for _, lib := range libs {
		seriesList, err := c.store.ListSeriesByLibraryLite(ctx, lib.ID)
		if err != nil {
			continue
		}
		for _, s := range seriesList {
			name := s.Name
			if s.Title.Valid && s.Title.String != "" {
				name = s.Title.String
			}
			allSeries = append(allSeries, scrapeSeriesEntry{ID: s.ID, Name: name})
		}
	}

	if len(allSeries) == 0 {
		return nil
	}

	totalCount := len(allSeries)
	providerName := provider.Name()
	taskKey := "scrape_all_series"
	if !c.startPausableCancelableTaskMsg(taskKey, "scrape", "task.msg.scrape.all_series.start", map[string]string{"provider": providerName}, totalCount) {
		return errTaskAlreadyRunning
	}
	c.setTaskMetadata(taskKey, map[string]string{"provider": providerKey, "label.provider": providerKey, "label.provider_name": providerName}, "全库")
	taskCtx, cleanup := c.newTaskContext(taskKey)

	c.runBackground(func() {
		defer cleanup()
		c.runScrapeTask(metadata.WithLocale(taskCtx, locale), taskKey, providerKey, providerName,
			"task.msg.scrape.cancelled_all", "task.msg.scrape.complete_all", "Scraping series metadata", provider, allSeries)
	})

	return nil
}

func (c *Controller) batchScrapeAllSeries(w http.ResponseWriter, r *http.Request) {
	ctx := requestContextWithLocale(r)

	var reqBody struct {
		Provider string `json:"provider"`
	}
	_ = json.NewDecoder(r.Body).Decode(&reqBody)

	if err := c.launchBatchScrapeAllSeriesTask(ctx, reqBody.Provider); err != nil {
		if strings.Contains(err.Error(), "task already running") {
			jsonResponse(w, http.StatusConflict, map[string]string{"error": "A batch scrape task is already running"})
			return
		}
		jsonError(w, http.StatusInternalServerError, "Failed to list libraries")
		return
	}

	provider := c.getProvider(reqBody.Provider)

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"message":  fmt.Sprintf("批量刮削(%s)已异步启动，任务已加入后台队列", provider.Name()),
		"provider": provider.Name(),
	})
}

// scrapeLibrary 批量刮削指定库的缺失元数据
func (c *Controller) launchLibraryScrapeTask(ctx context.Context, libraryID int64, providerKey string) error {
	provider := c.getProvider(providerKey)
	locale := metadata.LocaleFromContext(ctx)

	seriesList, err := c.store.ListSeriesByLibraryLite(ctx, libraryID)
	if err != nil {
		return err
	}

	var allSeries []scrapeSeriesEntry

	for _, s := range seriesList {
		// 跳过已经存在基础元数据的系列，只刮取缺失的
		if (s.Summary.Valid && s.Summary.String != "") || (s.Publisher.Valid && s.Publisher.String != "") {
			continue
		}
		name := s.Name
		if s.Title.Valid && s.Title.String != "" {
			name = s.Title.String
		}
		allSeries = append(allSeries, scrapeSeriesEntry{ID: s.ID, Name: name})
	}

	if len(allSeries) == 0 {
		return nil
	}

	totalCount := len(allSeries)
	providerName := provider.Name()
	taskKey := fmt.Sprintf("scrape_library_%d", libraryID)
	if !c.startPausableCancelableTaskMsg(taskKey, "scrape", "task.msg.scrape.library.start", map[string]string{"provider": providerName}, totalCount) {
		return errTaskAlreadyRunning
	}
	scopeName := ""
	if lib, err := c.store.GetLibrary(ctx, libraryID); err == nil {
		scopeName = lib.Name
	}
	c.setTaskMetadata(taskKey, map[string]string{"provider": providerKey, "label.provider": providerKey, "label.provider_name": providerName}, scopeName)
	taskCtx, cleanup := c.newTaskContext(taskKey)

	c.runBackground(func() {
		defer cleanup()
		c.runScrapeTask(metadata.WithLocale(taskCtx, locale), taskKey, providerKey, providerName,
			"task.msg.scrape.cancelled_library", "task.msg.scrape.complete_library", "Scraping library series metadata", provider, allSeries)
	})

	return nil
}

func (c *Controller) scrapeLibrary(w http.ResponseWriter, r *http.Request) {
	ctx := requestContextWithLocale(r)
	libraryID, err := parseID(r, "libraryId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid library ID")
		return
	}

	var reqBody struct {
		Provider string `json:"provider"`
	}
	_ = json.NewDecoder(r.Body).Decode(&reqBody)

	if err := c.launchLibraryScrapeTask(ctx, libraryID, reqBody.Provider); err != nil {
		if strings.Contains(err.Error(), "task already running") {
			jsonResponse(w, http.StatusConflict, map[string]string{"error": "A library scrape task is already running"})
			return
		}
		jsonError(w, http.StatusInternalServerError, "Failed to list series in library")
		return
	}

	provider := c.getProvider(reqBody.Provider)

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"message":  fmt.Sprintf("资源库批量刮削(%s)已异步启动，任务已加入后台队列", provider.Name()),
		"provider": provider.Name(),
	})
}

func (c *Controller) retryScrapeTask(task TaskStatus) error {
	provider := ""
	if task.Params != nil {
		provider = task.Params["provider"]
	}

	switch {
	case task.Key == "scrape_all_series":
		return c.launchBatchScrapeAllSeriesTask(context.Background(), provider)
	case strings.HasPrefix(task.Key, "scrape_library_") && task.ScopeID != nil:
		return c.launchLibraryScrapeTask(context.Background(), *task.ScopeID, provider)
	default:
		return fmt.Errorf("unsupported scrape retry target")
	}
}
