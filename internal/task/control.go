// 控制动作与读取面：暂停 / 恢复 / 取消作用在**运行**上，重启时的批量转**中断**，以及停机。
// 引擎只表达「为什么不行」，HTTP 状态码与英文文案属于 api。

package task

import (
	"context"
	"errors"
	"time"
)

// 控制动作的哨兵错误。
var (
	ErrRunNotRunning      = errors.New("run is not running")
	ErrRunNotPaused       = errors.New("run is not paused")
	ErrRunNotPausable     = errors.New("run cannot be paused")
	ErrRunNotCancelable   = errors.New("run cannot be cancelled")
	ErrRunNotControllable = errors.New("run has no live control handle")
)

// RunSnapshot 取一条运行此刻的快照：运行行加上它的控制能力。
func (e *Engine) RunSnapshot(ctx context.Context, runID int64) (Snapshot, error) {
	run, err := e.store.LoadRun(ctx, runID)
	if err != nil {
		return Snapshot{}, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.snapshotLocked(run), nil
}

// ListSnapshots 按谓词取一批运行的快照。
//
// 侧数据一次批量取回，不是逐条运行走 snapshotLocked：一页有几十条运行，逐条取就是几十轮查询。
func (e *Engine) ListSnapshots(ctx context.Context, filter RunFilter) ([]Snapshot, error) {
	runs, err := e.store.ListRuns(ctx, filter)
	if err != nil {
		return nil, err
	}
	runIDs := make([]int64, 0, len(runs))
	for _, run := range runs {
		runIDs = append(runIDs, run.ID)
	}
	side, err := e.store.LoadRunSideData(ctx, runIDs)
	if err != nil {
		return nil, err
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	snapshots := make([]Snapshot, 0, len(runs))
	for _, run := range runs {
		snapshots = append(snapshots, Snapshot{
			Run:          run,
			Capabilities: e.capabilitiesLocked(run),
			Side:         side[run.ID],
		})
	}
	return snapshots, nil
}

// Pause 按下这条运行的**暂停闸门**：任务体停在下一个可中断点，状态如实写作**已暂停**。
func (e *Engine) Pause(runID int64) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	run, err := e.store.LoadRun(context.Background(), runID)
	if err != nil {
		return err
	}
	return e.pauseLocked(run, PauseReasonManual)
}

// PauseAll 是「全部暂停」：把每条运行中的运行逐个按下**暂停闸门**，返回按下的条数。
//
// 它不是第二套机制，只是逐个按下同一个闸门——因此**不可暂停的运行不受影响**（它们没有可中断点，
// ComicInfo 回写那类每本书都是一次原子替换），而被按下的运行状态如实写作**已暂停**。
//
// 按下与写状态按**每条运行**成对做（都在 pauseLocked 里），而不是先按下全部闸门再统一写状态：
// 后者的两段之间界面会读到一批闸门已按下、状态却还写着运行中的运行。理由与停机时逐个取消并放行
// 相同（见 StopAll）。
func (e *Engine) PauseAll(ctx context.Context) (int, error) {
	return e.controlEach(ctx, StatusRunning, func(run Run) error {
		return e.pauseLocked(run, PauseReasonPauseAll)
	})
}

// pauseLocked 按下一条运行的闸门并落定**已暂停**。调用方持锁。
//
// 闸门与状态在同一次调用里成对落下：这两半分开做，就等于让界面在中间那段时间里说谎。
func (e *Engine) pauseLocked(run Run, reason PauseReason) error {
	if run.Status != StatusRunning {
		return ErrRunNotRunning
	}
	rt, err := e.controlHandleLocked(run.ID)
	if err != nil {
		return err
	}
	if !rt.canPause {
		return ErrRunNotPausable
	}

	now := e.clock()
	rt.gate.Pause()
	run.Status = StatusPaused
	run.PausedAt = &now
	run.PauseReason = reason
	applyMessage(&run, Result{Code: e.codes.Paused})
	e.commitControlLocked(&run, now)
	return nil
}

// Resume 放行**暂停闸门**，并把这一次暂停折进累计。
func (e *Engine) Resume(runID int64) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	run, err := e.store.LoadRun(context.Background(), runID)
	if err != nil {
		return err
	}
	return e.resumeLocked(run)
}

