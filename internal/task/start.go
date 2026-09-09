// 启动仪式：**唯一**一处把运行放进库里的地方，以及它连着的槽位闸门、**运行句柄**、
// 四条终态里由任务体裁决的那三条，与槽位释放后的放行。

package task

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"

	"manga-manager/internal/runhandle"
	"manga-manager/internal/taskcontrol"
)

// ErrInvalidRunSpec 是运行声明填不齐时的哨兵错误。
var ErrInvalidRunSpec = errors.New("run declaration is incomplete")

// RunSpec 是一份运行声明：「这是一次什么运行」的完整描述，一次性交给引擎。
//
// 整份声明必须原子落地。拆成启动之后的多次补写，会留下一个「运行已经出现在列表里、
// 却还没有上限与入参」的窗口——那是列表接口能观察到的，而补写的那几帧还会被首帧
// 刚写下的节流水位吞掉。
type RunSpec struct {
	// Identity 是这次运行属于哪个任务。四要素显式给出，不从字符串里反解。
	Identity Identity
	// Trigger 是**发起方**：谁叫来的。
	Trigger Trigger

	// Key 是这次运行的**任务键**：一个给人读的串，本包不解释它，也**不落盘**——它唯一的去处是
	// 交给 DecorateRunContext，由那里挂上任务体的 ctx，好让沿途每一行日志都带着它（ADR 0007：
	// 键退出寻址，不退出日志）。谁也不能从它反解身份，身份是四要素显式给出的那份。
	Key string
	// ScopeName 是作用域在界面上的显示名，是**过渡期**字段，原样落到运行上，本包不解释它。
	ScopeName string

	// StartCode 与 StartParams 是起始文案的 i18n 码与占位参数。
	//
	// 名字必须与 Args 拉开距离：这一路落进运行行的文案占位参数，Args 那一路落进重启入参。
	// 两者接反不会有编译错误，后果是**重启函数**读不回原始入参，静默回落到默认值。
	StartCode   string
	StartParams map[string]string

	Total     int
	CanCancel bool
	CanPause  bool

	// Args 是重启入参：**重启函数**从这里读回这次运行是拿什么参数发起的。
	Args map[string]string
	// Labels 是启动时就已知、整次运行不变的展示标签（刮削源名等）。
	// 开跑之后才变的标签走 runhandle.Frame.Labels，两条都是按键合并。
	Labels map[string]string
	// Limits 是这次运行实际生效的并发上限。零值表示没有上限可报（多数维护类工作如此），
	// 引擎不会为它凭空落一份全零的上限。
	Limits Limits

	// 三条由任务体裁决的**终态**各自的**默认**文案码。常规运行因此不必为收尾写任何代码；
	// 「部分成功」「第一阶段失败」这类变体由任务体经 Result.Code 覆盖对应的一条。
	CompleteCode string
	CancelCode   string
	FailCode     string
}

func (s RunSpec) validate() error {
	if err := s.Identity.Validate(); err != nil {
		return err
	}
	switch s.Trigger {
	case TriggerManual, TriggerScheduled, TriggerWatch, TriggerChained, TriggerResumed:
		return nil
	case "":
		return fmt.Errorf("%w: 缺少发起方", ErrInvalidRunSpec)
	default:
		return fmt.Errorf("%w: 未知的发起方 %q", ErrInvalidRunSpec, s.Trigger)
	}
}

// Result 是任务体对终态文案的可选修正。零值表示「用运行声明里的默认码」，
// 而不是「把文案清空」——绝大多数任务体返回的正是零值。
type Result struct {
	Code   string
	Params map[string]string
}

// orDefault 用运行声明里那条分支的默认码补齐没被覆盖的文案。
func (r Result) orDefault(code string) Result {
	if r.Code == "" {
		r.Code = code
	}
	return r
}

// Body 是任务体：干活，以及经交给它的**运行句柄**上报。
// 它不自己判**终态**、不自己起 goroutine、也不接触自己那条运行的 id。
type Body func(ctx context.Context, handle *runhandle.Handle) (Result, error)

