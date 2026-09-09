package api

import (
	"net/http"
	"strconv"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/storageio"
)

type StorageIODiagnosticsResponse struct {
	CacheDir       string                     `json:"cache_dir"`
	CacheVolume    string                     `json:"cache_volume"`
	Libraries      []StorageIOLibraryResponse `json:"libraries"`
	SameDiskCaches int                        `json:"same_disk_caches"`
	Scheduler      []StorageIOSchedulerState  `json:"scheduler"`
	// 「有没有运行被暂停」不在这里：那是一个关于**运行**的事实，归任务中心的实况帧（RunLive.Paused）。
	// 本响应只答存储侧的事，其中 StorageIOSchedulerState.BackgroundPaused 是 `storageio`
	// 自己那个按卷的内部开关，与用户面前的暂停不是一回事。
	RecentScanArchiveOpenRate  float64 `json:"recent_scan_archive_open_rate"`
	RecentCoverArchiveOpenRate float64 `json:"recent_cover_archive_open_rate"`
	RecentThumbnailWriteMillis int64   `json:"recent_thumbnail_write_ms"`
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
	latestScan := c.taskEngine.latestTaskByTypes("scan_library", "scan_series")
	// 封面那一格读的是**封面运行**：只有它生成封面，归档打开与缩略图落盘的耗时也只记在那边。
	// 缩略图重建只跑逐库强扫，封面归它**串联**出来的那些运行。
	latestCover := c.taskEngine.latestTaskByTypes("generate_covers")

	scanRate := taskArchiveOpenRate(latestScan)
	coverRate := taskArchiveOpenRate(latestCover)
	var thumbnailWriteMillis int64
	if latestCover != nil {
		thumbnailWriteMillis = taskMetricValue(latestCover, "thumbnail_write_ms")
	}
	return scanRate, coverRate, thumbnailWriteMillis
}

// taskArchiveOpenRate 估这条运行的归档打开速率。
//
// 指标优先、任务参数兜底：两条上报通道今天都落进指标（跨库的重建走**累加指标**，单库扫描整帧
// 定版），参数那一半只为升级前落下的运行留着——它们的这批数还是**重启入参**里的字符串。
// 前端的 runMetric 是同一个口径：先看 metrics，再退回 params。
func taskArchiveOpenRate(task *RunSnapshot) float64 {
	if task == nil {
		return 0
	}
	opened := taskMetricValue(task, "opened_archives")
	if opened <= 0 {
		return 0
	}
	durationMillis := taskMetricValue(task, "duration_ms")
	// 开始时刻为 nil 就是「还没开跑」（**排队中**）：它没有可以量的那一段。
	if durationMillis <= 0 && task.StartedAt != nil {
		durationMillis = time.Since(*task.StartedAt).Milliseconds()
	}
	if durationMillis <= 0 {
		return 0
	}
	return float64(opened) * 60000 / float64(durationMillis)
}

// taskMetricValue 取一个累计指标：先看指标那张表，再退回任务参数里那份字符串。
func taskMetricValue(task *RunSnapshot, key string) int64 {
	if value, ok := task.Metrics[key]; ok {
		return value
	}
	value, _ := parseTaskInt64(task.Params[key])
	return value
}

func parseTaskInt64(raw string) (int64, error) {
	return strconv.ParseInt(raw, 10, 64)
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
