package api

import (
	"manga-manager/internal/application/maintenance"
)

func (c *Controller) assembleMaintenanceUseCases() {
	c.maintenance = maintenance.New(c.taskEngine.runs, c.store, c.scans, c.koreader, c.currentConfig, maintenance.Effects{Publish: c.PublishEvent, WarmStats: c.warmDashboardStatsCacheAsync})
	if c.runtime != nil {
		c.runtime.Maintenance = c.maintenance
	}
}
