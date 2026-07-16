// 业务说明：本文件把仪表盘统计缓存从 Controller 上帝对象里抽成独立组件。statsCache 维护「结构性统计」
// （系列/书/页总数，含 books 全表扫描，仅扫描或库结构变化时失效）与「易变统计」（近 7 日活跃、已读数，走
// 索引、随阅读进度高频变化）两套独立的 (RWMutex + 缓存 + generation) 三元组：分层是为了让高频阅读只失效
// 易变缓存、不触发昂贵的结构性全表扫描。generation 计数保证「失效期间正在进行的加载」不会把陈旧结果写回。
// Controller 仅持有 *statsCache 引用并保留薄委托方法（供各处失效调用），异步预热因依赖生命周期仍在 Controller。

package api

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"manga-manager/internal/database"
)

// cachedStructuralStats 缓存结构性统计（含 books 全表扫描），仅在扫描/库结构变化时失效。
// 阅读进度变化不会失效它，从而避免高频阅读触发 70w 行全表 COUNT/SUM。
type cachedStructuralStats struct {
	stats     database.DashboardStructuralStats
	expiresAt time.Time
}

// cachedVolatileStats 缓存随阅读进度高频变化的统计（走索引，代价低）。
type cachedVolatileStats struct {
	stats     database.DashboardVolatileStats
	expiresAt time.Time
}

// statsCache 是纯缓存层，不持有 store：加载时由调用方传入当前 store。这样 Controller 是运行时 store 的
// 唯一持有者，避免「换掉 Controller.store 但缓存仍查旧 store」的双引用隐患（也让白盒测试替换 store 生效）。
type statsCache struct {
	structuralMu    sync.RWMutex
	structuralCache *cachedStructuralStats
	structuralGen   int64

	volatileMu    sync.RWMutex
	volatileCache *cachedVolatileStats
	volatileGen   int64
}

func newStatsCache() *statsCache {
	return &statsCache{}
}

// invalidateAll 失效全部统计缓存（结构性 + 易变）。用于扫描/库结构变化等会改变 total_books/total_pages 的场景。
func (s *statsCache) invalidateAll(reason string) {
	s.structuralMu.Lock()
	s.structuralGen++
	s.structuralCache = nil
	s.structuralMu.Unlock()

	s.volatileMu.Lock()
	s.volatileGen++
	s.volatileCache = nil
	s.volatileMu.Unlock()
	if reason != "" {
		slog.Debug("Invalidated dashboard stats cache", "reason", reason)
	}
}

// invalidateVolatile 仅失效易变统计缓存（read_books/active_days）。用于阅读进度更新等高频场景——这些操作
// 不改变结构性统计，避免触发 books 全表扫描。
func (s *statsCache) invalidateVolatile(reason string) {
	s.volatileMu.Lock()
	s.volatileGen++
	s.volatileCache = nil
	s.volatileMu.Unlock()
	if reason != "" {
		slog.Debug("Invalidated volatile dashboard stats cache", "reason", reason)
	}
}

func (s *statsCache) loadStructural(ctx context.Context, store database.Store) (*database.DashboardStructuralStats, error) {
	now := time.Now()
	s.structuralMu.RLock()
	if s.structuralCache != nil && now.Before(s.structuralCache.expiresAt) {
		stats := s.structuralCache.stats
		s.structuralMu.RUnlock()
		return &stats, nil
	}
	generation := s.structuralGen
	s.structuralMu.RUnlock()

	stats, err := store.GetDashboardStructuralStats(ctx)
	if err != nil {
		return nil, err
	}
	if stats == nil {
		stats = &database.DashboardStructuralStats{}
	}
	s.structuralMu.Lock()
	if generation == s.structuralGen {
		s.structuralCache = &cachedStructuralStats{
			stats:     *stats,
			expiresAt: now.Add(dashboardStatsCacheTTL),
		}
	}
	s.structuralMu.Unlock()
	return stats, nil
}

func (s *statsCache) loadVolatile(ctx context.Context, store database.Store) (*database.DashboardVolatileStats, error) {
	now := time.Now()
	s.volatileMu.RLock()
	if s.volatileCache != nil && now.Before(s.volatileCache.expiresAt) {
		stats := s.volatileCache.stats
		s.volatileMu.RUnlock()
		return &stats, nil
	}
	generation := s.volatileGen
	s.volatileMu.RUnlock()

	stats, err := store.GetDashboardVolatileStats(ctx)
	if err != nil {
		return nil, err
	}
	if stats == nil {
		stats = &database.DashboardVolatileStats{}
	}
	s.volatileMu.Lock()
	if generation == s.volatileGen {
		s.volatileCache = &cachedVolatileStats{
			stats:     *stats,
			expiresAt: now.Add(dashboardStatsCacheTTL),
		}
	}
	s.volatileMu.Unlock()
	return stats, nil
}

func (s *statsCache) loadDashboard(ctx context.Context, store database.Store) (*database.DashboardStats, error) {
	if store == nil {
		return nil, errors.New("store is not configured")
	}

	structural, err := s.loadStructural(ctx, store)
	if err != nil {
		return nil, err
	}
	volatile, err := s.loadVolatile(ctx, store)
	if err != nil {
		return nil, err
	}

	stats := &database.DashboardStats{
		TotalSeries:  structural.TotalSeries,
		TotalBooks:   structural.TotalBooks,
		TotalPages:   structural.TotalPages,
		LibrarySizes: structural.LibrarySizes,
		ReadBooks:    volatile.ReadBooks,
		ActiveDays7:  volatile.ActiveDays7,
	}
	return cloneDashboardStats(stats), nil
}
