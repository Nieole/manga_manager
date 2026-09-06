// 引擎的状态与它的构造点：运行时句柄、序号、投递节流与运行槽位。
// 启动仪式在 start.go，控制动作与重启转**中断**在 control.go。

package task

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"manga-manager/internal/diskwork"
	"manga-manager/internal/taskcontrol"
)

// DefaultSlots 是运行槽位的默认值：全局同时运行数的上限。
//
// 按**单一全局数字**限而不是按卷键限是知情的取舍（ADR 0005）——按卷更贴近真实瓶颈，
// 但一个数字更好解释，界面上也画得出「槽位 2/2」。取值来源属于装配方，本包只收一个参数。
const DefaultSlots = 2

// PublishInterval 是逐条目进度的最小投递间隔。
//
// 依据在前端而不是后端：进度条本身带 500ms 的宽度过渡动画，比这更密的推送浏览器画不出第二帧。
const PublishInterval = 200 * time.Millisecond

// publishGate 是单条运行的投递水位。
//
// 除时间外还记下上次**已投递**的展示态：阶段与文案的跃迁是用户正在等的语义变化，
// 被计数器节流吞掉会让运行长时间停在过期的阶段名上。纯计数推进吞掉多少都无所谓——
// 载荷是全量快照，下一条自然带上累积后的最新值。
type publishGate struct {
	at          time.Time
	status      RunStatus
	phase       string
	messageCode string
}

// suppresses 判断这一帧能否被本水位吞掉：仍在窗口内、且展示态一字未变。
func (g publishGate) suppresses(run Run, now time.Time, window time.Duration) bool {
	return now.Sub(g.at) < window &&
		g.status == run.Status &&
		g.phase == run.Phase &&
		g.messageCode == run.MessageCode
}

// taskRuntime 是一条活动运行的可控性：任务体的 ctx、它的取消函数与**暂停闸门**，
// 外加这次运行声明的控制能力。它活在进程里，因此重启后一条都不剩。
//
// 三样能力由 beginLocked 一次填齐，因此拿到一份非 nil 的句柄就等于 ctx、取消函数与闸门都在。
type taskRuntime struct {
	ctx       context.Context
	cancel    context.CancelFunc
	gate      *taskcontrol.PauseGate
	canPause  bool
	canCancel bool
}

// queuedRun 是一条**排队中**运行还没跑的那部分：它的声明与任务体。
// 队列放行要拿回这两样，而它们没法落盘——任务体是个闭包。
type queuedRun struct {
	spec RunSpec
	body Body
}

// ControlCodes 是引擎自己发出的那几条控制文案的 i18n 码。
//
// 引擎不认识具体的码：文案词汇属于 api。留空即这条状态跃迁不改文案。
type ControlCodes struct {
	Paused     string
	Resumed    string
	Cancelling string
	// Panicked 是任务体 panic 时的失败文案码。panic 兜底是引擎唯一直接对用户说话的地方，
	// 因此它也只用码——写死一句英文没有任何地方能翻译它。
	Panicked string
	// Interrupted 是重启时批量转**中断**的文案码。
	//
	// 它必须盖掉上一轮留下的展示态：留着的话，用户看到的是任务停下前那句「已暂停」或
	// 「正在扫描 vol01.zip」，而**中断**唯一说得出口的那句话一次都不会出现。
	Interrupted string
}

