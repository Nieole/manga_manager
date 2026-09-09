// 本文件是任务子域的**模型层**：领域的 task.Snapshot 与对外 RunSnapshot 之间的翻译、列表谓词的翻译、
// 身份四要素在两侧的互转，以及进度派生字段（percent/rate/eta）的计算。
//
// 这里的函数不碰可变状态、不加锁、不做 IO。唯一的例外是 runSnapshotFrom 这个方法：它要问一句
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

// runLiveSummaryFrom 把领域的**实况汇总**搬成对外形状。序号不搬：它属于推送信封（见 RunPush），
// 而整份实况帧是拉回来的，链上没有它的位置。
func runLiveSummaryFrom(live task.Live) RunLiveSummary {
	return RunLiveSummary{
		Active:    live.Active,
		Queued:    live.Queued,
		Slots:     live.Slots,
		Paused:    live.Paused,
		PausedAll: live.PausedAll,
	}
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

// taskFilters 是任务端点共用的过滤参数，由 taskFiltersFromQuery 从查询串解析而来。
//
// 它是 HTTP 侧的形状，不是落盘侧的形状：进库之前一律先翻成落盘侧的谓词——列表与清除走
// runFilterFrom（一次运行一行），任务清单走 taskListFilterFrom（一个任务一行）。
// 空串与零值一律表示「这一条不过滤」。
type taskFilters struct {
	Status string
	Scope  string
	Type   string
	// TaskID 是任务中心展开某一行时用的谓词：只要这个任务的历次运行。
	// 展开按需取，因此清单接口本身一条运行都不带回来。
	TaskID  int64
	ScopeID *int64
	Query   string
	Limit   int
}

// runFilterFrom 把任务端点共用的过滤参数翻成**运行**查询的谓词。
//
// 谓词整条下推到落盘侧，不取回内存里过一遍：只有一个来源，下推之后 Limit 截断的才是
// 过滤**之后**的那一页。
func runFilterFrom(filters taskFilters, order task.RunOrder) task.RunFilter {
	filter := task.RunFilter{
		TaskID:  filters.TaskID,
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

// taskListFilterFrom 把同一份过滤参数翻成**任务清单**的谓词。
//
// 分工在这里定死：类型、作用域与作用域 id 是任务身份上的列，状态与关键词判在这个任务
// **最近一次运行**上——也就是清单那一栏「上次结果」。同一排筛选器因此只有一个语义，
// 而不是三条筛任务、两条筛运行。条数上限对两层是同一个数：清单一行就是一个任务。
func taskListFilterFrom(filters taskFilters) task.TaskFilter {
	filter := task.TaskFilter{
		Scope:        task.Scope(strings.TrimSpace(filters.Scope)),
		ScopeID:      filters.ScopeID,
		LastRunQuery: strings.TrimSpace(filters.Query),
		Limit:        filters.Limit,
	}
	if status := strings.TrimSpace(filters.Status); status != "" {
		filter.LastRunStatuses = []task.RunStatus{task.RunStatus(status)}
	}
	if taskType := strings.TrimSpace(filters.Type); taskType != "" {
		filter.Types = []task.Type{task.Type(taskType)}
	}
	return filter
}

// ---- 快照翻译 ----

// runSnapshotFrom 把一帧领域快照翻成对外的运行快照。
//
// 三处来源各司其职：运行行给展示态与计数，控制能力由引擎按运行的活性**派生**（不是库里的列——
// 落成列的话重启后那几个布尔值会集体说谎），侧数据给指标、标签、重启入参与并发上限。
func (e *taskEngine) runSnapshotFrom(snapshot task.Snapshot, identity TaskIdentity) RunSnapshot {
	run := snapshot.Run
	snap := RunSnapshot{
		RunID:               run.ID,
		TaskID:              run.TaskID,
		Type:                identity.taskType,
		Scope:               identity.scope,
		ScopeID:             identity.scopeID,
		Variant:             identity.variant,
		ScopeName:           run.ScopeName,
		Trigger:             string(run.Trigger),
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
		CoalescedCount:      run.CoalescedCount,
		Phase:               run.Phase,
		CurrentItem:         run.CurrentItem,
		Metrics:             snapshot.Side.Metrics,
		Labels:              snapshot.Side.Labels,
		Params:              snapshot.Side.Args,
		UpdatedAt:           run.UpdatedAt,
		FinishedAt:          run.FinishedAt,
		Sequence:            run.Sequence,
	}
	// 零值时刻在契约里是「还没开跑」，不是公元 1 年：领域侧把**排队中**运行的开始时刻留作零值，
	// 等它真的进入运行中才补写（那一段排队不属于速率的分母）。翻译只在这一处做。
	if !run.StartedAt.IsZero() {
		startedAt := run.StartedAt
		snap.StartedAt = &startedAt
	}
	if snapshot.Side.Limits != nil {
		snap.EffectiveLimit = runLimitsFromDomain(*snapshot.Side.Limits)
	}
	enrichRunProgress(&snap)
	return snap
}

// runLimitsFromDomain 与 domain 是一对：并发上限在两侧是同一组真列，逐个搬。
func runLimitsFromDomain(limits task.Limits) *RunLimits {
	return &RunLimits{
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
func (limits RunLimits) domain() task.Limits {
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

// enrichRunProgress 按这次运行当前的计数与已耗时重算进度派生字段：百分比、速率、ETA。
//
// 三个字段一律先清空再算，不做累积：它总是在上一帧的快照上被调用，留着旧值就等于让这一帧
// 带上一帧的数——终态那句自相矛盾的 `2 / 2` 配 `50.0%` 正是这样来的。
//
// ETA 只属于**活动态**。**终态**的任务不会再动，「预计剩余时间」无从谈起，显示出来会让用户
// 以为它还在跑；对可重试的**中断**任务尤其误导。终态要看的是已经做完了多少（计数与百分比）
// 与花了多久（详情面板的开始 / 结束时刻），这两样都不经 ETA 这条通道。
//
// 速率的分母是运行真正在干活的那一段。活动态量到此刻，**终态**量到引擎盖上的结束时刻——
// **中断**也不例外：那一笔批量转写把结束时刻取成运行最后一次上报的时刻
// （见 task.Engine.MarkInterrupted），整段停机时长因此落在分母之外。分母里还要扣掉**暂停**：
// 那几段时间里任务一条都没处理，引擎逐段记下过（RunSnapshot.ControlPausedMillis），不扣的话
// 一次午饭时长的暂停就能把速率打到七分之一，并一路带进终态。
//
// 速率的缺席只由数据决定，不得由状态决定：分母非正或计数为零就不发。从未上报过进度的运行
// 两条都占——它最后一次上报的时刻仍是开始时刻。**排队中**的运行连开始时刻都还没有，
// 它的分母无从谈起，因此百分比之外的两个数一律不发。
func enrichRunProgress(task *RunSnapshot) {
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
	if task.StartedAt == nil {
		return
	}
	now := time.Now()
	elapsed := now.Sub(*task.StartedAt)
	if !taskIsActive(task.Status) && task.FinishedAt != nil {
		elapsed = task.FinishedAt.Sub(*task.StartedAt)
	}
	// 扣掉**暂停**：那段时间里任务一条都没处理，留在分母里等于把「等用户回来」算成了在干活。
	elapsed -= runPausedSoFar(*task, now)
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

// runPausedSoFar 返回这次运行至今的暂停总时长：已经折进累计的那些，加上此刻仍在进行的这一次。
//
// 仍在进行的那一段只在**已暂停**下计入：**取消中**的运行已被放行、正在收尾，它的 PausedAt
// 由 cancel 那一刻折进累计后清掉；**终态**同理由收尾清掉。
func runPausedSoFar(task RunSnapshot, now time.Time) time.Duration {
	total := time.Duration(task.ControlPausedMillis) * time.Millisecond
	if task.Status == "paused" && task.PausedAt != nil {
		if ongoing := now.Sub(*task.PausedAt); ongoing > 0 {
			total += ongoing
		}
	}
	return total
}
