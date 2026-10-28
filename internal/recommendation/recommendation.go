// Package recommendation owns reader-partitioned recommendation computation and cache lifetime.
package recommendation

import (
	"context"
	"fmt"
	"log/slog"
	"manga-manager/internal/database"
	"manga-manager/internal/metadata"
	"manga-manager/internal/reading"
	"strconv"
)

type Item struct {
	SeriesID                 int64
	Reason, Title, CoverPath string
}
type Store interface {
	GetTopReadingTags(context.Context, int64) ([]database.GetTopReadingTagsRow, error)
	GetUserTopReadingTags(context.Context, int64, int64) ([]database.GetTopReadingTagsRow, error)
	SampleCandidateSeriesForAI(context.Context, int64, int64) ([]database.CandidateSeriesForAI, error)
}

func Key(locale string, uid int64) string { return locale + "|" + strconv.FormatInt(uid, 10) }
func (c *Service) Load(ctx context.Context, store Store, subject reading.Subject, locale string, force bool, source func() metadata.Recommender) ([]Item, error) {
	uid, err := subject.UserID()
	if err != nil {
		return nil, err
	}
	key := Key(locale, uid)
	if !force {
		if cached := c.Cached(key); cached != nil {
			return cached, nil
		}
	}
	flightCtx := metadata.WithLocale(context.WithoutCancel(ctx), locale)
	value, err := c.Do(key, func() (any, error) {
		if !force {
			if cached := c.Cached(key); cached != nil {
				return cached, nil
			}
		}
		return c.compute(flightCtx, store, source, locale, uid)
	})
	if err != nil {
		return nil, err
	}
	return value.([]Item), nil
}
func (c *Service) compute(ctx context.Context, store Store, source func() metadata.Recommender, locale string, userID int64) ([]Item, error) {
	// 先记下缓存世代：LLM 推理要几秒到几十秒，期间可能发生删库并 purge 缓存。
	// 回填时若世代已变，说明这份结果基于已不存在的数据，storeAt 会丢弃它。
	cacheGen := c.SnapshotGen()

	// 1. 获取该用户最常看的 10 个标签。多用户下必须走 user_reading_activity，
	//    否则每个人拿到的都是全站合并出来的偏好。
	var (
		tagRows []database.GetTopReadingTagsRow
		err     error
	)
	if userID > 0 {
		tagRows, err = store.GetUserTopReadingTags(ctx, userID, 10)
	} else {
		tagRows, err = store.GetTopReadingTags(ctx, 10)
	}
	var userTags []string
	if err != nil {
		return nil, err
	}
	{
		for _, tr := range tagRows {
			userTags = append(userTags, tr.Name)
		}
	}

	// 2. 随机获取 20 本可能有兴趣的候选漫画。
	//    传 userID 是必需的：「排除已读过半」的过滤要看**这个用户**的进度，
	//    读全局 books.last_read_page 在多用户下恒为 0，等于没有过滤。
	candidateRows, err := store.SampleCandidateSeriesForAI(ctx, userID, 20)
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
		return []Item{}, nil // 没有候选则不推荐，空结果不缓存
	}

	// 3. 构建 Provider
	provider := source()

	// 4. 交给 LLM 甄选并产出理
	recList, err := provider.GenerateRecommendations(ctx, userTags, candidates, 3)
	if err != nil {
		slog.Error("LLM failed to generate recommendations", "error", err)
		return nil, err
	}

	// 5. 组合最终回包数据
	var finalRecs []Item
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
		finalRecs = append(finalRecs, Item{
			SeriesID:  rec.SeriesID,
			Reason:    rec.Reason,
			Title:     title,
			CoverPath: coverPath,
		})
	}

	// 回填缓存。键必须与 getRecommendations 读取时用的完全一致，否则写进去的分区
	// 永远读不到，每次首页请求都会重新调一次 LLM。
	c.StoreAt(cacheGen, Key(locale, userID), finalRecs)

	return finalRecs, nil
}
