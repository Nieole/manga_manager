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
	"manga-manager/internal/logger"
	"manga-manager/internal/runhandle"
	"manga-manager/internal/scanner"
	"manga-manager/internal/task"
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

// cancelLibraryScopedTasks 取消挂在这个库上、仍会变化的每一条运行。
//
// 判据是**作用域**而不是任务键前缀：库级的工作种类会随功能增加（扫描、清理、刮削、AI 分组、
// 封面生成……），而一份前缀清单漏补不会有编译错误，只会表现成删库几分钟后队列里那条运行
// 自己开跑。逃出去的只剩「重建索引 / 重建缩略图」那趟逐库强扫——它挂在系统作用域上，
// 删掉一个库不该把整趟重建停掉。
//
// 取消不掉的（不可取消、进程里没有句柄）一律不阻塞删库：库行删掉之后，外键约束会挡住任何回写。
func (c *Controller) cancelLibraryScopedTasks(libraryID int64) {
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
//
// **发起方**由调用方给：同一份声明可以是用户点的、守护 tick 到的、或文件监听器叫的，
// 三者跑法完全相同，差别只在任务中心那枚徽章上。
func (c *Controller) launchLibraryScanTask(lib database.Library, force bool, trigger task.Trigger) error {
	_, err := c.startLibraryScanRun(lib, force, trigger)
	return err
}

// startLibraryScanRun 与 launchLibraryScanTask 是同一次发起，另外交回这次发起落地成了什么。
//
// 只有文件监听器需要它：它要等这次扫描跑完（见 runWatchedLibraryScan），而**合并**掉的那次
// 发起任务体根本不会执行，等在它身上就是永远等下去。
func (c *Controller) startLibraryScanRun(lib database.Library, force bool, trigger task.Trigger) (task.Launched, error) {
	cfg := c.currentConfig()
	storagePolicy := config.ResolveStoragePolicy(cfg, lib.Path)

	spec := RunSpec{
		Key:         fmt.Sprintf("scan_library_%d", lib.ID),
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
		Limits:       c.taskLimitsForPath(lib.Path),
		CompleteCode: "task.msg.scan_library.complete",
		CancelCode:   "task.msg.scan_library.cancelled",
		FailCode:     "task.msg.scan_library.failed",
	}

	return c.taskEngine.start(libraryTask("scan_library", lib.ID, variantSole), trigger, spec, func(ctx context.Context, tp *runhandle.Handle) (TaskResult, error) {
		defer c.purgeReadingPathCaches()
		// 把**运行句柄**包成**扫描观察者**一起交出去：扫描器的报文不带身份，
		// 「这次扫描的进度写到哪」由这次交出的是谁回答。
		if err := c.scanner.ScanLibrary(ctx, lib.ID, lib.Path, force, newTaskScanObserver(tp)); err != nil {
			if errors.Is(err, context.Canceled) {
				c.invalidateDashboardStatsCache("scan_library_cancelled")
				return TaskResult{Params: map[string]string{"name": lib.Name}}, err
			}
			c.invalidateDashboardStatsCache("scan_library_failed")
			return TaskResult{}, err
		}
		c.warmDashboardStatsCacheAsync("scan_library_completed")
		c.chainBookHashBackfill(ctx, "scan_library")
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
	if err := c.launchLibraryScanTask(lib, isForce, task.TriggerManual); err != nil {
		writeTaskLaunchError(w, err, "A library scan is already running", "Failed to start library scan")
		return
	}

	jsonResponse(w, http.StatusOK, map[string]string{"status": "Scan initiated"})
}

// launchSeriesScanTask 是系列扫描任务的启动点，走引擎的启动入口。
//
// 存储画像与并发上限来自系列所属的**资料库**，因此任务声明要先查两跳（系列 → 资料库）才能拼齐；
// 查不到就按「没有上限可报」落地，见下。
func (c *Controller) launchSeriesScanTask(seriesID int64, force bool, trigger task.Trigger) error {
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
			limits = c.taskLimitsForPath(lib.Path)
		}
	}

	spec := RunSpec{
		Key:         fmt.Sprintf("scan_series_%d", seriesID),
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

	return c.taskEngine.Run(seriesTask("scan_series", seriesID, variantSole), trigger, spec, func(ctx context.Context, tp *runhandle.Handle) (TaskResult, error) {
		defer c.purgeReadingPathCaches()
		if err := c.scanner.ScanSeries(ctx, seriesID, force, newTaskScanObserver(tp)); err != nil {
			if errors.Is(err, context.Canceled) {
				c.invalidateDashboardStatsCache("scan_series_cancelled")
				return TaskResult{Params: idParams}, err
			}
			slog.ErrorContext(ctx, "ScanSeries Failed", "seriesId", seriesID, "error", err)
			c.invalidateDashboardStatsCache("scan_series_failed")
			return TaskResult{}, err
		}
		c.warmDashboardStatsCacheAsync("scan_series_completed")
		c.chainBookHashBackfill(ctx, "scan_series")
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

// launchCleanupLibraryTask 清理该资料库里已失效的资源记录，走引擎的启动入口。
//
// 任务声明里没有取消文案码，因为任务体用 context.Background() 跑（理由见那里），
// 引擎裁决不出**已取消**这条分支。两者必须一起改：只换成任务体的 ctx 的话，
// scanner.CleanupLibrary 会在可中断点返回 ctx.Err()，任务落进一条没有文案的已取消态，
// 界面上停着上一句「正在清理」不动。
func (c *Controller) launchCleanupLibraryTask(libraryID int64, trigger task.Trigger) error {
	_, err := c.startCleanupLibraryRun(libraryID, trigger)
	return err
}

// startCleanupLibraryRun 与 launchCleanupLibraryTask 是同一次发起，多交回的那样与
// startLibraryScanRun 同理：文件监听器要等它跑完（Stop 的契约是派生出去的清理也已退出）。
func (c *Controller) startCleanupLibraryRun(libraryID int64, trigger task.Trigger) (task.Launched, error) {
	idParams := map[string]string{"id": strconv.FormatInt(libraryID, 10)}
	scopeName := c.libraryScopeName(libraryID)

	spec := RunSpec{
		Key:          fmt.Sprintf("cleanup_library_%d", libraryID),
		StartCode:    "task.msg.cleanup_library.start",
		StartParams:  idParams,
		Total:        1,
		ScopeName:    scopeName,
		CompleteCode: "task.msg.cleanup_library.complete",
		FailCode:     "task.msg.cleanup_library.failed",
	}

	return c.taskEngine.start(libraryTask("cleanup_library", libraryID, variantSole), trigger, spec, func(taskCtx context.Context, tp *runhandle.Handle) (TaskResult, error) {
		tp.Phase("scanning_records", "task.msg.cleanup_library.scanning_records", idParams)
		// 刻意不用任务体 ctx 的**取消**能力：本任务不可取消，而停机会取消所有任务 ctx——用了它，
		// 一次关服就会把这个没人取消过的任务写成**已取消**。改动前先读本函数的 doc。
		// 只把它携带的**任务键**转移到这条不可取消的 ctx 上，否则这个任务跑出的日志按任务键一条也过滤不到。
		cleanupCtx := logger.WithRunID(logger.WithTaskKey(context.Background(), logger.TaskKeyFrom(taskCtx)), logger.RunIDFrom(taskCtx))
		if err := c.scanner.CleanupLibrary(cleanupCtx, libraryID); err != nil {
			slog.ErrorContext(cleanupCtx, "Failed to cleanup library", "library_id", libraryID, "error", err)
			return TaskResult{}, err
		}
		return TaskResult{Params: idParams}, nil
	})
}

// runWatchedLibraryScan / runWatchedLibraryCleanup 是交给文件监听器的两个出口
// （见 scanner.WatcherHooks）：各建一条**发起方**为「监听」的运行，并等它跑完。
//
// **等**是契约的一部分**而不是**顺手为之：监听器按扫描的成败决定敢不敢接着清理——改名重连写在
// 扫描末尾，抢在它前面清理就是把用户的阅读进度、书签与合集归属一起删掉。发起完就返回的话，
// 那条清理会在扫描还排着队的时候跑起来。
func (c *Controller) runWatchedLibraryScan(ctx context.Context, libraryID int64) error {
	lib, err := c.store.GetLibrary(ctx, libraryID)
	if err != nil {
		return err
	}
	// 监听触发的是一次**增量**扫描：它要的只是「把这批新文件收进来」。
	launched, err := c.startLibraryScanRun(lib, false, task.TriggerWatch)
	return c.awaitWatchedRun(ctx, launched, err)
}

func (c *Controller) runWatchedLibraryCleanup(ctx context.Context, libraryID int64) error {
	launched, err := c.startCleanupLibraryRun(libraryID, task.TriggerWatch)
	return c.awaitWatchedRun(ctx, launched, err)
}

// awaitWatchedRun 等这次发起的运行收尾，把结果讲给监听器听。
//
// 两种「没有运行可等」各交出自己的哨兵，监听器据此都不接着清理，但后续处置不同（见那两个符号）：
// 被**合并**掉的那次任务体不会执行，而排在前面那条还没跑完；被**停发**挡下的那次连运行都没建。
func (c *Controller) awaitWatchedRun(ctx context.Context, launched task.Launched, launchErr error) error {
	if launchErr != nil {
		return launchErr
	}
	if launched.Stalled != task.StallNone {
		return scanner.ErrScanSuppressed
	}
	if launched.Coalesced {
		return scanner.ErrScanCoalesced
	}
	return c.taskEngine.awaitRunOutcome(ctx, launched.Run.ID)
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
