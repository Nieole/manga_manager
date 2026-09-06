// 本文件是任务子域的**模型层**：领域的**运行**快照与对外 RunStatus 之间的翻译、列表谓词的翻译、
// 身份四要素在两侧的互转，以及进度派生字段（percent/rate/eta）的计算。
//
// 这里的函数不碰可变状态、不加锁、不做 IO。唯一的例外是 runStatusFrom 这个方法：它要问一句
// 「这个（类型，**变体**）注册了**重启函数**吗」，而那张注册表是装配期填好、此后只读的。
// 一旦某个函数需要读写 taskEngine 受锁保护的字段，它就该搬到 task_engine.go 去。

package api

import (
	"strings"
	"time"

	"manga-manager/internal/task"
)

// taskIsActive 判断任务是否处于「仍在占用运行槽位」的状态。
// cancelling 也算活动态：取消已请求但任务体尚未收尾，此时不应允许同一个任务再次发起。
func taskIsActive(status string) bool {
	return task.RunStatus(status).IsActive()
}

// ---- 身份四要素在两侧的互转 ----

// domain 把 api 侧的身份翻成领域侧的。
//
// 作用域 id 由 `*int64` 变成 `int64`，nil 落成 0：身份的唯一约束要比较这一列，而 SQL 里
// NULL 不等于 NULL——留 NULL 的话同一个系统级身份会被建出任意多条（见 task.Identity）。
func (id TaskIdentity) domain() task.Identity {
	identity := task.Identity{
		Type:    task.Type(id.taskType),
		Scope:   task.Scope(id.scope),
		Variant: task.Variant(id.variant),
	}
	if id.scopeID != nil {
		identity.ScopeID = *id.scopeID
	}
	return identity
}

// taskIdentityFromDomain 是 domain 的逆：0 号作用域 id 还原成 nil，好让它在 JSON 里 omitempty。
func taskIdentityFromDomain(identity task.Identity) TaskIdentity {
	converted := TaskIdentity{
		taskType: string(identity.Type),
		scope:    string(identity.Scope),
		variant:  TaskVariant(identity.Variant),
	}
	if identity.ScopeID != 0 {
		scopeID := identity.ScopeID
		converted.scopeID = &scopeID
	}
	return converted
}

// ---- 列表谓词 ----

// taskFilters 是六个任务端点共用的过滤参数，由 taskFiltersFromQuery 从查询串解析而来。
//
// 它是 HTTP 侧的形状，不是落盘侧的形状：进库之前一律先经 runFilterFrom 翻成 task.RunFilter。
// 空串与零值一律表示「这一条不过滤」。
type taskFilters struct {
	Status  string
	Scope   string
	Type    string
	ScopeID *int64
	Query   string
	Limit   int
}

// runFilterFrom 把六个任务端点共用的过滤参数翻成运行查询的谓词。
//
// 五条谓词整条下推到落盘侧，不再取回内存里过一遍：旧引擎必须在内存里判，因为它要先把内存表盖在
// 库记录上；现在只有一个来源，下推之后 Limit 截断的才是过滤**之后**的那一页。
func runFilterFrom(filters taskFilters, order task.RunOrder) task.RunFilter {
	filter := task.RunFilter{
		Scope:   task.Scope(strings.TrimSpace(filters.Scope)),
		ScopeID: filters.ScopeID,
		Query:   strings.TrimSpace(filters.Query),
		Order:   order,
		Limit:   filters.Limit,
	}
	if status := strings.TrimSpace(filters.Status); status != "" {
		filter.Statuses = []task.RunStatus{task.RunStatus(status)}
	}
	if taskType := strings.TrimSpace(filters.Type); taskType != "" {
		filter.Types = []task.Type{task.Type(taskType)}
	}
	return filter
}

// ---- 快照翻译 ----

// runStatusFrom 把一帧领域快照翻成对外的运行快照。
//
// 三处来源各司其职：运行行给展示态与计数，控制能力由引擎按运行的活性**派生**（不是库里的列——
// 落成列的话重启后那几个布尔值会集体说谎），侧数据给指标、标签、重启入参与并发上限。
func (e *taskEngine) runStatusFrom(snapshot task.Snapshot, identity TaskIdentity) RunStatus {
	run := snapshot.Run
	status := RunStatus{
		RunID:               run.ID,
		Key:                 run.Key,
		Type:                identity.taskType,
		Scope:               identity.scope,
		ScopeID:             identity.scopeID,
		Variant:             identity.variant,
		ScopeName:           run.ScopeName,
		Status:              string(run.Status),
		MessageCode:         run.MessageCode,
		MessageParams:       run.MessageParams,
		Error:               run.Error,
		Current:             run.Current,
		Total:               run.Total,
		CanCancel:           snapshot.Capabilities.CanCancel,
		CanPause:            snapshot.Capabilities.CanPause,
		CanResume:           snapshot.Capabilities.CanResume,
		Retryable:           e.isRetryableTask(identity.taskType, identity.variant),
		PausedAt:            run.PausedAt,
		PauseReason:         string(run.PauseReason),
		ControlPausedMillis: run.ControlPausedMillis,
		Phase:               run.Phase,
		CurrentItem:         run.CurrentItem,
		Metrics:             snapshot.Side.Metrics,
		Labels:              snapshot.Side.Labels,
		Params:              snapshot.Side.Args,
		StartedAt:           run.StartedAt,
		UpdatedAt:           run.UpdatedAt,
		FinishedAt:          run.FinishedAt,
		Sequence:            run.Sequence,
	}
	if snapshot.Side.Limits != nil {
		status.EffectiveLimit = taskLimitsFromDomain(*snapshot.Side.Limits)
	}
	enrichTaskProgress(&status)
	return status
}

