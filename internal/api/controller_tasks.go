// 业务说明：本文件由 controller.go 拆分而来，属于后端 API 层的任务引擎子域，负责任务状态模型、进度/指标聚合、持久化、生命周期（启动/更新/暂停/恢复/取消/完成）与任务列表接口。

package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/metadata"
	"manga-manager/internal/scanner"
	"manga-manager/internal/storageio"
	"manga-manager/internal/taskcontrol"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

// taskEngine 收敛后台任务引擎的全部内存状态：任务表、运行时句柄、序号、异步落盘的待写集合与唤醒信号，
// 以及任务重试注册表。把这些状态归一到一个结构体，使任务引擎的状态边界清晰、便于整体推理；任务方法仍挂在
// Controller 上，统一经 c.taskEngine 访问这些状态。除 relaunchers（启动后只读）外，其余字段由 mutex 保护。
type taskEngine struct {
	mutex    sync.Mutex
	tasks    map[string]TaskStatus
	runtimes map[string]*TaskRuntime
	seq      int64
	// persistPending 是待异步落盘的最新任务快照（按 key 合并），由 mutex 保护。进度更新只写内存 + 入此集合，
	// 专用 goroutine（startTaskPersister）节流批量写 SQLite，避免在临界区内同步写库阻塞任务 API 与系列详情页。
	// persistWake 在终态时唤醒该 goroutine 立即刷，缩短终态落库延迟（缓冲 1）。
	persistPending map[string]TaskStatus
	persistWake    chan struct{}
	// relaunchers 是任务重试的注册表（taskType -> 重启函数），也是"可重试类型"的唯一事实来源，在 NewController
	// 中构建，取代 retryTask 的中央 switch 与 isRetryableTaskType 两份硬编码清单。
	relaunchers map[string]taskRelauncher
}

func newTaskEngine() *taskEngine {
	return &taskEngine{
		tasks:          make(map[string]TaskStatus),
		runtimes:       make(map[string]*TaskRuntime),
		persistPending: make(map[string]TaskStatus),
		persistWake:    make(chan struct{}, 1),
	}
}

func inferTaskScope(taskType, key string) (string, *int64) {
	scope := "system"
	switch {
	case strings.Contains(taskType, "library"):
		scope = "library"
	case strings.Contains(taskType, "series"):
		scope = "series"
	}

	parts := strings.Split(key, "_")
	if len(parts) == 0 {
		return scope, nil
	}

	last := parts[len(parts)-1]
	id, err := strconv.ParseInt(last, 10, 64)
	if err != nil {
		return scope, nil
	}
	return scope, &id
}

// taskRelauncher 用原任务的 scope/params 重新发起一个同类型任务。返回 errTaskAlreadyRunning 表示
// 同类任务已在运行（映射为 409），返回其它错误视为内部错误（映射为 500）。
type taskRelauncher func(ctx context.Context, task TaskStatus) error

// errTaskAlreadyRunning 是重试时"同类任务已在运行"的哨兵错误。
var errTaskAlreadyRunning = errors.New("task already running")

