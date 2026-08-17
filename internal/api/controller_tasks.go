// 本文件是任务子域的 HTTP 层与装配层：任务列表/清理/重试/暂停/恢复/取消六个端点、
// 任务重试注册表（taskType -> 重启函数），以及任务面板上报的指标参数与有效并发数推导。
//
// 引擎自身的可变状态与状态机在 task_engine.go，TaskStatus 的纯转换函数在 task_model.go；
// 本文件只经 c.taskEngine 的方法操作任务表，出现 taskEngine.mutex 即说明状态逻辑漏到了这里。

package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"strconv"
	"strings"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/metadata"
	"manga-manager/internal/scanner"

	"github.com/go-chi/chi/v5"
)

// taskRelauncher 用原任务的 scope/params 重新发起一个同类型任务。返回 errTaskAlreadyRunning 表示
// 同类任务已在运行（映射为 409），返回其它错误视为内部错误（映射为 500）。
type taskRelauncher func(ctx context.Context, task TaskStatus) error

// errTaskAlreadyRunning 是重试时"同类任务已在运行"的哨兵错误。
var errTaskAlreadyRunning = errors.New("task already running")

// writeTaskLaunchError 把启动入口的错误翻成 HTTP 响应：只有「同类任务已在运行」是 409。
//
// 启动入口今天只会返回这一个哨兵错误，但签名已经放开成 error，把任何错误都翻成 409 会让
// 将来某个真正的内部错误伪装成「已在运行」，用户等一个永远不会出现的任务。
// 判定口径与 retryTask 一致；conflict 与 failure 是两条分支各自的英文提示。
func writeTaskLaunchError(w http.ResponseWriter, err error, conflict, failure string) {
	if errors.Is(err, errTaskAlreadyRunning) {
		jsonResponse(w, http.StatusConflict, map[string]string{"error": conflict})
		return
	}
	jsonError(w, http.StatusInternalServerError, failure)
}

// libraryScopeName 取资料库在界面上的显示名，取不到返回空串。
//
// 读不到库名不是启动失败：任务声明的其余部分不依赖它，而 claimTaskSlot 对空串本就无操作。
// 为一次读库失败挡下整个任务，用户失去的是任务本身，换来的只是一个标签。
func (c *Controller) libraryScopeName(libraryID int64) string {
	lib, err := c.store.GetLibrary(context.Background(), libraryID)
	if err != nil {
		return ""
	}
	return lib.Name
}

// buildTaskRelaunchers 注册各任务类型 -> 重启函数，是重试分发与"可重试类型"的唯一事实来源。
func (c *Controller) buildTaskRelaunchers() map[string]taskRelauncher {
	libraryID := func(task TaskStatus) (int64, error) {
		if task.ScopeID == nil {
			return 0, fmt.Errorf("task %q missing library id", task.Key)
		}
		return *task.ScopeID, nil
	}
	forceParam := func(task TaskStatus) bool {
		return task.Params != nil && task.Params["force"] == "true"
	}
	return map[string]taskRelauncher{
		"scan_library": func(ctx context.Context, task TaskStatus) error {
			id, err := libraryID(task)
			if err != nil {
				return err
			}
			lib, err := c.store.GetLibrary(ctx, id)
			if err != nil {
				return err
			}
			// 启动入口本就返回「同类任务已在运行」哨兵错误，重启函数原样透传即可，
			// 不必再把一个布尔值转换回哨兵错误。
			return c.launchLibraryScanTask(lib, forceParam(task))
		},
		"scan_series": func(ctx context.Context, task TaskStatus) error {
			if task.ScopeID == nil {
				return fmt.Errorf("task %q missing series id", task.Key)
			}
			return c.launchSeriesScanTask(*task.ScopeID, forceParam(task))
		},
		"cleanup_library": func(ctx context.Context, task TaskStatus) error {
			id, err := libraryID(task)
			if err != nil {
				return err
			}
			return c.launchCleanupLibraryTask(id)
		},
		"rebuild_index": func(ctx context.Context, _ TaskStatus) error {
			return c.launchRebuildIndexTask()
		},
		"rebuild_thumbnails": func(ctx context.Context, _ TaskStatus) error {
			return c.launchRebuildThumbnailsTask()
		},
		"scrape": func(ctx context.Context, task TaskStatus) error {
			return c.retryScrapeTask(task)
		},
		"ai_grouping": func(ctx context.Context, task TaskStatus) error {
			id, err := libraryID(task)
			if err != nil {
				return err
			}
			// locale 优先取任务持久化的原始值，其次取本次重试请求的语言（ctx 注入），最后回退 zh-CN。
			locale := ""
			if task.Params != nil {
				locale = task.Params["locale"]
			}
			if locale == "" {
				locale = metadata.LocaleFromContext(ctx)
			}
			if locale == "" {
				locale = "zh-CN"
			}
			return c.launchAIGroupingTask(id, locale)
		},
		"rebuild_book_hashes": func(ctx context.Context, _ TaskStatus) error {
			return c.launchRebuildBookHashesTask()
		},
		"rebuild_file_identities": func(ctx context.Context, _ TaskStatus) error {
			return c.launchRebuildFileIdentitiesTask()
		},
		"reconcile_koreader_progress": func(ctx context.Context, _ TaskStatus) error {
			return c.launchReconcileKOReaderProgressTask()
		},
		"refresh_koreader_matching": func(ctx context.Context, _ TaskStatus) error {
			return c.launchRefreshKOReaderMatchingTask()
		},
	}
}