// taskLimitsFromDomain 与 domain 是一对：并发上限在两侧是同一组真列，逐个搬。
func taskLimitsFromDomain(limits task.Limits) *TaskLimits {
	return &TaskLimits{
		ScanProfile:                limits.ScanProfile,
		ScannerWorkersConfigured:   limits.ScannerWorkersConfigured,
		ScannerWorkersEffective:    limits.ScannerWorkersEffective,
		StorageProfile:             limits.StorageProfile,
		VolumeKey:                  limits.VolumeKey,
		ScanConcurrency:            limits.ScanConcurrency,
		ArchiveOpenConcurrency:     limits.ArchiveOpenConcurrency,
		CoverConcurrency:           limits.CoverConcurrency,
		HashConcurrency:            limits.HashConcurrency,
		PauseBackgroundWhenReading: limits.PauseBackgroundWhenReading,
		IdleOnlyHeavyTasks:         limits.IdleOnlyHeavyTasks,
		DisableSameDiskPageCache:   limits.DisableSameDiskPageCache,
	}
}

// domain 把 api 侧的并发上限翻成领域侧的。
func (limits TaskLimits) domain() task.Limits {
	return task.Limits{
		ScanProfile:                limits.ScanProfile,
		ScannerWorkersConfigured:   limits.ScannerWorkersConfigured,
		ScannerWorkersEffective:    limits.ScannerWorkersEffective,
		StorageProfile:             limits.StorageProfile,
		VolumeKey:                  limits.VolumeKey,
		ScanConcurrency:            limits.ScanConcurrency,
		ArchiveOpenConcurrency:     limits.ArchiveOpenConcurrency,
		CoverConcurrency:           limits.CoverConcurrency,
		HashConcurrency:            limits.HashConcurrency,
		PauseBackgroundWhenReading: limits.PauseBackgroundWhenReading,
		IdleOnlyHeavyTasks:         limits.IdleOnlyHeavyTasks,
		DisableSameDiskPageCache:   limits.DisableSameDiskPageCache,
	}
}

func firstNonEmptyTaskValue(preferred, fallback string) string {
	if strings.TrimSpace(preferred) != "" {
		return preferred
	}
	return fallback
}

// enrichTaskProgress 按任务当前的计数与已耗时重算进度派生字段：百分比、速率、ETA。
//
// 三个字段一律先清空再算，不做累积：它总是在上一帧的快照上被调用，留着旧值就等于让这一帧
// 带上一帧的数——终态那句自相矛盾的 `2 / 2` 配 `50.0%` 正是这样来的。
//
// ETA 只属于**活动态**。**终态**的任务不会再动，「预计剩余时间」无从谈起，显示出来会让用户
// 以为它还在跑；对可重试的**中断**任务尤其误导。终态要看的是已经做完了多少（计数与百分比）
// 与花了多久（详情面板的开始 / 结束时刻），这两样都不经 ETA 这条通道。
//
// 速率只算给分母可信的状态，**中断**一个都不发，理由见函数内那道闸门。分母里还要扣掉**暂停**：
// 那几段时间里任务一条都没处理，引擎逐段记下过（RunStatus.ControlPausedMillis），不扣的话
// 一次午饭时长的暂停就能把速率打到七分之一，并一路带进终态。
func enrichTaskProgress(task *RunStatus) {
	if task == nil {
		return
	}
	task.Percent = nil
	task.RatePerMinute = 0
	task.EtaSeconds = nil

	if task.Total > 0 {
		percent := float64(task.Current) * 100 / float64(task.Total)
		if percent > 100 {
			percent = 100
		}
		task.Percent = &percent
	}
	// **中断**任务不发速率。它当年立起来的理由——中断那一笔 UPDATE 把 finished_at 与
	// updated_at 一起盖成重启时刻，分母里因此整段停机时长都算成了在干活——在新模型里
	// 已经不成立：收尾时刻取的是那行原有的心跳。**拆掉它是一次用户可见的行为变化**，
	// 因此不随接线顺手做。
	if task.Status == "interrupted" {
		return
	}
	now := time.Now()
	elapsed := now.Sub(task.StartedAt)
	if !taskIsActive(task.Status) && task.FinishedAt != nil {
		elapsed = task.FinishedAt.Sub(task.StartedAt)
	}
	// 扣掉**暂停**：那段时间里任务一条都没处理，留在分母里等于把「等用户回来」算成了在干活。
	elapsed -= taskPausedSoFar(*task, now)
	seconds := elapsed.Seconds()
	if seconds <= 0 || task.Current <= 0 {
		return
	}
	task.RatePerMinute = float64(task.Current) * 60 / seconds
	if taskIsActive(task.Status) && task.Total > task.Current {
		eta := int64(float64(task.Total-task.Current) / task.RatePerMinute * 60)
		task.EtaSeconds = &eta
	}
}

// taskPausedSoFar 返回这个任务至今的暂停总时长：已经折进累计的那些，加上此刻仍在进行的这一次。
//
// 仍在进行的那一段只在**已暂停**下计入：**取消中**的任务已被放行、正在收尾，它的 PausedAt
// 由 cancel 那一刻折进累计后清掉；**终态**同理由收尾清掉。
func taskPausedSoFar(task RunStatus, now time.Time) time.Duration {
	total := time.Duration(task.ControlPausedMillis) * time.Millisecond
	if task.Status == "paused" && task.PausedAt != nil {
		if ongoing := now.Sub(*task.PausedAt); ongoing > 0 {
			total += ongoing
		}
	}
	return total
}
