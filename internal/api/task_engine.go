// 任务子域在 api 这一侧的**适配层**：把控制端点（暂停 / 恢复 / 取消按**运行 id** 寻址，
// 重试与禁用按**任务 id**，清除仍按筛选条件，全部暂停 / 全部恢复作用在全体运行上）、对外那份 RunSnapshot 形状与
// **重启函数**注册表，接到 `internal/task` 的领域引擎与 `internal/taskstore` 的落盘上。
// **事实来源只有库，这一层不留任务表**（去留的论证见 taskEngine 的符号 doc）。
// 启动仪式在同包的 task_run.go，纯转换与派生字段在 task_model.go。

package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"manga-manager/internal/diskwork"
	"manga-manager/internal/logger"
	"manga-manager/internal/task"
)

// 任务控制（暂停/恢复/取消）与重试查找的哨兵错误。引擎只表达「为什么不行」，
// 具体的 HTTP 状态码与英文文案由 taskControlResponses 决定，避免引擎依赖传输层语义。
var (
	errTaskNotFound          = errors.New("task not found")
	errTaskNotRunning        = errors.New("task is not running")
	errTaskNotPaused         = errors.New("task is not paused")
	errTaskNotPausable       = errors.New("task cannot be paused")
	errTaskNotCancelable     = errors.New("task cannot be cancelled")
	errTaskGateUnavailable   = errors.New("task pause gate is not available")
	errTaskCancelUnavailable = errors.New("task cancellation is not available")
	// errNoRetryableRun 是「这个任务一条跑完的运行都没有」，与「任务不存在」分开：
	// 一个正在跑、还没跑完过的任务照样答不出「再跑一次哪一次」，而它就列在任务中心里。
	// 合成一条的话，用户对着屏幕上明明白白的那一行被告知任务不存在。
	errNoRetryableRun = errors.New("no finished run to retry")
)

// taskPanicMessageCode 是 panic 兜底下发的失败文案码，taskInterruptedMessageCode 是重启时
// 批量转**中断**的文案码。
//
// 这两处是引擎唯一直接面向用户说话的地方，因此也必须只用 i18n 码：一句写死在 Go 里的文案没有
// 任何地方能翻译它。上一版的中断文案正是一句写死的中文，英文用户在那里读到的是中文。
const (
	taskPanicMessageCode       = "task.msg.control.panicked"
	taskInterruptedMessageCode = "task.msg.control.interrupted"
)

// 推送通道上那两种帧的事件名。两种都只投给管理员：任务快照带着宿主机路径，而**实况汇总**
// 说的是「这台机器此刻在干几件事」——任务列表接口对普通用户是 403，事件流不按同一把尺子过滤，
// 那条 403 就等于没有。
const (
	runSnapshotEventPrefix = "run_snapshot:"
	runLiveEventPrefix     = "run_live:"
)

// taskEngineConfig 是任务引擎的全部外部依赖，一次性在装配期交齐。
//
// 收成结构体而不是位置参数：这几项里有三个都是函数，接反了不会有编译错误。
type taskEngineConfig struct {
	// Store 是任务与运行的落盘端口，不得为 nil：准入判据在它那里。
	Store task.Store
	// Publish 是推送通道本身：一帧已经序列化好的事件（运行快照与**实况汇总**都走它）。
	// 为 nil 时整条通道不接。
	Publish func(string)
	// RunBackground 开一个受停机管辖的 goroutine，不得为 nil。
	RunBackground func(func())
	// DiskWork 是交给**运行句柄**的**磁盘作业**入口，留 nil 的后果见 runhandle.New。
	DiskWork *diskwork.Runner
	// Now 让测试注入可控时钟；为 nil 时走 time.Now。
	Now func() time.Time
	// Slots 读**运行槽位**上限。它是函数而不是数：上限在设置里可改，改了要对**新的放行**生效，
	// 而不打断已经在跑的。为 nil 时领域引擎取它的默认值（task.DefaultSlots）。
	Slots func() int
	// Backoff 读**退避**的三个阈值，同样是函数而不是值，理由同 Slots。
	// 为 nil 时领域引擎取它的默认值（task.DefaultBackoff）。
	Backoff func() task.BackoffPolicy
	// SampleInterval 读**采样**的取点间隔，同样是函数而不是值，理由同 Slots。
	// 为 nil 时领域引擎取它的默认值（task.DefaultSampleInterval）。
	SampleInterval func() time.Duration
	// ResumeEnabled 读**可续跑**的全局开关（默认开），为 nil 即开着。
	// 它只管开关：哪些类型可续跑由重启函数注册表那一列说了算。
	ResumeEnabled func() bool
}

