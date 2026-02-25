package api

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/metadata"
)

func (c *Controller) searchMetadata(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		jsonError(w, http.StatusBadRequest, "Missing query parameter 'q'")
		return
	}

	provider := metadata.NewBangumiProvider()
	result, err := provider.FetchSeriesMetadata(r.Context(), query)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("Bangumi search failed: %v", err))
		return
	}

	if result == nil {
		jsonResponse(w, http.StatusOK, map[string]interface{}{"found": false, "message": "未在 Bangumi 上找到匹配的条目"})
		return
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"found":     true,
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

	provider := metadata.NewBangumiProvider()
	result, err := provider.FetchSeriesMetadata(r.Context(), searchTitle)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("Bangumi 刮削失败: %v", err))
		return
	}

	if result == nil {
		jsonResponse(w, http.StatusOK, map[string]interface{}{
			"scraped": false,
			"message": fmt.Sprintf("在 Bangumi 上未找到与『%s』匹配的条目", searchTitle),
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
		// 准备更新参数，尊重已锁定的字段
		updateParams := database.UpdateSeriesMetadataParams{
			ID: seriesID,
		}

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

		// 刮削到的标签自动追加（不清空已有的，做增量合并）
		if len(result.Tags) > 0 {
			for _, tagName := range result.Tags {
				if strings.TrimSpace(tagName) == "" {
					continue
				}
				if inserted, err := q.UpsertTag(r.Context(), tagName); err == nil {
					_ = q.LinkSeriesTag(r.Context(), database.LinkSeriesTagParams{SeriesID: seriesID, TagID: inserted.ID})
				}
			}
		}

		// 添加 Bangumi 链接（先检查是否已存在，避免重复）
		if result.SourceID > 0 {
			existingLinks, _ := q.GetLinksForSeries(r.Context(), seriesID)
			hasBangumiLink := false
			for _, l := range existingLinks {
				if l.Name == "Bangumi" {
					hasBangumiLink = true
					break
				}
			}
			if !hasBangumiLink {
				_, _ = q.LinkSeriesLink(r.Context(), database.LinkSeriesLinkParams{
					SeriesID: seriesID,
					Name:     "Bangumi",
					Url:      fmt.Sprintf("https://bgm.tv/subject/%d", result.SourceID),
				})
			}
		}

		return nil
	})

	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to save scraped metadata")
		return
	}

	// 刮削成功后返回最新的系列信息
	updated, _ := c.store.GetSeries(r.Context(), seriesID)
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"scraped":  true,
		"message":  fmt.Sprintf("成功从 Bangumi 刮削了『%s』的元数据", result.Title),
		"series":   updated,
		"metadata": result,
	})
}

// 批量刮削所有系列的元数据
func (c *Controller) batchScrapeAllSeries(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	libs, err := c.store.ListLibraries(ctx)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to list libraries")
		return
	}

	// 收集所有系列
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

	// 在后台异步执行批量刮削
	totalCount := len(allSeries)
	go func() {
		provider := metadata.NewBangumiProvider()
		successCount := 0

		for i, entry := range allSeries {
			log.Printf("[批量刮削] (%d/%d) 正在刮削: %s", i+1, totalCount, entry.Name)

			result, err := provider.FetchSeriesMetadata(context.Background(), entry.Name)
			if err != nil {
				log.Printf("[批量刮削] 刮削『%s』失败: %v", entry.Name, err)
				continue
			}
			if result == nil {
				log.Printf("[批量刮削] 未找到『%s』的 Bangumi 条目", entry.Name)
				continue
			}

			series, err := c.store.GetSeries(context.Background(), entry.ID)
			if err != nil {
				continue
			}

			// 解析已锁定字段
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

				// 标签增量合并
				for _, tagName := range result.Tags {
					if strings.TrimSpace(tagName) == "" {
						continue
					}
					if inserted, err := q.UpsertTag(context.Background(), tagName); err == nil {
						_ = q.LinkSeriesTag(context.Background(), database.LinkSeriesTagParams{SeriesID: entry.ID, TagID: inserted.ID})
					}
				}

				// Bangumi 链接
				if result.SourceID > 0 {
					existingLinks, _ := q.GetLinksForSeries(context.Background(), entry.ID)
					hasBangumiLink := false
					for _, l := range existingLinks {
						if l.Name == "Bangumi" {
							hasBangumiLink = true
							break
						}
					}
					if !hasBangumiLink {
						_, _ = q.LinkSeriesLink(context.Background(), database.LinkSeriesLinkParams{
							SeriesID: entry.ID,
							Name:     "Bangumi",
							Url:      fmt.Sprintf("https://bgm.tv/subject/%d", result.SourceID),
						})
					}
				}

				return nil
			})

			if err == nil {
				successCount++
				log.Printf("[批量刮削] 成功刮削: %s (Bangumi ID: %d)", result.Title, result.SourceID)
			}

			// 简单的速率限制，避免频繁请求 Bangumi API
			time.Sleep(500 * time.Millisecond)
		}

		log.Printf("[批量刮削] 全部完成！成功 %d/%d", successCount, totalCount)
		c.PublishEvent("scrape_complete")
	}()

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"message": fmt.Sprintf("批量刮削任务已异步启动，共 %d 个系列将逐一处理，请关注控制台日志", totalCount),
		"total":   totalCount,
	})
}
