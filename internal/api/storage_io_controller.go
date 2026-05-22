package api

import (
	"net/http"

	"manga-manager/internal/config"
	"manga-manager/internal/storageio"
)

type StorageIODiagnosticsResponse struct {
	CacheDir       string                     `json:"cache_dir"`
	CacheVolume    string                     `json:"cache_volume"`
	Libraries      []StorageIOLibraryResponse `json:"libraries"`
	SameDiskCaches int                        `json:"same_disk_caches"`
	Scheduler      []StorageIOSchedulerState  `json:"scheduler"`
	Paused         bool                       `json:"paused"`
}

type StorageIOSchedulerState struct {
	VolumeKey         string `json:"volume_key"`
	Active            int    `json:"active"`
	Limit             int    `json:"limit"`
	ReaderActive      int    `json:"reader_active"`
	ReaderWaiting     int    `json:"reader_waiting"`
	BackgroundWaiting int    `json:"background_waiting"`
	BackgroundPaused  bool   `json:"background_paused"`
	PauseReason       string `json:"pause_reason,omitempty"`
}

type StorageIOLibraryResponse struct {
	ID                         int64                  `json:"id"`
	Name                       string                 `json:"name"`
	Path                       string                 `json:"path"`
	VolumeKey                  string                 `json:"volume_key"`
	StorageProfile             string                 `json:"storage_profile"`
	IOPolicy                   config.StorageIOPolicy `json:"io_policy"`
	CacheOnSameVolume          bool                   `json:"cache_on_same_volume"`
	DisableSameDiskPageCache   bool                   `json:"disable_same_disk_page_cache"`
	HeavyBackgroundConcurrency int                    `json:"heavy_background_concurrency"`
}

func (c *Controller) getStorageIODiagnostics(w http.ResponseWriter, r *http.Request) {
	cfg := c.currentConfig()
	response := StorageIODiagnosticsResponse{
		CacheDir:    cfg.Cache.Dir,
		CacheVolume: config.VolumeKey(cfg.Cache.Dir),
		Libraries:   []StorageIOLibraryResponse{},
		Scheduler:   []StorageIOSchedulerState{},
		Paused:      storageio.Default.BackgroundPaused(),
	}

	libraries, err := c.store.ListLibraries(r.Context())
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to inspect storage IO policies")
		return
	}

	for _, lib := range libraries {
		policy := config.ResolveStoragePolicy(cfg, lib.Path)
		cacheOnSameVolume := config.SameVolume(cfg.Cache.Dir, lib.Path)
		if cacheOnSameVolume && policy.IOPolicy.DisableSameDiskPageCache {
			response.SameDiskCaches++
		}
		response.Libraries = append(response.Libraries, StorageIOLibraryResponse{
			ID:                         lib.ID,
			Name:                       lib.Name,
			Path:                       lib.Path,
			VolumeKey:                  policy.VolumeKey,
			StorageProfile:             policy.StorageProfile,
			IOPolicy:                   policy.IOPolicy,
			CacheOnSameVolume:          cacheOnSameVolume,
			DisableSameDiskPageCache:   policy.IOPolicy.DisableSameDiskPageCache,
			HeavyBackgroundConcurrency: storageIODiagnosticsConcurrency(policy.IOPolicy),
		})
	}
	for _, snapshot := range storageio.Default.Snapshot() {
		response.Scheduler = append(response.Scheduler, StorageIOSchedulerState{
			VolumeKey:         snapshot.VolumeKey,
			Active:            snapshot.Active,
			Limit:             snapshot.Limit,
			ReaderActive:      snapshot.ReaderActive,
			ReaderWaiting:     snapshot.ReaderWaiting,
			BackgroundWaiting: snapshot.BackgroundWaiting,
			BackgroundPaused:  snapshot.BackgroundPaused,
			PauseReason:       snapshot.PauseReason,
		})
	}

	jsonResponse(w, http.StatusOK, response)
}

func (c *Controller) pauseStorageIO(w http.ResponseWriter, r *http.Request) {
	storageio.Default.PauseBackground()
	jsonResponse(w, http.StatusAccepted, map[string]string{"message": "Background storage IO paused"})
}

func (c *Controller) resumeStorageIO(w http.ResponseWriter, r *http.Request) {
	storageio.Default.ResumeBackground()
	jsonResponse(w, http.StatusAccepted, map[string]string{"message": "Background storage IO resumed"})
}

func storageIODiagnosticsConcurrency(policy config.StorageIOPolicy) int {
	limit := 0
	for _, value := range []int{policy.ArchiveOpenConcurrency, policy.CoverConcurrency, policy.HashConcurrency} {
		if value <= 0 {
			continue
		}
		if limit == 0 || value < limit {
			limit = value
		}
	}
	return limit
}
