package api

import (
	"context"
	"manga-manager/internal/scanner"
	"manga-manager/internal/task"
)

// dispatchCoverBatch hands scan batches to application-owned cover runs.
func (c *Controller) dispatchCoverBatch(ctx context.Context, batch *scanner.CoverBatch) {
	c.scans.DispatchCoverBatch(ctx, batch)
}

func (c *Controller) discardCoverBatches(libraryID int64) { c.scans.DiscardCoverBatches(libraryID) }

// launchCoverRun preserves the existing restart adapter and trigger.
func (c *Controller) launchCoverRun(libraryID int64, trigger task.Trigger, backfill bool) error {
	_, err := c.startCoverRun(libraryID, trigger, backfill)
	return err
}

func (c *Controller) startCoverRun(libraryID int64, trigger task.Trigger, backfill bool) (task.Launched, error) {
	var launched task.Launched
	var err error
	if backfill {
		launched, err = c.scans.BackfillCovers(libraryID, trigger)
	} else {
		launched, err = c.scans.StartCovers(libraryID, trigger)
	}
	return launched, taskLaunchError(err)
}
