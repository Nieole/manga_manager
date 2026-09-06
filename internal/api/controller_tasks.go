// 本文件是任务子域的 HTTP 层与装配层：任务列表/清理/重试/暂停/恢复/取消六个端点、
// 任务重试注册表（taskType -> 重启函数），以及任务面板上报的 IO 实况（帧指标与任务参数两条通道）
// 与有效并发数推导。
//
// 引擎的适配层在 task_engine.go（状态机与落盘在 internal/task 与 internal/taskstore），
// 纯转换函数在 task_model.go；本文件只经 c.taskEngine 的方法操作运行，不碰它的字段。

package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"manga-manager/internal/config"
	"manga-manager/internal/metadata"
	"manga-manager/internal/scanner"
	"manga-manager/internal/taskrun"

	"github.com/go-chi/chi/v5"
)

// taskRelauncher 用原任务的作用域与任务参数重新发起同一个任务。返回 errTaskAlreadyRunning 表示
// 同一任务已在运行（映射为 409），返回其它错误视为内部错误（映射为 500）。
type taskRelauncher func(ctx context.Context, task TaskStatus) error

// taskDispatchKey 是**重启函数**注册表的键：身份四要素里决定「怎么跑」的那两项。
//
// 只按类型分发不够：一个类型下的两个**变体**跑法不同（书哈希重建的前台档与低优先级回填、
// 刮削的全库与单库），按类型分发会把回填重启成前台档——大批次、无停顿，正是回填刻意避开的
// 抢盘跑法，而原来那条仍停在终态。作用域与作用域 id 不进这个键：它们是重启函数从任务快照上
// 读回的**入参**（哪个库、哪个系列），不是挑哪一个重启函数的依据。
type taskDispatchKey struct {
	Type    string
	Variant TaskVariant
}

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

// taskParam 读一个**任务参数**，缺了给空串。**重启函数**靠它读回原始入参：一个任务重试时
// 除了作用域就只剩这些参数，读丢了不会有编译错误，后果是重试静默换了跑法（换成默认刮削源、
// 换个语种、丢掉发起理由）。
func taskParam(task TaskStatus, key string) string {
	if task.Params == nil {
		return ""
	}
	return task.Params[key]
}

