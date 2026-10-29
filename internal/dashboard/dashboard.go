// Package dashboard owns global structural caching and reader-bound dashboard views.
package dashboard

import (
	"context"
	"errors"
	"log/slog"
	"manga-manager/internal/reading"
	"sync"
	"time"

	"manga-manager/internal/database"

	"golang.org/x/sync/singleflight"
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
type Service struct {
	structuralMu    sync.RWMutex
	structuralCache *cachedStructuralStats
	structuralGen   int64

	volatileMu    sync.RWMutex
	volatileCache *cachedVolatileStats
	volatileGen   int64

	// group 合并同一类统计的并发冷加载。缺了它，缓存失效的那一瞬间（扫描刚结束、
	// 所有打开着看板的客户端同时收到 SSE refresh）每个请求都会各自跑一遍
	// GetDashboardStructuralStats——那是对 books 的全表 COUNT/SUM，几十万行的库上
	// 一次就要秒级，N 个并发请求就是 N 倍的重复扫描。
	group singleflight.Group
}

func New() *Service {
	return &Service{}
}

// invalidateAll 失效全部统计缓存（结构性 + 易变）。用于扫描/库结构变化等会改变 total_books/total_pages 的场景。
func (s *Service) InvalidateAll(reason string) {
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
func (s *Service) InvalidateVolatile(reason string) {
	s.volatileMu.Lock()
	s.volatileGen++
	s.volatileCache = nil
	s.volatileMu.Unlock()
	if reason != "" {
		slog.Debug("Invalidated volatile dashboard stats cache", "reason", reason)
	}
}

func (s *Service) loadStructural(ctx context.Context, store Store) (*database.DashboardStructuralStats, error) {
	now := time.Now()
	s.structuralMu.RLock()
	if s.structuralCache != nil && now.Before(s.structuralCache.expiresAt) {
		stats := s.structuralCache.stats
		s.structuralMu.RUnlock()
		return &stats, nil
	}
	generation := s.structuralGen
	s.structuralMu.RUnlock()

	loaded, err, _ := s.group.Do("structural", func() (any, error) {
		return store.GetDashboardStructuralStats(ctx)
	})
	if err != nil {
		return nil, err
	}
	stats, _ := loaded.(*database.DashboardStructuralStats)
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

func (s *Service) loadVolatile(ctx context.Context, store Store) (*database.DashboardVolatileStats, error) {
	now := time.Now()
	s.volatileMu.RLock()
	if s.volatileCache != nil && now.Before(s.volatileCache.expiresAt) {
		stats := s.volatileCache.stats
		s.volatileMu.RUnlock()
		return &stats, nil
	}
	generation := s.volatileGen
	s.volatileMu.RUnlock()

	loaded, err, _ := s.group.Do("volatile", func() (any, error) {
		return store.GetDashboardVolatileStats(ctx)
	})
	if err != nil {
		return nil, err
	}
	stats, _ := loaded.(*database.DashboardVolatileStats)
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

func (s *Service) LoadGlobal(ctx context.Context, store Store) (*database.DashboardStats, error) {
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

const dashboardStatsCacheTTL = 30 * time.Second

type Store interface {
	GetDashboardStructuralStats(context.Context) (*database.DashboardStructuralStats, error)
	GetDashboardVolatileStats(context.Context) (*database.DashboardVolatileStats, error)
	GetUserReadBooksCount(context.Context, int64) (int64, error)
	GetUserActiveDays(context.Context, int64, int) (int, error)
}

func (s *Service) Load(ctx context.Context, store Store, subject reading.Subject) (*database.DashboardStats, error) {
	uid, err := subject.UserID()
	if err != nil {
		return nil, err
	}
	out, err := s.LoadGlobal(ctx, store)
	if err != nil {
		return nil, err
	}
	if uid > 0 {
		n, err := store.GetUserReadBooksCount(ctx, uid)
		if err != nil {
			return nil, err
		}
		days, err := store.GetUserActiveDays(ctx, uid, 7)
		if err != nil {
			return nil, err
		}
		out.ReadBooks = int(n)
		out.ActiveDays7 = days
	}
	return out, nil
}
func cloneDashboardStats(stats *database.DashboardStats) *database.DashboardStats {
	if stats == nil {
		return nil
	}
	cloned := *stats
	if stats.LibrarySizes != nil {
		cloned.LibrarySizes = append([]database.LibrarySize(nil), stats.LibrarySizes...)
	}
	return &cloned
}
