package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/metadata"
)

// getProvider 根据名称返回对应的 Provider 实例
func (c *Controller) getProvider(name string) metadata.Provider {
	switch strings.ToLower(name) {
	case "ollama", "llm", "openai":
		provider := c.config.LLM.Provider
		endpoint := c.config.LLM.Endpoint
		model := c.config.LLM.Model
		apiKey := c.config.LLM.APIKey
		return metadata.NewAIProvider(provider, endpoint, model, apiKey, c.config.LLM.Timeout)
	default:
		return metadata.NewBangumiProvider()
	}
}

// availableProviders 返回可用的 provider 列表供前端展示
func (c *Controller) listProviders(w http.ResponseWriter, r *http.Request) {
	providers := []map[string]string{
		{"id": "bangumi", "name": "Bangumi", "description": "从 Bangumi 番组计划获取漫画元数据"},
		{"id": "llm", "name": "AI/LLM 解析", "description": "通过配置的大语言模型(如 Ollama, LM Studio, OpenAI)推理生成元数据"},
	}
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

	result, err := provider.FetchSeriesMetadata(r.Context(), query)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("%s search failed: %v", provider.Name(), err))
		return
	}

	if result == nil {
		jsonResponse(w, http.StatusOK, map[string]interface{}{"found": false, "message": fmt.Sprintf("未在 %s 上找到匹配的条目", provider.Name())})
		return
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"found":     true,
		"provider":  provider.Name(),
		"title":     result.Title,
		"summary":   result.Summary,
		"publisher": result.Publisher,
		"cover_url": result.CoverURL,
		"rating":    result.Rating,
		"tags":      result.Tags,
		"source_id": result.SourceID,
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

	results, total, err := provider.SearchMetadata(r.Context(), searchTitle, limit, offset)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("%s 搜索失败: %v", provider.Name(), err))
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

	err = c.applyMetadataToSeries(r.Context(), series, &result, providerName)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to apply metadata")
		return
	}

	updated, _ := c.store.GetSeries(r.Context(), seriesID)
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"series":  updated,
	})
}

func (c *Controller) applyMetadataToSeries(ctx context.Context, series database.Series, result *metadata.SeriesMetadata, providerName string) error {
	// 解析已锁定字段
	lockedSet := make(map[string]bool)
	if series.LockedFields.Valid && series.LockedFields.String != "" {
		for _, f := range strings.Split(series.LockedFields.String, ",") {
			lockedSet[strings.TrimSpace(f)] = true
		}
	}

	return c.store.ExecTx(ctx, func(q *database.Queries) error {
		updateParams := database.UpdateSeriesMetadataParams{ID: series.ID}

		if !lockedSet["title"] && result.Title != "" {
			updateParams.Title = sql.NullString{String: result.Title, Valid: true}
		} else {
			updateParams.Title = series.Title
		}

		if !lockedSet["summary"] && result.Summary != "" {
			updateParams.Summary = sql.NullString{String: result.Summary, Valid: true}
		} else {
			updateParams.Summary = series.Summary
		}

		if !lockedSet["publisher"] && result.Publisher != "" {
			updateParams.Publisher = sql.NullString{String: result.Publisher, Valid: true}
		} else {
			updateParams.Publisher = series.Publisher
		}

		if !lockedSet["rating"] && result.Rating > 0 {
			updateParams.Rating = sql.NullFloat64{Float64: result.Rating, Valid: true}
		} else {
			updateParams.Rating = series.Rating
		}

		updateParams.Status = series.Status
		updateParams.Language = series.Language
		updateParams.LockedFields = series.LockedFields

		_, err := q.UpdateSeriesMetadata(ctx, updateParams)
		if err != nil {
			return err
		}

		// 标签
		for _, tagName := range result.Tags {
			if strings.TrimSpace(tagName) == "" {
				continue
			}
			if inserted, err := q.UpsertTag(ctx, tagName); err == nil {
				_ = q.LinkSeriesTag(ctx, database.LinkSeriesTagParams{SeriesID: series.ID, TagID: inserted.ID})
			}
		}

		// 来源链接
		if result.SourceID > 0 && providerName != "" && strings.ToLower(providerName) != "ollama" && strings.ToLower(providerName) != "llm" {
			linkName := providerName
			if strings.ToLower(providerName) == "bangumi" {
				linkName = "Bangumi"
			}
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
			}
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

	result, err := provider.FetchSeriesMetadata(r.Context(), searchTitle)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("%s 刮削失败: %v", provider.Name(), err))
		return
	}

	if result == nil {
		jsonResponse(w, http.StatusOK, map[string]interface{}{
			"scraped": false,
			"message": fmt.Sprintf("在 %s 上未找到与『%s』匹配的条目", provider.Name(), searchTitle),
		})
		return
	}

	err = c.applyMetadataToSeries(r.Context(), series, result, provider.Name())
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to save scraped metadata")
		return
	}

	updated, _ := c.store.GetSeries(r.Context(), seriesID)
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"scraped":  true,
		"provider": provider.Name(),
		"message":  fmt.Sprintf("成功从 %s 刮削了『%s』的元数据", provider.Name(), result.Title),
		"series":   updated,
		"metadata": result,
	})
}

