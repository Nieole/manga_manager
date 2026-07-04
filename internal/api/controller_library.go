// 业务说明：本文件由 controller.go 拆分而来，属于后端 API 层的资料库管理子域，负责资料库增删改查、校验、扫描/系列扫描/清理任务的触发接口。

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

	err = c.store.DeleteLibrary(ctx, libraryID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to delete library")
		return
	}

	jsonResponse(w, http.StatusOK, map[string]string{"status": "deleted"})
}

type CreateLibraryRequest struct {
	Name                string `json:"name"`
	Path                string `json:"path"`
	ScanMode            string `json:"scan_mode"`
	KOReaderSyncEnabled *bool  `json:"koreader_sync_enabled"`
	ScanInterval        int64  `json:"scan_interval"`
	ScanFormats         string `json:"scan_formats"`
}

func (c *Controller) validateLibraryRequest(ctx context.Context, libraryID *int64, req CreateLibraryRequest) []config.ValidationIssue {
	issues := make([]config.ValidationIssue, 0)
	if strings.TrimSpace(req.Name) == "" {
		issues = append(issues, config.ValidationIssue{Field: "name", Message: "名称不能为空。", Severity: "error"})
	}
	if strings.TrimSpace(req.Path) == "" {
		issues = append(issues, config.ValidationIssue{Field: "path", Message: "路径不能为空。", Severity: "error"})
	} else {
		info, err := os.Stat(req.Path)
		if err != nil {
			issues = append(issues, config.ValidationIssue{Field: "path", Message: "路径不存在或不可访问。", Severity: "error"})
		} else if !info.IsDir() {
			issues = append(issues, config.ValidationIssue{Field: "path", Message: "这里只能选择目录。", Severity: "error"})
		}
	}

	if req.ScanInterval <= 0 {
		issues = append(issues, config.ValidationIssue{Field: "scan_interval", Message: "扫描间隔至少为 1 分钟。", Severity: "error"})
	}

	normalizedFormats := config.ParseScanFormats(req.ScanFormats)
	if len(normalizedFormats) == 0 {
		issues = append(issues, config.ValidationIssue{Field: "scan_formats", Message: "至少保留一个受支持的扫描格式。", Severity: "error"})
	}

	libs, err := c.store.ListLibraries(ctx)
	if err == nil {
		cleanTarget := filepath.Clean(req.Path)
		for _, lib := range libs {
			if libraryID != nil && lib.ID == *libraryID {
				continue
			}
			if filepath.Clean(lib.Path) == cleanTarget {
				issues = append(issues, config.ValidationIssue{Field: "path", Message: "这个目录已经被其他资源库使用。", Severity: "error"})
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
	if issues := c.validateLibraryRequest(ctx, nil, req); len(issues) > 0 {
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
		_ = c.watcher.WatchLibrary(createdLib.ID, createdLib.Path)
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
	if issues := c.validateLibraryRequest(ctx, &libraryID, validateReq); len(issues) > 0 {
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
			_ = c.watcher.WatchLibrary(updatedLib.ID, updatedLib.Path)
		}
	}

	jsonResponse(w, http.StatusOK, updatedLib)
}

func (c *Controller) launchLibraryScanTask(lib database.Library, force bool) bool {
	taskKey := fmt.Sprintf("scan_library_%d", lib.ID)
	if !c.startPausableCancelableTask(taskKey, "scan_library", fmt.Sprintf("开始扫描资源库: %s", lib.Name), 0) {
		return false
	}
	limits := c.taskLimitsForPath(lib.Path, force)
	storagePolicy := config.ResolveStoragePolicy(c.currentConfig(), lib.Path)
	c.setTaskMetadata(taskKey, map[string]string{
		"force":                    strconv.FormatBool(force),
		"scan_profile":             c.currentConfig().Scanner.ScanProfile,
		"storage_profile":          storagePolicy.StorageProfile,
		"volume_key":               storagePolicy.VolumeKey,
		"archive_open_concurrency": strconv.Itoa(storagePolicy.IOPolicy.ArchiveOpenConcurrency),
		"cover_concurrency":        strconv.Itoa(storagePolicy.IOPolicy.CoverConcurrency),
	}, lib.Name)
	c.setTaskEffectiveLimit(taskKey, limits)
	taskCtx, cleanupCancel := c.newTaskContext(taskKey)

	c.runBackground(func() {
		defer c.purgeReadingPathCaches()
		err := c.scanner.ScanLibrary(taskCtx, lib.ID, lib.Path, force)
		cleanupCancel()
		if errors.Is(err, context.Canceled) {
			c.invalidateDashboardStatsCache("scan_library_cancelled")
			c.completeTaskMsg(taskKey, "cancelled", "task.msg.scan_library.cancelled", map[string]string{"name": lib.Name})
			return
		}
		if err != nil {
			c.invalidateDashboardStatsCache("scan_library_failed")
			c.failTaskErrMsg(taskKey, "task.msg.scan_library.failed", nil, err.Error())
			return
		}
		c.finishTaskMsg(taskKey, "task.msg.scan_library.complete", map[string]string{"name": lib.Name})
		c.warmDashboardStatsCacheAsync("scan_library_completed")
		c.launchLowPriorityBookHashBackfillTask("scan_library")
	})

	return true
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
	if !c.launchLibraryScanTask(lib, isForce) {
		jsonResponse(w, http.StatusConflict, map[string]string{"error": "A library scan is already running"})
		return
	}

	jsonResponse(w, http.StatusOK, map[string]string{"status": "Scan initiated"})
}

func (c *Controller) launchSeriesScanTask(seriesID int64, force bool) bool {
	taskKey := fmt.Sprintf("scan_series_%d", seriesID)
	if !c.startPausableCancelableTask(taskKey, "scan_series", fmt.Sprintf("开始扫描系列 #%d", seriesID), 0) {
		return false
	}
	scopeName := ""
	if series, err := c.store.GetSeries(context.Background(), seriesID); err == nil {
		if series.Title.Valid && strings.TrimSpace(series.Title.String) != "" {
			scopeName = series.Title.String
		} else {
			scopeName = series.Name
		}
	}
	storagePolicy := config.ResolvedStoragePolicy{}
	if series, err := c.store.GetSeries(context.Background(), seriesID); err == nil {
		if lib, libErr := c.store.GetLibrary(context.Background(), series.LibraryID); libErr == nil {
			storagePolicy = config.ResolveStoragePolicy(c.currentConfig(), lib.Path)
			c.setTaskEffectiveLimit(taskKey, c.taskLimitsForPath(lib.Path, force))
		}
	}
	c.setTaskMetadata(taskKey, map[string]string{
		"force":                    strconv.FormatBool(force),
		"scan_profile":             c.currentConfig().Scanner.ScanProfile,
		"storage_profile":          storagePolicy.StorageProfile,
		"volume_key":               storagePolicy.VolumeKey,
		"archive_open_concurrency": strconv.Itoa(storagePolicy.IOPolicy.ArchiveOpenConcurrency),
		"cover_concurrency":        strconv.Itoa(storagePolicy.IOPolicy.CoverConcurrency),
	}, scopeName)
	taskCtx, cleanupCancel := c.newTaskContext(taskKey)

	c.runBackground(func() {
		defer c.purgeReadingPathCaches()
		err := c.scanner.ScanSeries(taskCtx, seriesID, force)
		cleanupCancel()
		if errors.Is(err, context.Canceled) {
			c.invalidateDashboardStatsCache("scan_series_cancelled")
			c.completeTaskMsg(taskKey, "cancelled", "task.msg.scan_series.cancelled", map[string]string{"id": strconv.FormatInt(seriesID, 10)})
			return
		}
		if err != nil {
			slog.Error("ScanSeries Failed", "seriesId", seriesID, "error", err)
			c.invalidateDashboardStatsCache("scan_series_failed")
			c.failTaskErrMsg(taskKey, "task.msg.scan_series.failed", nil, err.Error())
			return
		}
		c.finishTaskMsg(taskKey, "task.msg.scan_series.complete", map[string]string{"id": strconv.FormatInt(seriesID, 10)})
		c.warmDashboardStatsCacheAsync("scan_series_completed")
		c.launchLowPriorityBookHashBackfillTask("scan_series")
	})

	return true
}

func (c *Controller) scanSeries(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}

	forceParam := r.URL.Query().Get("force")
	isForce := forceParam == "true"
	if !c.launchSeriesScanTask(seriesID, isForce) {
		jsonResponse(w, http.StatusConflict, map[string]string{"error": "A series scan is already running"})
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

// 清理失效资源记录
func (c *Controller) launchCleanupLibraryTask(libraryID int64) bool {
	taskKey := fmt.Sprintf("cleanup_library_%d", libraryID)
	if !c.startTask(taskKey, "cleanup_library", fmt.Sprintf("开始清理资源库 #%d", libraryID), 1) {
		return false
	}
	scopeName := ""
	if lib, err := c.store.GetLibrary(context.Background(), libraryID); err == nil {
		scopeName = lib.Name
	}
	c.setTaskMetadata(taskKey, nil, scopeName)

	c.runBackground(func() {
		c.updateTaskDetailsMsg(taskKey, 0, 1, "task.msg.cleanup_library.scanning_records", map[string]string{"id": strconv.FormatInt(libraryID, 10)}, "scanning_records", "", nil, nil)
		err := c.scanner.CleanupLibrary(context.Background(), libraryID)
		if err != nil {
			slog.Error("Failed to cleanup library", "library_id", libraryID, "error", err)
			c.failTaskErrMsg(taskKey, "task.msg.cleanup_library.failed", nil, err.Error())
			return
		}
		c.finishTaskMsg(taskKey, "task.msg.cleanup_library.complete", map[string]string{"id": strconv.FormatInt(libraryID, 10)})
	})

	return true
}

func (c *Controller) cleanupLibrary(w http.ResponseWriter, r *http.Request) {
	libraryID, err := parseID(r, "libraryId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid library ID")
		return
	}
	if !c.launchCleanupLibraryTask(libraryID) {
		jsonResponse(w, http.StatusConflict, map[string]string{"error": "A library cleanup is already running"})
		return
	}

	jsonResponse(w, http.StatusOK, map[string]string{"status": "Cleanup initiated"})
}
