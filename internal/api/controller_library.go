// 本文件由 controller.go 拆分而来，属于后端 API 层的资料库管理子域，负责资料库增删改查、校验、扫描/系列扫描/清理任务的触发接口。

package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func (c *Controller) deleteLibrary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	libraryID, err := parseID(r, "libraryId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid library ID")
		return
	}

	if lib, err := c.store.GetLibrary(ctx, libraryID); err == nil && c.watcher != nil {
		c.watcher.UnwatchLibrary(lib.Path)
	}

	// 先取消该库的在跑任务再删行。取消是异步的（任务转入 cancelling，手上那批事务仍会刷完），
	// 所以这里的收益不是「彻底避免写冲突」——FK 会挡住对已删库的回写——而是缩短
	// 「扫描还在往一个马上要消失的库里写」的窗口，少一批注定失败的事务与错误日志。
	// 代价是 DeleteLibrary 万一失败，一个合法扫描已被取消；用户重新触发即可，
	// 比留下一个对着不存在的库空转的扫描要好。
	c.cancelLibraryScopedTasks(libraryID)

	err = c.store.DeleteLibrary(ctx, libraryID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to delete library")
		return
	}

	// 三份缓存都以「库里有哪些书」为前提，删库后必须一起失效，且只在删除确实成功之后做
	// ——删除失败时缓存仍然是对的，白清一次只会让下一批请求平白多打一次库。
	c.invalidateDashboardStatsCache("library_deleted")
	c.purgeReadingPathCaches()
	c.purgeRecommendationCache()

	jsonResponse(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// cancelLibraryScopedTasks 取消该库范围内、经任务引擎启动的在跑任务。
//
// 覆盖边界必须说清楚：只有这三类任务在引擎里有条目和可取消的 ctx。
// 而 interval 守护扫描、watcher 触发的扫描、以及全库重扫都是拿 context.Background()
// 直接调 scanner 的，既没有任务条目也无从取消——删库对它们无效，只能靠外键约束兜底
// （它们会空转刷一阵错误日志，但不会重建被删的行）。让扫描器自己感知库消失属于扫描器边界的改动。
func (c *Controller) cancelLibraryScopedTasks(libraryID int64) {
	for _, prefix := range []string{"scan_library_", "scrape_library_", "ai_grouping_library_"} {
		key := prefix + strconv.FormatInt(libraryID, 10)
		err := c.taskEngine.cancel(key)
		switch {
		case err == nil:
			slog.Info("Cancelled task for deleted library", "task_key", key, "library_id", libraryID)
		case errors.Is(err, errTaskNotFound), errors.Is(err, errTaskNotRunning),
			errors.Is(err, errTaskNotCancelable), errors.Is(err, errTaskCancelUnavailable):
			// 没有这个任务、已经结束、或还没登记好 runtime（启动与注册 runtime 之间有个窗口）。
			// 取消失败一律不阻塞删库：库行删掉之后，外键约束会挡住任何回写。
		default:
			slog.Warn("Failed to cancel task for deleted library", "task_key", key, "error", err)
		}
	}
}

type CreateLibraryRequest struct {
	Name                string `json:"name"`
	Path                string `json:"path"`
	ScanMode            string `json:"scan_mode"`
	KOReaderSyncEnabled *bool  `json:"koreader_sync_enabled"`
	ScanInterval        int64  `json:"scan_interval"`
	ScanFormats         string `json:"scan_formats"`
}

// validateLibraryRequest 的校验消息随 HTTP 响应结构直接下发、前端只能原样展示，故按 locale
// 在后端本地化（见 apiText）。locale 由各 handler 用 requestLocale(r) 传入。
func (c *Controller) validateLibraryRequest(ctx context.Context, locale string, libraryID *int64, req CreateLibraryRequest) []config.ValidationIssue {
	issues := make([]config.ValidationIssue, 0)
	if strings.TrimSpace(req.Name) == "" {
		issues = append(issues, config.ValidationIssue{Field: "name", Message: apiText(locale, "library.validation.name_required"), Severity: "error"})
	}
	if strings.TrimSpace(req.Path) == "" {
		issues = append(issues, config.ValidationIssue{Field: "path", Message: apiText(locale, "library.validation.path_required"), Severity: "error"})
	} else {
		info, err := os.Stat(req.Path)
		if err != nil {
			issues = append(issues, config.ValidationIssue{Field: "path", Message: apiText(locale, "library.validation.path_missing"), Severity: "error"})
		} else if !info.IsDir() {
			issues = append(issues, config.ValidationIssue{Field: "path", Message: apiText(locale, "library.validation.path_not_dir"), Severity: "error"})
		}
	}

	if req.ScanInterval <= 0 {
		issues = append(issues, config.ValidationIssue{Field: "scan_interval", Message: apiText(locale, "library.validation.interval_min"), Severity: "error"})
	}

	normalizedFormats := config.ParseScanFormats(req.ScanFormats)
	if len(normalizedFormats) == 0 {
		issues = append(issues, config.ValidationIssue{Field: "scan_formats", Message: apiText(locale, "library.validation.formats_empty"), Severity: "error"})
	}

	libs, err := c.store.ListLibraries(ctx)
	if err == nil {
		cleanTarget := filepath.Clean(req.Path)
		for _, lib := range libs {
			if libraryID != nil && lib.ID == *libraryID {
				continue
			}
			if filepath.Clean(lib.Path) == cleanTarget {
				issues = append(issues, config.ValidationIssue{Field: "path", Message: apiText(locale, "library.validation.path_in_use"), Severity: "error"})
				break
			}
		}
	}

	return issues
}

func (c *Controller) createLibrary(w http.ResponseWriter, r *http.Request) {
	var req CreateLibraryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	if req.Name == "" || req.Path == "" {
		jsonError(w, http.StatusBadRequest, "Name and Path are required")
		return
	}

	if req.ScanInterval <= 0 {
		req.ScanInterval = config.DefaultScanInterval
	}
	req.ScanFormats = config.NormalizeScanFormatsCSV(req.ScanFormats)

	ctx := r.Context()
	if issues := c.validateLibraryRequest(ctx, requestLocale(r), nil, req); len(issues) > 0 {
		jsonResponse(w, http.StatusUnprocessableEntity, map[string]interface{}{
			"error":      "Library validation failed",
			"validation": config.ValidationResult{Valid: false, Issues: issues},
		})
		return
	}
	libParams := database.CreateLibraryParams{
		Name:                req.Name,
		Path:                req.Path,
		ScanMode:            req.ScanMode,
		KoreaderSyncEnabled: req.KOReaderSyncEnabled == nil || *req.KOReaderSyncEnabled,
		ScanInterval:        req.ScanInterval,
		ScanFormats:         req.ScanFormats,
	}

	createdLib, err := c.store.CreateLibrary(ctx, libParams)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to create library")
		return
	}
	c.invalidateDashboardStatsCache("library_created")

	if createdLib.ScanMode == "watch" && c.watcher != nil {
		_ = c.watcher.WatchLibrary(createdLib.ID, createdLib.Path, createdLib.ScanFormats)
	}

	// 触发异步扫描任务，不阻塞前端 API 响应
	c.runBackground(func() {
		// 使用独立 context 避免跟随请求自动取消，创建库默认全量
		defer c.purgeReadingPathCaches()
		err := c.scanner.ScanLibrary(context.Background(), createdLib.ID, req.Path, false)
		if err != nil {
			// 在生产环境需要接入日志中心打印
			_ = err
			c.invalidateDashboardStatsCache("library_initial_scan_failed")
			return
		}
		c.warmDashboardStatsCacheAsync("library_initial_scan_completed")
	})

	jsonResponse(w, http.StatusCreated, createdLib)
}

