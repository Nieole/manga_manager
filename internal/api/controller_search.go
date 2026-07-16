// 业务说明：本文件由 controller.go 拆分而来，属于后端 API 层的搜索子域，负责全库/系列/图书的 SQLite FTS 搜索、结果合并、评分归一化与封面回填。

package api

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
)

func (c *Controller) searchBooks(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	target := r.URL.Query().Get("target") // "all", "series", "book"
	if target == "" {
		target = "all"
	}

	if query == "" {
		jsonResponse(w, http.StatusOK, map[string]interface{}{"hits": []interface{}{}})
		return
	}

	if target == "series" {
		res, err := c.searchSeriesWithSQLite(r.Context(), query, 20)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "Search failed")
			return
		}
		normalizeSearchScores(res)
		jsonResponse(w, http.StatusOK, res)
		return
	}

	if target == "book" || target == "all" || target == "title" {
		res, err := c.searchBooksWithSQLite(r.Context(), query, target, 20)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "Search failed")
			return
		}
		normalizeSearchScores(res)
		jsonResponse(w, http.StatusOK, res)
		return
	}

	jsonError(w, http.StatusBadRequest, "Invalid search target")
}

func (c *Controller) searchSeriesWithSQLite(ctx context.Context, query string, limit int32) (*SearchResult, error) {
	res := &SearchResult{}
	c.mergeSeriesSearchFallback(ctx, res, query, "series", limit)
	return res, nil
}

func (c *Controller) searchBooksWithSQLite(ctx context.Context, query, target string, limit int32) (*SearchResult, error) {
	res := &SearchResult{}
	if target == "all" {
		if err := c.mergeBookSearchHits(ctx, res, query, limit); err != nil {
			return nil, err
		}
		c.mergeSeriesSearchFallback(ctx, res, query, "all", limit)
		return res, nil
	}
	if err := c.mergeBookSearchHits(ctx, res, query, limit); err != nil {
		return nil, err
	}
	return res, nil
}

func (c *Controller) mergeBookSearchHits(ctx context.Context, res *SearchResult, query string, limit int32) error {
	if res == nil || strings.TrimSpace(query) == "" {
		return nil
	}
	rows, err := c.store.SearchGlobalBooks(ctx, query, limit)
	if err != nil {
		return err
	}
	for _, hit := range rows {
		title := hit.Name
		if hit.Title.Valid && hit.Title.String != "" {
			title = hit.Title.String
		}
		seriesName := hit.SeriesName
		if hit.SeriesTitle.Valid && hit.SeriesTitle.String != "" {
			seriesName = hit.SeriesTitle.String
		}
		coverPath := ""
		if hit.CoverPath.Valid {
			coverPath = hit.CoverPath.String
		}
		score := hit.Score
		if score <= 0 {
			score = 1
		}
		docID := "b_" + strconv.FormatInt(hit.ID, 10)
		res.Hits = append(res.Hits, &SearchHit{
			ID:    docID,
			Score: score,
			Fields: map[string]interface{}{
				"id":          docID,
				"title":       title,
				"series_name": seriesName,
				"type":        "book",
				"cover_path":  coverPath,
			},
		})
		if score > res.MaxScore {
			res.MaxScore = score
		}
	}
	if uint64(len(res.Hits)) > res.Total {
		res.Total = uint64(len(res.Hits))
	}
	return nil
}

// mergeSeriesSearchFallback uses SQLite FTS5 (trigram) series search. Series metadata lives
// in SQLite, and the FTS triggers keep name/title indexed with substring semantics that match
// manga titles well (this replaced the former Bleve-based full-text engine).
func (c *Controller) mergeSeriesSearchFallback(ctx context.Context, res *SearchResult, query, target string, limit int32) {
	if res == nil || strings.TrimSpace(query) == "" || (target != "all" && target != "series") {
		return
	}

	seen := make(map[string]struct{}, len(res.Hits))
	for _, hit := range res.Hits {
		seen[hit.ID] = struct{}{}
	}

	rows, err := c.store.SearchGlobalSeries(ctx, query, limit)
	if err != nil {
		slog.Warn("mergeSeriesSearchFallback: series lookup failed", "error", err)
		return
	}

	added := 0
	for _, hit := range rows {
		row := hit.SearchSeriesPagedRow
		docID := "s_" + strconv.FormatInt(row.ID, 10)
		if _, ok := seen[docID]; ok {
			continue
		}
		title := row.Name
		if row.Title.Valid && row.Title.String != "" {
			title = row.Title.String
		}
		coverPath := ""
		if row.CoverPath.Valid {
			coverPath = row.CoverPath.String
		}
		score := hit.Score
		if score <= 0 {
			score = 1
		}
		res.Hits = append(res.Hits, &SearchHit{
			ID:    docID,
			Score: score,
			Fields: map[string]interface{}{
				"id":          docID,
				"title":       title,
				"series_name": row.Name,
				"type":        "series",
				"cover_path":  coverPath,
			},
		})
		if score > res.MaxScore {
			res.MaxScore = score
		}
		seen[docID] = struct{}{}
		added++
		if target == "series" && len(res.Hits) >= int(limit) {
			break
		}
		if target == "all" && added >= int(limit) {
			break
		}
	}

	if uint64(len(res.Hits)) > res.Total {
		res.Total = uint64(len(res.Hits))
	}
	if res.MaxScore <= 0 && len(res.Hits) > 0 {
		res.MaxScore = 1
		for _, hit := range res.Hits {
			if hit.Score <= 0 {
				hit.Score = 1
			}
		}
	}
}

// normalizeSearchScores 将命中得分按本次结果的最高分缩放到 [0,1]，最佳匹配为 1.0。
func normalizeSearchScores(res *SearchResult) {
	if res == nil || len(res.Hits) == 0 || res.MaxScore <= 0 {
		return
	}
	for _, hit := range res.Hits {
		hit.Score = hit.Score / res.MaxScore
	}
}