// ResumeAll 是「全部恢复」：把每条**已暂停**的运行逐个放行，返回放行的条数。
//
// 它认状态而不认**暂停原因**：一条被单独按下的运行同样会被它放行。判据只留一处——按原因分拣的话，
// 用户按下「全部恢复」之后界面上还剩着几条已暂停，而那个按钮已经灰掉了。
func (e *Engine) ResumeAll(ctx context.Context) (int, error) {
	return e.controlEach(ctx, StatusPaused, e.resumeLocked)
}

// controlEach 把某个状态下的每条运行逐个交给 control，返回真正动到的条数。
//
// **逐个**是它的全部意义：每一条的闸门与状态在同一次 control 调用里一起落下，而不是先把全部闸门
// 按下（或放行）再统一改状态——那两段之间界面读到的是一批说着谎的运行。
//
// 单条被拒不中断整批，但只有控制哨兵才算「拒」：不可暂停的运行、以及进程里没有句柄的那些
// （上一轮留在库里的活动运行）本来就该跳过。其余错误（落盘故障之类）整批中止并上报——
// 一律吞掉的话，端点会拿着「按下了 0 条」回一个 202，用户看不出是没得按还是根本没按成。
func (e *Engine) controlEach(ctx context.Context, status RunStatus, control func(Run) error) (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	runs, err := e.store.ListRuns(ctx, RunFilter{Statuses: []RunStatus{status}, Order: OrderSequenceAsc})
	if err != nil {
		return 0, err
	}
	affected := 0
	for _, run := range runs {
		switch err := control(run); {
		case err == nil:
			affected++
		case errors.Is(err, ErrRunNotRunning), errors.Is(err, ErrRunNotPaused),
			errors.Is(err, ErrRunNotPausable), errors.Is(err, ErrRunNotControllable):
		default:
			return affected, err
		}
	}
	return affected, nil
}

// resumeLocked 放行一条运行的闸门并落定运行中。调用方持锁。
func (e *Engine) resumeLocked(run Run) error {
	if run.Status != StatusPaused {
		return ErrRunNotPaused
	}
	rt, err := e.controlHandleLocked(run.ID)
	if err != nil {
		return err
	}

	now := e.clock()
	rt.gate.Resume()
	run.Status = StatusRunning
	absorbPause(&run, now)
	applyMessage(&run, Result{Code: e.codes.Resumed})
	e.commitControlLocked(&run, now)
	return nil
}

// Cancel 请求取消。**排队中**的运行当场进**已取消**——它从未开跑，也从未占过槽位，
// 没有任务体需要收尾；活动运行则先进**取消中**，等任务体自己走到收尾。
func (e *Engine) Cancel(runID int64) error {
	e.mu.Lock()
	launch, err := e.cancelLocked(runID)
	e.mu.Unlock()
	if err != nil {
		return err
	}
	// 排队中那条走的是终态那条路，因此可能腾出位置放行下一条；放行必须在锁外。
	if launch != nil {
		launch()
	}
	return nil
}

