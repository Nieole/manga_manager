// Package catalogsearch owns complete FTS catalogue searches and score normalization.
package catalogsearch

import (
	"context"
	"errors"
	"manga-manager/internal/database"
	"manga-manager/internal/reading"
	"strconv"
	"strings"
)

type Store interface {
	SearchGlobalSeries(context.Context, string, int32) ([]database.SeriesSearchHit, error)
	SearchGlobalBooks(context.Context, string, int32) ([]database.BookSearchHit, error)
}
type Search struct{ store Store }

func New(store Store) *Search { return &Search{store: store} }

type Result struct {
	Hits     []*Hit
	Total    uint64
	MaxScore float64
}
type Hit struct {
	ID     string
	Score  float64
	Fields map[string]interface{}
}

var ErrTarget = errors.New("invalid search target")

func (c *Search) Query(ctx context.Context, subject reading.Subject, query, target string, limit int32) (*Result, error) {
	if _, err := subject.UserID(); err != nil {
		return nil, err
	}
	var result *Result
	var err error
	switch target {
	case "series":
		result, err = c.searchSeriesWithSQLite(ctx, query, limit)
	case "book", "all", "title":
		result, err = c.searchBooksWithSQLite(ctx, query, target, limit)
	default:
		return nil, ErrTarget
	}
	if err != nil {
		return nil, err
	}
	normalizeSearchScores(result)
	return result, nil
}
func (c *Search) searchSeriesWithSQLite(ctx context.Context, query string, limit int32) (*Result, error) {
	res := &Result{}
	if err := c.mergeSeriesSearchFallback(ctx, res, query, "series", limit); err != nil {
		return nil, err
	}
	return res, nil
}

func (c *Search) searchBooksWithSQLite(ctx context.Context, query, target string, limit int32) (*Result, error) {
	res := &Result{}
	if target == "all" {
		if err := c.mergeBookHits(ctx, res, query, limit); err != nil {
			return nil, err
		}
		if err := c.mergeSeriesSearchFallback(ctx, res, query, "all", limit); err != nil {
			return nil, err
		}
		return res, nil
	}
	if err := c.mergeBookHits(ctx, res, query, limit); err != nil {
		return nil, err
	}
	return res, nil
}

func (c *Search) mergeBookHits(ctx context.Context, res *Result, query string, limit int32) error {
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
		res.Hits = append(res.Hits, &Hit{
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
func (c *Search) mergeSeriesSearchFallback(ctx context.Context, res *Result, query, target string, limit int32) error {
	if res == nil || strings.TrimSpace(query) == "" || (target != "all" && target != "series") {
		return nil
	}

	seen := make(map[string]struct{}, len(res.Hits))
	for _, hit := range res.Hits {
		seen[hit.ID] = struct{}{}
	}

	rows, err := c.store.SearchGlobalSeries(ctx, query, limit)
	if err != nil {
		return err
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
		res.Hits = append(res.Hits, &Hit{
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
	return nil
}

// normalizeSearchScores 将命中得分按本次结果的最高分缩放到 [0,1]，最佳匹配为 1.0。
func normalizeSearchScores(res *Result) {
	if res == nil || len(res.Hits) == 0 || res.MaxScore <= 0 {
		return
	}
	for _, hit := range res.Hits {
		hit.Score = hit.Score / res.MaxScore
	}
}