// taskEngine 是领域引擎的适配器：两侧的翻译、按**运行**与**任务**寻址的那几个入口，与一份身份缓存。
//
// **内存表的去留**：没有了。旧引擎那张表当年是活动任务的唯一可写副本，列表还要把它盖在库记录上，
// 那正是「同一件事有两个答案」的来源——库里写着 running、内存里已经完成，于是筛选谓词必须在合并
// 之后判，而两支各判一遍时新增一条筛选只补一处不会有编译错误。准入下沉到库上那两条部分唯一索引
// 之后，合并没有了理由：查询一律直接问库，一页里既有还在跑的也有历史的，筛选整条下推到 SQL。
// 随之没有理由的还有异步落盘那条 goroutine 与它那把「删库要等在途写完」的串行锁：写入方只剩一个，
// 而它写的就是被读的那张表。
//
// 进程里因此只剩两样，都不是第二份真相：
//   - 领域引擎登记的**运行时句柄**（ctx、**暂停闸门**、取消函数）。它们本来就只活在进程里，
//     重启后一条不剩——这也正是控制能力必须**派生**而不是落列的原因。
//   - 本层这份**身份缓存**。身份行一旦建出就不再变，缓存的是一份读不坏的东西；它只服务投递，
//     因为领域引擎在自己的临界区里调投递函数，那一刻做不了查询。查询路径一律走 LoadTasks 问库。
type taskEngine struct {
	// ---- 装配期注入，之后只读 ----

	engine   *task.Engine
	runStore task.Store

	// runBackground 与 now 是本层为**测试接缝**留下的两处间接。领域引擎在构造期就把它们收进去了，
	// 而接缝的用法是「构造之后再换掉」——播种要在这一刻决定任务体何时、乃至是否执行，
	// 节流的时序断言要一个可控时钟。经这两个字段转一道，换掉它们仍然生效。
	//
	// **调用约束**：换的时机只能是测试自己的 goroutine 上、且此刻没有任何任务体在飞。
	// 它们属于「装配期注入、之后只读」的那组，不受 mutex 保护。
	//
	// slots 同理转一道：它在生产里读的是配置快照（因此改设置当场生效），而要观察「八条运行同时
	// 在跑」的用例得先把上限抬上去——领域引擎在构造期就把这个函数收进去了，不转一道就换不掉。
	// 归一化不在这里做：小于 1 与 nil 一样交给领域引擎兜底，判据因此只有一处。
	runBackground  func(func())
	now            func() time.Time
	slots          func() int
	backoff        func() task.BackoffPolicy
	sampleInterval func() time.Duration
	// resumeEnabled 读**可续跑**的全局开关；为 nil 即开着（默认开）。
	// 白名单本身不从这里来，它由 dispatch 那一列派生，见 buildResumePolicy。
	resumeEnabled func() bool

	// dispatch 是「再发起一次」的注册表（(类型, **变体**) -> 重启函数与**可续跑**），
	// 也是「可重试」与「可续跑」两条判据的唯一事实来源。
	// 在 newControllerCore 中一次性填好（重启函数要调 Controller 的领域方法，故由 Controller 构建），
	// 此后只读，不需要持锁。
	dispatch map[taskDispatchKey]taskDispatch

	// ---- 受 pushMutex 保护的状态 ----

	// lastPushed 是**投递链**上一帧推出去的序号，供下一帧填 Prev。它自己一把锁，理由见 linkPush。
	pushMutex  sync.Mutex
	lastPushed int64

	// ---- 受 mutex 保护的状态 ----

	mutex sync.Mutex
	// identities 是任务 id -> **身份**的进程内缓存。
	//
	// 它只服务投递：领域引擎在自己的临界区里调投递函数，那一刻不能再去查库。身份行建出之后
	// 不再变，因此这份缓存读不坏；查询路径一律走 LoadTasks 直接问库，不碰它。
	identities map[int64]TaskIdentity
}

