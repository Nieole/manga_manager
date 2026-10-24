package api

import (
	"context"
	"time"

	"manga-manager/internal/application"
	"manga-manager/internal/task"
)

const cleanupRunHistoryTaskKey = application.CleanupRunHistoryTaskKey

// startRunHistoryJanitor delegates scheduling to the application lifecycle use case.
func (c *Controller) startRunHistoryJanitor() {
	c.taskEngine.runs.RunHistoryJanitor(c.lifecycleDone(), c.currentConfig)
}

// launchCleanupRunHistoryTask is the compatibility entry for existing adapters.
func (c *Controller) launchCleanupRunHistoryTask() error {
	_, err := c.taskEngine.runs.CleanupRunHistory(context.Background(), c.currentConfig(), task.TriggerChained)
	return err
}

func retentionAge(days int) time.Duration { return application.RetentionAge(days) }
