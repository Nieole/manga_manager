// 任务子域在 api 这一侧的**适配层**：把控制端点（六个按**任务键**寻址，全部暂停 / 全部恢复作用在
// 全体运行上）、对外那份 RunStatus 形状与**重启函数**注册表，接到 `internal/task` 的领域引擎与
// `internal/taskstore` 的落盘上。
// **事实来源只有库，这一层不留任务表**（去留的论证见 taskEngine 的符号 doc）。
// 启动仪式在同包的 task_run.go，纯转换与派生字段在 task_model.go。

package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
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

// taskSlotsUnlimited 关掉领域引擎自带的运行槽位。
//
// 上限、界面上的「槽位 2/2」与**排队中**的展示是一整块，要一起落地：只放开这个常数而界面跟不上，
// 推出去的是一个前端不认识的 `queued` 状态，用户看到的是第三个后台任务莫名其妙地不动。
// 数值取一个大到不可能撞上的常数，而不是给领域开一个「不限」的特例。
const taskSlotsUnlimited = math.MaxInt32

// taskEngineConfig 是任务引擎的全部外部依赖，一次性在装配期交齐。
//
// 收成结构体而不是位置参数：这几项里有三个都是函数，接反了不会有编译错误。
type taskEngineConfig struct {
	// Store 是任务与运行的落盘端口，不得为 nil：准入判据在它那里。
	Store task.Store
	// Publish 把一帧任务快照投给 SSE 订阅者；为 nil 时不投递。
	Publish func(string)
	// RunBackground 开一个受停机管辖的 goroutine，不得为 nil。
	RunBackground func(func())
	// DiskWork 是交给**运行句柄**的**磁盘作业**入口，留 nil 的后果见 runhandle.New。
	DiskWork *diskwork.Runner
	// Now 让测试注入可控时钟；为 nil 时走 time.Now。
	Now func() time.Time
}

