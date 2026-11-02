package api

import (
	"context"
	"manga-manager/internal/scanner"
	"manga-manager/internal/task"
)

// dispatchCoverBatch hands scan batches to application-owned cover runs.
func (c *apiFixture) dispatchCoverBatch(ctx context.Context, batch *scanner.CoverBatch) {
	c.scans.DispatchCoverBatch(ctx, batch)
}

// launchCoverRun preserves the existing restart adapter and trigger.
func (c *apiFixture) launchCoverRun(libraryID int64, trigger task.Trigger, backfill bool) error {
	_, err := c.startCoverRun(libraryID, trigger, backfill)
	return err
}

func (c *apiFixture) startCoverRun(libraryID int64, trigger task.Trigger, backfill bool) (task.Launched, error) {
	var launched task.Launched
	var err error
	if backfill {
		launched, err = c.scans.BackfillCovers(libraryID, trigger)
	} else {
		launched, err = c.scans.StartCovers(libraryID, trigger)
	}
	return launched, taskLaunchError(err)
}
