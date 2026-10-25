package api

import (
	"context"
	"manga-manager/internal/database"
	"manga-manager/internal/external"
)

// launchExternalLibraryScanTask bridges existing task fixtures to the application use case.
func (c *Controller) launchExternalLibraryScanTask(libraryID int64, sessionID string) error {
	_, err := c.diskJobs.ScanExternal(context.Background(), libraryID, sessionID)
	return err
}

// launchExternalLibraryTransferTask forwards an already accepted plan without replanning.
func (c *Controller) launchExternalLibraryTransferTask(libraryID int64, sessionID string, plan external.TransferPlan) error {
	_, err := c.diskJobs.TransferPrepared(context.Background(), libraryID, sessionID, plan)
	return err
}

// launchWriteSeriesComicInfoTask forwards the accepted snapshot to the application use case.
func (c *Controller) launchWriteSeriesComicInfoTask(series database.Series, books []database.Book, tags []database.Tag, authors []database.Author) error {
	_, err := c.diskJobs.WriteComicInfoPrepared(context.Background(), series, books, tags, authors)
	return err
}