// buildTaskRelaunchers 注册各任务类型 -> 重启函数，是重试分发与"可重试类型"的唯一事实来源，
// 取代此前分散在 retryTask 的 switch 与 isRetryableTaskType 两份需同步维护的硬编码清单。
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
			if !c.launchLibraryScanTask(lib, forceParam(task)) {
				return errTaskAlreadyRunning
			}
			return nil
		},
		"scan_series": func(ctx context.Context, task TaskStatus) error {
			if task.ScopeID == nil {
				return fmt.Errorf("task %q missing series id", task.Key)
			}
			if !c.launchSeriesScanTask(*task.ScopeID, forceParam(task)) {
				return errTaskAlreadyRunning
			}
			return nil
		},
		"cleanup_library": func(ctx context.Context, task TaskStatus) error {
			id, err := libraryID(task)
			if err != nil {
				return err
			}
			if !c.launchCleanupLibraryTask(id) {
				return errTaskAlreadyRunning
			}
			return nil
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
			// locale 优先取任务持久化的原始值，其次取本次重试请求的语言（ctx 注入），最后回退 zh-CN，
			// 修复此前无条件硬编码 zh-CN 导致非中文用户重试 AI 分组回落中文的问题。
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
			if !c.launchAIGroupingTask(id, locale) {
				return errTaskAlreadyRunning
			}
			return nil
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

// isRetryableTaskType 由注册表派生：注册了 relauncher 的类型即可重试，消除第二份硬编码清单。
func (c *Controller) isRetryableTaskType(taskType string) bool {
	_, ok := c.taskEngine.relaunchers[taskType]
	return ok
}

func taskStatusFromRecord(record database.TaskRecord) TaskStatus {
	task := TaskStatus{
		Key:        record.Key,
		Type:       record.Type,
		Scope:      record.Scope,
		ScopeID:    record.ScopeID,
		ScopeName:  record.ScopeName,
		Status:     record.Status,
		Message:    record.Message,
		Error:      record.Error,
		Current:    record.Current,
		Total:      record.Total,
		CanCancel:  record.CanCancel,
		Retryable:  record.Retryable,
		Params:     record.Params,
		StartedAt:  record.StartedAt,
		UpdatedAt:  record.UpdatedAt,
		FinishedAt: record.FinishedAt,
		Sequence:   record.Sequence,
	}
	hydrateTaskStatusDerivedFields(&task)
	return task
}

func taskRecordFromStatus(task TaskStatus) database.TaskRecord {
	task.Params = taskParamsWithDerivedFields(task)
	return database.TaskRecord{
		Key:        task.Key,
		Type:       task.Type,
		Scope:      task.Scope,
		ScopeID:    task.ScopeID,
		ScopeName:  task.ScopeName,
		Status:     task.Status,
		Message:    task.Message,
		Error:      task.Error,
		Current:    task.Current,
		Total:      task.Total,
		CanCancel:  task.CanCancel,
		Retryable:  task.Retryable,
		Params:     task.Params,
		StartedAt:  task.StartedAt,
		UpdatedAt:  task.UpdatedAt,
		FinishedAt: task.FinishedAt,
		Sequence:   task.Sequence,
	}
}

func hydrateTaskStatusDerivedFields(task *TaskStatus) {
	if task == nil || task.Params == nil {
		enrichTaskProgress(task)
		return
	}
	task.Phase = firstNonEmptyTaskValue(task.Phase, task.Params["phase"])
	task.CurrentItem = firstNonEmptyTaskValue(task.CurrentItem, task.Params["current_item"])
	task.PauseReason = firstNonEmptyTaskValue(task.PauseReason, task.Params["pause_reason"])
	task.MessageCode = firstNonEmptyTaskValue(task.MessageCode, task.Params["message_code"])
	if raw := strings.TrimSpace(task.Params["can_pause"]); raw != "" {
		task.CanPause, _ = strconv.ParseBool(raw)
	}
	if raw := strings.TrimSpace(task.Params["can_resume"]); raw != "" {
		task.CanResume, _ = strconv.ParseBool(raw)
	}
	if pausedAt := strings.TrimSpace(task.Params["paused_at"]); task.PausedAt == nil && pausedAt != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, pausedAt); err == nil {
			task.PausedAt = &parsed
		}
	}

	for key, value := range task.Params {
		switch {
		case strings.HasPrefix(key, "metric."):
			if task.Metrics == nil {
				task.Metrics = make(map[string]int64)
			}
			if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
				task.Metrics[strings.TrimPrefix(key, "metric.")] = parsed
			}
		case strings.HasPrefix(key, "label."):
			if task.Labels == nil {
				task.Labels = make(map[string]string)
			}
			task.Labels[strings.TrimPrefix(key, "label.")] = value
		case strings.HasPrefix(key, "msgparam."):
			if task.MessageParams == nil {
				task.MessageParams = make(map[string]string)
			}
			task.MessageParams[strings.TrimPrefix(key, "msgparam.")] = value
		case strings.HasPrefix(key, "limit."):
			applyTaskLimitParam(task, strings.TrimPrefix(key, "limit."), value)
		}
	}
	enrichTaskProgress(task)
}

func applyTaskLimitParam(task *TaskStatus, key, value string) {
	if task.EffectiveLimit == nil {
		task.EffectiveLimit = &TaskLimits{}
	}
	parseInt := func() int {
		parsed, _ := strconv.Atoi(value)
		return parsed
	}
	parseBool := func() bool {
		parsed, _ := strconv.ParseBool(value)
		return parsed
	}
	switch key {
	case "scan_profile":
		task.EffectiveLimit.ScanProfile = value
	case "scanner_workers_configured":
		task.EffectiveLimit.ScannerWorkersConfigured = parseInt()
	case "scanner_workers_effective":
		task.EffectiveLimit.ScannerWorkersEffective = parseInt()
	case "storage_profile":
		task.EffectiveLimit.StorageProfile = value
	case "volume_key":
		task.EffectiveLimit.VolumeKey = value
	case "scan_concurrency":
		task.EffectiveLimit.ScanConcurrency = parseInt()
	case "archive_open_concurrency":
		task.EffectiveLimit.ArchiveOpenConcurrency = parseInt()
	case "cover_concurrency":
		task.EffectiveLimit.CoverConcurrency = parseInt()
	case "hash_concurrency":
		task.EffectiveLimit.HashConcurrency = parseInt()
	case "pause_background_when_reading":
		task.EffectiveLimit.PauseBackgroundWhenReading = parseBool()
	case "idle_only_heavy_tasks":
		task.EffectiveLimit.IdleOnlyHeavyTasks = parseBool()
	case "disable_same_disk_page_cache":
		task.EffectiveLimit.DisableSameDiskPageCache = parseBool()
	}
}

