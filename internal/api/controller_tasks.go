// 本文件是任务子域的 HTTP 层与装配层：八个端点（六个按**任务键**寻址，全部暂停 / 全部恢复作用在
// 全体运行上）、「再发起一次」的注册表（（类型，**变体**）-> 重启函数与**可续跑**），
// 以及任务面板上报的 IO 实况（帧指标与任务参数两条通道）与有效并发数推导。
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
	"manga-manager/internal/runhandle"
	"manga-manager/internal/scanner"
	"manga-manager/internal/task"
)

// taskRelauncher 用原运行的作用域与**任务参数**重新发起同一个任务。返回 errTaskAlreadyRunning
// 表示同一任务已在运行（映射为 409），返回其它错误视为内部错误（映射为 500）。
//
// **发起方由调用方给**：用户点重试是手动，重启之后的续跑是**恢复**。它不是这里的默认值——
// 猜错的话，半夜自己接着跑的那条运行会在界面上写着「手动」，而那正是用户要的答案。
type taskRelauncher func(ctx context.Context, run RunSnapshot, trigger task.Trigger) error

// taskDispatch 是一个（类型，**变体**）在「再发起一次」这件事上的全部声明：怎么发起，
// 以及重启之后**没人看着**时可不可以自己发起。
//
// 两条判据分开写而不是合成一个布尔值（规格关键决定 7）：**可重试**只要求「能再发起一次」，
// **可续跑**还要求「在无人看着时再发起一次也不会造成意外」。合成一个的话，
// 断电后开机 ComicInfo 回写就自己动文件去了。
//
// 方向是单向的：可续跑比可重试严格，因此续跑必须有重启函数——一个「白名单里有、却没人发得起」
// 的类型只会在每次重启时静默什么都不做。
type taskDispatch struct {
	// Relaunch 是这个（类型，变体）的**重启函数**，注册了即**可重试**。
	Relaunch taskRelauncher
	// Resumable 是**可续跑**：服务重启后自动重排队，**发起方**记恢复。
	// 判据是「它会不会改磁盘内容、会不会花钱」——两者都不会才进白名单。
	Resumable bool
}

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
func taskParam(run RunSnapshot, key string) string {
	if run.Params == nil {
		return ""
	}
	return run.Params[key]
}