type UpdateLibraryRequest struct {
	Name                string `json:"name"`
	Path                string `json:"path"`
	ScanMode            string `json:"scan_mode"`
	KOReaderSyncEnabled *bool  `json:"koreader_sync_enabled"`
	ScanInterval        int64  `json:"scan_interval"`
	ScanFormats         string `json:"scan_formats"`
}

func (c *Controller) updateLibrary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	libraryID, err := parseID(r, "libraryId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid library ID")
		return
	}

	var req UpdateLibraryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	if req.Name == "" || req.Path == "" {
		jsonError(w, http.StatusBadRequest, "Name and Path are required")
		return
	}
	existingLib, err := c.store.GetLibrary(ctx, libraryID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "Library not found")
		return
	}

	if req.ScanInterval <= 0 {
		req.ScanInterval = config.DefaultScanInterval
	}
	req.ScanFormats = config.NormalizeScanFormatsCSV(req.ScanFormats)
	koreaderSyncEnabled := existingLib.KoreaderSyncEnabled
	if req.KOReaderSyncEnabled != nil {
		koreaderSyncEnabled = *req.KOReaderSyncEnabled
	}

	validateReq := CreateLibraryRequest{
		Name:                req.Name,
		Path:                req.Path,
		ScanMode:            req.ScanMode,
		KOReaderSyncEnabled: &koreaderSyncEnabled,
		ScanInterval:        req.ScanInterval,
		ScanFormats:         req.ScanFormats,
	}
	if issues := c.validateLibraryRequest(ctx, requestLocale(r), &libraryID, validateReq); len(issues) > 0 {
		jsonResponse(w, http.StatusUnprocessableEntity, map[string]interface{}{
			"error":      "Library validation failed",
			"validation": config.ValidationResult{Valid: false, Issues: issues},
		})
		return
	}

	libParams := database.UpdateLibraryParams{
		ID:                  libraryID,
		Name:                req.Name,
		Path:                req.Path,
		ScanMode:            req.ScanMode,
		KoreaderSyncEnabled: koreaderSyncEnabled,
		ScanInterval:        req.ScanInterval,
		ScanFormats:         req.ScanFormats,
	}

	updatedLib, err := c.store.UpdateLibrary(ctx, libParams)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to update library")
		return
	}
	c.invalidateDashboardStatsCache("library_updated")

	if c.watcher != nil {
		c.watcher.UnwatchLibrary(existingLib.Path)
		if updatedLib.ScanMode == "watch" {
			_ = c.watcher.WatchLibrary(updatedLib.ID, updatedLib.Path, updatedLib.ScanFormats)
		}
	}

	jsonResponse(w, http.StatusOK, updatedLib)
}