func newTaskEngine(cfg taskEngineConfig) *taskEngine {
	// 领域引擎自己也拦这一道，但它收到的是本层那个转发闭包——非 nil，于是拦不住。
	// 不在这里补一句，装配期漏掉后台能力就要等到第一个任务体启动时才炸。
	if cfg.RunBackground == nil {
		panic("api: taskEngineConfig.RunBackground 不得为 nil")
	}
	e := &taskEngine{
		runStore:       cfg.Store,
		runBackground:  cfg.RunBackground,
		now:            cfg.Now,
		slots:          cfg.Slots,
		backoff:        cfg.Backoff,
		sampleInterval: cfg.SampleInterval,
		resumeEnabled:  cfg.ResumeEnabled,
		identities:     make(map[int64]TaskIdentity),
	}
	e.engine = task.New(task.Config{
		Store:              cfg.Store,
		Publish:            e.publisher(cfg.Publish),
		PublishLive:        e.livePublisher(cfg.Publish),
		RunBackground:      func(fn func()) { e.runBackground(fn) },
		DiskWork:           cfg.DiskWork,
		DecorateRunContext: decorateRunContext,
		Now:                e.clock,
		Slots:              e.slotLimit,
		Backoff:            e.backoffPolicy,
		SampleInterval:     e.samplingInterval,
		Resume:             e.buildResumePolicy,
		ControlCodes: task.ControlCodes{
			Paused:      "task.msg.control.paused",
			Resumed:     "task.msg.control.resumed",
			Cancelling:  "task.msg.control.cancelling",
			Panicked:    taskPanicMessageCode,
			Interrupted: taskInterruptedMessageCode,
		},
	})
	return e
}

// slotLimit 读此刻的**运行槽位**上限。领域引擎收的是这个方法而不是 cfg.Slots，
// 好让构造之后换掉仍然生效。
//
// 装配期没给就交回 0，由领域引擎按它的默认值兜底——在这里也写一遍那个默认值，
// 等于让「没人说上限时该是几」有两个答案。
func (e *taskEngine) slotLimit() int {
	if e.slots == nil {
		return 0
	}
	return e.slots()
}

// backoffPolicy 读此刻的**退避**三个阈值。领域引擎收的是这个方法而不是 cfg.Backoff，
// 理由同 slotLimit：构造之后换掉仍然生效，而不合法的取值一律交给领域引擎兜底——
// 在这里也判一遍，等于让「三个数该是几」有两个答案。
func (e *taskEngine) backoffPolicy() task.BackoffPolicy {
	if e.backoff == nil {
		return task.BackoffPolicy{}
	}
	return e.backoff()
}

// samplingInterval 读此刻的**采样**取点间隔。领域引擎收的是这个方法而不是 cfg.SampleInterval，
// 理由同 slotLimit：构造之后换掉仍然生效，而非正数一律交给领域引擎兜底——
// 在这里也判一遍，等于让「没人说间隔时该是多久」有两个答案。
func (e *taskEngine) samplingInterval() time.Duration {
	if e.sampleInterval == nil {
		return 0
	}
	return e.sampleInterval()
}

// sampleActiveRuns 催领域引擎给每条活动运行取一个**采样**点。
//
// 节拍在 Controller 那边（startRunSampler），但它经这道门面下来而不是自己伸手抓 e.engine：
// 「谁能碰领域引擎」这件事，本层是唯一的答案，绕过去一次就多一条谁也没在看的路。
func (e *taskEngine) sampleActiveRuns(ctx context.Context) {
	e.engine.SampleActiveRuns(ctx)
}

// clock 返回当前时刻（测试可经 now 字段注入）。领域引擎收的是这个方法而不是 cfg.Now，
// 好让构造之后换掉时钟仍然生效。
func (e *taskEngine) clock() time.Time {
	if e.now != nil {
		return e.now()
	}
	return time.Now()
}

// decorateRunContext 给任务体的 ctx 挂上**任务键**与运行标识。
//
// 两样都走同一个日志 handler：任务体沿途每一行带 ctx 的日志因此自动带上它们，不必在调用点手写。
// 任务键回答「这行属于哪件事」，运行标识回答「属于那件事的第几次」——同一个库连着扫三次，
// 只按任务键过滤会把三次混在一起，而排障要的恰恰是其中一次。
//
// 键取自**运行声明**而不是运行行：它不落盘（ADR 0007），而队列放行那条路上的运行是从库里
// 读回来的。取运行行上的话，排过队的那些任务体日志会静静地少掉任务键那一格。
func decorateRunContext(ctx context.Context, run task.Run, spec task.RunSpec) context.Context {
	return logger.WithRunID(logger.WithTaskKey(ctx, spec.Key), run.ID)
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

// linkPush 记下这一帧的序号，交回上一帧的。首帧交回 0。
//
// **这把锁只保证这个字段自己读写安全，不保证链的次序。** 次序来自另一把锁：投递一律发生在领域
// 引擎的临界区里，因此「先编号的先入队」是那把锁的结论，不是这里的。真出现第二个不走领域引擎的
// 投递方时，两帧仍可能编号有序而入队乱序——那时要动的是投递口，不是把这里换成更大的锁。
//
// 它不与 taskEngine.mutex 合并，是因为那把锁护的是身份缓存：两样东西的生命周期与访问路径都不同，
// 合成一把之后，任何一处想在持锁时做点别的都得先想清楚另一处。
func (e *taskEngine) linkPush(sequence int64) int64 {
	e.pushMutex.Lock()
	defer e.pushMutex.Unlock()
	prev := e.lastPushed
	e.lastPushed = sequence
	return prev
}

// ---- 身份缓存 ----

func (e *taskEngine) rememberIdentity(taskID int64, identity TaskIdentity) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.identities[taskID] = identity
}