// Launched 是一次发起落地成了什么：哪一条运行接住了它，以及本次交出的任务体会不会执行。
//
// 两者必须一起交出去。只交运行的话，调用方分不出「这条运行是本次建的」与「本次被**合并**进了
// 一条别人排下的运行」——而后者的任务体不会执行，等在它身上的调用方会永远等下去。
type Launched struct {
	Run Run
	// Coalesced 为真表示本次发起被合并进了 Run 那一条已经排着的运行：
	// 它跑的是同一件事，本次交出的任务体不会执行。
	Coalesced bool
	// Stalled 非空表示这次**自动**发起被**停发**挡下：一条运行都没建（Run 是零值），
	// 交出的任务体不会执行，而它就是挡下的原因。
	//
	// 它不是错误：连败到停发、退避没到期、人工禁用，三者都是系统按用户的设置正常工作。
	// 调用方要么什么都不做（守护扫描），要么据此不接着做下一步（监听器的清理）。
	Stalled StallReason
}

// Start 是往库里放一条运行的**唯一入口**。
//
// 返回的运行可能是运行中，也可能是**排队中**——槽位满、同一个任务已有活动运行、或者
// 「全部暂停」的闸门关着，这次发起就先排队，条件一满足由引擎放行。**冲突不再被丢弃**。
//
// 这个任务已经有一条排队中的运行时，本次发起**合并**进那一条：不新建，只把它的合并计数加一，
// 返回的正是那条排队运行。守护扫描要的只是「确保扫过」，合并与各排一条效果相同，
// 而后者会堆成一串一模一样的运行。被合并掉的那份任务体不会执行——排在前面的那条跑的是同一件事。
//
// **自动**发起（定时、监听）还要多过一道**停发**闸门：连败到阈值、**退避**没到期、或者
// 人工禁用了这个任务，本次就一条运行都不建，交回的 Launched.Stalled 说明为什么。手动发起
// 从不经过那道闸门，反而在那里把连败与退避清零。
//
// 刻意保留的不变量：槽位闸门**同步**执行、任务体**异步**执行。Start 返回时运行已在列表里、
// 而任务体尚未开跑，HTTP 层才能立即返回而不被任务体阻塞。
//
// ctx 只管这一次准入期间的落盘查询，不是任务体的 ctx：任务体那份由引擎另建，
// 因此它活得比发起它的那个请求久。
func (e *Engine) Start(ctx context.Context, spec RunSpec, body Body) (Launched, error) {
	if err := spec.validate(); err != nil {
		return Launched{}, err
	}
	if body == nil {
		return Launched{}, fmt.Errorf("%w: 缺少任务体", ErrInvalidRunSpec)
	}
	owner, err := e.store.EnsureTask(ctx, spec.Identity)
	if err != nil {
		return Launched{}, err
	}
	// 停发闸门与准入在**同一个临界区**里：那一行长期属性另有一个写入方（收尾那一侧），
	// 分成两段就会有一次复位与一次失败计数互相覆盖。
	e.mu.Lock()
	stalled := e.gateLaunchLocked(ctx, &owner, spec.Trigger)
	if stalled != StallNone {
		e.mu.Unlock()
		return Launched{Stalled: stalled}, nil
	}
	launched, launch, err := e.admitLocked(ctx, owner, spec, body)
	e.mu.Unlock()
	if err != nil {
		return Launched{}, err
	}
	// 放在锁外：注入同步执行版的测试装置会当场跑完任务体，而任务体的每一次上报都要这把锁。
	if launch != nil {
		launch()
	}
	return launched, nil
}

