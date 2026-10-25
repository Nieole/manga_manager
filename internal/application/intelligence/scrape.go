package intelligence

import (
	"context"
	"fmt"
	"log/slog"
	"manga-manager/internal/metadata"
	"manga-manager/internal/proposal"
	"manga-manager/internal/runhandle"
	"manga-manager/internal/task"
	"strconv"
	"time"
)

const scrapeRateLimitDelay = 500 * time.Millisecond

// scrapeSeriesEntry 是刮削任务的最小工作单元（系列 id + 用于检索的名称）。
type scrapeSeriesEntry struct {
	ID   int64
	Name string
}

// scrapeMetrics 聚合刮削任务的实时计数；toMap 转换成任务进度上报用的 map，
// 全库/单库两条刮削路径共用同一份字段集，避免两处各自维护一份 map 字面量。
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

// scrapeProviderLabels 是刮削任务在任务面板上的**刮削源**标签。整个任务期间不变，因此写进任务
// 声明、首帧就带齐；随条目变化的那两个标签由任务体逐条补上（两条路都是按键合并）。
func scrapeProviderLabels(providerKey, providerName string) map[string]string {
	return map[string]string{"provider": providerKey, "provider_name": providerName}
}

// frame 把刮削任务的一次上报翻成**一帧**：**计数推进**、**阶段**、文案与指标同属一次事件，
// 拆开报会被投递水位撕断（撕开的样子见 runhandle.Handle.Report）。
// 系列名同时是当前条目与文案占位参数；收集阶段那一帧还没有条目，两处都留空。
func (m scrapeMetrics) frame(current int, phase, code, seriesName string) runhandle.Frame {
	total := m.total
	frame := runhandle.Frame{Current: &current, Total: &total, Phase: phase, Code: code, Metrics: m.toMap()}
	if seriesName != "" {
		frame.Item = seriesName
		frame.Params = map[string]string{"name": seriesName}
	}
	return frame
}

// runScrapeTask 是全库/单库两种批量刮削的共享任务体：对 entries 逐个请求 provider、写入元数据
// 审阅队列、按速率限制推进，并经运行句柄上报每一帧。logMsg 承载两个入口的日志差异，
// 终态文案的差异则由各自的任务声明承担。ctx 必须已注入 locale。
// 两个入口必须共用这一份实现，分叉成两份各自维护会导致日志与进度上报互相漂移。
//
// 各个可中断点只把错误返回上去，由引擎裁决**终态**：取消落已取消，其余落失败。
// 启动入口交下来的 ctx 没有 deadline，**暂停闸门**也只返回 nil 或 ctx.Err()，因此今天走不到
// 失败那条；将来若给任务上下文加了超时，这条等价即失效。
func (c *Service) runScrapeTask(ctx context.Context, handle *runhandle.Handle, provider metadata.Provider, logMsg string, entries []scrapeSeriesEntry) (task.Result, error) {
	providerName := provider.Name()
	m := scrapeMetrics{total: len(entries)}
	handle.Report(m.frame(0, "collecting_series", "task.msg.scrape.collecting_series", ""))

	for i, entry := range entries {
		if err := handle.Checkpoint(ctx); err != nil {
			return task.Result{}, err
		}
		slog.InfoContext(ctx, logMsg, "provider", providerName, "progress", fmt.Sprintf("%d/%d", i+1, m.total), "series_name", entry.Name)

		m.providerRequests++
		m.processed = i
		requesting := m.frame(i, "requesting_provider", "task.msg.scrape.requesting_provider", entry.Name)
		requesting.Labels = map[string]string{
			"current_series_id":   strconv.FormatInt(entry.ID, 10),
			"current_series_name": entry.Name,
		}
		handle.Report(requesting)

		result, err := provider.FetchSeriesMetadata(ctx, entry.Name)
		if err != nil {
			m.failed++
			m.providerErrors++
			slog.WarnContext(ctx, "Scraping failed for series", "provider", providerName, "series_name", entry.Name, "error", err)
			continue
		}
		if result == nil {
			m.notFound++
			slog.InfoContext(ctx, "Entry not found by provider", "provider", providerName, "series_name", entry.Name)
			continue
		}

		series, err := c.store.GetSeries(ctx, entry.ID)
		if err != nil {
			continue
		}

		handle.Report(m.frame(i, "queueing_review", "task.msg.scrape.queueing_review", entry.Name))
		if err := handle.Checkpoint(ctx); err != nil {
			return task.Result{}, err
		}
		queued, err := c.proposals.Queue(ctx, series, result, providerName, entry.Name, proposal.QueueOptions{})
		switch {
		case err != nil:
			m.failed++
			slog.WarnContext(ctx, "Scraping failed for series", "provider", providerName, "series_name", entry.Name, "error", err)
		case queued.Status == proposal.QueueQueued:
			m.success++
			m.queuedReview++
			slog.InfoContext(ctx, "Queued metadata review", "provider", providerName, "series_title", result.Title)
		case queued.Status == proposal.QueueReusedExisting:
			m.success++
		default:
			// 「无变更」「差异全被锁」「此前已拒绝」都是正常结果，既不计成功也不计失败——
			// 计进失败会让一次完全正常的全库刮削在任务面板上报出一片红。
		}
		m.processed = i + 1
		handle.Report(m.frame(i+1, "rate_limited_wait", "task.msg.scrape.rate_limited_wait", entry.Name))

		// 速率限制
		if err := handle.Checkpoint(ctx); err != nil {
			return task.Result{}, err
		}
		select {
		case <-time.After(scrapeRateLimitDelay):
			m.rateLimitedWait += scrapeRateLimitDelay
		case <-ctx.Done():
			return task.Result{}, ctx.Err()
		}
	}

	slog.InfoContext(ctx, "Scrape task completed", "provider", providerName, "success_count", m.success, "total_count", m.total)
	c.publish()
	return task.Result{Params: map[string]string{"success": strconv.Itoa(m.success), "total": strconv.Itoa(m.total)}}, nil
}