func (e *taskEngine) cachedIdentity(taskID int64) TaskIdentity {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	return e.identities[taskID]
}

// resolveIdentities 按任务 id 批量取回身份并顺手补进缓存。
//
// 查询路径走这里而不是读缓存：缓存里只有本进程启动过的那些任务，而列表要列的多数是重启之前的历史。
func (e *taskEngine) resolveIdentities(ctx context.Context, taskIDs []int64) (map[int64]TaskIdentity, error) {
	owners, err := e.runStore.LoadTasks(ctx, taskIDs)
	if err != nil {
		return nil, err
	}
	identities := make(map[int64]TaskIdentity, len(owners))
	e.mutex.Lock()
	defer e.mutex.Unlock()
	for id, owner := range owners {
		identity := taskIdentityFromDomain(owner.Identity)
		identities[id] = identity
		e.identities[id] = identity
	}
	return identities, nil
}

// ---- 可重试与可续跑 ----

// isRetryableTask 由注册表派生：注册了**重启函数**的（类型，**变体**）即可重试。
// 「哪些可重试」不得另立第二份清单——两份清单一旦不同步，界面上的重试按钮会指向一个没人能重启的任务。
func (e *taskEngine) isRetryableTask(taskType string, variant TaskVariant) bool {
	_, ok := e.relauncherFor(taskType, variant)
	return ok
}

// relauncherFor 返回这个（类型，**变体**）的重启函数；未注册即不可重试。
func (e *taskEngine) relauncherFor(taskType string, variant TaskVariant) (taskRelauncher, bool) {
	entry, ok := e.dispatch[taskDispatchKey{Type: taskType, Variant: variant}]
	if !ok || entry.Relaunch == nil {
		return nil, false
	}
	return entry.Relaunch, true
}

// buildResumePolicy 把注册表里那一列**可续跑**翻成领域侧的白名单，配上全局开关。
//
// 白名单同样只有注册表一份事实来源：在这里另抄一份类型名清单的话，两份不同步时，
// 界面上说着不可续跑的类型会在重启后自己跑起来——而那正是这条白名单要挡住的事。
//
// 装配期没给开关就按**开着**算，理由同 slotLimit：不合法与没给都交给领域侧兜底。
func (e *taskEngine) buildResumePolicy() task.ResumePolicy {
	keys := make([]task.ResumeKey, 0, len(e.dispatch))
	for key, entry := range e.dispatch {
		if entry.Resumable {
			keys = append(keys, task.ResumeKey{Type: task.Type(key.Type), Variant: task.Variant(key.Variant)})
		}
	}
	policy := task.NewResumePolicy(keys...)
	policy.Disabled = e.resumeEnabled != nil && !e.resumeEnabled()
	return policy
}

// ---- 查询 ----

// listRunSnapshots 按谓词取一页运行。
//
// 只有一个来源——库。旧引擎在这里要把内存表盖在库记录上，因此筛选谓词必须在合并之后判；
// 现在筛选整条下推到 SQL，Limit 截断的就是过滤之后的那一页。
func (e *taskEngine) listRunSnapshots(ctx context.Context, filters taskFilters) ([]RunSnapshot, error) {
	snapshots, err := e.engine.ListSnapshots(ctx, runFilterFrom(filters, task.OrderLiveFirst))
	if err != nil {
		return nil, err
	}
	return e.statusesFrom(ctx, snapshots)
}

// live 取实况区那一帧：仍会变化的全部运行，加上由它们数出来的汇总。
//
// 汇总从同一批运行数出来而不是另发几句 COUNT：两条路各查一次，中间的一次状态跃迁就能让
// 「活动 2」配着三张运行卡片。仍会变化的运行任何时刻都只有个位数（准入索引限死每任务至多两条），
// 全取回来不比数一遍贵。
//
// 定序是**手动优先**而不是纯序号降序：自动发起的扫描每报一帧就把自己顶到最前，
// 用户刚按下的那一条会一路下沉。前端把 SSE 帧折进这一帧时用的是同一条判据（见 runLive.ts）。
func (e *taskEngine) live(ctx context.Context) (RunLive, error) {
	snapshots, err := e.engine.ListSnapshots(ctx, task.RunFilter{Statuses: task.LiveStatuses(), Order: task.OrderManualFirst})
	if err != nil {
		return RunLive{}, err
	}
	runs, err := e.statusesFrom(ctx, snapshots)
	if err != nil {
		return RunLive{}, err
	}
	// 汇总交给领域去数，本层不自己再数一遍：推送那条路数的是同一件事，各写一遍的话，
	// 两边只要错开一次，界面上的「槽位 2/2」就会配着三张运行卡片。
	domainRuns := make([]task.Run, 0, len(snapshots))
	for _, snapshot := range snapshots {
		domainRuns = append(domainRuns, snapshot.Run)
	}
	return RunLive{RunLiveSummary: runLiveSummaryFrom(e.engine.Summarize(domainRuns)), Runs: runs}, nil
}