// Config 是引擎的全部外部依赖，一次性在装配期交齐。
type Config struct {
	// Store 是落盘端口，不得为 nil：准入判据在它那里，没有它引擎无从判断「已经在跑」。
	Store Store
	// Publish 把一帧快照交给订阅者；为 nil 时不投递。它只收领域快照，
	// 事件名与序列化属于传输层。
	Publish func(Snapshot)
	// RunBackground 开一个受停机管辖的 goroutine，不得为 nil。任务体必须经这项能力启动：
	// 外部替引擎开 goroutine 再反向伸手改运行，会多套一层调度，停机竞态下运行被静默丢弃。
	// 测试注入同步执行版即可确定性地断言**终态**，不必等待真实 goroutine。
	RunBackground func(func())
	// DiskWork 是交给**运行句柄**的**磁盘作业**入口，留 nil 的后果见 taskrun.New。
	DiskWork *diskwork.Runner
	// DecorateRunContext 在任务体的 ctx 建好之后再加一层，为 nil 时不加。
	//
	// 它存在的唯一理由是日志：这条 ctx 上跑出来的每一行日志要带上运行标识与**任务键**，
	// 而那两样的属性名与注入方式属于日志层，不属于本包。调用点手写等于绝大多数调用点都不会带，
	// 所以只能在这里一次性套上。
	DecorateRunContext func(context.Context, Run) context.Context
	// Now 让测试注入可控时钟；为 nil 时走 time.Now。
	// 节流的正确性只能靠时序断言证明——固定 sleep 的用例既慢，又杀不掉「水位只写一次」这类错误实现。
	Now func() time.Time
	// Slots 是运行槽位上限；小于 1 时取 DefaultSlots。
	Slots int
	// ControlCodes 是引擎自己发出的控制文案码。
	ControlCodes ControlCodes
}

// Engine 是任务引擎：它是**往库里放一条运行的唯一入口**，槽位、上下文、后台 goroutine、
// 四条**终态**与 panic 兜底都归它。
//
// 并发约定：
//   - 除装配期注入的那几项外，全部字段由 mu 保护。
//   - 名字带 Locked 后缀的方法要求调用方已持锁；其余方法自行加解锁。
//   - 落盘端口在临界区内被调用，因此实现方不得回调进引擎。
type Engine struct {
	store         Store
	publish       func(Snapshot)
	runBackground func(func())
	diskWork      *diskwork.Runner
	decorate      func(context.Context, Run) context.Context
	now           func() time.Time
	slots         int
	codes         ControlCodes

	mu sync.Mutex
	// seq 是运行的单调序号，装配期从库里已用掉的最大值接上，因此跨重启单调。
	seq int64
	// runtimes 按运行 id 登记活动运行的可控性；一条运行进入终态即删除。
	runtimes map[int64]*taskRuntime
	// queued 按运行 id 存住**排队中**运行的声明与任务体，等槽位放行时取回。
	queued map[int64]queuedRun
	// gates 是每条运行的投递水位。
	gates map[int64]publishGate
}

// New 建一个引擎。Store 与 RunBackground 缺一不可——两者都是装配期的编程错误，
// 留到运行期表现成「运行凭空消失」比当场炸掉更难查。
func New(cfg Config) *Engine {
	if cfg.Store == nil {
		panic("task: Config.Store 不得为 nil")
	}
	if cfg.RunBackground == nil {
		panic("task: Config.RunBackground 不得为 nil")
	}
	slots := cfg.Slots
	if slots < 1 {
		slots = DefaultSlots
	}
	e := &Engine{
		store:         cfg.Store,
		publish:       cfg.Publish,
		runBackground: cfg.RunBackground,
		diskWork:      cfg.DiskWork,
		decorate:      cfg.DecorateRunContext,
		now:           cfg.Now,
		slots:         slots,
		codes:         cfg.ControlCodes,
		runtimes:      make(map[int64]*taskRuntime),
		queued:        make(map[int64]queuedRun),
		gates:         make(map[int64]publishGate),
	}
	e.seq = e.restoredSequence()
	return e
}

// Slots 返回运行槽位上限，供界面画出「槽位 n/N」。
func (e *Engine) Slots() int { return e.slots }

// clock 返回当前时刻（测试可经 Config.Now 注入）。
func (e *Engine) clock() time.Time {
	if e.now != nil {
		return e.now()
	}
	return time.Now()
}

// restoredSequence 取库里已用掉的最大序号，供新引擎接着往下发。
//
// 读失败从 0 开始是降级不是失败：服务照常可用，代价只是任务中心的历史定序退回按插入顺序。
func (e *Engine) restoredSequence() int64 {
	maxSequence, err := e.store.MaxRunSequence(context.Background())
	if err != nil {
		slog.Warn("Failed to restore run sequence", "error", err)
		return 0
	}
	return maxSequence
}