// buildTaskRelaunchers 注册（类型，**变体**）-> 重启函数，是重试分发与「可重试」的唯一事实来源。
func (c *Controller) buildTaskRelaunchers() map[taskDispatchKey]taskRelauncher {
	libraryID := func(task TaskStatus) (int64, error) {
		if task.ScopeID == nil {
			return 0, fmt.Errorf("task %q missing library id", task.Key)
		}
		return *task.ScopeID, nil
	}
	forceParam := func(task TaskStatus) bool {
		return taskParam(task, "force") == "true"
	}
	return map[taskDispatchKey]taskRelauncher{
		{Type: "scan_library", Variant: variantSole}: func(ctx context.Context, task TaskStatus) error {
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
		{Type: "scan_series", Variant: variantSole}: func(ctx context.Context, task TaskStatus) error {
			if task.ScopeID == nil {
				return fmt.Errorf("task %q missing series id", task.Key)
			}
			return c.launchSeriesScanTask(*task.ScopeID, forceParam(task))
		},
		{Type: "cleanup_library", Variant: variantSole}: func(ctx context.Context, task TaskStatus) error {
			id, err := libraryID(task)
			if err != nil {
				return err
			}
			return c.launchCleanupLibraryTask(id)
		},
		{Type: "rebuild_index", Variant: variantSole}: func(ctx context.Context, _ TaskStatus) error {
			return c.launchRebuildIndexTask()
		},
		{Type: "rebuild_thumbnails", Variant: variantSole}: func(ctx context.Context, _ TaskStatus) error {
			return c.launchRebuildThumbnailsTask()
		},
		{Type: "scrape", Variant: variantScrapeAllLibraries}: func(ctx context.Context, task TaskStatus) error {
			return c.launchBatchScrapeAllSeriesTask(ctx, taskParam(task, "provider"))
		},
		{Type: "scrape", Variant: variantScrapeOneLibrary}: func(ctx context.Context, task TaskStatus) error {
			id, err := libraryID(task)
			if err != nil {
				return err
			}
			return c.launchLibraryScrapeTask(ctx, id, taskParam(task, "provider"))
		},
		{Type: "ai_grouping", Variant: variantSole}: func(ctx context.Context, task TaskStatus) error {
			id, err := libraryID(task)
			if err != nil {
				return err
			}
			// locale 优先取任务持久化的原始值，其次取本次重试请求的语言（ctx 注入），最后回退 zh-CN。
			locale := firstNonEmptyTaskValue(taskParam(task, "locale"), metadata.LocaleFromContext(ctx))
			return c.launchAIGroupingTask(id, firstNonEmptyTaskValue(locale, "zh-CN"))
		},
		{Type: "rebuild_book_hashes", Variant: variantHashRebuildForeground}: func(ctx context.Context, _ TaskStatus) error {
			return c.launchRebuildBookHashesTask()
		},
		{Type: "rebuild_book_hashes", Variant: variantHashRebuildBackfill}: func(ctx context.Context, task TaskStatus) error {
			// 发起理由是这个变体的原始入参，重试要沿用而不是另编一个。
			return c.launchLowPriorityBookHashBackfillTask(firstNonEmptyTaskValue(taskParam(task, "reason"), "manual_retry"))
		},
		{Type: "rebuild_file_identities", Variant: variantSole}: func(ctx context.Context, _ TaskStatus) error {
			return c.launchRebuildFileIdentitiesTask()
		},
		{Type: "reconcile_koreader_progress", Variant: variantSole}: func(ctx context.Context, _ TaskStatus) error {
			return c.launchReconcileKOReaderProgressTask()
		},
		{Type: "refresh_koreader_matching", Variant: variantSole}: func(ctx context.Context, _ TaskStatus) error {
			return c.launchRefreshKOReaderMatchingTask()
		},
	}
}

// ---- 任务指标与并发上限的上报（依赖运行时配置，不属于任务引擎的内部状态）----

// taskIOFrameMetrics 是**任务句柄**的 IO 实况在**一帧**里的那几个键。
// reportHashProgress 与 koreaderFingerprintFrame 共用一份，键名不会各自漂移。
func taskIOFrameMetrics(handleIO taskrun.IOMetrics) map[string]int64 {
	return map[string]int64{
		"hashed_files": handleIO.HashedFiles,
		"io_wait_ms":   handleIO.IOWaitMillis,
		"paused_ms":    handleIO.PausedMillis,
	}
}

// taskIOMetricsParams 是同一份实况在**任务参数**那条通道里的形状：存储 IO 面板按参数名读它。
// 空的档位与卷键滤掉不写，理由见 koreaderFingerprintFrame。
func taskIOMetricsParams(handleIO taskrun.IOMetrics) map[string]string {
	params := map[string]string{
		"io_wait_ms":   strconv.FormatInt(handleIO.IOWaitMillis, 10),
		"paused_ms":    strconv.FormatInt(handleIO.PausedMillis, 10),
		"hashed_files": strconv.FormatInt(handleIO.HashedFiles, 10),
	}
	if handleIO.StorageProfile != "" {
		params["storage_profile"] = handleIO.StorageProfile
	}
	if handleIO.VolumeKey != "" {
		params["volume_key"] = handleIO.VolumeKey
	}
	return params
}

// taskLimitsForPath 拼出任务面板上那块并发徽章：这条路径上的存储画像，加上 scanner.WorkerCount
// 在它上面给出的 worker 数。
//
// 入参必须是一条**真的会被扫**的库路径，否则报出来的数字没有对应的实物——只有扫描任务调它，
// 别的后台任务受**磁盘作业**按工种裁定的上限约束，与扫描 worker 数无关。
func (c *Controller) taskLimitsForPath(path string) TaskLimits {
	cfg := c.currentConfig()
	profile := scanner.NormalizeScanProfile(cfg.Scanner.ScanProfile)
	policy := config.ResolveStoragePolicy(cfg, path)
	return TaskLimits{
		ScanProfile:                string(profile),
		ScannerWorkersConfigured:   cfg.Scanner.Workers,
		ScannerWorkersEffective:    scanner.WorkerCount(cfg, path, scanner.ScanOptions{Profile: profile}),
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
func taskFiltersFromQuery(r *http.Request) taskFilters {
	query := r.URL.Query()
	filters := taskFilters{
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
	// 关键词与条数由引擎自己丢掉（它们只用于列表展示，用来做删除条件会让「删了什么」不可预期）：
	// 判据只留一处，别的调用方绕不过去。
	removed, err := c.taskEngine.clear(r.Context(), taskFiltersFromQuery(r))
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
	// 判据是**活动态**而不只是运行中：**取消中**与**已暂停**同样占着运行槽位，此时重启等于让同一件事
	// 跑两遍。下游启动入口的**任务键**闸门只在重启函数回到同一条键时才兜得住，一类多键的类型上兜不住。
	if taskIsActive(task.Status) {
		jsonError(w, http.StatusConflict, "Task is already running")
		return
	}
	if !task.Retryable {
		jsonError(w, http.StatusConflict, "Task is not retryable")
		return
	}

	relaunch, ok := c.taskEngine.relauncherFor(task.Type, task.Variant)
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