// awaitRunOutcome 等这条运行收尾，把它的**终态**翻成调用方看得懂的一个错误：
// 完成为 nil，已取消为 context.Canceled，失败与中断带上它自己的错误串。
//
// 等的判据是运行的状态，不是「任务体返回了没有」：**排队中**被取消的运行任务体根本不会执行
// （用户在实况区按下那张排队卡片上的取消就是这一种），只等任务体的话，等待方会一直挂到停机。
func (e *taskEngine) awaitRunOutcome(ctx context.Context, runID int64) error {
	run, err := e.engine.Await(ctx, runID)
	if err != nil {
		return err
	}
	switch run.Status {
	case task.StatusCompleted:
		return nil
	case task.StatusCancelled:
		// 取消不是故障：等待方（文件监听器）据此不记错误日志，但也不会把它当成功。
		return context.Canceled
	default:
		return fmt.Errorf("run %d ended as %s: %s", runID, run.Status, run.Error)
	}
}

// listTaskSummaries 取任务清单：一个任务一行，行上带它最近一次运行。
func (e *taskEngine) listTaskSummaries(ctx context.Context, filters taskFilters) ([]TaskSummary, error) {
	owners, err := e.runStore.ListTasks(ctx, taskListFilterFrom(filters))
	if err != nil {
		return nil, err
	}
	taskIDs := make([]int64, 0, len(owners))
	for _, owner := range owners {
		taskIDs = append(taskIDs, owner.ID)
	}
	latest, err := e.engine.LatestSnapshots(ctx, taskIDs)
	if err != nil {
		return nil, err
	}

	summaries := make([]TaskSummary, 0, len(owners))
	for _, owner := range owners {
		identity := taskIdentityFromDomain(owner.Identity)
		summary := TaskSummary{
			TaskID:        owner.ID,
			Type:          identity.taskType,
			Scope:         identity.scope,
			ScopeID:       identity.scopeID,
			Variant:       identity.variant,
			Disabled:      owner.Disabled,
			FailStreak:    owner.FailStreak,
			LastSuccessAt: owner.LastSuccessAt,
			BackoffUntil:  owner.BackoffUntil,
			// **停发**由领域按同一份阈值判，本层不拿那三个字段自己再推一遍：推错的后果是
			// 界面上的红点与真正被挡下的那次发起各说各话。
			StallReason: string(e.engine.StallOf(owner.TaskAttributes)),
		}
		if snapshot, ok := latest[owner.ID]; ok {
			run := e.runSnapshotFrom(snapshot, identity)
			summary.ScopeName = run.ScopeName
			summary.LastRun = &run
		}
		summaries = append(summaries, summary)
	}
	return summaries, nil
}

// statusesFrom 把一批领域快照翻成对外形状，身份一次批量取回。
func (e *taskEngine) statusesFrom(ctx context.Context, snapshots []task.Snapshot) ([]RunSnapshot, error) {
	taskIDs := make([]int64, 0, len(snapshots))
	for _, snapshot := range snapshots {
		taskIDs = append(taskIDs, snapshot.Run.TaskID)
	}
	identities, err := e.resolveIdentities(ctx, taskIDs)
	if err != nil {
		return nil, err
	}
	items := make([]RunSnapshot, 0, len(snapshots))
	for _, snapshot := range snapshots {
		items = append(items, e.runSnapshotFrom(snapshot, identities[snapshot.Run.TaskID]))
	}
	return items, nil
}

// firstStatusFor 取符合这条谓词的第一条运行的对外快照；一条都不符合即 found 为 false。
//
// 「一条都没有」交回布尔值而不是某个哨兵错误：符合与否是查询的正常结果，而「这该翻成哪一句话」
// 各调用方答得不一样——重试要分「任务不存在」与「它一次都还没跑完」，用例只想知道在不在。
func (e *taskEngine) firstStatusFor(ctx context.Context, filter task.RunFilter) (status RunSnapshot, found bool, err error) {
	snapshots, err := e.engine.ListSnapshots(ctx, filter)
	if err != nil {
		return RunSnapshot{}, false, err
	}
	if len(snapshots) == 0 {
		return RunSnapshot{}, false, nil
	}
	items, err := e.statusesFrom(ctx, snapshots)
	if err != nil {
		return RunSnapshot{}, false, err
	}
	return items[0], true, nil
}