func taskParamsWithDerivedFields(task TaskStatus) map[string]string {
	params := make(map[string]string, len(task.Params)+24)
	for k, v := range task.Params {
		params[k] = v
	}
	put := func(key, value string) {
		if strings.TrimSpace(value) != "" {
			params[key] = value
		}
	}
	put("phase", task.Phase)
	put("current_item", task.CurrentItem)
	put("pause_reason", task.PauseReason)
	// 把可本地化消息码/参数一并落进 params，使已完成任务从 DB 读回后仍能本地化渲染
	// （Message 对编码任务为空，若不持久化 code，读回后只会剩任务类型名）。
	put("message_code", task.MessageCode)
	for key, value := range task.MessageParams {
		put("msgparam."+key, value)
	}
	params["can_pause"] = strconv.FormatBool(task.CanPause)
	params["can_resume"] = strconv.FormatBool(task.CanResume)
	if task.PausedAt != nil {
		params["paused_at"] = task.PausedAt.Format(time.RFC3339Nano)
	}
	for key, value := range task.Metrics {
		params["metric."+key] = strconv.FormatInt(value, 10)
	}
	for key, value := range task.Labels {
		put("label."+key, value)
	}
	if task.EffectiveLimit != nil {
		limit := task.EffectiveLimit
		put("limit.scan_profile", limit.ScanProfile)
		params["limit.scanner_workers_configured"] = strconv.Itoa(limit.ScannerWorkersConfigured)
		params["limit.scanner_workers_effective"] = strconv.Itoa(limit.ScannerWorkersEffective)
		put("limit.storage_profile", limit.StorageProfile)
		put("limit.volume_key", limit.VolumeKey)
		params["limit.scan_concurrency"] = strconv.Itoa(limit.ScanConcurrency)
		params["limit.archive_open_concurrency"] = strconv.Itoa(limit.ArchiveOpenConcurrency)
		params["limit.cover_concurrency"] = strconv.Itoa(limit.CoverConcurrency)
		params["limit.hash_concurrency"] = strconv.Itoa(limit.HashConcurrency)
		params["limit.pause_background_when_reading"] = strconv.FormatBool(limit.PauseBackgroundWhenReading)
		params["limit.idle_only_heavy_tasks"] = strconv.FormatBool(limit.IdleOnlyHeavyTasks)
		params["limit.disable_same_disk_page_cache"] = strconv.FormatBool(limit.DisableSameDiskPageCache)
	}
	if len(params) == 0 {
		return nil
	}
	return params
}

func firstNonEmptyTaskValue(preferred, fallback string) string {
	if strings.TrimSpace(preferred) != "" {
		return preferred
	}
	return fallback
}

func enrichTaskProgress(task *TaskStatus) {
	if task == nil {
		return
	}
	if task.Total > 0 {
		percent := float64(task.Current) * 100 / float64(task.Total)
		if percent > 100 {
			percent = 100
		}
		task.Percent = &percent
	}
	elapsed := time.Since(task.StartedAt).Seconds()
	if task.Status != "running" && task.Status != "paused" && task.FinishedAt != nil {
		elapsed = task.FinishedAt.Sub(task.StartedAt).Seconds()
	}
	if elapsed > 0 && task.Current > 0 {
		task.RatePerMinute = float64(task.Current) * 60 / elapsed
		if task.Total > task.Current {
			eta := int64(float64(task.Total-task.Current) / task.RatePerMinute * 60)
			task.EtaSeconds = &eta
		}
	}
}

// persistTaskStatus 记录一个待异步落盘的任务快照。调用方必须持有 taskMutex：这里只写内存里的
// taskPersistPending（按 key 合并，最新快照胜），真正的 UpsertTask 由 startTaskPersister 在锁外
// 节流批量执行，避免在临界区内同步写 SQLite（扫描批量事务期间可阻塞任务 API 与系列详情页最长
// busy_timeout）。单一写入方 + 按 key 合并，避免进度写与终态写乱序覆盖。
func (c *Controller) persistTaskStatus(task TaskStatus) {
	if c.store == nil {
		return
	}
	if c.taskEngine.persistPending == nil {
		c.taskEngine.persistPending = make(map[string]TaskStatus)
	}
	c.taskEngine.persistPending[task.Key] = task
}

// persistTaskStatusFinal 用于任务终态（完成/失败/取消）：仍走同一异步队列（保持单一写入方、不与
// 进度写乱序），但额外唤醒落盘 goroutine 立即刷，缩短终态落库延迟。调用方持有 taskMutex。
func (c *Controller) persistTaskStatusFinal(task TaskStatus) {
	c.persistTaskStatus(task)
	select {
	case c.taskEngine.persistWake <- struct{}{}:
	default:
	}
}

// startTaskPersister 是任务快照的唯一落盘 goroutine：500ms 节流一次，终态唤醒时立即刷，关闭前再刷
// 一次，保证优雅关闭时最新进度/终态落库。经 runBackground 登记 backgroundWG，Close() 会等待其退出。
func (c *Controller) startTaskPersister() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-c.lifecycleDone():
			c.flushTaskPersist()
			return
		case <-c.taskEngine.persistWake:
			c.flushTaskPersist()
		case <-ticker.C:
			c.flushTaskPersist()
		}
	}
}

// flushTaskPersist 在锁内取出并清空待落盘集合，再在锁外逐个 UpsertTask（避免持锁写库）。
func (c *Controller) flushTaskPersist() {
	if c.store == nil {
		return
	}
	c.taskEngine.mutex.Lock()
	if len(c.taskEngine.persistPending) == 0 {
		c.taskEngine.mutex.Unlock()
		return
	}
	pending := c.taskEngine.persistPending
	c.taskEngine.persistPending = make(map[string]TaskStatus)
	c.taskEngine.mutex.Unlock()

	for _, task := range pending {
		if err := c.store.UpsertTask(context.Background(), taskRecordFromStatus(task)); err != nil {
			slog.Warn("Failed to persist task status", "task_key", task.Key, "error", err)
		}
	}
}

func (c *Controller) startTask(key, taskType, message string, total int) bool {
	return c.startTaskWithOptionsCore(key, taskType, message, "", nil, total, false, false)
}

func (c *Controller) startCancelableTask(key, taskType, message string, total int) bool {
	return c.startTaskWithOptionsCore(key, taskType, message, "", nil, total, true, false)
}

func (c *Controller) startPausableCancelableTask(key, taskType, message string, total int) bool {
	return c.startTaskWithOptionsCore(key, taskType, message, "", nil, total, true, true)
}

// startTaskMsg 等是启动方法的 i18n 版：初始消息用稳定码 + 占位参数。
func (c *Controller) startTaskMsg(key, taskType, code string, params map[string]string, total int) bool {
	return c.startTaskWithOptionsCore(key, taskType, "", code, params, total, false, false)
}