// launchLibraryScanTask 是资料库扫描任务的启动点，走引擎的启动入口。
//
// 启动仪式（槽位闸门、元数据、并发上限、可取消可暂停的上下文、后台 goroutine、三条终态分支、
// panic 兜底）全部由引擎承担；这里只剩两样东西：一份任务声明，和一个任务体。
// 任务体里保留的仍是**领域**动作——缓存失效、预热、串联后台哈希回填——这些不该由引擎代劳。
func (c *Controller) launchLibraryScanTask(lib database.Library, force bool) error {
	cfg := c.currentConfig()
	storagePolicy := config.ResolveStoragePolicy(cfg, lib.Path)

	spec := TaskSpec{
		Key:         fmt.Sprintf("scan_library_%d", lib.ID),
		Type:        "scan_library",
		StartCode:   "task.msg.scan_library.start",
		StartParams: map[string]string{"name": lib.Name},
		CanCancel:   true,
		CanPause:    true,
		ScopeName:   lib.Name,
		Metadata: map[string]string{
			"force":                    strconv.FormatBool(force),
			"scan_profile":             cfg.Scanner.ScanProfile,
			"storage_profile":          storagePolicy.StorageProfile,
			"volume_key":               storagePolicy.VolumeKey,
			"archive_open_concurrency": strconv.Itoa(storagePolicy.IOPolicy.ArchiveOpenConcurrency),
			"cover_concurrency":        strconv.Itoa(storagePolicy.IOPolicy.CoverConcurrency),
		},
		Limits:       c.taskLimitsForPath(lib.Path, force),
		CompleteCode: "task.msg.scan_library.complete",
		CancelCode:   "task.msg.scan_library.cancelled",
		FailCode:     "task.msg.scan_library.failed",
	}

	return c.taskEngine.Run(spec, func(ctx context.Context, tp *TaskProgress) (TaskResult, error) {
		// 扫描器回调不在任务体的调用栈上，把句柄登记出去它才有地方报进度（见 scanProgressHandles）。
		releaseProgress := c.scanProgress.track(libraryScanTarget(lib.ID), tp)
		defer releaseProgress()
		defer c.purgeReadingPathCaches()
		if err := c.scanner.ScanLibrary(ctx, lib.ID, lib.Path, force); err != nil {
			if errors.Is(err, context.Canceled) {
				c.invalidateDashboardStatsCache("scan_library_cancelled")
				return TaskResult{Params: map[string]string{"name": lib.Name}}, err
			}
			c.invalidateDashboardStatsCache("scan_library_failed")
			return TaskResult{}, err
		}
		c.warmDashboardStatsCacheAsync("scan_library_completed")
		c.chainBookHashBackfill("scan_library")
		return TaskResult{Params: map[string]string{"name": lib.Name}}, nil
	})
}

