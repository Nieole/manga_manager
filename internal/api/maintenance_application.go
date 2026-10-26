package api

import (
	"context"
	"manga-manager/internal/application/maintenance"
	"manga-manager/internal/task"
)

func (c *Controller) assembleMaintenanceUseCases() {
	c.maintenance = maintenance.New(c.taskEngine.runs, c.store, c.scans, c.koreader, c.currentConfig, maintenance.Effects{Publish: c.PublishEvent, WarmStats: c.warmDashboardStatsCacheAsync})
}
func (c *Controller) restartMaintenanceRun(ctx context.Context, run RunSnapshot, trigger task.Trigger) error {
	return taskLaunchError(c.maintenance.Restart(ctx, task.Identity{Type: task.Type(run.Type), Scope: task.Scope(run.Scope), Variant: task.Variant(run.Variant)}, run.Params, trigger))
}