func (c *Controller) startPausableCancelableTaskMsg(key, taskType, code string, params map[string]string, total int) bool {
	return c.startTaskWithOptionsCore(key, taskType, "", code, params, total, true, true)
}

func (c *Controller) startTaskWithOptionsCore(key, taskType, message, code string, params map[string]string, total int, canCancel bool, canPause bool) bool {
	c.taskEngine.mutex.Lock()
	defer c.taskEngine.mutex.Unlock()

	if c.taskEngine.tasks == nil {
		c.taskEngine.tasks = make(map[string]TaskStatus)
	}

	if existing, ok := c.taskEngine.tasks[key]; ok && taskIsActive(existing.Status) {
		return false
	}

	now := time.Now()
	c.taskEngine.seq++
	scope, scopeID := inferTaskScope(taskType, key)
	task := TaskStatus{
		Key:           key,
		Type:          taskType,
		Scope:         scope,
		ScopeID:       scopeID,
		Status:        "running",
		Message:       message,
		MessageCode:   code,
		MessageParams: params,
		Current:       0,
		Total:         total,
		CanCancel:     canCancel,
		CanPause:      canPause,
		Retryable:     c.isRetryableTaskType(taskType),
		StartedAt:     now,
		UpdatedAt:     now,
		Sequence:      c.taskEngine.seq,
	}
	c.taskEngine.tasks[key] = task
	c.pruneTasksLocked()
	c.persistTaskStatus(task)
	c.publishTaskStatusLocked(task)
	return true
}

func taskIsActive(status string) bool {
	return status == "running" || status == "paused" || status == "cancelling"
}

func (c *Controller) newTaskContext(key string) (context.Context, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	gate := taskcontrol.NewPauseGate()
	taskCtx := taskcontrol.WithPauseGate(ctx, gate)

	c.taskEngine.mutex.Lock()
	if c.taskEngine.runtimes == nil {
		c.taskEngine.runtimes = make(map[string]*TaskRuntime)
	}
	c.taskEngine.runtimes[key] = &TaskRuntime{
		Context:   taskCtx,
		Cancel:    cancel,
		PauseGate: gate,
		StartedAt: time.Now(),
	}
	c.taskEngine.mutex.Unlock()

	cleanup := func() {
		c.taskEngine.mutex.Lock()
		delete(c.taskEngine.runtimes, key)
		c.taskEngine.mutex.Unlock()
	}

	return taskCtx, cleanup
}

// applyTaskMessage 在任务上设置显示消息，保证 Message 与 MessageCode 互斥（后设者胜）：
// 传入 code 时走 i18n（记录 code+params、清空 Message），否则退回直接设置 Message（未迁移 i18n 的调用点）。
func applyTaskMessage(task *TaskStatus, message, code string, params map[string]string) {
	if code != "" {
		task.MessageCode = code
		task.MessageParams = params
		task.Message = ""
		return
	}
	if message != "" {
		task.Message = message
		task.MessageCode = ""
		task.MessageParams = nil
	}
}

func (c *Controller) updateTask(key string, current, total int, message string) {
	c.updateTaskCore(key, current, total, message, "", nil)
}

// updateTaskMsg 是 updateTask 的 i18n 版：只发稳定消息码 + 占位参数，由前端本地化渲染。
func (c *Controller) updateTaskMsg(key string, current, total int, code string, params map[string]string) {
	c.updateTaskCore(key, current, total, "", code, params)
}

func (c *Controller) updateTaskCore(key string, current, total int, message, code string, params map[string]string) {
	c.taskEngine.mutex.Lock()
	defer c.taskEngine.mutex.Unlock()

	task, ok := c.taskEngine.tasks[key]
	if !ok {
		return
	}
	task.Current = current
	if total >= 0 {
		task.Total = total
	}
	applyTaskMessage(&task, message, code, params)
	task.UpdatedAt = time.Now()
	c.taskEngine.seq++
	task.Sequence = c.taskEngine.seq
	enrichTaskProgress(&task)
	c.taskEngine.tasks[key] = task
	c.persistTaskStatus(task)
	c.publishTaskStatusLocked(task)
}

func (c *Controller) updateTaskDetails(key string, current, total int, message, phase, currentItem string, metrics map[string]int64, labels map[string]string) {
	c.updateTaskDetailsCore(key, current, total, message, "", nil, phase, currentItem, metrics, labels)
}

// updateTaskDetailsMsg 是 updateTaskDetails 的 i18n 版：消息改为稳定码 + 占位参数，其余（phase/currentItem/
// metrics/labels）语义不变。
func (c *Controller) updateTaskDetailsMsg(key string, current, total int, code string, params map[string]string, phase, currentItem string, metrics map[string]int64, labels map[string]string) {
	c.updateTaskDetailsCore(key, current, total, "", code, params, phase, currentItem, metrics, labels)
}

