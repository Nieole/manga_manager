package api

import (
	"context"

	"manga-manager/internal/task"
)

// launchCleanupRunHistoryTask submits retention work in the isolated fixture.
func (c *apiFixture) launchCleanupRunHistoryTask() error {
	_, err := c.taskEngine.runs.CleanupRunHistory(context.Background(), c.currentConfig(), task.TriggerChained)
	return err
}