// ---- 任务指标与并发上限的上报（依赖运行时配置，不属于任务引擎的内部状态）----

// minPositive 取诸项里最小的正值；全非正时返回 0，即不设上限。
func minPositive(values ...int) int {
	limit := 0
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if limit == 0 || value < limit {
			limit = value
		}
	}
	return limit
}

func taskIOMetricsParams(metrics taskIOMetrics) map[string]string {
	params := map[string]string{
		"io_wait_ms":   strconv.FormatInt(metrics.IOWaitMillis, 10),
		"paused_ms":    strconv.FormatInt(metrics.PausedMillis, 10),
		"hashed_files": strconv.FormatInt(metrics.HashedFiles, 10),
	}
	if metrics.StorageProfile != "" {
		params["storage_profile"] = metrics.StorageProfile
	}
	if metrics.VolumeKey != "" {
		params["volume_key"] = metrics.VolumeKey
	}
	return params
}

func (c *Controller) taskLimitsForPath(path string, force bool) TaskLimits {
	cfg := c.currentConfig()
	profile := scanner.NormalizeScanProfile(cfg.Scanner.ScanProfile)
	if profile == scanner.ScanProfileRepair {
		force = true
	}
	_ = force
	policy := config.ResolveStoragePolicy(cfg, path)
	workers := cfg.Scanner.Workers
	if workers <= 0 {
		workers = runtime.NumCPU() * 2
	}
	limit := policy.IOPolicy.ScanConcurrency
	if profile != scanner.ScanProfileFast {
		limit = minPositive(limit, policy.IOPolicy.ArchiveOpenConcurrency)
	}
	if profile == scanner.ScanProfileIdentity || profile == scanner.ScanProfileRepair {
		limit = minPositive(limit, policy.IOPolicy.HashConcurrency)
	}
	effectiveWorkers := workers
	if limit > 0 && effectiveWorkers > limit {
		effectiveWorkers = limit
	}
	if effectiveWorkers < 1 {
		effectiveWorkers = 1
	}
	return TaskLimits{
		ScanProfile:                string(profile),
		ScannerWorkersConfigured:   cfg.Scanner.Workers,
		ScannerWorkersEffective:    effectiveWorkers,
		StorageProfile:             policy.StorageProfile,
		VolumeKey:                  policy.VolumeKey,
		ScanConcurrency:            policy.IOPolicy.ScanConcurrency,
		ArchiveOpenConcurrency:     policy.IOPolicy.ArchiveOpenConcurrency,
		CoverConcurrency:           policy.IOPolicy.CoverConcurrency,
		HashConcurrency:            policy.IOPolicy.HashConcurrency,
		PauseBackgroundWhenReading: policy.IOPolicy.PauseBackgroundWhenReading,
		IdleOnlyHeavyTasks:         policy.IOPolicy.IdleOnlyHeavyTasks,
		DisableSameDiskPageCache:   policy.IOPolicy.DisableSameDiskPageCache,
	}
}

// ---- HTTP 端点 ----

// taskFiltersFromQuery 解析六个任务端点共用的过滤参数。无法解析的 scope_id/limit 按「不过滤」处理。
func taskFiltersFromQuery(r *http.Request) database.TaskFilters {
	query := r.URL.Query()
	filters := database.TaskFilters{
		Status: strings.TrimSpace(query.Get("status")),
		Scope:  strings.TrimSpace(query.Get("scope")),
		Type:   strings.TrimSpace(query.Get("type")),
		Query:  strings.ToLower(strings.TrimSpace(query.Get("q"))),
	}
	if raw := strings.TrimSpace(query.Get("scope_id")); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil {
			filters.ScopeID = &parsed
		}
	}
	if raw := query.Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			filters.Limit = parsed
		}
	}
	return filters
}