// admitLocked 是新运行落地的**唯一**机制：过任务闸门与槽位闸门，然后编号、落盘、写侧数据、
// 建**运行时句柄**、投递首帧。调用方持锁；返回的 launch 必须在锁外调用。
//
// 这几步漏掉任何一步都不会有编译错误，后果各不相同——漏投递则界面上运行不出现，
// 漏落盘则重启后运行凭空消失，漏建句柄则那条运行暂停不了也取消不了。
func (e *Engine) admitLocked(ctx context.Context, owner Task, spec RunSpec, body Body) (Launched, func(), error) {
	// 「第几次」取最大值加一而不是行数加一：保留裁剪删掉旧运行之后，按行数算会撞上一个
	// 已经用过的编号，而用户看到的「第几次」会倒着走。
	highest, err := e.store.MaxNthRun(ctx, owner.ID)
	if err != nil {
		return Launched{}, nil, err
	}
	wait, err := e.mustQueueLocked(ctx, owner.ID)
	if err != nil {
		return Launched{}, nil, err
	}

	now := e.clock()
	run := Run{
		TaskID:    owner.ID,
		ScopeName: spec.ScopeName,
		Trigger:   spec.Trigger,
		NthRun:    highest + 1,
		Status:    StatusRunning,
		Total:     spec.Total,
		UpdatedAt: now,
		StartedAt: now,
		Sequence:  e.nextSequenceLocked(),
	}
	applyMessage(&run, Result{Code: spec.StartCode, Params: spec.StartParams})
	// 还轮不到它的留在**排队中**：它不占槽位，却仍会开跑。开始时刻要等真的开跑才写，
	// 否则排队那段时长会被算进速率的分母。
	if wait {
		run.Status = StatusQueued
		run.StartedAt = time.Time{}
	}

	created, err := e.store.CreateRun(ctx, run)
	// 「最多一条排队中的运行」这条约束由**数据库**说了算，不在内存里再判一次：
	// 撞上它就是「这个任务已经排着了」，本次发起合并进那一条。
	if errors.Is(err, ErrRunAlreadyQueued) {
		return e.coalesceLocked(ctx, owner.ID)
	}
	if err != nil {
		return Launched{}, nil, err
	}
	e.writeSideDataLocked(ctx, created.ID, spec)

	if created.Status == StatusQueued {
		e.queued[created.ID] = queuedRun{spec: spec, body: body}
		e.publishLocked(created)
		return Launched{Run: created}, nil, nil
	}
	launch := e.beginLocked(created, spec, body)
	e.publishLocked(created)
	return Launched{Run: created}, launch, nil
}

// mustQueueLocked 判这次发起该不该先进**排队中**。调用方持锁。
//
// 三条各自的理由不同，但答案相同——都是「现在还不能开跑，但这次发起不该被丢掉」：
//   - 「全部暂停」的闸门关着：放它进去等于用户按下的那一下没按住盘。
//   - 同一个任务已有活动运行：同一件事不会同时跑两遍，这是准入索引那条约束的正面表达。
//   - 槽位已满：全局同时运行数的上限（按单一数字而不是按磁盘卷，ADR 0005）。
//
// 这里判的是「该写哪个状态」，不是准入本身：真正拦下第二条活动运行与第二条排队运行的是
// 数据库那两条部分唯一索引，判据只有那一处。
func (e *Engine) mustQueueLocked(ctx context.Context, taskID int64) (bool, error) {
	if e.pausedAll {
		return true, nil
	}
	active, err := e.store.CountRuns(ctx, RunFilter{TaskID: taskID, Statuses: activeStatuses})
	if err != nil {
		return false, err
	}
	if active > 0 {
		return true, nil
	}
	used, err := e.activeRunCountLocked(ctx)
	if err != nil {
		return false, err
	}
	return used >= e.Slots(), nil
}

// coalesceLocked 把这一次发起**合并**进这个任务已有的那条排队运行：不新建，只把计数加一。
// 调用方持锁。
//
// 合并掉的是**这一次的声明与任务体**：排在前面那条跑的是同一件事，再排一条只会让列表里堆出
// 一串一模一样的运行。计数必须落盘且发得出去——否则用户看到的是一条孤零零的排队，
// 不知道它代表了几次发起。
//
// 序号要重取一个：合并是一次用户看得见的变化，不取的话那条排队在任务中心里一动不动。
func (e *Engine) coalesceLocked(ctx context.Context, taskID int64) (Launched, func(), error) {
	queue, err := e.store.ListRuns(ctx, RunFilter{
		TaskID:   taskID,
		Statuses: []RunStatus{StatusQueued},
		Order:    OrderSequenceAsc,
		Limit:    1,
	})
	if err != nil {
		return Launched{}, nil, err
	}
	// 索引说有、查回来却没有：把准入哨兵原样交出去，调用方至少知道这次发起没有落地。
	if len(queue) == 0 {
		return Launched{}, nil, ErrRunAlreadyQueued
	}

	run := queue[0]
	run.CoalescedCount++
	// 事件只在**第一次**被合并时落一条：守护与监听扫描会在一次长扫期间反复撞上同一条排队运行，
	// 每次落一条就能在一条还没起跑的运行上堆出几千行，而「一共被合并了几次」这件事
	// CoalescedCount 已经记着、也已经发到界面上了。事件回答的是「它被合并过」。
	if run.CoalescedCount == 1 {
		e.emitControlLocked(run.ID, ControlCoalesced)
	}
	run.UpdatedAt = e.clock()
	run.Sequence = e.nextSequenceLocked()
	e.saveLocked(run)
	e.publishLocked(run)
	return Launched{Run: run, Coalesced: true}, nil, nil
}

