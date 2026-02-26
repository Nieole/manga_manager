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
	case "ollama", "llm":
		endpoint := c.config.Ollama.Endpoint
		model := c.config.Ollama.Model
		return metadata.NewOllamaProvider(endpoint, model)
	default:
		return metadata.NewBangumiProvider()
	}
}

// availableProviders 返回可用的 provider 列表供前端展示
func (c *Controller) listProviders(w http.ResponseWriter, r *http.Request) {
	providers := []map[string]string{
		{"id": "bangumi", "name": "Bangumi", "description": "从 Bangumi 番组计划获取漫画元数据"},
		{"id": "ollama", "name": "Ollama LLM", "description": "通过本地 Ollama 大语言模型推理生成元数据"},
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

	// 解析已锁定字段
	lockedSet := make(map[string]bool)
	if series.LockedFields.Valid && series.LockedFields.String != "" {
		for _, f := range strings.Split(series.LockedFields.String, ",") {
			lockedSet[strings.TrimSpace(f)] = true
		}
	}

	// 在事务内更新系列元数据
	err = c.store.ExecTx(r.Context(), func(q *database.Queries) error {
		updateParams := database.UpdateSeriesMetadataParams{ID: seriesID}

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

		// 保持现有的 status, language 和 locked_fields 不变
		updateParams.Status = series.Status
		updateParams.Language = series.Language
		updateParams.LockedFields = series.LockedFields

		_, err := q.UpdateSeriesMetadata(r.Context(), updateParams)
		if err != nil {
			return err
		}

		// 刮削到的标签自动追加（增量合并）
		for _, tagName := range result.Tags {
			if strings.TrimSpace(tagName) == "" {
				continue
			}
			if inserted, err := q.UpsertTag(r.Context(), tagName); err == nil {
				_ = q.LinkSeriesTag(r.Context(), database.LinkSeriesTagParams{SeriesID: seriesID, TagID: inserted.ID})
			}
		}

		// 添加来源链接（先检查是否已存在，避免重复）
		if result.SourceID > 0 {
			linkName := provider.Name()
			linkURL := ""
			if strings.ToLower(reqBody.Provider) != "ollama" && strings.ToLower(reqBody.Provider) != "llm" {
				linkURL = fmt.Sprintf("https://bgm.tv/subject/%d", result.SourceID)
			}
			if linkURL != "" {
				existingLinks, _ := q.GetLinksForSeries(r.Context(), seriesID)
				hasLink := false
				for _, l := range existingLinks {
					if l.Name == linkName {
						hasLink = true
						break
					}
				}
				if !hasLink {
					_, _ = q.LinkSeriesLink(r.Context(), database.LinkSeriesLinkParams{
						SeriesID: seriesID,
						Name:     linkName,
						Url:      linkURL,
					})
				}
			}
		}

		return nil
	})

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

			lockedSet := make(map[string]bool)
			if series.LockedFields.Valid && series.LockedFields.String != "" {
				for _, f := range strings.Split(series.LockedFields.String, ",") {
					lockedSet[strings.TrimSpace(f)] = true
				}
			}

			err = c.store.ExecTx(context.Background(), func(q *database.Queries) error {
				updateParams := database.UpdateSeriesMetadataParams{ID: entry.ID}

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

				_, err := q.UpdateSeriesMetadata(context.Background(), updateParams)
				if err != nil {
					return err
				}

				for _, tagName := range result.Tags {
					if strings.TrimSpace(tagName) == "" {
						continue
					}
					if inserted, err := q.UpsertTag(context.Background(), tagName); err == nil {
						_ = q.LinkSeriesTag(context.Background(), database.LinkSeriesTagParams{SeriesID: entry.ID, TagID: inserted.ID})
					}
				}

				// 来源链接
				if result.SourceID > 0 && strings.ToLower(reqBody.Provider) != "ollama" && strings.ToLower(reqBody.Provider) != "llm" {
					linkURL := fmt.Sprintf("https://bgm.tv/subject/%d", result.SourceID)
					existingLinks, _ := q.GetLinksForSeries(context.Background(), entry.ID)
					hasLink := false
					for _, l := range existingLinks {
						if l.Name == providerName {
							hasLink = true
							break
						}
					}
					if !hasLink {
						_, _ = q.LinkSeriesLink(context.Background(), database.LinkSeriesLinkParams{
							SeriesID: entry.ID,
							Name:     providerName,
							Url:      linkURL,
						})
					}
				}

				return nil
			})

			if err == nil {
				successCount++
				slog.Info("Successfully unified metadata", "provider", providerName, "series_title", result.Title)
			}

			// 速率限制
			time.Sleep(500 * time.Millisecond)
		}

		slog.Info("Batch scrape completed", "provider", providerName, "success_count", successCount, "total_count", totalCount)
		c.PublishEvent("scrape_complete")
	}()

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"message":  fmt.Sprintf("批量刮削(%s)已异步启动，共 %d 个系列将逐一处理", providerName, totalCount),
		"total":    totalCount,
		"provider": providerName,
	})
}