func (c *Controller) listTasks(w http.ResponseWriter, r *http.Request) {
	items, err := c.taskEngine.listTaskStatuses(r.Context(), taskFiltersFromQuery(r))
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to list tasks")
		return
	}
	jsonResponse(w, http.StatusOK, items)
}

func (c *Controller) clearTasks(w http.ResponseWriter, r *http.Request) {
	filters := taskFiltersFromQuery(r)
	// 清理不接受 q / limit：这两个只用于列表展示，用它们做删除条件会让「删了什么」不可预期。
	filters.Query = ""
	filters.Limit = 0

	removed, err := c.taskEngine.clear(r.Context(), filters)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to clear tasks")
		return
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"removed": removed,
	})
}

func (c *Controller) retryTask(w http.ResponseWriter, r *http.Request) {
	taskKey := chi.URLParam(r, "taskKey")
	if taskKey == "" {
		jsonError(w, http.StatusBadRequest, "Missing task key")
		return
	}

	task, err := c.taskEngine.snapshotForRetry(r.Context(), taskKey)
	if err != nil {
		if errors.Is(err, errTaskNotFound) {
			jsonError(w, http.StatusNotFound, "Task not found")
			return
		}
		jsonError(w, http.StatusInternalServerError, "Failed to load task")
		return
	}
	if task.Status == "running" {
		jsonError(w, http.StatusConflict, "Task is already running")
		return
	}
	if !task.Retryable {
		jsonError(w, http.StatusConflict, "Task is not retryable")
		return
	}

	relaunch, ok := c.taskEngine.relauncherFor(task.Type)
	if !ok {
		jsonError(w, http.StatusBadRequest, "Unsupported retry type")
		return
	}

	// 用本次重试请求自身的 Accept-Language 构造 ctx，供 relauncher（如 AI 分组）在无持久化
	// locale 时恢复语言。
	if err := relaunch(requestContextWithLocale(r), task); err != nil {
		if errors.Is(err, errTaskAlreadyRunning) {
			jsonError(w, http.StatusConflict, "Task is already running")
			return
		}
		// 区分错误语义：仅"已在运行"是 409，其它（缺少 scope、GetLibrary 失败等内部错误）返回 500。
		slog.Error("Task retry failed", "task_key", taskKey, "task_type", task.Type, "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to retry task")
		return
	}

	jsonResponse(w, http.StatusAccepted, map[string]string{"message": "Task retry queued"})
}

// taskControlResponses 把任务引擎的控制哨兵错误映射为 HTTP 状态码与响应文案。
// 引擎只判断「为什么不行」，传输层语义留在这里，两侧改动互不牵连。
var taskControlResponses = map[error]struct {
	status  int
	message string
}{
	errTaskNotFound:          {http.StatusNotFound, "Task not found"},
	errTaskNotRunning:        {http.StatusConflict, "Task is not running"},
	errTaskNotPaused:         {http.StatusConflict, "Task is not paused"},
	errTaskNotPausable:       {http.StatusConflict, "Task cannot be paused"},
	errTaskNotCancelable:     {http.StatusConflict, "Task cannot be cancelled"},
	errTaskGateUnavailable:   {http.StatusConflict, "Task pause gate is not available"},
	errTaskCancelUnavailable: {http.StatusConflict, "Task cancellation is not available"},
}

func writeTaskControlError(w http.ResponseWriter, err error) {
	if mapped, ok := taskControlResponses[err]; ok {
		jsonError(w, mapped.status, mapped.message)
		return
	}
	jsonError(w, http.StatusInternalServerError, "Task control failed")
}

// taskControlHandler 生成暂停/恢复/取消三个端点：它们只在「调哪个引擎方法」和「成功文案」上不同。
func (c *Controller) taskControlHandler(control func(*taskEngine, string) error, okMessage string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		taskKey := chi.URLParam(r, "taskKey")
		if taskKey == "" {
			jsonError(w, http.StatusBadRequest, "Missing task key")
			return
		}
		if err := control(c.taskEngine, taskKey); err != nil {
			writeTaskControlError(w, err)
			return
		}
		jsonResponse(w, http.StatusAccepted, map[string]string{"message": okMessage})
	}
}

func (c *Controller) pauseTask(w http.ResponseWriter, r *http.Request) {
	c.taskControlHandler((*taskEngine).pause, "Task pause requested")(w, r)
}

func (c *Controller) resumeTask(w http.ResponseWriter, r *http.Request) {
	c.taskControlHandler((*taskEngine).resume, "Task resumed")(w, r)
}

func (c *Controller) cancelTask(w http.ResponseWriter, r *http.Request) {
	c.taskControlHandler((*taskEngine).cancel, "Task cancellation requested")(w, r)
}