// nextSequenceLocked 发一个新序号。每一次会被用户看见的变化都要取一个：
// 序号是任务中心的主排序键，不取就等于这次变化不改变它在列表里的位置。
func (e *Engine) nextSequenceLocked() int64 {
	e.seq++
	return e.seq
}

// activeRunCountLocked 数此刻占着槽位的运行条数。判据是**活动态**，排队中不算。
func (e *Engine) activeRunCountLocked(ctx context.Context) (int, error) {
	return e.store.CountRuns(ctx, RunFilter{Statuses: activeStatuses})
}

// controlHandleLocked 取回这条运行的**运行时句柄**；进程里没有它就控制不了它——
// 重启之后库里那些活动运行正是如此。调用方持锁。
func (e *Engine) controlHandleLocked(runID int64) (*taskRuntime, error) {
	rt := e.runtimes[runID]
	if rt == nil {
		return nil, ErrRunNotControllable
	}
	return rt, nil
}

// capabilitiesLocked 按运行此刻的活性派生它能接受哪些控制动作。
//
// **终态**一律全 false；**取消中**已经在收尾，再按一次没有意义；**排队中**只能取消——
// 它还没开跑，没有闸门可按。
func (e *Engine) capabilitiesLocked(run Run) Capabilities {
	if run.Status == StatusQueued {
		entry, ok := e.queued[run.ID]
		return Capabilities{CanCancel: ok && entry.spec.CanCancel}
	}
	rt, err := e.controlHandleLocked(run.ID)
	if err != nil {
		return Capabilities{}
	}
	switch run.Status {
	case StatusRunning:
		return Capabilities{CanPause: rt.canPause, CanCancel: rt.canCancel}
	case StatusPaused:
		return Capabilities{CanResume: true, CanCancel: rt.canCancel}
	default:
		return Capabilities{}
	}
}

// snapshotLocked 把一条运行装成一帧：运行行、控制能力与侧数据。调用方持锁。
//
// 侧数据每次现取而不是在引擎里存一份镜像：它的事实来源是那四张侧表，而**累加**类指标的
// 累加发生在落盘侧——在引擎里再累一遍，两份数只要错开一次就再也对不回去。
// 取的代价由投递水位兜住：逐条目进度每 200ms 才出去一帧。
func (e *Engine) snapshotLocked(run Run) Snapshot {
	snapshot := Snapshot{Run: cloneRun(run), Capabilities: e.capabilitiesLocked(run)}
	side, err := e.store.LoadRunSideData(context.Background(), []int64{run.ID})
	if err != nil {
		slog.Warn("Failed to load run side data", "run_id", run.ID, "error", err)
		return snapshot
	}
	snapshot.Side = side[run.ID]
	return snapshot
}

// publishLocked 无条件投递一帧。状态跃迁走它：启动、终态、暂停/恢复/取消是用户在等的变化，
// 吞掉哪怕一条都会让界面停在错误的状态上。调用方持锁。
func (e *Engine) publishLocked(run Run) {
	if e.publish == nil {
		return
	}
	e.publish(e.snapshotLocked(run))
}

// publishProgressLocked 是**计数推进**专用的投递入口，带节流。调用方持锁。
//
// 节流只跳过**投递**，运行行照常写：被跳过期间的进度由下一条带出去（载荷本就是全量快照）。
func (e *Engine) publishProgressLocked(run Run) {
	now := e.clock()
	if gate, ok := e.gates[run.ID]; ok && gate.suppresses(run, now, PublishInterval) {
		return
	}
	e.gates[run.ID] = publishGate{
		at:          now,
		status:      run.Status,
		phase:       run.Phase,
		messageCode: run.MessageCode,
	}
	e.publishLocked(run)
}

// saveLocked 把一条运行写回落盘端口。写失败只告警不回滚：内存里没有第二份真相可以回退到，
// 而让一次进度更新把整个任务体带崩比丢一帧更糟。调用方持锁。
func (e *Engine) saveLocked(run Run) {
	if err := e.store.SaveRun(context.Background(), run); err != nil {
		slog.Warn("Failed to persist run", "run_id", run.ID, "error", err)
	}
}
