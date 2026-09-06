package api

import (
	"log/slog"
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
	// Paused 回答的是**有没有运行被暂停**——用户面前的暂停只有这一个概念（全部暂停把每条运行
	// 逐个按下**暂停闸门**）。它与 StorageIOSchedulerState.BackgroundPaused 是两件事：
	// 那个是 `storageio` 的内部开关，按卷报，没有任何用户端点驱动它。
	Paused                     bool    `json:"paused"`
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
	// 读不到暂停态不挡整份诊断：这一个布尔值决定的只是任务中心顶部那个按钮的取向，
	// 而它旁边那几十项容量与限流的数与它无关。
	paused, err := c.taskEngine.anyRunPaused(r.Context())
	if err != nil {
		slog.Warn("Failed to count paused runs", "error", err)
	}
	response.Paused = paused
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
	latestCover := c.taskEngine.latestTaskByTypes("rebuild_thumbnails")

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
// 指标优先、任务参数兜底：上报侧今天有两条通道——跨库的重建走**累加指标**，单库扫描把整份报文
// 写成**重启入参**里的一批字符串。上一版看不出差别，因为累加值当时还被镜像成一份字符串塞回
// params；镜像随 params 堆一起没了，而把扫描那条改道是一次用户可见的搬家（数字会从参数面板
// 挪进指标面板），因此没有随接线一起做。前端的 taskMetric 早就是同一个口径：先看 metrics，再退回 params。
func taskArchiveOpenRate(task *RunStatus) float64 {
	if task == nil {
		return 0
	}
	opened := taskMetricValue(task, "opened_archives")
	if opened <= 0 {
		return 0
	}
	durationMillis := taskMetricValue(task, "duration_ms")
	if durationMillis <= 0 && !task.StartedAt.IsZero() {
		durationMillis = time.Since(task.StartedAt).Milliseconds()
	}
	if durationMillis <= 0 {
		return 0
	}
	return float64(opened) * 60000 / float64(durationMillis)
}

// taskMetricValue 取一个累计指标：先看指标那张表，再退回任务参数里那份字符串。
func taskMetricValue(task *RunStatus, key string) int64 {
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
