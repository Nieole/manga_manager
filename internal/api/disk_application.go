package api

import "manga-manager/internal/application/diskjobs"

// assembleDiskUseCases binds the shared run entry and external session manager.
func (c *Controller) assembleDiskUseCases() {
	c.diskJobs = diskjobs.New(c.taskEngine.runs, c.store, c.external, diskjobs.Effects{Publish: c.PublishEvent})
}
