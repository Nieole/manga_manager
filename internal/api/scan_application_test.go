package api

import (
	"context"
	"manga-manager/internal/application/scanning"
)

// assembleScanUseCases binds application effects before opening scan inputs.
func (c *apiFixture) assembleScanUseCases() {
	c.scans = scanning.New(c.taskEngine.runs, c.store, c.scanner, c.currentConfig, scanning.Effects{
		PurgeReadingPaths: c.media.purgeReadingPathCaches,
		InvalidateStats:   c.invalidateDashboardStatsCache,
		WarmStats:         c.warmDashboardStatsCacheAsync,
		Backfill:          func(ctx context.Context, reason string) { c.maintenance.ChainBookHashBackfill(ctx, reason) },
		Publish:           c.PublishEvent,
	})
	c.families.library = &libraryHTTP{store: c.store, scans: c.scans, libraryManagement: c.libraryManagement}
	if c.runtime != nil {
		c.runtime.Scans = c.scans
	}
	if c.scanner != nil {
		c.scanner.SetBatchCallback(c.handleScannerBatchEvent)
		c.scanner.SetCoverRunLauncher(c.dispatchCoverBatch)
	}
}
