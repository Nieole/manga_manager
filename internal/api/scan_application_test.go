package api

import "manga-manager/internal/application/scanning"

// assembleScanUseCases binds application effects before opening scan inputs.
func (c *Controller) assembleScanUseCases() {
	c.scans = scanning.New(c.taskEngine.runs, c.store, c.scanner, c.currentConfig, scanning.Effects{
		PurgeReadingPaths: c.purgeReadingPathCaches,
		InvalidateStats:   c.invalidateDashboardStatsCache,
		WarmStats:         c.warmDashboardStatsCacheAsync,
		Backfill:          c.chainBookHashBackfill,
		Publish:           c.PublishEvent,
	})
	if c.runtime != nil {
		c.runtime.Scans = c.scans
	}
	if c.scanner != nil {
		c.scanner.SetBatchCallback(c.handleScannerBatchEvent)
		c.scanner.SetCoverRunLauncher(c.dispatchCoverBatch)
	}
}
