// Task observation adapters translate domain snapshots and live frames into transport contracts.

package api

import (
	"encoding/json"
	"log/slog"
	"manga-manager/internal/task"
)

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

// publisher 把领域快照翻成对外的任务快照并交给 SSE。publish 为 nil 时整条通道不接。
//
// 序列化在这里做而不是在领域里：事件名与 JSON 形状属于传输层。
func (e *taskEngine) publisher(publish func(string)) func(task.Snapshot) {
	if publish == nil {
		return nil
	}
	return func(snapshot task.Snapshot) {
		snap := e.runSnapshotFrom(snapshot, e.cachedIdentity(snapshot.Run.TaskID))
		e.push(publish, runSnapshotEventPrefix, RunPush{Sequence: snap.Sequence, Run: &snap})
	}
}

// livePublisher 把一帧领域的**实况汇总**翻成对外形状并交给 SSE。publish 为 nil 时整条通道不接。
func (e *taskEngine) livePublisher(publish func(string)) func(task.Live) {
	if publish == nil {
		return nil
	}
	return func(live task.Live) {
		summary := runLiveSummaryFrom(live)
		e.push(publish, runLiveEventPrefix, RunPush{Sequence: live.Sequence, Live: &summary})
	}
}

// push 把一帧接到**投递链**上再交给 SSE：补齐 Prev、序列化、带上事件名投出去。
//
// 统一经 sseBroker 投递（非阻塞、buffer 满则丢弃并告警）——丢帧靠序号链让前端自己发现，
// 不在这里改成阻塞：投递方此刻在引擎的临界区里，堵在这里等于把整个任务子域堵住。
//
// 接链在序列化**之前**：序列化炸了这一帧就没了，而链已经往前走，于是下一帧接不上、
// 前端整份重拉一次——那正是对的。反过来（炸了就不接链）会让这次丢帧无声无息。
func (e *taskEngine) push(publish func(string), event string, frame RunPush) {
	frame.Prev = e.linkPush(frame.Sequence)
	payload, err := json.Marshal(frame)
	if err != nil {
		slog.Warn("Failed to marshal a task push frame", "event", event, "error", err)
		return
	}
	publish(event + string(payload))
}
