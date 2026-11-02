package api

import "manga-manager/internal/application/diskjobs"

// assembleDiskUseCases binds the shared run entry and external session manager.
func (c *apiFixture) assembleDiskUseCases() {
	c.diskJobs = diskjobs.New(c.taskEngine.runs, c.store, c.external, diskjobs.Effects{Publish: c.PublishEvent, PurgeReadingPaths: func() {
		if c.content != nil {
			c.media.purgeReadingPathCaches()
		}
	}})
	c.families.disk = &diskHTTP{store: c.store, diskJobs: c.diskJobs, external: c.external, content: c.content, engine: c.taskEngine.engine}
	if c.runtime != nil {
		c.runtime.Disk = c.diskJobs
	}
}