// snapshotForRetry 取回任务快照供重试：按**任务 id** 取它最近一条进入**终态**的运行。
//
// 不按**任务键**寻址（ADR 0007）：键是同一身份历次运行共用的一个串，而队列一开，同一个键此刻
// 可以有两条仍会变化的运行——「最近那一条」由序号决定、序号每有一帧就换一次主人，寻址对象
// 本身因此是不确定的。任务 id 是界面上那张卡片自己带着的（RunSnapshot.TaskID），按它寻址没有第二种解释。
//
// 只挑**终态**：重试的意思是「那次跑完的，再跑一次」，而**排队中**与**活动态**的运行还没跑完，
// 身上没有可重放的东西。不挑的话，只要这个任务有一条排队中的运行，它就永远是「最近那一条」
// ——序号在入队与每次**合并**时都会重取——于是用户点的是那条刚失败的卡片，重放的却是它的入参。
//
// 「最近」判的是序号而不是时间列：序号由引擎在临界区里单调发放，而每一次会被用户看见的变化都取一个。
//
// 一条都没挑出来时再问一次这个任务在不在，好把两种来路分开：不存在的 id 与「存在、但一次都还
// 没跑完」答的不是同一句话，而后者就列在任务中心里正跑着。多的这一次查询只落在失败路径上。
//
// 旧引擎在这里要先查内存表再退回查库，因为内存表是有上限的缓存、重启后更是空的，而**中断**任务
// 恰恰只在库里。现在只有库一个来源，这条分岔随之消失。
func (e *taskEngine) snapshotForRetry(ctx context.Context, taskID int64) (RunSnapshot, error) {
	status, found, err := e.firstStatusFor(ctx, task.RunFilter{
		TaskID:   taskID,
		Statuses: task.TerminalStatuses(),
		Order:    task.OrderSequenceDesc,
		Limit:    1,
	})
	if err != nil {
		return RunSnapshot{}, err
	}
	if found {
		return status, nil
	}
	owners, err := e.runStore.LoadTasks(ctx, []int64{taskID})
	if err != nil {
		return RunSnapshot{}, err
	}
	if _, ok := owners[taskID]; !ok {
		return RunSnapshot{}, errTaskNotFound
	}
	return RunSnapshot{}, errNoRetryableRun
}

// latestRunByTypes 返回给定类型中最近**开跑过**的那一次运行；无匹配返回 nil。
// 供存储 IO 面板估算扫描/封面速率。
//
// **排队中**的运行不算：它没有开始时刻，那几个速率一个都答不出，而它的序号恰恰是最新的
// （入队与每次**合并**都取一个），不排除的话它会顶掉真正在跑的那条，面板上的数静默变成 0。
func (e *taskEngine) latestRunByTypes(types ...string) *RunSnapshot {
	ctx := context.Background()
	domainTypes := make([]task.Type, 0, len(types))
	for _, taskType := range types {
		domainTypes = append(domainTypes, task.Type(taskType))
	}
	snapshots, err := e.engine.ListSnapshots(ctx, task.RunFilter{
		Types:    domainTypes,
		Statuses: task.StartedStatuses(),
		Order:    task.OrderSequenceDesc,
		Limit:    1,
	})
	if err != nil || len(snapshots) == 0 {
		if err != nil {
			slog.Warn("Failed to look up latest run by type", "error", err)
		}
		return nil
	}
	items, err := e.statusesFrom(ctx, snapshots)
	if err != nil || len(items) == 0 {
		return nil
	}
	return &items[0]
}

// ---- 控制：清理 / 暂停 / 恢复 / 取消 / 停机 ----

// clear 按过滤条件删除运行记录，返回删除的行数。
//
// **仍会变化的运行永不被删**由落盘侧写死（见 task.Store.DeleteRuns），不靠这里再判一遍：
// 删掉一条还在跑的运行，它后续的每一次上报都会落空，而任务体仍在动磁盘。
//
// 旧引擎在这里要先清内存、再等一批在途落盘写完才敢删库，否则删掉的行会被写回来。落盘只剩一处
// 之后那条串行没有了对象：这一句 DELETE 与写入方走的是同一个库。
func (e *taskEngine) clear(ctx context.Context, filters taskFilters) (int64, error) {
	// 清理不接受关键词与条数：它们只用于列表展示，用它们做删除条件会让「删了什么」不可预期。
	filters.Query = ""
	filters.Limit = 0
	return e.runStore.DeleteRuns(ctx, runFilterFrom(filters, task.OrderSequenceDesc))
}