// writeSideDataLocked 把运行声明里不属于运行行的那几样交给各自的侧表。调用方持锁。
func (e *Engine) writeSideDataLocked(ctx context.Context, runID int64, spec RunSpec) {
	if spec.Limits != (Limits{}) {
		if err := e.store.SaveRunLimits(ctx, runID, spec.Limits); err != nil {
			slog.Warn("Failed to persist run limits", "run_id", runID, "error", err)
		}
	}
	if len(spec.Args) > 0 {
		if err := e.store.MergeRunArgs(ctx, runID, spec.Args); err != nil {
			slog.Warn("Failed to persist run args", "run_id", runID, "error", err)
		}
	}
	if len(spec.Labels) > 0 {
		if err := e.store.MergeRunLabels(ctx, runID, spec.Labels); err != nil {
			slog.Warn("Failed to persist run labels", "run_id", runID, "error", err)
		}
	}
}

// beginLocked 为任务体建立可取消 + 可暂停的 ctx、登记**运行时句柄**，并交回启动它的闭包。
// 调用方持锁；闭包必须在锁外调用。
//
// 它只被 admitLocked 与队列放行调用，因此「拿得到一份运行时句柄」等价于「刚刚过了槽位闸门」。
// 单独暴露出去就等于开了一条给任意运行 id 凭空造句柄的路。
func (e *Engine) beginLocked(run Run, spec RunSpec, body Body) func() {
	ctx, cancel := context.WithCancel(context.Background())
	gate := taskcontrol.NewPauseGate()
	runCtx := taskcontrol.WithPauseGate(ctx, gate)
	if e.decorate != nil {
		runCtx = e.decorate(runCtx, run, spec)
	}
	e.runtimes[run.ID] = &taskRuntime{
		ctx:       runCtx,
		cancel:    cancel,
		gate:      gate,
		canPause:  spec.CanPause,
		canCancel: spec.CanCancel,
	}
	handle := e.newHandle(run.ID)
	runID := run.ID

	return func() {
		e.runGoroutine(runID, func() {
			// 必须用 defer 归还运行时句柄：它因此不依赖任务体走哪条出口，panic 也会经这里归还，
			// 之后才由 runGoroutine 的兜底把运行置为失败态。写成裸调用的话，任务体一 panic
			// 就泄漏一份 ctx 与**暂停闸门**。
			defer e.discardRuntime(runID)
			result, err := body(runCtx, handle)
			e.settle(runID, spec, result, err)
		})
	}
}

// Handle 交出一条**仍活着**的运行的**运行句柄**；这条运行没有运行时句柄时返回 false。
//
// 判据是「引擎此刻登记着它的运行时句柄」而不是「库里有这条运行」：登记只发生在 beginLocked，
// 也就是刚过完准入闸门的那一刻。因此拿得到句柄仍然等价于「这次运行是经启动入口来的、且还在跑」，
// 而不是「谁猜对了一个运行 id 谁就能往里写」。终态与重启之后的运行一律拿不到。
func (e *Engine) Handle(runID int64) (*runhandle.Handle, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, err := e.controlHandleLocked(runID); err != nil {
		return nil, false
	}
	return e.newHandle(runID), true
}