func (c *Controller) scanLibrary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	libID, err := parseID(r, "libraryId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid library ID")
		return
	}

	lib, err := c.store.GetLibrary(ctx, libID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "Library not found")
		return
	}

	forceParam := r.URL.Query().Get("force")
	isForce := forceParam == "true"
	if err := c.launchLibraryScanTask(lib, isForce); err != nil {
		writeTaskLaunchError(w, err, "A library scan is already running", "Failed to start library scan")
		return
	}

	jsonResponse(w, http.StatusOK, map[string]string{"status": "Scan initiated"})
}

// launchSeriesScanTask 是系列扫描任务的启动点，走引擎的启动入口。
//
// 存储画像与并发上限来自系列所属的**资料库**，因此任务声明要先查两跳（系列 → 资料库）才能拼齐；
// 查不到就按「没有上限可报」落地，见下。
func (c *Controller) launchSeriesScanTask(seriesID int64, force bool) error {
	idParams := map[string]string{"id": strconv.FormatInt(seriesID, 10)}
	scopeName := ""
	storagePolicy := config.ResolvedStoragePolicy{}
	// 并发上限只在真找到了所属资料库时才有意义：查不到就让任务声明里的 Limits 留零值，
	// 引擎不会为它凭空造一份全零的上限（那会在任务面板上显示成「0 并发」的假数据）。
	limits := TaskLimits{}
	if series, err := c.store.GetSeries(context.Background(), seriesID); err == nil {
		scopeName = series.Name
		if series.Title.Valid && strings.TrimSpace(series.Title.String) != "" {
			scopeName = series.Title.String
		}
		if lib, libErr := c.store.GetLibrary(context.Background(), series.LibraryID); libErr == nil {
			storagePolicy = config.ResolveStoragePolicy(c.currentConfig(), lib.Path)
			limits = c.taskLimitsForPath(lib.Path, force)
		}
	}

	spec := TaskSpec{
		Key:         fmt.Sprintf("scan_series_%d", seriesID),
		Type:        "scan_series",
		StartCode:   "task.msg.scan_series.start",
		StartParams: idParams,
		CanCancel:   true,
		CanPause:    true,
		ScopeName:   scopeName,
		Metadata: map[string]string{
			"force":                    strconv.FormatBool(force),
			"scan_profile":             c.currentConfig().Scanner.ScanProfile,
			"storage_profile":          storagePolicy.StorageProfile,
			"volume_key":               storagePolicy.VolumeKey,
			"archive_open_concurrency": strconv.Itoa(storagePolicy.IOPolicy.ArchiveOpenConcurrency),
			"cover_concurrency":        strconv.Itoa(storagePolicy.IOPolicy.CoverConcurrency),
		},
		Limits:       limits,
		CompleteCode: "task.msg.scan_series.complete",
		CancelCode:   "task.msg.scan_series.cancelled",
		FailCode:     "task.msg.scan_series.failed",
	}

	return c.taskEngine.Run(spec, func(ctx context.Context, tp *TaskProgress) (TaskResult, error) {
		releaseProgress := c.scanProgress.track(seriesScanTarget(seriesID), tp)
		defer releaseProgress()
		defer c.purgeReadingPathCaches()
		if err := c.scanner.ScanSeries(ctx, seriesID, force); err != nil {
			if errors.Is(err, context.Canceled) {
				c.invalidateDashboardStatsCache("scan_series_cancelled")
				return TaskResult{Params: idParams}, err
			}
			slog.Error("ScanSeries Failed", "seriesId", seriesID, "error", err)
			c.invalidateDashboardStatsCache("scan_series_failed")
			return TaskResult{}, err
		}
		c.warmDashboardStatsCacheAsync("scan_series_completed")
		c.chainBookHashBackfill("scan_series")
		return TaskResult{Params: idParams}, nil
	})
}