// buildTaskDispatch 注册（类型，**变体**）-> 这个类型怎么再发起一次、以及重启之后能不能自己发起。
// 它是**可重试**与**可续跑**两条判据的唯一事实来源，两份清单一旦分家，界面上说不可续跑的类型
// 会在重启后自己跑起来。
//
// **可续跑白名单**（规格关键决定 7）：资料库扫描、系列扫描、封面生成、重建缩略图、清理缩略图、
// 书哈希重建与低优先级回填、文件身份重建、KOReader 进度对账与匹配刷新。
//
// **不可续跑**的那几个（清理资料库、外部库扫描与传输、ComicInfo 回写、刮削、AI 分组）
// 各自的理由都写在它那一行上：要么改磁盘内容、要么花钱。它们照样可重试——那是用户按下的那一下。
func (c *Controller) buildTaskDispatch() map[taskDispatchKey]taskDispatch {
	libraryID := func(run RunSnapshot) (int64, error) {
		if run.ScopeID == nil {
			return 0, fmt.Errorf("task %d missing library id", run.TaskID)
		}
		return *run.ScopeID, nil
	}
	forceParam := func(run RunSnapshot) bool {
		return taskParam(run, "force") == "true"
	}
	return map[taskDispatchKey]taskDispatch{
		{Type: "scan_library", Variant: variantSole}: {Resumable: true, Relaunch: func(ctx context.Context, run RunSnapshot, trigger task.Trigger) error {
			id, err := libraryID(run)
			if err != nil {
				return err
			}
			lib, err := c.store.GetLibrary(ctx, id)
			if err != nil {
				return err
			}
			// 启动入口本就返回「同类任务已在运行」哨兵错误，重启函数原样透传即可，
			// 不必再把一个布尔值转换回哨兵错误。
			return c.launchLibraryScanTask(lib, forceParam(run), trigger)
		}},
		// 封面生成的重启函数不认领任何批（进程里那一批随重启一起没了），
		// 而是把这个库里还缺封面的书重新排一遍，见 launchCoverRun。
		{Type: "generate_covers", Variant: variantSole}: {Resumable: true, Relaunch: func(ctx context.Context, run RunSnapshot, trigger task.Trigger) error {
			id, err := libraryID(run)
			if err != nil {
				return err
			}
			return c.launchCoverRun(id, trigger, true)
		}},
		{Type: "scan_series", Variant: variantSole}: {Resumable: true, Relaunch: func(ctx context.Context, run RunSnapshot, trigger task.Trigger) error {
			if run.ScopeID == nil {
				return fmt.Errorf("task %d missing series id", run.TaskID)
			}
			return c.launchSeriesScanTask(*run.ScopeID, forceParam(run), trigger)
		}},
		// 不可续跑：它删的是记录，而「这些记录该不该删」在无人看着时没人裁决。
		{Type: "cleanup_library", Variant: variantSole}: {Relaunch: func(ctx context.Context, run RunSnapshot, trigger task.Trigger) error {
			id, err := libraryID(run)
			if err != nil {
				return err
			}
			return c.launchCleanupLibraryTask(id, trigger)
		}},
		// 不可续跑：它重灌完索引还要**同步等一次全量扫描**，白名单里没有这一条。
		{Type: "rebuild_index", Variant: variantSole}: {Relaunch: func(ctx context.Context, _ RunSnapshot, trigger task.Trigger) error {
			return c.launchRebuildIndexTask(trigger)
		}},
		{Type: "rebuild_thumbnails", Variant: variantSole}: {Resumable: true, Relaunch: func(ctx context.Context, _ RunSnapshot, trigger task.Trigger) error {
			return c.launchRebuildThumbnailsTask(trigger)
		}},
		{Type: "cleanup_thumbnails", Variant: variantSole}: {Resumable: true, Relaunch: func(ctx context.Context, _ RunSnapshot, trigger task.Trigger) error {
			return c.launchCleanupThumbnailsTask(trigger)
		}},
		// 刮削两个变体都不可续跑：它们要调外部源，有的按次计费。
		{Type: "scrape", Variant: variantScrapeAllLibraries}: {Relaunch: func(ctx context.Context, run RunSnapshot, trigger task.Trigger) error {
			return c.launchBatchScrapeAllSeriesTask(ctx, taskParam(run, "provider"), trigger)
		}},
		{Type: "scrape", Variant: variantScrapeOneLibrary}: {Relaunch: func(ctx context.Context, run RunSnapshot, trigger task.Trigger) error {
			id, err := libraryID(run)
			if err != nil {
				return err
			}
			return c.launchLibraryScrapeTask(ctx, id, taskParam(run, "provider"), trigger)
		}},
		// 不可续跑：它调 LLM，那是花钱的。
		{Type: "ai_grouping", Variant: variantSole}: {Relaunch: func(ctx context.Context, run RunSnapshot, trigger task.Trigger) error {
			id, err := libraryID(run)
			if err != nil {
				return err
			}
			// locale 优先取任务持久化的原始值，其次取本次重试请求的语言（ctx 注入），最后回退 zh-CN。
			locale := firstNonEmptyTaskValue(taskParam(run, "locale"), metadata.LocaleFromContext(ctx))
			return c.launchAIGroupingTask(id, firstNonEmptyTaskValue(locale, "zh-CN"), trigger)
		}},
		{Type: "rebuild_book_hashes", Variant: variantHashRebuildForeground}: {Resumable: true, Relaunch: func(ctx context.Context, _ RunSnapshot, trigger task.Trigger) error {
			return c.launchRebuildBookHashesTask(trigger)
		}},
		{Type: "rebuild_book_hashes", Variant: variantHashRebuildBackfill}: {Resumable: true, Relaunch: func(ctx context.Context, run RunSnapshot, trigger task.Trigger) error {
			// 发起理由是这个变体的原始入参，再发起一次要沿用而不是另编一个。
			return c.launchLowPriorityBookHashBackfillTask(firstNonEmptyTaskValue(taskParam(run, "reason"), "manual_retry"), trigger)
		}},
		{Type: "rebuild_file_identities", Variant: variantSole}: {Resumable: true, Relaunch: func(ctx context.Context, _ RunSnapshot, trigger task.Trigger) error {
			return c.launchRebuildFileIdentitiesTask(trigger)
		}},
		{Type: "reconcile_koreader_progress", Variant: variantSole}: {Resumable: true, Relaunch: func(ctx context.Context, _ RunSnapshot, trigger task.Trigger) error {
			return c.launchReconcileKOReaderProgressTask(trigger)
		}},
		{Type: "refresh_koreader_matching", Variant: variantSole}: {Resumable: true, Relaunch: func(ctx context.Context, _ RunSnapshot, trigger task.Trigger) error {
			return c.launchRefreshKOReaderMatchingTask(trigger)
		}},
	}
}

