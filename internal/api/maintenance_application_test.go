package api

import (
	"manga-manager/internal/metadata"

	"manga-manager/internal/application/maintenance"
)

func (c *apiFixture) assembleMaintenanceUseCases() {
	c.maintenance = maintenance.New(c.taskEngine.runs, c.store, c.scans, c.koreader, c.currentConfig, maintenance.Effects{Publish: c.PublishEvent, WarmStats: c.warmDashboardStatsCacheAsync})
	c.families.maintenance = &maintenanceHTTP{initials: c.store, maintenance: c.maintenance, intelligence: c.intelligence, getMetadataFetcher: func(name string) metadata.Fetcher { return metadata.NewConfiguredProvider(c.currentConfig(), name) }}
	if c.runtime != nil {
		c.runtime.Maintenance = c.maintenance
	}
}