func (c *Controller) scanSeries(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}

	forceParam := r.URL.Query().Get("force")
	isForce := forceParam == "true"
	if err := c.launchSeriesScanTask(seriesID, isForce); err != nil {
		writeTaskLaunchError(w, err, "A series scan is already running", "Failed to start series scan")
		return
	}

	jsonResponse(w, http.StatusOK, map[string]string{"status": "Scan initiated"})
}

func (c *Controller) getSeriesByLibrary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	libID, err := parseID(r, "libraryId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid library ID")
		return
	}

	series, err := c.store.ListSeriesByLibrary(ctx, libID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to fetch series")
		return
	}

	if series == nil {
		series = []database.ListSeriesByLibraryRow{}
	}
	jsonResponse(w, http.StatusOK, series)
}

// launchCleanupLibraryTask 清理该资料库里已失效的资源记录，走引擎的启动入口。
//
// 任务声明里没有取消文案码，因为任务体用 context.Background() 跑（理由见那里），
// 引擎裁决不出**已取消**这条分支。两者必须一起改：只换成任务体的 ctx 的话，
// scanner.CleanupLibrary 会在可中断点返回 ctx.Err()，任务落进一条没有文案的已取消态，
// 界面上停着上一句「正在清理」不动。
func (c *Controller) launchCleanupLibraryTask(libraryID int64) error {
	idParams := map[string]string{"id": strconv.FormatInt(libraryID, 10)}
	scopeName := c.libraryScopeName(libraryID)

	spec := TaskSpec{
		Key:          fmt.Sprintf("cleanup_library_%d", libraryID),
		Type:         "cleanup_library",
		StartCode:    "task.msg.cleanup_library.start",
		StartParams:  idParams,
		Total:        1,
		ScopeName:    scopeName,
		CompleteCode: "task.msg.cleanup_library.complete",
		FailCode:     "task.msg.cleanup_library.failed",
	}

	return c.taskEngine.Run(spec, func(_ context.Context, tp *TaskProgress) (TaskResult, error) {
		tp.Phase("scanning_records", "task.msg.cleanup_library.scanning_records", idParams)
		// 刻意不用任务体的 ctx：本任务不可取消，而停机会取消所有任务 ctx——用了它，
		// 一次关服就会把这个没人取消过的任务写成**已取消**。改动前先读本函数的 doc。
		if err := c.scanner.CleanupLibrary(context.Background(), libraryID); err != nil {
			slog.Error("Failed to cleanup library", "library_id", libraryID, "error", err)
			return TaskResult{}, err
		}
		return TaskResult{Params: idParams}, nil
	})
}

func (c *Controller) cleanupLibrary(w http.ResponseWriter, r *http.Request) {
	libraryID, err := parseID(r, "libraryId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid library ID")
		return
	}
	if err := c.launchCleanupLibraryTask(libraryID); err != nil {
		writeTaskLaunchError(w, err, "A library cleanup is already running", "Failed to start library cleanup")
		return
	}

	jsonResponse(w, http.StatusOK, map[string]string{"status": "Cleanup initiated"})
}