// ---- 任务指标与并发上限的上报（依赖运行时配置，不属于任务引擎的内部状态）----

// taskIOFrameMetrics 是**运行句柄**的 IO 实况在**一帧**里的那几个键。
// reportHashProgress 与 koreaderFingerprintFrame 共用一份，键名不会各自漂移。
func taskIOFrameMetrics(handleIO runhandle.IOMetrics) map[string]int64 {
	return map[string]int64{
		"hashed_files": handleIO.HashedFiles,
		"io_wait_ms":   handleIO.IOWaitMillis,
		"paused_ms":    handleIO.PausedMillis,
	}
}

// taskIOMetricsParams 是同一份实况在**任务参数**那条通道里的形状：存储 IO 面板按参数名读它。
// 空的档位与卷键滤掉不写，理由见 koreaderFingerprintFrame。
func taskIOMetricsParams(handleIO runhandle.IOMetrics) map[string]string {
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

// taskFiltersFromQuery 解析任务端点共用的过滤参数。无法解析的 scope_id/task_id/limit 按「不过滤」处理。
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
	if raw := strings.TrimSpace(query.Get("task_id")); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil && parsed > 0 {
			filters.TaskID = parsed
		}
	}
	if raw := query.Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			filters.Limit = parsed
		}
	}
	return filters
}

// listTasks 取一页**运行**。任务中心展开某一行时带上 task_id，取回的就是那个任务的历次运行。
func (c *Controller) listTasks(w http.ResponseWriter, r *http.Request) {
	items, err := c.taskEngine.listRunSnapshots(r.Context(), taskFiltersFromQuery(r))
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to list tasks")
		return
	}
	jsonResponse(w, http.StatusOK, items)
}

// listTaskSummaries 取**任务清单**：一个任务一行，带它最近一次运行，不带历次运行。
func (c *Controller) listTaskSummaries(w http.ResponseWriter, r *http.Request) {
	items, err := c.taskEngine.listTaskSummaries(r.Context(), taskFiltersFromQuery(r))
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to list tasks")
		return
	}
	jsonResponse(w, http.StatusOK, items)
}

