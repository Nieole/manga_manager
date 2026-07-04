// 业务说明：本文件是业务实现，属于后端 HTTP API 层，负责把前端请求转换为数据库、扫描器、图片处理和元数据服务调用。
// 它承载资料库浏览、阅读器取页、系列维护、任务进度、系统设置和静态资源缓存等对外业务契约。
// 维护时应重点关注请求参数校验、错误语义、缓存头、并发任务状态和前后端字段兼容性。

package api

import (
	"net/http"
	"strconv"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/storageio"
)

type StorageIODiagnosticsResponse struct {
	CacheDir                   string                     `json:"cache_dir"`
	CacheVolume                string                     `json:"cache_volume"`
	Libraries                  []StorageIOLibraryResponse `json:"libraries"`
	SameDiskCaches             int                        `json:"same_disk_caches"`
	Scheduler                  []StorageIOSchedulerState  `json:"scheduler"`
	Paused                     bool                       `json:"paused"`
	RecentScanArchiveOpenRate  float64                    `json:"recent_scan_archive_open_rate"`
	RecentCoverArchiveOpenRate float64                    `json:"recent_cover_archive_open_rate"`
	RecentThumbnailWriteMillis int64                      `json:"recent_thumbnail_write_ms"`
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
	response.RecentScanArchiveOpenRate, response.RecentCoverArchiveOpenRate, response.RecentThumbnailWriteMillis = c.recentStorageIOTaskRates()

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

func (c *Controller) recentStorageIOTaskRates() (float64, float64, int64) {
	c.taskEngine.mutex.Lock()
	defer c.taskEngine.mutex.Unlock()

	var latestScan *TaskStatus
	var latestCover *TaskStatus
	for _, task := range c.taskEngine.tasks {
		switch task.Type {
		case "scan_library", "scan_series":
			if latestScan == nil || task.UpdatedAt.After(latestScan.UpdatedAt) {
				copyTask := task
				latestScan = &copyTask
			}
		case "rebuild_thumbnails":
			if latestCover == nil || task.UpdatedAt.After(latestCover.UpdatedAt) {
				copyTask := task
				latestCover = &copyTask
			}
		}
	}

	scanRate := taskArchiveOpenRate(latestScan)
	coverRate := taskArchiveOpenRate(latestCover)
	var thumbnailWriteMillis int64
	if latestCover != nil && latestCover.Params != nil {
		thumbnailWriteMillis, _ = parseTaskInt64(latestCover.Params["thumbnail_write_ms"])
	}
	return scanRate, coverRate, thumbnailWriteMillis
}

func taskArchiveOpenRate(task *TaskStatus) float64 {
	if task == nil || task.Params == nil {
		return 0
	}
	opened, _ := parseTaskInt64(task.Params["opened_archives"])
	if opened <= 0 {
		return 0
	}
	durationMillis, _ := parseTaskInt64(task.Params["duration_ms"])
	if durationMillis <= 0 && !task.StartedAt.IsZero() {
		durationMillis = time.Since(task.StartedAt).Milliseconds()
	}
	if durationMillis <= 0 {
		return 0
	}
	return float64(opened) * 60000 / float64(durationMillis)
}

func parseTaskInt64(raw string) (int64, error) {
	return strconv.ParseInt(raw, 10, 64)
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
