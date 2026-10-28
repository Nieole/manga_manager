package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"manga-manager/internal/application/libraries"
	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/task"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type deleteLibraryResponse struct {
	libraries.DeleteResult
	Error  string `json:"error,omitempty"`
	Code   string `json:"code,omitempty"`
	Status string `json:"status,omitempty"`
}

func (c *Controller) deleteLibrary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	libraryID, err := parseID(r, "libraryId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid library ID")
		return
	}

	result, err := c.libraryManagement.Delete(ctx, libraryID)
	if err != nil {
		status, code, message := http.StatusInternalServerError, "library_delete_failed", "Failed to delete library"
		switch {
		case errors.Is(err, task.ErrLibraryUnavailable):
			status, code, message = http.StatusConflict, "library_busy", "Library is in use"
		case errors.Is(err, sql.ErrNoRows):
			status, code, message = http.StatusNotFound, "library_not_found", "Library not found"
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			status, code, message = http.StatusRequestTimeout, "library_delete_cancelled", "Library deletion was cancelled"
		}
		jsonResponse(w, status, deleteLibraryResponse{DeleteResult: result, Error: message, Code: code})
		return
	}
	jsonResponse(w, http.StatusOK, deleteLibraryResponse{DeleteResult: result, Status: "deleted"})
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
	releaseRoot, rootErr := c.moves.GuardRoot(req.Path)
	if rootErr != nil {
		writeLibraryUnavailable(w)
		return
	}
	defer releaseRoot()
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

	// 建库后的首扫是一条正常的扫描运行，因此在任务中心里看得见、也停得下来。
	// **发起方**记手动：它是用户刚按下「添加资料库」的直接后果，用户正等着看它扫到哪了。
	// 发起是同步的（准入落库那一下），任务体在后台跑，不阻塞这次响应。
	if err := c.launchLibraryScanTask(createdLib, false, task.TriggerManual); err != nil {
		slog.WarnContext(ctx, "Initial library scan could not be started", "library_id", createdLib.ID, "error", err)
	}

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
	releaseRoot, rootErr := c.moves.GuardRoot(req.Path)
	if rootErr != nil {
		writeLibraryUnavailable(w)
		return
	}
	defer releaseRoot()
	release, ok := c.guardLibraryRead(w, libraryID)
	if !ok {
		return
	}
	defer release()

	existingLib, err := c.store.GetLibrary(ctx, libraryID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "Library not found")
		return
	}
	protection, err := c.store.LibraryRelocationProtection(ctx, libraryID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Unable to inspect relocation protection")
		return
	}
	if protection != nil && req.ScanMode != "none" {
		writeRelocationProtected(w)
		return
	}
	if !sameNativePath(existingLib.Path, req.Path) {
		var populated bool
		if err = c.store.DB().QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM series WHERE library_id=?) OR EXISTS(SELECT 1 FROM books WHERE library_id=?)`, libraryID, libraryID).Scan(&populated); err != nil {
			jsonError(w, http.StatusInternalServerError, "Unable to inspect library")
			return
		}
		if populated {
			jsonResponse(w, http.StatusConflict, map[string]string{"code": "relocation_required", "error": "Use library path relocation to preserve existing records"})
			return
		}
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

// launchLibraryScanTask adapts legacy callers to the application scan use case.
func (c *Controller) launchLibraryScanTask(lib database.Library, force bool, trigger task.Trigger) error {
	_, err := c.startLibraryScanRun(lib, force, trigger)
	return err
}

// startLibraryScanRun preserves the application launch result for the HTTP adapter.
func (c *Controller) startLibraryScanRun(lib database.Library, force bool, trigger task.Trigger) (task.Launched, error) {
	launched, err := c.scans.ScanLibrary(lib, force, trigger)
	return launched, taskLaunchError(err)
}

// scanCoalescedNoticeCode 交出这次资料库扫描发起该回给用户的那句话的 i18n **文案码**，
// 空串表示没有话要回、照常答「已发起」。
//
// 三个条件缺一不可，而**发起方**是最容易漏掉的那一个：守护扫描与监听扫描同样会被**合并**，
// 但它们没有人在等一句回话——守护 tick 什么都不做，监听器按既有的约定出口重新排期
// （见 application.Runs.AwaitWatched）。不带**强制**的手动发起也不回：并进的那条跑的正是同一件事，
// 用户要的东西已经排在那里了；只有强制那一次是「跑的和我要的不是一回事」。
func scanCoalescedNoticeCode(launched task.Launched, force bool, trigger task.Trigger) string {
	if !launched.Coalesced || !force || trigger != task.TriggerManual {
		return ""
	}
	return "library.scan.force_coalesced"
}

// scanLibrary 是手动扫描端点。它走 startLibraryScanRun 而不是那层薄壳，因为这里有人在等答案。
//
// 这个库已经排着一条扫描时，本次发起被**合并**进那一条：本次的声明与任务体一起作废，跑的是
// 先排上那份。带着**强制**来的用户因此什么都没得到——他要的重扫并没有发生，而排在前面那条
// 不是强制的。这一种回一句说明，其余照常回「已发起」。
//
// 合并的语义在这里一个字不改：不顶替排队那条的声明、不新建第二条运行、不把**强制**立成**变体**。
// 变了的只有这次发起交回给用户的那句话。
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
	launched, err := c.startLibraryScanRun(lib, isForce, task.TriggerManual)
	if err != nil {
		writeTaskLaunchError(w, err, "A library scan is already running", "Failed to start library scan")
		return
	}
	if code := scanCoalescedNoticeCode(launched, isForce, task.TriggerManual); code != "" {
		jsonResponse(w, http.StatusOK, map[string]string{
			"status":  "Scan coalesced",
			"message": apiText(requestLocale(r), code),
		})
		return
	}

	jsonResponse(w, http.StatusOK, map[string]string{"status": "Scan initiated"})
}

// launchSeriesScanTask adapts legacy series inputs to the application use case.
func (c *Controller) launchSeriesScanTask(seriesID int64, force bool, trigger task.Trigger) error {
	return taskLaunchError(c.scans.ScanSeries(seriesID, force, trigger))
}

func (c *Controller) scanSeries(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}

	forceParam := r.URL.Query().Get("force")
	isForce := forceParam == "true"
	if err := c.launchSeriesScanTask(seriesID, isForce, task.TriggerManual); err != nil {
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

// launchCleanupLibraryTask adapts legacy inputs to protected application cleanup.
func (c *Controller) launchCleanupLibraryTask(libraryID int64, trigger task.Trigger) error {
	_, err := c.startCleanupLibraryRun(libraryID, trigger)
	return err
}

// startCleanupLibraryRun exposes the shared cleanup launch result.
func (c *Controller) startCleanupLibraryRun(libraryID int64, trigger task.Trigger) (task.Launched, error) {
	launched, err := c.scans.CleanupLibrary(libraryID, trigger)
	return launched, taskLaunchError(err)
}

// runWatchedLibraryScan is the lifecycle-owned filesystem watcher scan adapter.
func (c *Controller) runWatchedLibraryScan(ctx context.Context, libraryID int64) error {
	return c.scans.WatchLibrary(ctx, libraryID)
}

func (c *Controller) runWatchedLibraryCleanup(ctx context.Context, libraryID int64) error {
	return c.scans.WatchCleanup(ctx, libraryID)
}

func (c *Controller) cleanupLibrary(w http.ResponseWriter, r *http.Request) {
	libraryID, err := parseID(r, "libraryId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid library ID")
		return
	}
	if err := c.launchCleanupLibraryTask(libraryID, task.TriggerManual); err != nil {
		writeTaskLaunchError(w, err, "A library cleanup is already running", "Failed to start library cleanup")
		return
	}

	jsonResponse(w, http.StatusOK, map[string]string{"status": "Cleanup initiated"})
}