// taskEngine 是领域引擎的适配器：两侧的翻译、按**任务键**寻址的那几个入口，与一份身份缓存。
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
	runBackground func(func())
	now           func() time.Time

	// relaunchers 是任务重试的注册表（(类型, **变体**) -> 重启函数），也是「可重试」的唯一事实来源。
	// 在 newControllerCore 中一次性填好（重启函数要调 Controller 的领域方法，故由 Controller 构建），
	// 此后只读，不需要持锁。
	relaunchers map[taskDispatchKey]taskRelauncher

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
		runStore:      cfg.Store,
		runBackground: cfg.RunBackground,
		now:           cfg.Now,
		identities:    make(map[int64]TaskIdentity),
	}
	e.engine = task.New(task.Config{
		Store:              cfg.Store,
		Publish:            e.publisher(cfg.Publish),
		RunBackground:      func(fn func()) { e.runBackground(fn) },
		DiskWork:           cfg.DiskWork,
		DecorateRunContext: decorateRunContext,
		Now:                e.clock,
		Slots:              taskSlotsUnlimited,
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
func decorateRunContext(ctx context.Context, run task.Run) context.Context {
	return logger.WithRunID(logger.WithTaskKey(ctx, run.Key), run.ID)
}

// publisher 把领域快照翻成对外的任务快照并交给 SSE。publish 为 nil 时整条通道不接。
//
// 序列化在这里做而不是在领域里：事件名与 JSON 形状属于传输层。
func (e *taskEngine) publisher(publish func(string)) func(task.Snapshot) {
	if publish == nil {
		return nil
	}
	return func(snapshot task.Snapshot) {
		status := e.runStatusFrom(snapshot, e.cachedIdentity(snapshot.Run.TaskID))
		payload, err := json.Marshal(status)
		if err != nil {
			slog.Warn("Failed to marshal task status", "task_key", status.Key, "error", err)
			return
		}
		// 统一经 sseBroker 投递（非阻塞、buffer 满则丢弃并告警）。
		publish("run_snapshot:" + string(payload))
	}
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

// ---- 可重试 ----

// isRetryableTask 由注册表派生：注册了 relauncher 的（类型，**变体**）即可重试。
// 「哪些可重试」不得另立第二份清单——两份清单一旦不同步，界面上的重试按钮会指向一个没人能重启的任务。
func (e *taskEngine) isRetryableTask(taskType string, variant TaskVariant) bool {
	_, ok := e.relauncherFor(taskType, variant)
	return ok
}

// relauncherFor 返回这个（类型，**变体**）的重启函数；未注册即不可重试。
func (e *taskEngine) relauncherFor(taskType string, variant TaskVariant) (taskRelauncher, bool) {
	relaunch, ok := e.relaunchers[taskDispatchKey{Type: taskType, Variant: variant}]
	return relaunch, ok
}

// ---- 查询 ----

// listRunStatuses 按谓词取一页运行。
//
// 只有一个来源——库。旧引擎在这里要把内存表盖在库记录上，因此筛选谓词必须在合并之后判；
// 现在筛选整条下推到 SQL，Limit 截断的就是过滤之后的那一页。
func (e *taskEngine) listRunStatuses(ctx context.Context, filters taskFilters) ([]RunStatus, error) {
	snapshots, err := e.engine.ListSnapshots(ctx, runFilterFrom(filters, task.OrderLiveFirst))
	if err != nil {
		return nil, err
	}
	return e.statusesFrom(ctx, snapshots)
}

// statusesFrom 把一批领域快照翻成对外形状，身份一次批量取回。
func (e *taskEngine) statusesFrom(ctx context.Context, snapshots []task.Snapshot) ([]RunStatus, error) {
	taskIDs := make([]int64, 0, len(snapshots))
	for _, snapshot := range snapshots {
		taskIDs = append(taskIDs, snapshot.Run.TaskID)
	}
	identities, err := e.resolveIdentities(ctx, taskIDs)
	if err != nil {
		return nil, err
	}
	items := make([]RunStatus, 0, len(snapshots))
	for _, snapshot := range snapshots {
		items = append(items, e.runStatusFrom(snapshot, identities[snapshot.Run.TaskID]))
	}
	return items, nil
}

// latestRunFilterFor 是「这个**任务键**最近的那一次运行」的谓词。
//
// 「最近」判的是序号而不是时间列：序号由引擎在临界区里单调发放，而每一次会被用户看见的变化都取一个，
// 因此同一个键上活着的那一条恒排在它自己的历史之前。
func latestRunFilterFor(key string) task.RunFilter {
	return task.RunFilter{Key: key, Order: task.OrderSequenceDesc, Limit: 1}
}

// latestRunByKey 取这个任务键最近的那一次运行；查不到即 errTaskNotFound。
//
// 只取运行行，不装快照：控制动作要的只是一个运行 id，而装快照要连带把四张侧表读一遍。
func (e *taskEngine) latestRunByKey(ctx context.Context, key string) (task.Run, error) {
	runs, err := e.runStore.ListRuns(ctx, latestRunFilterFor(key))
	if err != nil {
		return task.Run{}, err
	}
	if len(runs) == 0 {
		return task.Run{}, errTaskNotFound
	}
	return runs[0], nil
}

// snapshotForRetry 取回任务快照供重试：按**任务键**取它最近的那一次运行。
//
// 旧引擎在这里要先查内存表再退回查库，因为内存表是有上限的缓存、重启后更是空的，而**中断**任务
// 恰恰只在库里。现在只有库一个来源，这条分岔随之消失。
func (e *taskEngine) snapshotForRetry(ctx context.Context, key string) (RunStatus, error) {
	snapshots, err := e.engine.ListSnapshots(ctx, latestRunFilterFor(key))
	if err != nil {
		return RunStatus{}, err
	}
	if len(snapshots) == 0 {
		return RunStatus{}, errTaskNotFound
	}
	items, err := e.statusesFrom(ctx, snapshots)
	if err != nil {
		return RunStatus{}, err
	}
	return items[0], nil
}

// latestTaskByTypes 返回给定类型中最近活动的那一次运行；无匹配返回 nil。
// 供存储 IO 面板估算扫描/封面速率。
func (e *taskEngine) latestTaskByTypes(types ...string) *RunStatus {
	ctx := context.Background()
	domainTypes := make([]task.Type, 0, len(types))
	for _, taskType := range types {
		domainTypes = append(domainTypes, task.Type(taskType))
	}
	snapshots, err := e.engine.ListSnapshots(ctx, task.RunFilter{
		Types: domainTypes,
		Order: task.OrderSequenceDesc,
		Limit: 1,
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

func (e *taskEngine) pause(key string) error {
	return e.control(key, e.engine.Pause)
}

func (e *taskEngine) resume(key string) error {
	return e.control(key, e.engine.Resume)
}

// pauseAll 与 resumeAll 是「全部暂停 / 全部恢复」：领域引擎把每条运行逐个按下或放行，
// 返回真正动到的条数。本层不预筛「哪些能暂停」——那份判据在领域，抄第二遍就会与按钮对不上。
func (e *taskEngine) pauseAll(ctx context.Context) (int, error) {
	return e.engine.PauseAll(ctx)
}

func (e *taskEngine) resumeAll(ctx context.Context) (int, error) {
	return e.engine.ResumeAll(ctx)
}

// anyRunPaused 回答「此刻有没有运行被暂停」，供诊断接口的暂停字段与前端顶部那个按钮取向。
//
// 问的是库而不是列表接口取回的那一页：那一页带着用户的筛选与条数上限，回答不了全局的问题。
func (e *taskEngine) anyRunPaused(ctx context.Context) (bool, error) {
	count, err := e.runStore.CountRuns(ctx, task.RunFilter{Statuses: []task.RunStatus{task.StatusPaused}})
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (e *taskEngine) cancel(key string) error {
	return e.control(key, e.engine.Cancel)
}

// control 把按**任务键**寻址的控制动作转成按运行寻址：取这个键最近的那一次运行，交给领域引擎。
//
// 「这次运行还能不能接受这个动作」一律由领域裁决，本层不预判：预判等于把状态机抄第二遍，
// 而两份判据只要错开一次，界面上按钮的可用性就与按下去的结果对不上。
func (e *taskEngine) control(key string, action func(int64) error) error {
	run, err := e.latestRunByKey(context.Background(), key)
	if err != nil {
		return err
	}
	return taskControlError(action(run.ID))
}

// taskControlError 把领域的控制哨兵翻成本层的哨兵。
//
// 领域只有一条「进程里没有这条运行的句柄」，而本层历来分成闸门与取消两条：两条对外的英文提示
// 不同，且删库那条路径按它们分别放过。翻成闸门那条即可——取消不了与暂停不了在这里是同一个原因。
func taskControlError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, task.ErrRunNotFound):
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

// markInterrupted 把上次运行留下的**活动态**与**排队中**运行全部转成**中断**：任务体随进程一起
// 没了，库里那行却还停在活动态。它属于装配期，早于任何新运行落地。
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

	count, err := e.engine.MarkInterrupted(ctx)
	if err != nil {
		slog.Warn("Failed to recover interrupted runs", "error", err)
		return
	}
	if count > 0 {
		slog.Info("Recovered interrupted runs", "count", count)
	}
}

// stopAllRuntimes 在停机时逐个取消运行的 ctx 并放行它的**暂停闸门**。
func (e *taskEngine) stopAllRuntimes() {
	e.engine.StopAll()
}
