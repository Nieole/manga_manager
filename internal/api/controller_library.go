package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/task"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (c *Controller) deleteLibrary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	libraryID, err := parseID(r, "libraryId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid library ID")
		return
	}

	release, ok := c.guardLibraryRead(w, libraryID)
	if !ok {
		return
	}
	defer release()

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

// cancelLibraryScopedTasks 取消挂在这个库上、仍会变化的每一条运行。
//
// 判据是**作用域**而不是任务键前缀：库级的工作种类会随功能增加（扫描、清理、刮削、AI 分组、
// 封面生成……），而一份前缀清单漏补不会有编译错误，只会表现成删库几分钟后队列里那条运行
// 自己开跑。逃出去的只剩「重建索引 / 重建缩略图」那趟逐库强扫——它挂在系统作用域上，
// 删掉一个库不该把整趟重建停掉。
//
// 取消不掉的（不可取消、进程里没有句柄）一律不阻塞删库：库行删掉之后，外键约束会挡住任何回写。
func (c *Controller) cancelLibraryScopedTasks(libraryID int64) {
	// 还没被任何一条运行认领的封面批也要丢掉：库都没了，那些封面不必再生成，
	// 而挂着的批不会有人来认领——取消运行带不走它们。
	c.discardCoverBatches(libraryID)
	cancelled, err := c.taskEngine.cancelRunsForScope(task.ScopeLibrary, libraryID)
	if err != nil {
		slog.Warn("Failed to cancel runs for deleted library", "library_id", libraryID, "error", err)
		return
	}
	if cancelled > 0 {
		slog.Info("Cancelled runs for deleted library", "library_id", libraryID, "count", cancelled)
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