// pruneHistory 按**分层保留**裁剪历史，返回各层清掉的行数。
//
// 它经领域引擎而不是直接问落盘端口：「活动态与排队中的运行永不被带走」是领域的不变量，
// 而清理运行不清掉自己这一条，靠的正是裁剪发生在它自己那条运行还活着的时候（见 task.Engine.PruneHistory）。
func (e *taskEngine) pruneHistory(ctx context.Context, policy task.RetentionPolicy) (task.PruneResult, error) {
	return e.engine.PruneHistory(ctx, policy)
}

// setTaskDisabled 翻转人工禁用开关，**按任务 id 寻址**（规格关键决定 15：禁用作用在**任务**上）。
// 它一条运行都不动——禁用一个正在跑的任务，那次运行照常跑到底。
func (e *taskEngine) setTaskDisabled(ctx context.Context, taskID int64, disabled bool) error {
	_, err := e.engine.SetTaskDisabled(ctx, taskID, disabled)
	return taskControlError(err)
}

// pauseRun / resumeRun / cancelRun 是三个控制动作，**按运行 id 寻址**。
//
// 不按**任务键**：队列出现之后，同一个键此刻可以有两条仍会变化的运行（一条在跑、一条排队），
// 而「这个键最近的那一次」在两者之间来回跳——序号每有一帧就换一次主人。用户按下的是排队那条
// 卡片上的取消，动到的却可能是正在跑的那条。运行 id 是界面上那张卡片自己带着的（RunSnapshot.RunID），
// 按它寻址就没有第二种解释（关键决定 15：暂停 / 恢复 / 取消作用在**运行**上）。
//
// 「这次运行还能不能接受这个动作」一律由领域裁决，本层不预判：预判等于把状态机抄第二遍，
// 而两份判据只要错开一次，界面上按钮的可用性就与按下去的结果对不上。
func (e *taskEngine) pauseRun(runID int64) error {
	return taskControlError(e.engine.Pause(runID))
}

func (e *taskEngine) resumeRun(runID int64) error {
	return taskControlError(e.engine.Resume(runID))
}

func (e *taskEngine) cancelRun(runID int64) error {
	return taskControlError(e.engine.Cancel(runID))
}

// cancelRunsForScope 取消挂在这个**作用域**对象上、仍会变化的每一条运行，返回真正取消掉的条数。
//
// 判据是作用域列而不是一串**任务键**前缀：一份前缀清单说的是「今天有哪几类工作会挂在库上」，
// 而那个答案每加一个库级任务类型就变一次，漏补不会有编译错误，只会表现成删库几分钟后
// 队列里那条运行自己开跑。作用域是身份上的真列，这句话因此不必随类型清单一起维护。
//
// 单条取消不了（不可取消、进程里没有句柄）不阻断其余那些：删库不因为一条取消不掉就半途而废。
func (e *taskEngine) cancelRunsForScope(scope task.Scope, scopeID int64) (int, error) {
	runs, err := e.runStore.ListRuns(context.Background(), task.RunFilter{
		Scope:    scope,
		ScopeID:  &scopeID,
		Statuses: task.LiveStatuses(),
		Order:    task.OrderSequenceAsc,
	})
	if err != nil {
		return 0, err
	}
	cancelled := 0
	for _, run := range runs {
		if err := e.cancelRun(run.ID); err != nil {
			slog.Debug("Skipped cancelling a live run", "run_id", run.ID, "error", err)
			continue
		}
		cancelled++
	}
	return cancelled, nil
}

// pauseAll 与 resumeAll 是「全部暂停 / 全部恢复」：领域引擎把每条运行逐个按下或放行，
// 返回真正动到的条数。本层不预筛「哪些能暂停」——那份判据在领域，抄第二遍就会与按钮对不上。
func (e *taskEngine) pauseAll(ctx context.Context) (int, error) {
	return e.engine.PauseAll(ctx)
}

func (e *taskEngine) resumeAll(ctx context.Context) (int, error) {
	return e.engine.ResumeAll(ctx)
}

// releaseQueued 催一次队列放行，供**运行槽位**上限刚被调大之后调用。
//
// 平时不必催：收尾时引擎自己会放行。但调大上限的那一刻没有任何运行收尾，队列却已经可以往前走——
// 不催的话，用户把上限从 2 调到 5 之后什么也不会发生，要等到某条正在跑的运行结束。
func (e *taskEngine) releaseQueued() {
	e.engine.ReleaseQueued()
}