func (c *Controller) updateTaskDetailsCore(key string, current, total int, message, code string, params map[string]string, phase, currentItem string, metrics map[string]int64, labels map[string]string) {
	c.taskEngine.mutex.Lock()
	defer c.taskEngine.mutex.Unlock()

	task, ok := c.taskEngine.tasks[key]
	if !ok {
		return
	}
	if !taskIsActive(task.Status) {
		return
	}
	task.Current = current
	if total >= 0 {
		task.Total = total
	}
	applyTaskMessage(&task, message, code, params)
	if phase != "" {
		task.Phase = phase
	}
	if currentItem != "" {
		task.CurrentItem = currentItem
	}
	if len(metrics) > 0 {
		if task.Metrics == nil {
			task.Metrics = make(map[string]int64, len(metrics))
		}
		for k, v := range metrics {
			task.Metrics[k] = v
		}
	}
	if len(labels) > 0 {
		if task.Labels == nil {
			task.Labels = make(map[string]string, len(labels))
		}
		for k, v := range labels {
			task.Labels[k] = v
		}
	}
	task.UpdatedAt = time.Now()
	c.taskEngine.seq++
	task.Sequence = c.taskEngine.seq
	enrichTaskProgress(&task)
	c.taskEngine.tasks[key] = task
	c.persistTaskStatus(task)
	c.publishTaskStatusLocked(task)
}

func (c *Controller) setTaskMetadata(key string, params map[string]string, scopeName string) {
	c.taskEngine.mutex.Lock()
	defer c.taskEngine.mutex.Unlock()

	task, ok := c.taskEngine.tasks[key]
	if !ok {
		return
	}
	task.Params = params
	if strings.TrimSpace(scopeName) != "" {
		task.ScopeName = scopeName
	}
	c.taskEngine.seq++
	task.Sequence = c.taskEngine.seq
	hydrateTaskStatusDerivedFields(&task)
	c.taskEngine.tasks[key] = task
	c.persistTaskStatus(task)
	c.publishTaskStatusLocked(task)
}

func (c *Controller) mergeTaskParams(key string, params map[string]string) {
	if len(params) == 0 {
		return
	}
	c.taskEngine.mutex.Lock()
	defer c.taskEngine.mutex.Unlock()

	task, ok := c.taskEngine.tasks[key]
	if !ok {
		return
	}
	if task.Params == nil {
		task.Params = make(map[string]string, len(params))
	}
	for k, v := range params {
		task.Params[k] = v
	}
	task.UpdatedAt = time.Now()
	c.taskEngine.seq++
	task.Sequence = c.taskEngine.seq
	hydrateTaskStatusDerivedFields(&task)
	c.taskEngine.tasks[key] = task
	c.persistTaskStatus(task)
	c.publishTaskStatusLocked(task)
}

func (c *Controller) mergeRunningTaskMetricSums(key string, increments map[string]int64, params map[string]string) {
	c.taskEngine.mutex.Lock()
	defer c.taskEngine.mutex.Unlock()

	task, ok := c.taskEngine.tasks[key]
	if !ok || task.Status != "running" {
		return
	}
	if task.Params == nil {
		task.Params = make(map[string]string, len(params)+len(increments))
	}
	for k, v := range params {
		if strings.TrimSpace(v) != "" {
			task.Params[k] = v
		}
	}
	for k, inc := range increments {
		if inc == 0 {
			continue
		}
		current, _ := strconv.ParseInt(task.Params[k], 10, 64)
		task.Params[k] = strconv.FormatInt(current+inc, 10)
		if task.Metrics == nil {
			task.Metrics = make(map[string]int64)
		}
		task.Metrics[k] += inc
	}
	task.UpdatedAt = time.Now()
	c.taskEngine.seq++
	task.Sequence = c.taskEngine.seq
	hydrateTaskStatusDerivedFields(&task)
	c.taskEngine.tasks[key] = task
	c.persistTaskStatus(task)
	c.publishTaskStatusLocked(task)
}

func (c *Controller) acquireTaskStorageToken(ctx context.Context, libraryPath string, kind storageio.WorkKind) (config.ResolvedStoragePolicy, func(), time.Duration, time.Duration, error) {
	policy := config.ResolveStoragePolicy(c.currentConfig(), libraryPath)
	limit := minPositiveStorageLimit(policy.IOPolicy.HashConcurrency, policy.IOPolicy.ArchiveOpenConcurrency)
	if kind == storageio.WorkKindCoverBuild {
		limit = minPositiveStorageLimit(policy.IOPolicy.CoverConcurrency, policy.IOPolicy.ArchiveOpenConcurrency)
	}
	if limit <= 0 || strings.TrimSpace(policy.VolumeKey) == "" {
		return policy, func() {}, 0, 0, nil
	}
	lease, err := storageio.Default.Acquire(ctx, storageio.Request{
		VolumeKey:        policy.VolumeKey,
		Limit:            limit,
		Kind:             kind,
		PauseWhenReading: policy.IOPolicy.PauseBackgroundWhenReading,
		IdleOnly:         policy.IOPolicy.IdleOnlyHeavyTasks,
	})
	if err != nil {
		return policy, nil, lease.Wait, lease.PausedWait, err
	}
	return policy, lease.Release, lease.Wait, lease.PausedWait, nil
}

