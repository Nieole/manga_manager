// 业务说明：本文件由 controller.go 拆分而来，属于后端 API 层的任务引擎子域，负责任务状态模型、进度/指标聚合、持久化、生命周期（启动/更新/暂停/恢复/取消/完成）与任务列表接口。

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/scanner"
	"manga-manager/internal/storageio"
	"manga-manager/internal/taskcontrol"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

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

func isRetryableTaskType(taskType string) bool {
	switch taskType {
	case "scan_library", "scan_series", "cleanup_library", "rebuild_index", "rebuild_thumbnails", "scrape", "ai_grouping", "rebuild_book_hashes", "rebuild_file_identities", "reconcile_koreader_progress":
		return true
	case "refresh_koreader_matching":
		return true
	default:
		return false
	}
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
	if c.taskPersistPending == nil {
		c.taskPersistPending = make(map[string]TaskStatus)
	}
	c.taskPersistPending[task.Key] = task
}

// persistTaskStatusFinal 用于任务终态（完成/失败/取消）：仍走同一异步队列（保持单一写入方、不与
// 进度写乱序），但额外唤醒落盘 goroutine 立即刷，缩短终态落库延迟。调用方持有 taskMutex。
func (c *Controller) persistTaskStatusFinal(task TaskStatus) {
	c.persistTaskStatus(task)
	select {
	case c.taskPersistWake <- struct{}{}:
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
		case <-c.taskPersistWake:
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
	c.taskMutex.Lock()
	if len(c.taskPersistPending) == 0 {
		c.taskMutex.Unlock()
		return
	}
	pending := c.taskPersistPending
	c.taskPersistPending = make(map[string]TaskStatus)
	c.taskMutex.Unlock()

	for _, task := range pending {
		if err := c.store.UpsertTask(context.Background(), taskRecordFromStatus(task)); err != nil {
			slog.Warn("Failed to persist task status", "task_key", task.Key, "error", err)
		}
	}
}

func (c *Controller) startTask(key, taskType, message string, total int) bool {
	return c.startTaskWithOptions(key, taskType, message, total, false, false)
}

func (c *Controller) startCancelableTask(key, taskType, message string, total int) bool {
	return c.startTaskWithOptions(key, taskType, message, total, true, false)
}

func (c *Controller) startPausableCancelableTask(key, taskType, message string, total int) bool {
	return c.startTaskWithOptions(key, taskType, message, total, true, true)
}

func (c *Controller) startTaskWithOptions(key, taskType, message string, total int, canCancel bool, canPause bool) bool {
	c.taskMutex.Lock()
	defer c.taskMutex.Unlock()

	if c.tasks == nil {
		c.tasks = make(map[string]TaskStatus)
	}

	if existing, ok := c.tasks[key]; ok && taskIsActive(existing.Status) {
		return false
	}

	now := time.Now()
	c.taskSeq++
	scope, scopeID := inferTaskScope(taskType, key)
	task := TaskStatus{
		Key:       key,
		Type:      taskType,
		Scope:     scope,
		ScopeID:   scopeID,
		Status:    "running",
		Message:   message,
		Current:   0,
		Total:     total,
		CanCancel: canCancel,
		CanPause:  canPause,
		Retryable: isRetryableTaskType(taskType),
		StartedAt: now,
		UpdatedAt: now,
		Sequence:  c.taskSeq,
	}
	c.tasks[key] = task
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

	c.taskMutex.Lock()
	if c.taskRuntimes == nil {
		c.taskRuntimes = make(map[string]*TaskRuntime)
	}
	c.taskRuntimes[key] = &TaskRuntime{
		Context:   taskCtx,
		Cancel:    cancel,
		PauseGate: gate,
		StartedAt: time.Now(),
	}
	c.taskMutex.Unlock()

	cleanup := func() {
		c.taskMutex.Lock()
		delete(c.taskRuntimes, key)
		c.taskMutex.Unlock()
	}

	return taskCtx, cleanup
}

func (c *Controller) updateTask(key string, current, total int, message string) {
	c.taskMutex.Lock()
	defer c.taskMutex.Unlock()

	task, ok := c.tasks[key]
	if !ok {
		return
	}
	task.Current = current
	if total >= 0 {
		task.Total = total
	}
	if message != "" {
		task.Message = message
	}
	task.UpdatedAt = time.Now()
	c.taskSeq++
	task.Sequence = c.taskSeq
	enrichTaskProgress(&task)
	c.tasks[key] = task
	c.persistTaskStatus(task)
	c.publishTaskStatusLocked(task)
}

func (c *Controller) updateTaskDetails(key string, current, total int, message, phase, currentItem string, metrics map[string]int64, labels map[string]string) {
	c.taskMutex.Lock()
	defer c.taskMutex.Unlock()

	task, ok := c.tasks[key]
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
	if message != "" {
		task.Message = message
	}
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
	c.taskSeq++
	task.Sequence = c.taskSeq
	enrichTaskProgress(&task)
	c.tasks[key] = task
	c.persistTaskStatus(task)
	c.publishTaskStatusLocked(task)
}

func (c *Controller) setTaskMetadata(key string, params map[string]string, scopeName string) {
	c.taskMutex.Lock()
	defer c.taskMutex.Unlock()

	task, ok := c.tasks[key]
	if !ok {
		return
	}
	task.Params = params
	if strings.TrimSpace(scopeName) != "" {
		task.ScopeName = scopeName
	}
	c.taskSeq++
	task.Sequence = c.taskSeq
	hydrateTaskStatusDerivedFields(&task)
	c.tasks[key] = task
	c.persistTaskStatus(task)
	c.publishTaskStatusLocked(task)
}

func (c *Controller) mergeTaskParams(key string, params map[string]string) {
	if len(params) == 0 {
		return
	}
	c.taskMutex.Lock()
	defer c.taskMutex.Unlock()

	task, ok := c.tasks[key]
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
	c.taskSeq++
	task.Sequence = c.taskSeq
	hydrateTaskStatusDerivedFields(&task)
	c.tasks[key] = task
	c.persistTaskStatus(task)
	c.publishTaskStatusLocked(task)
}

func (c *Controller) mergeRunningTaskMetricSums(key string, increments map[string]int64, params map[string]string) {
	c.taskMutex.Lock()
	defer c.taskMutex.Unlock()

	task, ok := c.tasks[key]
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
	c.taskSeq++
	task.Sequence = c.taskSeq
	hydrateTaskStatusDerivedFields(&task)
	c.tasks[key] = task
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
	c.taskMutex.Lock()
	defer c.taskMutex.Unlock()

	task, ok := c.tasks[key]
	if !ok {
		return
	}
	task.EffectiveLimit = &limit
	task.UpdatedAt = time.Now()
	c.taskSeq++
	task.Sequence = c.taskSeq
	hydrateTaskStatusDerivedFields(&task)
	c.tasks[key] = task
	c.persistTaskStatus(task)
	c.publishTaskStatusLocked(task)
}

func (c *Controller) finishTask(key, message string) {
	c.completeTask(key, "completed", message)
}

func (c *Controller) failTask(key, message string) {
	c.failTaskWithError(key, message, message)
}

func (c *Controller) completeTask(key, status, message string) {
	c.taskMutex.Lock()
	defer c.taskMutex.Unlock()

	task, ok := c.tasks[key]
	if !ok {
		return
	}
	now := time.Now()
	task.Status = status
	task.Message = message
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
	c.taskSeq++
	task.Sequence = c.taskSeq
	c.tasks[key] = task
	delete(c.taskRuntimes, key)
	c.persistTaskStatusFinal(task)
	c.publishTaskStatusLocked(task)
}

func (c *Controller) failTaskWithError(key, message, taskError string) {
	c.taskMutex.Lock()
	defer c.taskMutex.Unlock()

	task, ok := c.tasks[key]
	if !ok {
		return
	}
	now := time.Now()
	task.Status = "failed"
	task.Message = message
	task.Error = taskError
	task.CanCancel = false
	task.CanPause = false
	task.CanResume = false
	task.PausedAt = nil
	task.UpdatedAt = now
	task.FinishedAt = &now
	c.taskSeq++
	task.Sequence = c.taskSeq
	c.tasks[key] = task
	delete(c.taskRuntimes, key)
	c.persistTaskStatusFinal(task)
	c.publishTaskStatusLocked(task)
}

func (c *Controller) publishTaskStatusLocked(task TaskStatus) {
	if c.messages == nil {
		return
	}
	payload, err := json.Marshal(task)
	if err != nil {
		slog.Warn("Failed to marshal task status", "task_key", task.Key, "error", err)
		return
	}
	select {
	case c.messages <- "task_progress:" + string(payload):
	default:
		slog.Warn("SSE message channel full, dropping task progress", "task_key", task.Key)
	}
}

func (c *Controller) pruneTasksLocked() {
	if len(c.tasks) <= maxRetainedTasks {
		return
	}

	items := make([]TaskStatus, 0, len(c.tasks))
	for _, task := range c.tasks {
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
	c.tasks = next
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
	c.taskMutex.Lock()
	if c.tasks == nil {
		c.tasks = make(map[string]TaskStatus)
	}
	items := make([]TaskStatus, 0, len(records)+len(c.tasks))
	seen := make(map[string]bool, len(records))
	for _, record := range records {
		task := taskStatusFromRecord(record)
		// 进度改为异步落盘后，活动任务的内存快照比 DB 记录更新（DB 可能滞后最多一个落盘周期）。
		// 同时存在于内存与 DB 时用内存版本，避免 API 返回被滞后的 DB 进度覆盖。
		if memTask, ok := c.tasks[task.Key]; ok {
			task = memTask
		}
		items = append(items, task)
		seen[task.Key] = true
	}
	for _, task := range c.tasks {
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
	c.taskMutex.Unlock()

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

	c.taskMutex.Lock()
	if c.tasks == nil {
		c.tasks = make(map[string]TaskStatus)
	}
	for key, task := range c.tasks {
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
		delete(c.tasks, key)
		// 同步清掉待落盘快照：否则异步落盘 goroutine 会把刚被 DeleteTasks 删掉的任务重新 UpsertTask 复活。
		delete(c.taskPersistPending, key)
	}
	c.taskMutex.Unlock()

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

	c.taskMutex.Lock()
	task, ok := c.tasks[taskKey]
	c.taskMutex.Unlock()
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

	var err error
	switch task.Type {
	case "scan_library":
		if task.ScopeID == nil {
			err = fmt.Errorf("missing library id")
			break
		}
		lib, getErr := c.store.GetLibrary(r.Context(), *task.ScopeID)
		if getErr != nil {
			err = getErr
			break
		}
		force := task.Params != nil && task.Params["force"] == "true"
		if !c.launchLibraryScanTask(lib, force) {
			err = fmt.Errorf("task already running")
		}
	case "scan_series":
		if task.ScopeID == nil {
			err = fmt.Errorf("missing series id")
			break
		}
		force := task.Params != nil && task.Params["force"] == "true"
		if !c.launchSeriesScanTask(*task.ScopeID, force) {
			err = fmt.Errorf("task already running")
		}
	case "cleanup_library":
		if task.ScopeID == nil {
			err = fmt.Errorf("missing library id")
			break
		}
		if !c.launchCleanupLibraryTask(*task.ScopeID) {
			err = fmt.Errorf("task already running")
		}
	case "rebuild_index":
		err = c.launchRebuildIndexTask()
	case "rebuild_thumbnails":
		err = c.launchRebuildThumbnailsTask()
	case "scrape":
		err = c.retryScrapeTask(task)
	case "ai_grouping":
		if task.ScopeID == nil {
			err = fmt.Errorf("missing library id")
			break
		}
		if !c.launchAIGroupingTask(*task.ScopeID, "zh-CN") {
			err = fmt.Errorf("task already running")
		}
	case "rebuild_book_hashes":
		err = c.launchRebuildBookHashesTask()
	case "rebuild_file_identities":
		err = c.launchRebuildFileIdentitiesTask()
	case "reconcile_koreader_progress":
		err = c.launchReconcileKOReaderProgressTask()
	case "refresh_koreader_matching":
		err = c.launchRefreshKOReaderMatchingTask()
	default:
		err = fmt.Errorf("unsupported retry type")
	}

	if err != nil {
		jsonError(w, http.StatusConflict, fmt.Sprintf("Retry failed: %v", err))
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

	c.taskMutex.Lock()
	task, ok := c.tasks[taskKey]
	if !ok {
		c.taskMutex.Unlock()
		jsonError(w, http.StatusNotFound, "Task not found")
		return
	}
	if task.Status != "running" {
		c.taskMutex.Unlock()
		jsonError(w, http.StatusConflict, "Task is not running")
		return
	}
	if !task.CanPause {
		c.taskMutex.Unlock()
		jsonError(w, http.StatusConflict, "Task cannot be paused")
		return
	}
	runtime := c.taskRuntimes[taskKey]
	if runtime == nil || runtime.PauseGate == nil {
		c.taskMutex.Unlock()
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
	task.Message = "任务已暂停，等待继续执行"
	task.UpdatedAt = now
	c.taskSeq++
	task.Sequence = c.taskSeq
	enrichTaskProgress(&task)
	c.tasks[taskKey] = task
	c.persistTaskStatus(task)
	c.publishTaskStatusLocked(task)
	c.taskMutex.Unlock()

	jsonResponse(w, http.StatusAccepted, map[string]string{"message": "Task pause requested"})
}

func (c *Controller) resumeTask(w http.ResponseWriter, r *http.Request) {
	taskKey := chi.URLParam(r, "taskKey")
	if taskKey == "" {
		jsonError(w, http.StatusBadRequest, "Missing task key")
		return
	}

	c.taskMutex.Lock()
	task, ok := c.tasks[taskKey]
	if !ok {
		c.taskMutex.Unlock()
		jsonError(w, http.StatusNotFound, "Task not found")
		return
	}
	if task.Status != "paused" {
		c.taskMutex.Unlock()
		jsonError(w, http.StatusConflict, "Task is not paused")
		return
	}
	runtime := c.taskRuntimes[taskKey]
	if runtime == nil || runtime.PauseGate == nil {
		c.taskMutex.Unlock()
		jsonError(w, http.StatusConflict, "Task pause gate is not available")
		return
	}
	runtime.PauseGate.Resume()
	task.Status = "running"
	task.CanPause = true
	task.CanResume = false
	task.PausedAt = nil
	task.PauseReason = ""
	task.Message = "任务已继续执行"
	task.UpdatedAt = time.Now()
	c.taskSeq++
	task.Sequence = c.taskSeq
	enrichTaskProgress(&task)
	c.tasks[taskKey] = task
	c.persistTaskStatus(task)
	c.publishTaskStatusLocked(task)
	c.taskMutex.Unlock()

	jsonResponse(w, http.StatusAccepted, map[string]string{"message": "Task resumed"})
}

func (c *Controller) cancelTask(w http.ResponseWriter, r *http.Request) {
	taskKey := chi.URLParam(r, "taskKey")
	if taskKey == "" {
		jsonError(w, http.StatusBadRequest, "Missing task key")
		return
	}

	c.taskMutex.Lock()
	task, ok := c.tasks[taskKey]
	if !ok {
		c.taskMutex.Unlock()
		jsonError(w, http.StatusNotFound, "Task not found")
		return
	}
	if task.Status != "running" && task.Status != "paused" {
		c.taskMutex.Unlock()
		jsonError(w, http.StatusConflict, "Task is not running")
		return
	}
	if !task.CanCancel {
		c.taskMutex.Unlock()
		jsonError(w, http.StatusConflict, "Task cannot be cancelled")
		return
	}
	runtime := c.taskRuntimes[taskKey]
	if runtime == nil || runtime.Cancel == nil {
		c.taskMutex.Unlock()
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
	task.Message = "正在取消任务..."
	task.UpdatedAt = time.Now()
	c.taskSeq++
	task.Sequence = c.taskSeq
	c.tasks[taskKey] = task
	c.persistTaskStatus(task)
	c.publishTaskStatusLocked(task)
	c.taskMutex.Unlock()

	jsonResponse(w, http.StatusAccepted, map[string]string{"message": "Task cancellation requested"})
}