// ScrapeAll launches all known series with system-wide access coverage.
func (c *Service) ScrapeAll(ctx context.Context, providerKey string, trigger task.Trigger) (task.Launched, error) {
	provider := c.sources.Metadata(providerKey)
	locale := metadata.LocaleFromContext(ctx)
	libs, err := c.store.ListLibraries(ctx)
	if err != nil {
		return task.Launched{}, err
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
		return task.Launched{}, nil
	}

	providerName := provider.Name()
	spec := task.RunSpec{
		Key:         "scrape_all_series",
		StartCode:   "task.msg.scrape.all_series.start",
		StartParams: map[string]string{"provider": providerName},
		Total:       len(allSeries),
		CanCancel:   true,
		CanPause:    true,
		// **重启函数**只从这里读回刮削源；换成显示名重试就会回落到默认源。
		Args:         map[string]string{"provider": providerKey, "locale": locale},
		Labels:       scrapeProviderLabels(providerKey, providerName),
		ScopeName:    "全库",
		CompleteCode: "task.msg.scrape.complete_all",
		CancelCode:   "task.msg.scrape.cancelled_all",
		FailCode:     "task.msg.scrape.failed_all",
	}

	spec.Identity = task.Identity{Type: "scrape", Scope: task.ScopeSystem, Variant: "all_libraries"}
	spec.Trigger = trigger
	spec.LibraryIDs = []int64{task.AllLibraries}
	return c.runs.Start(ctx, spec, func(taskCtx context.Context, handle *runhandle.Handle) (task.Result, error) {
		return c.runScrapeTask(metadata.WithLocale(taskCtx, locale), handle, provider, "Scraping series metadata", allSeries)
	})
}

// ScrapeLibrary launches series still missing basic metadata in one library.
func (c *Service) ScrapeLibrary(ctx context.Context, libraryID int64, providerKey string, trigger task.Trigger) (task.Launched, error) {
	provider := c.sources.Metadata(providerKey)
	locale := metadata.LocaleFromContext(ctx)

	seriesList, err := c.store.ListSeriesByLibraryLite(ctx, libraryID)
	if err != nil {
		return task.Launched{}, err
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
		return task.Launched{}, nil
	}

	providerName := provider.Name()
	scopeName := ""
	if lib, err := c.store.GetLibrary(ctx, libraryID); err == nil {
		scopeName = lib.Name
	}
	spec := task.RunSpec{
		Key:          fmt.Sprintf("scrape_library_%d", libraryID),
		StartCode:    "task.msg.scrape.library.start",
		StartParams:  map[string]string{"provider": providerName},
		Total:        len(allSeries),
		CanCancel:    true,
		CanPause:     true,
		Args:         map[string]string{"provider": providerKey, "locale": locale},
		Labels:       scrapeProviderLabels(providerKey, providerName),
		ScopeName:    scopeName,
		CompleteCode: "task.msg.scrape.complete_library",
		CancelCode:   "task.msg.scrape.cancelled_library",
		FailCode:     "task.msg.scrape.failed_library",
	}

	spec.Identity = task.Identity{Type: "scrape", Scope: task.ScopeLibrary, ScopeID: libraryID, Variant: "one_library"}
	spec.Trigger = trigger
	spec.LibraryIDs = []int64{libraryID}
	return c.runs.Start(ctx, spec, func(taskCtx context.Context, handle *runhandle.Handle) (task.Result, error) {
		return c.runScrapeTask(metadata.WithLocale(taskCtx, locale), handle, provider, "Scraping library series metadata", allSeries)
	})
}