func minPositiveStorageLimit(values ...int) int {
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
		limit = minPositiveStorageLimit(limit, policy.IOPolicy.ArchiveOpenConcurrency)
	}
	if profile == scanner.ScanProfileIdentity || profile == scanner.ScanProfileRepair {
		limit = minPositiveStorageLimit(limit, policy.IOPolicy.HashConcurrency)
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

func (c *Controller) setTaskEffectiveLimit(key string, limit TaskLimits) {
	c.taskEngine.mutex.Lock()
	defer c.taskEngine.mutex.Unlock()

	task, ok := c.taskEngine.tasks[key]
	if !ok {
		return
	}
	task.EffectiveLimit = &limit
	task.UpdatedAt = time.Now()
	c.taskEngine.seq++
	task.Sequence = c.taskEngine.seq
	hydrateTaskStatusDerivedFields(&task)
	c.taskEngine.tasks[key] = task
	c.persistTaskStatus(task)
	c.publishTaskStatusLocked(task)
}

func (c *Controller) finishTask(key, message string) {
	c.completeTaskCore(key, "completed", message, "", nil)
}

// finishTaskMsg 是 finishTask 的 i18n 版：只发稳定消息码 + 占位参数。
func (c *Controller) finishTaskMsg(key, code string, params map[string]string) {
	c.completeTaskCore(key, "completed", "", code, params)
}

func (c *Controller) failTask(key, message string) {
	c.failTaskCore(key, message, "", nil, message)
}

// completeTaskMsg 是 completeTask 的 i18n 版（多用于取消态等终态）。
func (c *Controller) completeTaskMsg(key, status, code string, params map[string]string) {
	c.completeTaskCore(key, status, "", code, params)
}

func (c *Controller) completeTaskCore(key, status, message, code string, params map[string]string) {
	c.taskEngine.mutex.Lock()
	defer c.taskEngine.mutex.Unlock()

	task, ok := c.taskEngine.tasks[key]
	if !ok {
		return
	}
	now := time.Now()
	task.Status = status
	applyTaskMessage(&task, message, code, params)
	if status != "failed" {
		task.Error = ""
	}
	task.CanCancel = false
	task.CanPause = false
	task.CanResume = false
	task.PausedAt = nil
	task.PauseReason = ""
	if task.Total > 0 {
		task.Current = task.Total
	}
	task.UpdatedAt = now
	task.FinishedAt = &now
	c.taskEngine.seq++
	task.Sequence = c.taskEngine.seq
	c.taskEngine.tasks[key] = task
	delete(c.taskEngine.runtimes, key)
	c.persistTaskStatusFinal(task)
	c.publishTaskStatusLocked(task)
}

func (c *Controller) failTaskWithError(key, message, taskError string) {
	c.failTaskCore(key, message, "", nil, taskError)
}

// failTaskErrMsg 是 failTaskWithError 的 i18n 版：显示消息用稳定码，taskError 保留原始技术错误串（诊断用，不翻译）。
func (c *Controller) failTaskErrMsg(key, code string, params map[string]string, taskError string) {
	c.failTaskCore(key, "", code, params, taskError)
}

func (c *Controller) failTaskCore(key, message, code string, params map[string]string, taskError string) {
	c.taskEngine.mutex.Lock()
	defer c.taskEngine.mutex.Unlock()

	task, ok := c.taskEngine.tasks[key]
	if !ok {
		return
	}
	now := time.Now()
	task.Status = "failed"
	applyTaskMessage(&task, message, code, params)
	task.Error = taskError
	task.CanCancel = false
	task.CanPause = false
	task.CanResume = false
	task.PausedAt = nil
	task.UpdatedAt = now
	task.FinishedAt = &now
	c.taskEngine.seq++
	task.Sequence = c.taskEngine.seq
	c.taskEngine.tasks[key] = task
	delete(c.taskEngine.runtimes, key)
	c.persistTaskStatusFinal(task)
	c.publishTaskStatusLocked(task)
}

func (c *Controller) publishTaskStatusLocked(task TaskStatus) {
	payload, err := json.Marshal(task)
	if err != nil {
		slog.Warn("Failed to marshal task status", "task_key", task.Key, "error", err)
		return
	}
	// 统一经 sseBroker 投递（非阻塞、buffer 满则丢弃并告警）。
	c.sse.publish("task_progress:" + string(payload))
}

func (c *Controller) pruneTasksLocked() {
	if len(c.taskEngine.tasks) <= maxRetainedTasks {
		return
	}

	items := make([]TaskStatus, 0, len(c.taskEngine.tasks))
	for _, task := range c.taskEngine.tasks {
		items = append(items, task)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Sequence == items[j].Sequence {
			if items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
				if items[i].StartedAt.Equal(items[j].StartedAt) {
					return items[i].Key > items[j].Key
				}
				return items[i].StartedAt.After(items[j].StartedAt)
			}
			return items[i].UpdatedAt.After(items[j].UpdatedAt)
		}
		return items[i].Sequence > items[j].Sequence
	})

	next := make(map[string]TaskStatus, len(items))
	for _, task := range items {
		if len(next) >= maxRetainedTasks {
			break
		}
		next[task.Key] = task
	}
	c.taskEngine.tasks = next
}

func (c *Controller) listTasks(w http.ResponseWriter, r *http.Request) {
	statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))
	scopeFilter := strings.TrimSpace(r.URL.Query().Get("scope"))
	typeFilter := strings.TrimSpace(r.URL.Query().Get("type"))
	scopeIDFilter := strings.TrimSpace(r.URL.Query().Get("scope_id"))
	queryFilter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	limit := 0
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	var scopeID *int64
	if scopeIDFilter != "" {
		if parsed, err := strconv.ParseInt(scopeIDFilter, 10, 64); err == nil {
			scopeID = &parsed
		}
	}

	items, err := c.listTaskStatuses(r.Context(), database.TaskFilters{
		Status:  statusFilter,
		Scope:   scopeFilter,
		Type:    typeFilter,
		ScopeID: scopeID,
		Query:   queryFilter,
		Limit:   limit,
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to list tasks")
		return
	}
	jsonResponse(w, http.StatusOK, items)
}

