package api

import (
	"context"
	"log/slog"
	"time"
)

// Isolated application rigs share the fixture statistics resource.
func (c *apiFixture) invalidateDashboardStatsCache(reason string) {
	c.stats.InvalidateAll(reason)
}

func (c *apiFixture) warmDashboardStatsCacheAsync(reason string) {
	c.runBackground(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := c.stats.LoadGlobal(ctx, c.store); err != nil {
			slog.Debug("Failed to warm dashboard stats cache", "reason", reason, "error", err)
		}
	})
}
