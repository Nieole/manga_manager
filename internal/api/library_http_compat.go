package api

// Compatibility methods serve legacy constructors and isolated fixtures only.
// Production Host routes mount the bound families directly. Remove in ticket 47.
import (
	"context"
	"manga-manager/internal/database"
	"manga-manager/internal/task"
	"net/http"
)

func (c *Controller) cleanupLibrary(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().library.cleanupLibrary(w, r)
}

func (c *Controller) runWatchedLibraryCleanup(ctx context.Context, libraryID int64) error {
	return c.httpFamilies().library.runWatchedLibraryCleanup(ctx, libraryID)
}

func (c *Controller) runWatchedLibraryScan(ctx context.Context, libraryID int64) error {
	return c.httpFamilies().library.runWatchedLibraryScan(ctx, libraryID)
}

func (c *Controller) startCleanupLibraryRun(libraryID int64, trigger task.Trigger) (task.Launched, error) {
	return c.httpFamilies().library.startCleanupLibraryRun(libraryID, trigger)
}

func (c *Controller) scanSeries(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().library.scanSeries(w, r)
}

func (c *Controller) launchSeriesScanTask(seriesID int64, force bool, trigger task.Trigger) error {
	return c.httpFamilies().library.launchSeriesScanTask(seriesID, force, trigger)
}

func (c *Controller) scanLibrary(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().library.scanLibrary(w, r)
}

func (c *Controller) startLibraryScanRun(lib database.Library, force bool, trigger task.Trigger) (task.Launched, error) {
	return c.httpFamilies().library.startLibraryScanRun(lib, force, trigger)
}

func (c *Controller) launchLibraryScanTask(lib database.Library, force bool, trigger task.Trigger) error {
	return c.httpFamilies().library.launchLibraryScanTask(lib, force, trigger)
}

func (c *Controller) updateLibrary(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().library.updateLibrary(w, r)
}

func (c *Controller) createLibrary(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().library.createLibrary(w, r)
}

func (c *Controller) deleteLibrary(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().library.deleteLibrary(w, r)
}

func (c *Controller) launchRebuildIndexTask(trigger task.Trigger) error {
	return c.httpFamilies().maintenance.launchRebuildIndexTask(trigger)
}

func (c *Controller) launchRefreshKOReaderMatchingTask(trigger task.Trigger) error {
	return c.httpFamilies().maintenance.launchRefreshKOReaderMatchingTask(trigger)
}

func (c *Controller) launchReconcileKOReaderProgressTask(trigger task.Trigger) error {
	return c.httpFamilies().maintenance.launchReconcileKOReaderProgressTask(trigger)
}

func (c *Controller) launchRebuildBookHashesTask(trigger task.Trigger) error {
	return c.httpFamilies().maintenance.launchRebuildBookHashesTask(trigger)
}

func (c *Controller) chainBookHashBackfill(ctx context.Context, reason string) {
	c.httpFamilies().maintenance.chainBookHashBackfill(ctx, reason)
}

func (c *Controller) launchLowPriorityBookHashBackfillTask(reason string, trigger task.Trigger) error {
	return c.httpFamilies().maintenance.launchLowPriorityBookHashBackfillTask(reason, trigger)
}

func (c *Controller) launchRebuildFileIdentitiesTask(trigger task.Trigger) error {
	return c.httpFamilies().maintenance.launchRebuildFileIdentitiesTask(trigger)
}

func (c *Controller) launchCleanupThumbnailsTask(trigger task.Trigger) error {
	return c.httpFamilies().maintenance.launchCleanupThumbnailsTask(trigger)
}

func (c *Controller) launchRebuildThumbnailsTask(trigger task.Trigger) error {
	return c.httpFamilies().maintenance.launchRebuildThumbnailsTask(trigger)
}

func (c *Controller) rebuildIndex(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().maintenance.rebuildIndex(w, r)
}

func (c *Controller) transferToExternalLibrary(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().disk.transferToExternalLibrary(w, r)
}

func (c *Controller) getExternalLibrarySeries(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().disk.getExternalLibrarySeries(w, r)
}

func (c *Controller) getExternalLibrarySession(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().disk.getExternalLibrarySession(w, r)
}

func (c *Controller) createExternalLibrarySession(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().disk.createExternalLibrarySession(w, r)
}

func (c *Controller) writeBookComicInfo(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().disk.writeBookComicInfo(w, r)
}

func (c *Controller) exportBookComicInfo(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().disk.exportBookComicInfo(w, r)
}

func (c *Controller) exportSeriesComicInfoArchive(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().disk.exportSeriesComicInfoArchive(w, r)
}

func (c *Controller) relocationDetails(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().relocation.relocationDetails(w, r)
}

func (c *Controller) latestRelocationPlan(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().relocation.latestRelocationPlan(w, r)
}

func (c *Controller) getRelocationPlan(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().relocation.getRelocationPlan(w, r)
}

func (c *Controller) createRelocationPlan(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().relocation.createRelocationPlan(w, r)
}

func (c *Controller) getRelocationOperation(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().relocation.getRelocationOperation(w, r)
}

func (c *Controller) commitRelocationPlan(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().relocation.commitRelocationPlan(w, r)
}

func (c *Controller) relocationSourceRoots(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().relocation.relocationSourceRoots(w, r)
}

func (c *Controller) releaseRelocationProtection(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().relocation.releaseRelocationProtection(w, r)
}

func (c *Controller) createRestorationCheck(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().relocation.createRestorationCheck(w, r)
}

func (c *Controller) getRelocationRestoration(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().relocation.getRelocationRestoration(w, r)
}

func (c *Controller) recentStorageIOTaskRates() (float64, float64, int64) {
	return c.httpFamilies().storage.recentStorageIOTaskRates()
}

func (c *Controller) getStorageIODiagnostics(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().storage.getStorageIODiagnostics(w, r)
}

func (c *Controller) resumeAllTasks(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().runs.resumeAllTasks(w, r)
}

func (c *Controller) pauseAllTasks(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().runs.pauseAllTasks(w, r)
}

func (c *Controller) cancelRun(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().runs.cancelRun(w, r)
}

func (c *Controller) resumeRun(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().runs.resumeRun(w, r)
}

func (c *Controller) pauseRun(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().runs.pauseRun(w, r)
}

func (c *Controller) retryTask(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().runs.retryTask(w, r)
}

func (c *Controller) clearTasks(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().runs.clearTasks(w, r)
}

func (c *Controller) getTaskCenterLive(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().runs.getTaskCenterLive(w, r)
}

func (c *Controller) listTaskSummaries(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().runs.listTaskSummaries(w, r)
}

func (c *Controller) listTasks(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().runs.listTasks(w, r)
}

func (c *Controller) getRunEvents(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().runs.getRunEvents(w, r)
}

func (c *Controller) getRunSamples(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().runs.getRunSamples(w, r)
}

func (c *Controller) enableTask(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().runs.enableTask(w, r)
}

func (c *Controller) disableTask(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().runs.disableTask(w, r)
}

func (c *Controller) getLibraries(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().library.getLibraries(w, r)
}

func (c *Controller) scrapeLibrary(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().maintenance.scrapeLibrary(w, r)
}

func (c *Controller) batchScrapeAllSeries(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().maintenance.batchScrapeAllSeries(w, r)
}

func (c *Controller) applyKOReaderMatching(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().maintenance.applyKOReaderMatching(w, r)
}

func (c *Controller) removeBooks(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().removal.removeBooks(w, r)
}

func (c *Controller) browseDirs(w http.ResponseWriter, r *http.Request) {
	c.httpFamilies().library.browseDirs(w, r)
}