func (c *Controller) listTaskStatuses(ctx context.Context, filters database.TaskFilters) ([]TaskStatus, error) {
	records, err := c.store.ListTasks(ctx, filters)
	if err != nil {
		return nil, err
	}
	c.taskEngine.mutex.Lock()
	if c.taskEngine.tasks == nil {
		c.taskEngine.tasks = make(map[string]TaskStatus)
	}
	items := make([]TaskStatus, 0, len(records)+len(c.taskEngine.tasks))
	seen := make(map[string]bool, len(records))
	for _, record := range records {
		task := taskStatusFromRecord(record)
		// 进度改为异步落盘后，活动任务的内存快照比 DB 记录更新（DB 可能滞后最多一个落盘周期）。
		// 同时存在于内存与 DB 时用内存版本，避免 API 返回被滞后的 DB 进度覆盖。
		if memTask, ok := c.taskEngine.tasks[task.Key]; ok {
			task = memTask
		}
		items = append(items, task)
		seen[task.Key] = true
	}
	for _, task := range c.taskEngine.tasks {
		if seen[task.Key] {
			continue
		}
		if filters.Status != "" && task.Status != filters.Status {
			continue
		}
		if filters.Scope != "" && task.Scope != filters.Scope {
			continue
		}
		if filters.Type != "" && task.Type != filters.Type {
			continue
		}
		if filters.ScopeID != nil && (task.ScopeID == nil || *task.ScopeID != *filters.ScopeID) {
			continue
		}
		if filters.Query != "" {
			haystack := strings.ToLower(task.Key + " " + task.Message + " " + task.Error)
			if !strings.Contains(haystack, strings.ToLower(filters.Query)) {
				continue
			}
		}
		items = append(items, task)
	}
	c.taskEngine.mutex.Unlock()

	sort.Slice(items, func(i, j int) bool {
		if items[i].Sequence == items[j].Sequence {
			if items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
				if items[i].StartedAt.Equal(items[j].StartedAt) {
					return items[i].Key > items[j].Key
				}
				return items[i].StartedAt.After(items[j].StartedAt)
			}
			return items[i].UpdatedAt.After(items[j].UpdatedAt)
		}
		return items[i].Sequence > items[j].Sequence
	})
	if filters.Limit > 0 && len(items) > filters.Limit {
		items = items[:filters.Limit]
	}
	return items, nil
}

func (c *Controller) clearTasks(w http.ResponseWriter, r *http.Request) {
	statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))
	scopeFilter := strings.TrimSpace(r.URL.Query().Get("scope"))
	typeFilter := strings.TrimSpace(r.URL.Query().Get("type"))
	scopeIDFilter := strings.TrimSpace(r.URL.Query().Get("scope_id"))
	var scopeID *int64
	if scopeIDFilter != "" {
		if parsed, err := strconv.ParseInt(scopeIDFilter, 10, 64); err == nil {
			scopeID = &parsed
		}
	}
	removed, err := c.store.DeleteTasks(r.Context(), database.TaskFilters{
		Status:  statusFilter,
		Scope:   scopeFilter,
		Type:    typeFilter,
		ScopeID: scopeID,
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to clear tasks")
		return
	}

	c.taskEngine.mutex.Lock()
	if c.taskEngine.tasks == nil {
		c.taskEngine.tasks = make(map[string]TaskStatus)
	}
	for key, task := range c.taskEngine.tasks {
		if statusFilter != "" && task.Status != statusFilter {
			continue
		}
		if scopeFilter != "" && task.Scope != scopeFilter {
			continue
		}
		if typeFilter != "" && task.Type != typeFilter {
			continue
		}
		if scopeID != nil && (task.ScopeID == nil || *task.ScopeID != *scopeID) {
			continue
		}
		if task.Status == "running" {
			continue
		}
		delete(c.taskEngine.tasks, key)
		// 同步清掉待落盘快照：否则异步落盘 goroutine 会把刚被 DeleteTasks 删掉的任务重新 UpsertTask 复活。
		delete(c.taskEngine.persistPending, key)
	}
	c.taskEngine.mutex.Unlock()

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

	c.taskEngine.mutex.Lock()
	task, ok := c.taskEngine.tasks[taskKey]
	c.taskEngine.mutex.Unlock()
	if !ok {
		records, err := c.store.ListTasks(r.Context(), database.TaskFilters{Query: taskKey, Limit: 20})
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "Failed to load task")
			return
		}
		for _, record := range records {
			if record.Key == taskKey {
				task = taskStatusFromRecord(record)
				ok = true
				break
			}
		}
		if !ok {
			jsonError(w, http.StatusNotFound, "Task not found")
			return
		}
	}
	if task.Status == "running" {
		jsonError(w, http.StatusConflict, "Task is already running")
		return
	}
	if !task.Retryable {
		jsonError(w, http.StatusConflict, "Task is not retryable")
		return
	}

	relaunch, ok := c.taskEngine.relaunchers[task.Type]
	if !ok {
		jsonError(w, http.StatusBadRequest, "Unsupported retry type")
		return
	}

	// 用本次重试请求自身的 Accept-Language 构造 ctx，供 relauncher（如 AI 分组）在无持久化 locale 时
	// 恢复语言，修复此前无条件硬编码 zh-CN。
	if err := relaunch(requestContextWithLocale(r), task); err != nil {
		if errors.Is(err, errTaskAlreadyRunning) {
			jsonError(w, http.StatusConflict, "Task is already running")
			return
		}
		// 区分错误语义：仅"已在运行"是 409，其它（缺少 scope、GetLibrary 失败等内部错误）返回 500，
		// 修复此前把所有重试失败一律误报为 409 的问题。
		slog.Error("Task retry failed", "task_key", taskKey, "task_type", task.Type, "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to retry task")
		return
	}

	jsonResponse(w, http.StatusAccepted, map[string]string{"message": "Task retry queued"})
}

