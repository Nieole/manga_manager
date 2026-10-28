package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"manga-manager/internal/application/libraries"
	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/task"
	"net/http"
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

type CreateLibraryRequest = libraries.CreateInput
type UpdateLibraryRequest = libraries.UpdateInput

func localizeLibraryIssues(locale string, validation *libraries.ValidationError) []config.ValidationIssue {
	issues := make([]config.ValidationIssue, 0, len(validation.Issues))
	for _, issue := range validation.Issues {
		issues = append(issues, config.ValidationIssue{Field: issue.Field, Message: apiText(locale, issue.Code), Severity: "error"})
	}
	return issues
}

func writeLibrarySaveError(w http.ResponseWriter, r *http.Request, err error) {
	var validation *libraries.ValidationError
	var busy *task.LibraryBusyError
	switch {
	case errors.As(err, &validation):
		jsonResponse(w, http.StatusUnprocessableEntity, map[string]any{"error": "Library validation failed", "validation": config.ValidationResult{Valid: false, Issues: localizeLibraryIssues(requestLocale(r), validation)}})
	case errors.Is(err, libraries.ErrRelocationRequired):
		jsonResponse(w, http.StatusConflict, map[string]string{"code": "relocation_required", "error": "Use library path relocation to preserve existing records"})
	case errors.Is(err, libraries.ErrRelocationProtected):
		writeRelocationProtected(w)
	case errors.As(err, &busy):
		w.Header().Set("Retry-After", "1")
		jsonResponse(w, http.StatusServiceUnavailable, map[string]any{"error": "Library is temporarily unavailable", "code": "library_unavailable", "conflicts": busy.Conflicts})
	case errors.Is(err, task.ErrLibraryUnavailable):
		writeLibraryUnavailable(w)
	case errors.Is(err, sql.ErrNoRows):
		jsonError(w, http.StatusNotFound, "Library not found")
	default:
		jsonError(w, http.StatusInternalServerError, "Failed to save library")
	}
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
	result, err := c.libraryManagement.Create(r.Context(), req)
	if err != nil {
		writeLibrarySaveError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusCreated, result)
}

func (c *Controller) updateLibrary(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "libraryId")
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
	result, err := c.libraryManagement.Update(r.Context(), id, req)
	if err != nil {
		writeLibrarySaveError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, result)
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
