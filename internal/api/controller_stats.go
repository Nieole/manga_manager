package api

import (
	"context"
	"log/slog"
	"time"
)

// invalidateDashboardStatsCache / invalidateVolatileStatsCache 是 statsCache 的薄委托（保留方法名以维持
// 各处失效调用点不变）。缓存的状态与加载逻辑已抽入 stats_cache.go。
func (c *Controller) invalidateDashboardStatsCache(reason string) {
	c.stats.InvalidateAll(reason)
}

func (c *Controller) invalidateVolatileStatsCache(reason string) {
	c.stats.InvalidateVolatile(reason)
}

// warmDashboardStatsCacheAsync 经受生命周期管理的后台 goroutine 预热仪表盘缓存（故留在 Controller）。
func (c *Controller) warmDashboardStatsCacheAsync(reason string) {
	c.runBackground(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := c.stats.LoadGlobal(ctx, c.store); err != nil {
			slog.Debug("Failed to warm dashboard stats cache", "reason", reason, "error", err)
		}
	})
}