func (c *Controller) pauseTask(w http.ResponseWriter, r *http.Request) {
	taskKey := chi.URLParam(r, "taskKey")
	if taskKey == "" {
		jsonError(w, http.StatusBadRequest, "Missing task key")
		return
	}

	c.taskEngine.mutex.Lock()
	task, ok := c.taskEngine.tasks[taskKey]
	if !ok {
		c.taskEngine.mutex.Unlock()
		jsonError(w, http.StatusNotFound, "Task not found")
		return
	}
	if task.Status != "running" {
		c.taskEngine.mutex.Unlock()
		jsonError(w, http.StatusConflict, "Task is not running")
		return
	}
	if !task.CanPause {
		c.taskEngine.mutex.Unlock()
		jsonError(w, http.StatusConflict, "Task cannot be paused")
		return
	}
	runtime := c.taskEngine.runtimes[taskKey]
	if runtime == nil || runtime.PauseGate == nil {
		c.taskEngine.mutex.Unlock()
		jsonError(w, http.StatusConflict, "Task pause gate is not available")
		return
	}
	now := time.Now()
	runtime.PauseGate.Pause()
	task.Status = "paused"
	task.CanPause = false
	task.CanResume = true
	task.PausedAt = &now
	task.PauseReason = "manual_pause"
	applyTaskMessage(&task, "", "task.msg.control.paused", nil)
	task.UpdatedAt = now
	c.taskEngine.seq++
	task.Sequence = c.taskEngine.seq
	enrichTaskProgress(&task)
	c.taskEngine.tasks[taskKey] = task
	c.persistTaskStatus(task)
	c.publishTaskStatusLocked(task)
	c.taskEngine.mutex.Unlock()

	jsonResponse(w, http.StatusAccepted, map[string]string{"message": "Task pause requested"})
}

func (c *Controller) resumeTask(w http.ResponseWriter, r *http.Request) {
	taskKey := chi.URLParam(r, "taskKey")
	if taskKey == "" {
		jsonError(w, http.StatusBadRequest, "Missing task key")
		return
	}

	c.taskEngine.mutex.Lock()
	task, ok := c.taskEngine.tasks[taskKey]
	if !ok {
		c.taskEngine.mutex.Unlock()
		jsonError(w, http.StatusNotFound, "Task not found")
		return
	}
	if task.Status != "paused" {
		c.taskEngine.mutex.Unlock()
		jsonError(w, http.StatusConflict, "Task is not paused")
		return
	}
	runtime := c.taskEngine.runtimes[taskKey]
	if runtime == nil || runtime.PauseGate == nil {
		c.taskEngine.mutex.Unlock()
		jsonError(w, http.StatusConflict, "Task pause gate is not available")
		return
	}
	runtime.PauseGate.Resume()
	task.Status = "running"
	task.CanPause = true
	task.CanResume = false
	task.PausedAt = nil
	task.PauseReason = ""
	applyTaskMessage(&task, "", "task.msg.control.resumed", nil)
	task.UpdatedAt = time.Now()
	c.taskEngine.seq++
	task.Sequence = c.taskEngine.seq
	enrichTaskProgress(&task)
	c.taskEngine.tasks[taskKey] = task
	c.persistTaskStatus(task)
	c.publishTaskStatusLocked(task)
	c.taskEngine.mutex.Unlock()

	jsonResponse(w, http.StatusAccepted, map[string]string{"message": "Task resumed"})
}

func (c *Controller) cancelTask(w http.ResponseWriter, r *http.Request) {
	taskKey := chi.URLParam(r, "taskKey")
	if taskKey == "" {
		jsonError(w, http.StatusBadRequest, "Missing task key")
		return
	}

	c.taskEngine.mutex.Lock()
	task, ok := c.taskEngine.tasks[taskKey]
	if !ok {
		c.taskEngine.mutex.Unlock()
		jsonError(w, http.StatusNotFound, "Task not found")
		return
	}
	if task.Status != "running" && task.Status != "paused" {
		c.taskEngine.mutex.Unlock()
		jsonError(w, http.StatusConflict, "Task is not running")
		return
	}
	if !task.CanCancel {
		c.taskEngine.mutex.Unlock()
		jsonError(w, http.StatusConflict, "Task cannot be cancelled")
		return
	}
	runtime := c.taskEngine.runtimes[taskKey]
	if runtime == nil || runtime.Cancel == nil {
		c.taskEngine.mutex.Unlock()
		jsonError(w, http.StatusConflict, "Task cancellation is not available")
		return
	}

	runtime.Cancel()
	if runtime.PauseGate != nil {
		runtime.PauseGate.Resume()
	}
	task.CanCancel = false
	task.CanPause = false
	task.CanResume = false
	task.Status = "cancelling"
	applyTaskMessage(&task, "", "task.msg.control.cancelling", nil)
	task.UpdatedAt = time.Now()
	c.taskEngine.seq++
	task.Sequence = c.taskEngine.seq
	c.taskEngine.tasks[taskKey] = task
	c.persistTaskStatus(task)
	c.publishTaskStatusLocked(task)
	c.taskEngine.mutex.Unlock()

	jsonResponse(w, http.StatusAccepted, map[string]string{"message": "Task cancellation requested"})
}