// newHandle 把这条运行的三条写入通道包成闭包，交出它的**运行句柄**。
//
// 运行 id 在这里一次性绑定，此后不出现在句柄上：给句柄开一个 id 形参等于把「谁有资格写
// 由谁拿到句柄决定」这条结构约束重新打开。
func (e *Engine) newHandle(runID int64) *runhandle.Handle {
	return runhandle.New(runhandle.Writes{
		Report:      func(frame runhandle.Frame) { e.report(runID, frame) },
		MergeParams: func(args map[string]string) { e.mergeArgs(runID, args) },
		AddMetrics: func(increments map[string]int64, args map[string]string) {
			e.addMetrics(runID, increments, args)
		},
		ItemFailed: func(item, reason string) { e.failItem(runID, item, reason) },
		Warn:       func(code, detail string, count int64) { e.warn(runID, code, detail, count) },
	}, e.diskWork)
}

// runGoroutine 在受停机管辖的后台 goroutine 里执行任务体，并保证任务体一旦 panic，
// 这条运行被置为失败态。
//
// panic 的任务体走不到 settle，运行将永远停在运行中；而活动运行既占着槽位、又挡着同一个任务
// 的下一次发起，那个任务在进程重启前再也发不起来。把 panic 转成一次显式失败，
// 用户至少能看到原因并重试。
func (e *Engine) runGoroutine(runID int64, fn func()) {
	e.runBackground(func() {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("Background run panicked", "run_id", runID, "panic", rec, "stack", string(debug.Stack()))
				e.finalize(runID, StatusFailed, Result{Code: e.codes.Panicked}, fmt.Sprint(rec))
			}
		}()
		fn()
	})
}

// discardRuntime 丢掉这条运行的运行时句柄。它与终态写入各自会做一次，两次都做是无害的。
func (e *Engine) discardRuntime(runID int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.runtimes, runID)
}

// settle 是任务体裁决的那三条**终态**的**唯一**裁决处：**任务体返回的错误**决定进哪一条，
// Result 只能改文案。
//
// 不得再开「任务体自行调用收尾方法」的第二条路径：两条路径并存时，
// 「忘了收尾」与「收了两次」都不会有编译错误。第四条终态**中断**不由任务体产生，
// 它是重启时的批量转写（见 MarkInterrupted），因此不经这里。
func (e *Engine) settle(runID int64, spec RunSpec, result Result, err error) {
	switch {
	case err == nil:
		e.finalize(runID, StatusCompleted, result.orDefault(spec.CompleteCode), "")
	case errors.Is(err, context.Canceled):
		e.finalize(runID, StatusCancelled, result.orDefault(spec.CancelCode), "")
	default:
		e.finalize(runID, StatusFailed, result.orDefault(spec.FailCode), err.Error())
	}
}

// finalize 把一条运行落成 status 指名的终态，并在槽位空出来之后放行排在最前的那条排队运行。
func (e *Engine) finalize(runID int64, status RunStatus, message Result, runError string) {
	e.mu.Lock()
	launch := e.finalizeLocked(runID, status, message, runError)
	e.mu.Unlock()
	if launch != nil {
		launch()
	}
}

// finalizeLocked 写终态并交回「放行下一批排队运行」的闭包。调用方持锁；闭包必须在锁外调用。
//
// 已经是终态的运行原样返回：迟到的报文与重复收尾都会走到这里，放行会把一条已经收尾的运行
// 在界面上拽回运行中。
func (e *Engine) finalizeLocked(runID int64, status RunStatus, message Result, runError string) func() {
	ctx := context.Background()
	run, err := e.store.LoadRun(ctx, runID)
	if err != nil {
		slog.Warn("Failed to load run for finalize", "run_id", runID, "error", err)
		return nil
	}
	if run.Status.IsTerminal() {
		return nil
	}

	now := e.clock()
	run.Status = status
	applyMessage(&run, message)
	// 技术错误串只属于失败态：其余三条终态一律清空，否则上一帧的错误会一路留到「已取消」上。
	run.Error = runError
	absorbPause(&run, now)
	// 只有**完成**补齐计数：把总数声明成阶段数却从不推进计数的那些工作靠这一笔才显示成 1 / 1。
	// **已取消**不跟着补——被取消掉的条目一个都没处理，补上去这个数就答不出「做完了多少」。
	if status == StatusCompleted && run.Total > 0 {
		run.Current = run.Total
	}
	run.UpdatedAt = now
	run.FinishedAt = &now
	run.Sequence = e.nextSequenceLocked()

	delete(e.runtimes, runID)
	delete(e.queued, runID)
	// 终态丢掉两道水位：这条运行不会再有帧、也不会再取到点，留着只是泄漏。
	delete(e.gates, runID)
	delete(e.samples, runID)
	// 被上限挡掉的那些条目失败在这里结账：落一条「还有 N 条未列出」，并丢掉计账。
	e.flushOmittedItemFailuresLocked(runID)
	// 连败与**退避**记在**任务**上，而这里是四条终态里唯一由任务体裁决的三条汇合处：
	// 记在别处就得在每条分支上各记一遍，而漏掉「失败」那一条不会有任何编译错误。
	e.recordOutcomeLocked(ctx, run.TaskID, status, now)
	e.saveLocked(run)
	e.publishLocked(run)
	// 等这条运行收尾的人在落盘**之后**才被叫醒：他们醒来第一件事就是把它读回来。
	e.releaseWaitersLocked(runID)
	return e.releaseQueuedLocked()
}