// taskControlError 把领域的控制哨兵翻成本层的哨兵。
//
// 领域只有一条「进程里没有这条运行的句柄」，而本层历来分成闸门与取消两条：两条对外的英文提示
// 不同，且删库那条路径按它们分别放过。翻成闸门那条即可——取消不了与暂停不了在这里是同一个原因。
func taskControlError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, task.ErrRunNotFound), errors.Is(err, task.ErrTaskNotFound):
		return errTaskNotFound
	case errors.Is(err, task.ErrRunNotRunning):
		return errTaskNotRunning
	case errors.Is(err, task.ErrRunNotPaused):
		return errTaskNotPaused
	case errors.Is(err, task.ErrRunNotPausable):
		return errTaskNotPausable
	case errors.Is(err, task.ErrRunNotCancelable):
		return errTaskNotCancelable
	case errors.Is(err, task.ErrRunNotControllable):
		return errTaskGateUnavailable
	default:
		return err
	}
}

// markInterrupted 是重启恢复的整条路：把上次运行留下的**活动态**与**排队中**运行全部转成
// **中断**（任务体随进程一起没了，库里那行却还停在活动态），再把**可续跑**的那几条重新发起一次。
// 它属于装配期，因此那几条恢复运行就是本进程头几条落地的运行。
//
// 先把这批运行的身份读进缓存再转写：转写过程中每条都会投递一帧，而投递那一刻在领域引擎的
// 临界区里，补不了身份。缓存不上不是失败，只是那几帧少了类型与作用域——它们发生在任何
// SSE 订阅者接上之前。
func (e *taskEngine) markInterrupted(ctx context.Context) {
	live, err := e.runStore.ListRuns(ctx, task.RunFilter{Statuses: task.LiveStatuses()})
	if err != nil {
		slog.Warn("Failed to list live runs for recovery", "error", err)
	} else {
		taskIDs := make([]int64, 0, len(live))
		for _, run := range live {
			taskIDs = append(taskIDs, run.TaskID)
		}
		if _, err := e.resolveIdentities(ctx, taskIDs); err != nil {
			slog.Warn("Failed to resolve identities for recovery", "error", err)
		}
	}

	outcome, err := e.engine.MarkInterrupted(ctx)
	if err != nil {
		slog.Warn("Failed to recover interrupted runs", "error", err)
	}
	if outcome.Marked > 0 {
		slog.Info("Recovered interrupted runs", "count", outcome.Marked)
	}
	e.resumeRuns(ctx, outcome.Resume)
}

// resumeRuns 把**可续跑**白名单挑出来的那几条中断运行重新发起一次，**发起方记恢复**。
//
// 恢复出来的是**新一次运行**：原来那条留在**中断**，不改回运行中——「上一次断在哪」是用户要的答案。
// 重排队走的是同一个启动入口，因此槽位上限照样管着它们：一次重启恢复出十条也不会同时开跑。
//
// **一条失败不拖累其余**：库被删了、路径没了都会让某个重启函数当场出错，而这一切发生在开机那一刻、
// 用户还没登录。整批中断的话，剩下那些既不会跑，也没有人知道它们本该跑。
func (e *taskEngine) resumeRuns(ctx context.Context, snapshots []task.Snapshot) {
	if len(snapshots) == 0 {
		return
	}
	runs, err := e.statusesFrom(ctx, snapshots)
	if err != nil {
		slog.Warn("Failed to resolve runs for resume", "error", err)
		return
	}
	resumed := 0
	for _, run := range runs {
		relaunch, ok := e.relauncherFor(run.Type, run.Variant)
		// 白名单与重启函数同出一张注册表，因此走不到这里；留着是因为「白名单里有、却没人发得起」
		// 的类型只会静默什么都不做，而那种漏配值得留一行日志。
		if !ok {
			slog.Warn("Resumable run has no relauncher", "run_id", run.RunID, "task_id", run.TaskID, "task_type", run.Type)
			continue
		}
		if err := relaunch(ctx, run, task.TriggerResumed); err != nil {
			slog.Warn("Failed to resume interrupted run", "run_id", run.RunID, "task_id", run.TaskID, "task_type", run.Type, "error", err)
			continue
		}
		resumed++
	}
	if resumed > 0 {
		slog.Info("Resumed interrupted runs", "count", resumed)
	}
}

// stopAllRuntimes 在停机时逐个取消运行的 ctx 并放行它的**暂停闸门**。
func (e *taskEngine) stopAllRuntimes() {
	e.engine.StopAll()
}
