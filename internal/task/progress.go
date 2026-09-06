// **运行句柄**那三条写入通道在引擎这一侧的落点：整帧上报、按键合并重启入参、按键累加指标。
// 三条的准入判据都是**活动态**，理由各不相同，见各自的 doc。

package task

import (
	"context"
	"log/slog"

	"manga-manager/internal/runhandle"
)

// report 把一帧上报写进运行行并按节流水位投递。
//
// 运行进入**终态**后一律忽略：**扫描观察者**不在任务体的调用栈上，晚一拍很常见，
// 放行会把一条已经收尾的运行在界面上拽回运行中。**排队中**同样忽略——它还没开跑。
func (e *Engine) report(runID int64, frame runhandle.Frame) {
	e.mu.Lock()
	defer e.mu.Unlock()

	ctx := context.Background()
	run, ok := e.loadActiveLocked(ctx, runID)
	if !ok {
		return
	}
	if frame.Current != nil {
		run.Current = *frame.Current
	}
	if frame.Total != nil {
		run.Total = *frame.Total
	}
	applyMessage(&run, Result{Code: frame.Code, Params: frame.Params})
	// **阶段切换**落一条事件，相邻两条相减即是那一段的耗时。判据是「与上一条不同」而不是
	// 「这一帧带了阶段」：扫描器每 250ms 报一次，报的多数是同一个阶段名，照单全收会让事件流
	// 变成第二个日志，而时间线上会挤满零长的段。
	if frame.Phase != "" && frame.Phase != run.Phase {
		run.Phase = frame.Phase
		e.emitEventLocked(runID, Event{Kind: EventPhase, Phase: frame.Phase})
	}
	if frame.Item != "" {
		run.CurrentItem = frame.Item
	}
	if len(frame.Metrics) > 0 {
		if err := e.store.SetRunMetrics(ctx, runID, frame.Metrics); err != nil {
			slog.Warn("Failed to persist run metrics", "run_id", runID, "error", err)
		}
	}
	if len(frame.Labels) > 0 {
		if err := e.store.MergeRunLabels(ctx, runID, frame.Labels); err != nil {
			slog.Warn("Failed to persist run labels", "run_id", runID, "error", err)
		}
	}
	e.touchLocked(&run)
}

// mergeArgs 按键合并重启入参。判据与另外两条一致，都是活动态：一份迟到的报文落在已收尾的
// 运行上，会把它的序号推到任务中心最前、再投递一帧已经作废的展示态。
func (e *Engine) mergeArgs(runID int64, args map[string]string) {
	if len(args) == 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	ctx := context.Background()
	run, ok := e.loadActiveLocked(ctx, runID)
	if !ok {
		return
	}
	e.storeArgsLocked(ctx, runID, args)
	e.touchLocked(&run)
}

// addMetrics 按键累加指标增量，并顺带补齐随同一份报文而来的描述性入参。
//
// 判据必须是**活动态**而不是运行中：**已暂停**与**取消中**的任务体仍在收尾，此刻丢掉的是
// 累加值——它们不像整帧那样带着全量当前值，丢一次就永远补不回来。
func (e *Engine) addMetrics(runID int64, increments map[string]int64, args map[string]string) {
	if len(increments) == 0 && len(args) == 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	ctx := context.Background()
	run, ok := e.loadActiveLocked(ctx, runID)
	if !ok {
		return
	}
	e.storeArgsLocked(ctx, runID, args)
	if len(increments) > 0 {
		if err := e.store.AddRunMetrics(ctx, runID, increments); err != nil {
			slog.Warn("Failed to persist run metrics", "run_id", runID, "error", err)
		}
	}
	e.touchLocked(&run)
}

// storeArgsLocked 把重启入参交给侧表，空表是无操作。调用方持锁。
func (e *Engine) storeArgsLocked(ctx context.Context, runID int64, args map[string]string) {
	if len(args) == 0 {
		return
	}
	if err := e.store.MergeRunArgs(ctx, runID, args); err != nil {
		slog.Warn("Failed to persist run args", "run_id", runID, "error", err)
	}
}

// loadActiveLocked 取回一条仍在**活动态**的运行；查不到、读不出或已不活动都返回 false。
// 调用方持锁。
func (e *Engine) loadActiveLocked(ctx context.Context, runID int64) (Run, bool) {
	run, err := e.store.LoadRun(ctx, runID)
	if err != nil || !run.Status.IsActive() {
		return Run{}, false
	}
	return run, true
}

// touchLocked 记下这次变化并按节流水位投递：编号、盖时刻、落盘、投递四步一起做。
// 拆开做会漏掉其中一步而没有编译错误——漏编号则这条变化不改变它在任务中心的位置，
// 漏落盘则重启后这段进度消失。调用方持锁。
func (e *Engine) touchLocked(run *Run) {
	run.UpdatedAt = e.clock()
	run.Sequence = e.nextSequenceLocked()
	e.saveLocked(*run)
	e.publishProgressLocked(*run)
}