// 批量刮削所有系列的元数据
func (c *Controller) batchScrapeAllSeries(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// 从请求体读取 provider 参数
	var reqBody struct {
		Provider string `json:"provider"`
	}
	_ = json.NewDecoder(r.Body).Decode(&reqBody)

	provider := c.getProvider(reqBody.Provider)

	libs, err := c.store.ListLibraries(ctx)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to list libraries")
		return
	}

	type seriesEntry struct {
		ID   int64
		Name string
	}
	var allSeries []seriesEntry

	for _, lib := range libs {
		seriesList, err := c.store.ListSeriesByLibrary(ctx, lib.ID)
		if err != nil {
			continue
		}
		for _, s := range seriesList {
			name := s.Name
			if s.Title.Valid && s.Title.String != "" {
				name = s.Title.String
			}
			allSeries = append(allSeries, seriesEntry{ID: s.ID, Name: name})
		}
	}

	if len(allSeries) == 0 {
		jsonResponse(w, http.StatusOK, map[string]interface{}{"message": "没有找到任何系列", "total": 0})
		return
	}

	totalCount := len(allSeries)
	providerName := provider.Name()

	go func() {
		successCount := 0

		for i, entry := range allSeries {
			slog.Info("Scraping series metadata", "provider", providerName, "progress", fmt.Sprintf("%d/%d", i+1, totalCount), "series_name", entry.Name)

			// 向前端推送进度
			c.PublishEvent(fmt.Sprintf(`task_progress:{"type":"scrape","current":%d,"total":%d,"message":"刮削: %s"}`, i+1, totalCount, entry.Name))

			result, err := provider.FetchSeriesMetadata(context.Background(), entry.Name)
			if err != nil {
				slog.Warn("Scraping failed for series", "provider", providerName, "series_name", entry.Name, "error", err)
				continue
			}
			if result == nil {
				slog.Info("Entry not found by provider", "provider", providerName, "series_name", entry.Name)
				continue
			}

			series, err := c.store.GetSeries(context.Background(), entry.ID)
			if err != nil {
				continue
			}

			err = c.applyMetadataToSeries(context.Background(), series, result, providerName)
			if err == nil {
				successCount++
				slog.Info("Successfully unified metadata", "provider", providerName, "series_title", result.Title)
			}

			// 速率限制
			time.Sleep(500 * time.Millisecond)
		}

		slog.Info("Batch scrape completed", "provider", providerName, "success_count", successCount, "total_count", totalCount)
		c.PublishEvent(fmt.Sprintf(`task_progress:{"type":"scrape","current":%d,"total":%d,"message":"刮削完成，成功 %d/%d"}`, totalCount, totalCount, successCount, totalCount))
		c.PublishEvent("refresh")
	}()

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"message":  fmt.Sprintf("批量刮削(%s)已异步启动，共 %d 个系列将逐一处理", providerName, totalCount),
		"total":    totalCount,
		"provider": providerName,
	})
}

// scrapeLibrary 批量刮削指定库的缺失元数据
func (c *Controller) scrapeLibrary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	libraryID, err := parseID(r, "libraryId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid library ID")
		return
	}

	var reqBody struct {
		Provider string `json:"provider"`
	}
	_ = json.NewDecoder(r.Body).Decode(&reqBody)
	provider := c.getProvider(reqBody.Provider)

	seriesList, err := c.store.ListSeriesByLibrary(ctx, libraryID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to list series in library")
		return
	}

	type seriesEntry struct {
		ID   int64
		Name string
	}
	var allSeries []seriesEntry

	for _, s := range seriesList {
		// 跳过已经存在基础元数据的系列，只刮取缺失的
		if (s.Summary.Valid && s.Summary.String != "") || (s.Publisher.Valid && s.Publisher.String != "") {
			continue
		}
		name := s.Name
		if s.Title.Valid && s.Title.String != "" {
			name = s.Title.String
		}
		allSeries = append(allSeries, seriesEntry{ID: s.ID, Name: name})
	}

	if len(allSeries) == 0 {
		jsonResponse(w, http.StatusOK, map[string]interface{}{"message": "没有找到需要补充元数据的系列", "total": 0})
		return
	}

	totalCount := len(allSeries)
	providerName := provider.Name()

	go func() {
		successCount := 0
		for i, entry := range allSeries {
			slog.Info("Scraping library series metadata", "provider", providerName, "progress", fmt.Sprintf("%d/%d", i+1, totalCount), "series_name", entry.Name)

			c.PublishEvent(fmt.Sprintf(`task_progress:{"type":"scrape","current":%d,"total":%d,"message":"刮削: %s"}`, i+1, totalCount, entry.Name))

			result, err := provider.FetchSeriesMetadata(context.Background(), entry.Name)
			if err != nil {
				continue
			}
			if result == nil {
				continue
			}

			series, err := c.store.GetSeries(context.Background(), entry.ID)
			if err != nil {
				continue
			}

			err = c.applyMetadataToSeries(context.Background(), series, result, providerName)
			if err == nil {
				successCount++
			}
			// 速率限制
			time.Sleep(500 * time.Millisecond)
		}

		slog.Info("Library scrape completed", "provider", providerName, "success_count", successCount, "total_count", totalCount)
		c.PublishEvent(fmt.Sprintf(`task_progress:{"type":"scrape","current":%d,"total":%d,"message":"刮削资源库完成，成功 %d/%d"}`, totalCount, totalCount, successCount, totalCount))
		c.PublishEvent("refresh")
	}()

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"message":  fmt.Sprintf("资源库批量刮削(%s)已异步启动，共 %d 个缺失元数据的系列将逐一处理", providerName, totalCount),
		"total":    totalCount,
		"provider": providerName,
	})
}