// cancelLocked 是取消的两条分支。调用方持锁；返回的 launch 必须在锁外调用。
func (e *Engine) cancelLocked(runID int64) (func(), error) {
	run, err := e.store.LoadRun(context.Background(), runID)
	if err != nil {
		return nil, err
	}

	if run.Status == StatusQueued {
		entry, ok := e.queued[runID]
		if !ok || !entry.spec.CanCancel {
			return nil, ErrRunNotCancelable
		}
		return e.finalizeLocked(runID, StatusCancelled, Result{Code: entry.spec.CancelCode}, ""), nil
	}
	if run.Status != StatusRunning && run.Status != StatusPaused {
		return nil, ErrRunNotRunning
	}
	rt, err := e.controlHandleLocked(runID)
	if err != nil {
		return nil, err
	}
	if !rt.canCancel {
		return nil, ErrRunNotCancelable
	}

	now := e.clock()
	rt.cancel()
	// 一并放行**暂停闸门**：暂停中的任务体卡在闸门上，只取消不放行会让它永远收不到取消信号。
	rt.gate.Resume()
	run.Status = StatusCancelling
	// 闸门刚被放行，这次暂停到此为止：不在这里结账的话，从暂停直接取消的运行会把整段暂停
	// 留在分母里，一路带进已取消那一帧。
	absorbPause(&run, now)
	applyMessage(&run, Result{Code: e.codes.Cancelling})
	e.commitControlLocked(&run, now)
	return nil, nil
}

// commitControlLocked 落定一次控制动作引起的状态跃迁：编号、盖时刻、落盘、无条件投递。
//
// 控制动作一律不走节流：它们是用户按下按钮后在等的那一帧，吞掉一条界面就停在错误的状态上。
// 调用方持锁。
func (e *Engine) commitControlLocked(run *Run, now time.Time) {
	run.UpdatedAt = now
	run.Sequence = e.nextSequenceLocked()
	e.saveLocked(*run)
	e.publishLocked(*run)
}

// MarkInterrupted 把仍会变化的运行（**活动态**与**排队中**）全部转入**中断**，返回转写的条数。
//
// 它属于装配期：进程里此刻不该有任何任务体在飞。**中断**不是失败——任务体没有出错，
// 只是没跑完，因此可重试；哪些类型可以自己接着跑属于**可续跑**白名单，不在本包。
//
// 结束时刻取的是运行原来的 UpdatedAt 而不是此刻：进度落盘本来每隔一小段就刷一次，
// 那个字段本身就是心跳。盖成重启时刻的话，整段停机时长都会被算成在干活，速率随之作废。
func (e *Engine) MarkInterrupted(ctx context.Context) (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	runs, err := e.store.ListRuns(ctx, RunFilter{Statuses: liveStatuses, Order: OrderSequenceAsc})
	if err != nil {
		return 0, err
	}
	now := e.clock()
	marked := 0
	for _, run := range runs {
		heartbeat := run.UpdatedAt
		run.Status = StatusInterrupted
		// 上一轮那次暂停就此结账：不折进累计的话，速率的分母里会凭空少掉那一段。
		absorbPause(&run, heartbeat)
		applyMessage(&run, Result{Code: e.codes.Interrupted})
		run.PausedAt = nil
		run.FinishedAt = &heartbeat
		run.UpdatedAt = now
		run.Sequence = e.nextSequenceLocked()
		delete(e.runtimes, run.ID)
		delete(e.queued, run.ID)
		delete(e.gates, run.ID)
		e.saveLocked(run)
		e.publishLocked(run)
		marked++
	}
	return marked, nil
}

// StopAll 在停机时逐个取消运行的 ctx 并放行它的**暂停闸门**，顺序与单条取消一致。
// 先在锁内收集句柄再在锁外调用：取消与放行会唤醒任务体，而任务体的上报要拿同一把锁。
//
// 取消与放行按**每条运行**成对做，而不是先放行全部再取消全部：后者的两段之间有一个窗口，
// 被放行的已暂停运行拿着尚未取消的 ctx 回去干活——过可中断点、取**存储令牌**、跑完一整个任务体。
//
// 放行本身不能省：暂停闸门的等待有一条不带 ctx 的分支（只等放行），只取消 ctx 唤不醒它。
func (e *Engine) StopAll() {
	e.mu.Lock()
	runtimes := make([]*taskRuntime, 0, len(e.runtimes))
	for _, rt := range e.runtimes {
		if rt != nil {
			runtimes = append(runtimes, rt)
		}
	}
	e.mu.Unlock()

	for _, rt := range runtimes {
		rt.cancel()
		rt.gate.Resume()
	}
}
