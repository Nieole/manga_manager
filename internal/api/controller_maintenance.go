package api

import (
	"manga-manager/internal/task"
	"net/http"
)

func (c *maintenanceHTTP) rebuildIndex(w http.ResponseWriter, r *http.Request) {
	if err := c.launchRebuildIndexTask(task.TriggerManual); err != nil {
		writeTaskLaunchError(w, err, "A search index rebuild is already running", "Failed to rebuild search index")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"message": apiText(requestLocale(r), "maintenance.search_index_rebuilt")})
}

func (c *maintenanceHTTP) rebuildThumbnails(w http.ResponseWriter, r *http.Request) {
	if err := c.launchRebuildThumbnailsTask(task.TriggerManual); err != nil {
		writeTaskLaunchError(w, err, "A thumbnail rebuild is already running", "Failed to start thumbnail rebuild")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"message": apiText(requestLocale(r), "maintenance.thumbnails_rebuilding")})
}

func (c *maintenanceHTTP) cleanupThumbnails(w http.ResponseWriter, r *http.Request) {
	if err := c.launchCleanupThumbnailsTask(task.TriggerManual); err != nil {
		writeTaskLaunchError(w, err, "A thumbnail cleanup is already running", "Failed to start thumbnail cleanup")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"message": apiText(requestLocale(r), "maintenance.cover_cleanup_started")})
}

func (c *maintenanceHTTP) rebuildFileIdentities(w http.ResponseWriter, r *http.Request) {
	if err := c.launchRebuildFileIdentitiesTask(task.TriggerManual); err != nil {
		writeTaskLaunchError(w, err, "A file identity rebuild is already running", "Failed to start file identity rebuild")
		return
	}
	jsonResponse(w, http.StatusAccepted, map[string]string{"message": apiText(requestLocale(r), "maintenance.file_identity_rebuild_started")})
}
func (c *maintenanceHTTP) launchRebuildThumbnailsTask(trigger task.Trigger) error {
	return taskLaunchError(c.maintenance.RebuildThumbnails(trigger))
}

func (c *maintenanceHTTP) launchCleanupThumbnailsTask(trigger task.Trigger) error {
	return taskLaunchError(c.maintenance.CleanupThumbnails(trigger))
}

func (c *maintenanceHTTP) launchRebuildFileIdentitiesTask(trigger task.Trigger) error {
	return taskLaunchError(c.maintenance.RebuildFileIdentities(trigger))
}

func (c *maintenanceHTTP) launchLowPriorityBookHashBackfillTask(reason string, trigger task.Trigger) error {
	return taskLaunchError(c.maintenance.BackfillBookHashes(reason, trigger))
}

func (c *maintenanceHTTP) launchRebuildBookHashesTask(trigger task.Trigger) error {
	return taskLaunchError(c.maintenance.RebuildBookHashes(trigger))
}

func (c *maintenanceHTTP) launchReconcileKOReaderProgressTask(trigger task.Trigger) error {
	return taskLaunchError(c.maintenance.ReconcileKOReaderProgress(trigger))
}

func (c *maintenanceHTTP) launchRefreshKOReaderMatchingTask(trigger task.Trigger) error {
	return taskLaunchError(c.maintenance.RefreshKOReaderMatching(trigger))
}

func (c *maintenanceHTTP) launchRebuildIndexTask(trigger task.Trigger) error {
	_, err := c.maintenance.RebuildIndex(trigger)
	return taskLaunchError(err)
}