// ReleaseQueued 在槽位有空余时放行**排队中**的运行。
//
// 收尾时引擎自己会放行，这个入口是给「上限刚被调大」那一刻用的：此刻没有任何运行收尾，
// 队列却已经可以往前走。不催这一下的话，用户把上限从 2 调到 5 之后什么也不会发生——
// 要等到某条正在跑的运行结束，而那可能是几小时之后。
func (e *Engine) ReleaseQueued() {
	e.mu.Lock()
	// 一条也放不出去是常态（队列可能是空的），但**运行槽位**上限本身就是**实况汇总**里的一个数：
	// 不投这一帧，用户把上限从 2 调到 5 之后界面上仍写着「槽位 0/2」，直到下一次状态跃迁。
	// 放出去几条就算几遍也没意义，因此整批只算一次。
	flushLive := e.deferLiveLocked()
	launch := e.releaseQueuedLocked()
	flushLive()
	e.mu.Unlock()
	if launch != nil {
		launch()
	}
}

// releaseQueuedLocked 在槽位有空余时按序放行**排队中**的运行，交回启动它们的闭包。
// 调用方持锁；闭包必须在锁外调用。
//
// 定序取序号升序：先排上的先跑。任务体没法落盘（它是个闭包），因此重启后留在库里的排队运行
// 在这里放不出去——把它们重新发起来是**恢复**那条路的事。
//
// 「全部暂停」期间一条都不放：那几条排队运行没有闸门可按（任务体还没起），拦住放行是唯一
// 按得住它们的地方。闸门由「全部恢复」打开，并在那里一并放开。
func (e *Engine) releaseQueuedLocked() func() {
	if e.pausedAll {
		return nil
	}
	ctx := context.Background()
	used, err := e.activeRunCountLocked(ctx)
	if err != nil {
		slog.Warn("Failed to count active runs", "error", err)
		return nil
	}
	slots := e.Slots()
	if used >= slots {
		return nil
	}
	queue, err := e.store.ListRuns(ctx, RunFilter{Statuses: []RunStatus{StatusQueued}, Order: OrderSequenceAsc})
	if err != nil {
		slog.Warn("Failed to list queued runs", "error", err)
		return nil
	}

	var launches []func()
	// 同一个任务同一时刻只能有一次活动运行，因此一轮里最多放行它的一条。
	claimed := make(map[int64]bool, len(queue))
	for _, run := range queue {
		if used >= slots {
			break
		}
		entry, ok := e.queued[run.ID]
		if !ok || claimed[run.TaskID] {
			continue
		}
		active, err := e.store.CountRuns(ctx, RunFilter{TaskID: run.TaskID, Statuses: activeStatuses})
		if err != nil || active > 0 {
			claimed[run.TaskID] = true
			continue
		}

		now := e.clock()
		run.Status = StatusRunning
		run.StartedAt = now
		run.UpdatedAt = now
		run.Sequence = e.nextSequenceLocked()
		delete(e.queued, run.ID)
		launches = append(launches, e.beginLocked(run, entry.spec, entry.body))
		e.saveLocked(run)
		e.publishLocked(run)

		claimed[run.TaskID] = true
		used++
	}
	if len(launches) == 0 {
		return nil
	}
	return func() {
		for _, launch := range launches {
			launch()
		}
	}
}