// getTaskCenterLive 取**实况区**那一帧：仍会变化的运行与它们的汇总。它不看筛选参数，理由见 RunLive。
func (c *Controller) getTaskCenterLive(w http.ResponseWriter, r *http.Request) {
	frame, err := c.taskEngine.live(r.Context())
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to load live runs")
		return
	}
	jsonResponse(w, http.StatusOK, frame)
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
	taskID, err := parseID(r, "taskID")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid task ID")
		return
	}

	run, err := c.taskEngine.snapshotForRetry(r.Context(), taskID)
	if err != nil {
		// 两条都是 404，但话不一样：一个任务此刻正跑着第一次、还没有可重放的东西，与它压根不存在
		// 是两回事。前者就明明白白列在任务中心里，答「任务不存在」会把用户支去查一个并不存在的数据丢失。
		if errors.Is(err, errNoRetryableRun) {
			jsonError(w, http.StatusNotFound, "No finished run to retry")
			return
		}
		if errors.Is(err, errTaskNotFound) {
			jsonError(w, http.StatusNotFound, "Task not found")
			return
		}
		jsonError(w, http.StatusInternalServerError, "Failed to load task")
		return
	}
	// 这里**不再判一次「是不是已经在跑」**：准入只剩一处，而那一处如今的答案不是拒绝而是排队——
	// 重启函数回到同一个**身份**上，撞上活动运行就进**排队中**，撞上排队的就被**合并**进去。
	// 在这里补一道 409 等于让「什么叫已经在跑」重新有两个答案，而其中一个还会把这次重试丢掉。
	if !run.Retryable {
		jsonError(w, http.StatusConflict, "Task is not retryable")
		return
	}

	relaunch, ok := c.taskEngine.relauncherFor(run.Type, run.Variant)
	if !ok {
		jsonError(w, http.StatusBadRequest, "Unsupported retry type")
		return
	}

	// 用本次重试请求自身的 Accept-Language 构造 ctx，供 relauncher（如 AI 分组）在无持久化
	// locale 时恢复语言。**发起方是手动**：重试是用户按下的那一下，重启之后的续跑另有一条路。
	if err := relaunch(requestContextWithLocale(r), run, task.TriggerManual); err != nil {
		if errors.Is(err, errTaskAlreadyRunning) {
			jsonError(w, http.StatusConflict, "Task is already running")
			return
		}
		// 区分错误语义：仅"已在运行"是 409，其它（缺少 scope、GetLibrary 失败等内部错误）返回 500。
		slog.Error("Task retry failed", "task_id", taskID, "run_id", run.RunID, "task_type", run.Type, "error", err)
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

// runControlHandler 生成暂停/恢复/取消三个端点：它们只在「调哪个引擎方法」和「成功文案」上不同。
//
// 寻址用**运行 id** 而不是**任务键**：队列出现之后，同一个键此刻可以有两条仍会变化的运行
// （一条在跑、一条排队），按键寻址就答不出用户按的是哪一张卡片上的按钮（见 taskEngine.pauseRun）。
//
// **重试加不进这个生成器，也加不进 setTaskAutoLaunch**——键退出寻址之后六个端点都按对象寻址了，
// 看着只差一个参数名，实则不然：重试要先取回**终态**快照、从注册表里认出**重启函数**、再发起，
// 并带自己那条「没有可重试的运行」的 404，这三步在两个生成器里都无处安放。真要合并，还得给
// 暂停 / 恢复 / 取消三个引擎方法各补一个它们用不上的 ctx 形参。能合并的仍只有原来那两组。
func (c *Controller) runControlHandler(control func(*taskEngine, int64) error, okMessage string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		runID, err := parseID(r, "runID")
		if err != nil {
			jsonError(w, http.StatusBadRequest, "Invalid run ID")
			return
		}
		if err := control(c.taskEngine, runID); err != nil {
			writeTaskControlError(w, err)
			return
		}
		jsonResponse(w, http.StatusAccepted, map[string]string{"message": okMessage})
	}
}

func (c *Controller) pauseRun(w http.ResponseWriter, r *http.Request) {
	c.runControlHandler((*taskEngine).pauseRun, "Run pause requested")(w, r)
}

func (c *Controller) resumeRun(w http.ResponseWriter, r *http.Request) {
	c.runControlHandler((*taskEngine).resumeRun, "Run resumed")(w, r)
}

func (c *Controller) cancelRun(w http.ResponseWriter, r *http.Request) {
	c.runControlHandler((*taskEngine).cancelRun, "Run cancellation requested")(w, r)
}

// pauseAllTasks 与 resumeAllTasks 是任务中心顶部那对按钮：**全部暂停就是逐个按下暂停闸门**，
// 不是第二套机制。它们按对象作用在**运行**上，因此不按**任务键**寻址，也没有 404 这条分支。
//
// 响应里回的是真正动到的条数：不可暂停的运行被跳过（界面上另有说明），
// 而「一条都没动到」与「按下了三条」对用户不是同一件事。
func (c *Controller) pauseAllTasks(w http.ResponseWriter, r *http.Request) {
	c.bulkControlHandler((*taskEngine).pauseAll, "paused", "Failed to pause runs")(w, r)
}

func (c *Controller) resumeAllTasks(w http.ResponseWriter, r *http.Request) {
	c.bulkControlHandler((*taskEngine).resumeAll, "resumed", "Failed to resume runs")(w, r)
}

// bulkControlHandler 生成全部暂停 / 全部恢复两个端点：它们只在「调哪个引擎方法」、
// 计数字段名与失败文案上不同。
func (c *Controller) bulkControlHandler(control func(*taskEngine, context.Context) (int, error), countField, failure string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		affected, err := control(c.taskEngine, r.Context())
		if err != nil {
			jsonError(w, http.StatusInternalServerError, failure)
			return
		}
		jsonResponse(w, http.StatusAccepted, map[string]int{countField: affected})
	}
}
