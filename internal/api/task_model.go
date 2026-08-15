// 本文件是任务子域的**模型层**，只放纯函数——TaskStatus 与数据库 TaskRecord 之间的双向
// 转换、派生字段（percent/rate/eta、phase/metrics/labels/limit 的 params 编解码）、消息码与
// 直接消息的互斥规则，以及快照深拷贝。这里的函数不碰共享状态、不加锁、不做 IO，因此可被
// 引擎（task_engine.go）与 HTTP 层（controller_tasks.go）同时调用而无需考虑时序；一旦某个
// 函数需要读写 taskEngine 的字段，它就该搬到 task_engine.go 去。

package api

import (
	"strconv"
	"strings"
	"time"

	"manga-manager/internal/database"
)

// inferTaskScope 从任务类型与 key 推断作用域（system/library/series）及其 id。
// key 的约定是 "<动作>_<对象>_<id>"，末段能解析成整数时即为 scope id。
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

// taskIsActive 判断任务是否处于「仍在占用运行槽位」的状态。
// cancelling 也算活动态：取消已请求但任务体尚未收尾，此时不应允许同 key 再次启动。
func taskIsActive(status string) bool {
	return status == "running" || status == "paused" || status == "cancelling"
}

// applyTaskMessage 在任务上设置显示消息。消息词汇只有 i18n 码一种，因此它只收码与占位参数；
// 空码是无操作，用于「这一帧不改文案」。
//
// 清空 Message 不是可省的防御：它与 MessageCode 必须互斥，理由见 TaskStatus.Message 的字段 doc。
func applyTaskMessage(task *TaskStatus, code string, params map[string]string) {
	if code == "" {
		return
	}
	task.MessageCode = code
	// 克隆而不是存下调用方那份：任务上的每个可变 map 都归引擎所有，没有例外——
	// 「这几个归引擎、那个不归」是记不住的，而记错一次的后果见 taskEngine 的符号 doc。
	task.MessageParams = cloneStringMap(params)
	task.Message = ""
}

// cloneTaskStatus 返回 task 的深拷贝：**每一个**引用类型字段都要复制，不只是那几个 map。
// 任何会让快照逃出 taskEngine.mutex 临界区的路径（异步落盘、HTTP 序列化、重试取值）都必须先克隆，
// 否则调用方读到的是仍在被写入的活 map。
//
// 给 TaskStatus 加引用类型字段的人必须顺手加到这里——漏掉不会有编译错误，后果是那个字段
// 在锁外被读、同时被持锁的进度回调写。EffectiveLimit 就是这样一个非 map 的引用字段
// （hydrateTaskStatusDerivedFields 会经 applyTaskLimitParam 穿过这个指针写）。
func cloneTaskStatus(task TaskStatus) TaskStatus {
	task.MessageParams = cloneStringMap(task.MessageParams)
	task.Labels = cloneStringMap(task.Labels)
	task.Params = cloneStringMap(task.Params)
	if task.Metrics != nil {
		metrics := make(map[string]int64, len(task.Metrics))
		for k, v := range task.Metrics {
			metrics[k] = v
		}
		task.Metrics = metrics
	}
	if task.EffectiveLimit != nil {
		limits := *task.EffectiveLimit
		task.EffectiveLimit = &limits
	}
	return task
}

func cloneStringMap(src map[string]string) map[string]string {
	if src == nil {
		return nil
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
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
