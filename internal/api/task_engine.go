// 后台任务引擎的状态与生命周期：任务表、运行时句柄、异步落盘、SSE 投递与节流、淘汰、查询与控制。
// 任务子域的全部可变状态归它所有，只依赖注入进来的外部能力——落盘的 store、投递 SSE 的 publish、
// 感知停机的 done、开受停机管辖 goroutine 的 runBackground、执行**磁盘作业**的 diskWork。
// Controller 不得直接触碰任务表。
// 启动仪式在同包的 task_run.go，纯转换与派生字段在 task_model.go。

package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/diskwork"
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

// taskProgressPublishInterval 是逐条目进度的 SSE 最小投递间隔。
//
// 取 200ms 的依据在前端而不是后端：进度条本身带 500ms 的宽度过渡动画，
// 比这更密的推送浏览器根本画不出第二帧。仓库里其余节流也都更粗
// （扫描器上报 250ms、任务落盘 500ms），所以这道闸门不会让任何现有可见刷新变粗，
// 只砍掉逐条目回调那种上百赫兹的空转。
const taskProgressPublishInterval = 200 * time.Millisecond

// taskPublishGate 是单个任务的 SSE 发布水位。
//
// 除时间外还记下上次**已发布**的展示态：阶段与文案的跃迁（「清理缓存」→「重建中」）
// 是用户正在等的语义变化，被计数器节流吞掉会让任务气泡长时间停在过期的阶段名上。
// 而纯计数器推进吞掉多少都无所谓——载荷是全量快照，下一条自然带上累积后的最新值。
type taskPublishGate struct {
	at          time.Time
	status      string
	phase       string
	messageCode string
	message     string
}

// suppresses 判断这条进度能否被本水位吞掉：仍在窗口内、且展示态一字未变。
func (g taskPublishGate) suppresses(task TaskStatus, now time.Time, window time.Duration) bool {
	return now.Sub(g.at) < window &&
		g.status == task.Status &&
		g.phase == task.Phase &&
		g.messageCode == task.MessageCode &&
		g.message == task.Message
}

// taskEngine 是后台任务引擎：任务表、运行时句柄、序号、异步落盘的待写集合与唤醒信号，以及任务重试注册表。
//
// 并发约定：
//   - 除 relaunchers（装配期写入，之后只读）外，全部字段由 mutex 保护。
//   - 名字带 Locked 后缀的方法要求调用方已持锁；其余方法自行加解锁。
//   - 任何要把 TaskStatus 带出临界区的路径（异步落盘、HTTP 序列化、重试取参）必须先 cloneTaskStatus，
//     否则会与仍在持锁原地写 Metrics/Params 的进度回调撞成 `fatal error: concurrent map read and
//     map write`——那是 runtime throw 而非 panic，recover 与 middleware.Recoverer 都拦不住。
type taskEngine struct {
	// ---- 外部依赖（装配期注入，之后只读）----

	// store 用于任务快照落盘与查询历史任务；为 nil 时引擎退化为纯内存模式（部分白盒测试如此使用）。
	store database.Store
	// publish 把任务状态变更投递给 SSE 订阅者；为 nil 时不投递。
	publish func(string)
	// done 返回 Controller 的生命周期信号，落盘 goroutine 据此退出前做最后一次刷盘。
	done func() <-chan struct{}
	// runBackground 开一个受停机管辖的 goroutine（登记停机 WaitGroup、关闭后拒绝新任务）。
	// 任务体必须经这项能力启动：外部替引擎开 goroutine 再反向伸手改任务表，会多套一层调度，
	// 停机竞态下任务被静默丢弃、运行时句柄泄漏，且两处都不会有编译错误。
	// 测试注入同步执行版本即可确定性地断言终态，不必等待真实 goroutine。
	runBackground func(func())
	// diskWork 是交给**任务句柄**的**磁盘作业**入口，留 nil 的后果见 taskrun.New。
	// 不启动任务体的白盒测试即如此构造。
	diskWork *diskwork.Runner

	// ---- 受 mutex 保护的状态 ----

	mutex    sync.Mutex
	tasks    map[string]TaskStatus
	runtimes map[string]*TaskRuntime
	// persistMutex 把「异步落盘」与「清除任务」串成一条，两者都在 mutex 之外调用 store。
	// flushTaskPersist 在 mutex 内把待写集合换出去、在锁外逐条 UpsertTask，clear 的 DeleteTasks
	// 插在这中间时，删掉的行会被随后那笔 UpsertTask 复活——待写集合里的 delete 够不着已经换出去的那批。
	// 嵌套顺序恒为 persistMutex → mutex，反过来拿会死锁。
	persistMutex sync.Mutex
	// seq 是任务的单调序号，也是任务中心的主排序键。装配期从库里已用掉的最大值接上（restoredTaskSequence），
	// 因此跨重启单调——它是「最近活动」的唯一事实来源，时间列不是（见 database.SqlStore.ListTasks）。
	seq int64
	// persistPending 是待异步落盘的最新任务快照（按 key 合并）。进度更新只写内存 + 入此集合，
	// 专用 goroutine（startTaskPersister）节流批量写 SQLite，避免在临界区内同步写库阻塞任务 API 与系列详情页。
	// persistWake 在终态时唤醒该 goroutine 立即刷，缩短终态落库延迟（缓冲 1）。
	persistPending map[string]TaskStatus
	persistWake    chan struct{}

	// relaunchers 是任务重试的注册表（taskType -> 重启函数），也是「可重试类型」的唯一事实来源。
	// 在 newControllerCore 中一次性填好（重启函数要调 Controller 的领域方法，故由 Controller 构建），
	// 此后只读，不需要持锁。
	relaunchers map[string]taskRelauncher

	// publishGates 记录每个任务上次投递进度的时刻与展示态，用于节流逐条目进度（见 taskPublishGate）。
	publishGates map[string]taskPublishGate

	// now 让测试注入可控时钟。生产恒为 nil，走 time.Now。
	// 节流的正确性只能靠时序断言证明——固定 sleep 的用例既慢又杀不掉「水位只写一次」这类错误实现。
	now func() time.Time
}

// clock 返回当前时刻（测试可经 now 字段注入）。
func (e *taskEngine) clock() time.Time {
	if e.now != nil {
		return e.now()
	}
	return time.Now()
}

func newTaskEngine(store database.Store, publish func(string), done func() <-chan struct{}, runBackground func(func()), diskWork *diskwork.Runner) *taskEngine {
	return &taskEngine{
		store:          store,
		publish:        publish,
		done:           done,
		runBackground:  runBackground,
		diskWork:       diskWork,
		tasks:          make(map[string]TaskStatus),
		runtimes:       make(map[string]*TaskRuntime),
		seq:            restoredTaskSequence(store),
		persistPending: make(map[string]TaskStatus),
		persistWake:    make(chan struct{}, 1),
		publishGates:   make(map[string]taskPublishGate),
	}
}

// restoredTaskSequence 取库里已用掉的最大任务序号，供新引擎接着往下发。
//
// 它放在构造期而不是某条启动路径上：序号从 0 重来不会有任何编译错误或运行时报错，只会让重启后
// 新起的任务排在全部历史之后——而任务表没有行数上限（maxRetainedTasks 只裁内存表，DB 只有用户手动
// 清理才删），一次大库扫描就能留下几千条，于是前端那一页 50 条里一个新任务都看不到。
//
// 纯内存引擎（store 为 nil）与读库失败都从 0 开始：这是降级不是失败，服务照常可用，
// 代价只是任务中心的历史定序退回旧行为。
func restoredTaskSequence(store database.Store) int64 {
	if store == nil {
		return 0
	}
	maxSequence, err := store.MaxTaskSequence(context.Background())
	if err != nil {
		slog.Warn("Failed to restore task sequence", "error", err)
		return 0
	}
	return maxSequence
}

// isRetryableTaskType 由注册表派生：注册了 relauncher 的类型即可重试。
// 「哪些类型可重试」不得另立第二份清单——两份清单一旦不同步，界面上的重试按钮会指向一个没人能重启的任务。
func (e *taskEngine) isRetryableTaskType(taskType string) bool {
	_, ok := e.relaunchers[taskType]
	return ok
}

// relauncherFor 返回该任务类型的重启函数；未注册即不可重试。
func (e *taskEngine) relauncherFor(taskType string) (taskRelauncher, bool) {
	relaunch, ok := e.relaunchers[taskType]
	return relaunch, ok
}

// ---- 落盘 ----

// persistTaskStatus 记录一个待异步落盘的任务快照。调用方必须持有 mutex：这里只写内存里的
// persistPending（按 key 合并，最新快照胜），真正的 UpsertTask 由 startTaskPersister 在锁外
// 节流批量执行，避免在临界区内同步写 SQLite（扫描批量事务期间可阻塞任务 API 与系列详情页最长
// busy_timeout）。单一写入方 + 按 key 合并，避免进度写与终态写乱序覆盖。
func (e *taskEngine) persistTaskStatus(task TaskStatus) {
	if e.store == nil {
		return
	}
	if e.persistPending == nil {
		e.persistPending = make(map[string]TaskStatus)
	}
	// 必须存深拷贝：TaskStatus 的结构体拷贝共享同一批 map header，而 flushTaskPersist 会在
	// 释放 mutex 之后才遍历这些 map。若存的是活任务的 map，进度回调（持锁原地写）与落盘
	// 遍历（锁外读）就会重叠，触发 concurrent map read and map write 的 fatal error。
	e.persistPending[task.Key] = cloneTaskStatus(task)
}

// persistTaskStatusFinal 用于任务终态（完成/失败/取消）：仍走同一异步队列（保持单一写入方、不与
// 进度写乱序），但额外唤醒落盘 goroutine 立即刷，缩短终态落库延迟。调用方持有 mutex。
func (e *taskEngine) persistTaskStatusFinal(task TaskStatus) {
	e.persistTaskStatus(task)
	select {
	case e.persistWake <- struct{}{}:
	default:
	}
}

// startTaskPersister 是任务快照的唯一落盘 goroutine：500ms 节流一次，终态唤醒时立即刷，关闭前再刷
// 一次，保证优雅关闭时最新进度/终态落库。经 runBackground 登记 backgroundWG，Close() 会等待其退出。
func (e *taskEngine) startTaskPersister() {
	if e.done == nil {
		return
	}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-e.done():
			e.flushTaskPersist()
			return
		case <-e.persistWake:
			e.flushTaskPersist()
		case <-ticker.C:
			e.flushTaskPersist()
		}
	}
}

// flushTaskPersist 在锁内取出并清空待落盘集合，再在锁外逐个 UpsertTask（避免持锁写库）。
// 整个过程持有 persistMutex：这一批已经不在待写集合里，clear 只能靠这把锁等它写完再删。
func (e *taskEngine) flushTaskPersist() {
	if e.store == nil {
		return
	}
	e.persistMutex.Lock()
	defer e.persistMutex.Unlock()

	e.mutex.Lock()
	if len(e.persistPending) == 0 {
		e.mutex.Unlock()
		return
	}
	pending := e.persistPending
	e.persistPending = make(map[string]TaskStatus)
	e.mutex.Unlock()

	for _, task := range pending {
		if err := e.store.UpsertTask(context.Background(), taskRecordFromStatus(task)); err != nil {
			slog.Warn("Failed to persist task status", "task_key", task.Key, "error", err)
		}
	}
}

// publishTaskProgressLocked 是**逐条目进度**专用的投递入口，带节流。调用方持有 mutex。
//
// 只有它会被节流；启动、终态、暂停/恢复/取消一律走 publishTaskStatusLocked 无条件投递——
// 那些是用户在等的状态跃迁，吞掉哪怕一条都会让界面停在错误的状态上。
//
// 节流只跳过**投递**，内存状态照常更新：任务表里始终是最新值，被跳过期间的进度会由
// 下一条投递带出去（载荷本就是全量快照，不是增量）。
func (e *taskEngine) publishTaskProgressLocked(task TaskStatus) {
	if e.publishGates == nil {
		e.publishGates = make(map[string]taskPublishGate)
	}
	now := e.clock()
	if gate, ok := e.publishGates[task.Key]; ok && gate.suppresses(task, now, taskProgressPublishInterval) {
		return
	}
	e.publishGates[task.Key] = taskPublishGate{
		at:          now,
		status:      task.Status,
		phase:       task.Phase,
		messageCode: task.MessageCode,
		message:     task.Message,
	}
	e.publishTaskStatusLocked(task)
}

// publishTaskStatusLocked 把任务快照投递给 SSE 订阅者。调用方持有 mutex：
// json.Marshal 在锁内完成，保证序列化读到的 map 不会被并发写。
func (e *taskEngine) publishTaskStatusLocked(task TaskStatus) {
	if e.publish == nil {
		return
	}
	payload, err := json.Marshal(task)
	if err != nil {
		slog.Warn("Failed to marshal task status", "task_key", task.Key, "error", err)
		return
	}
	// 统一经 sseBroker 投递（非阻塞、buffer 满则丢弃并告警）。
	e.publish("task_progress:" + string(payload))
}

// ---- 写入 ----

// writeTaskLocked 是任务表的**唯一写入出口**：就地重算调用方手里那份快照的派生字段，再落进
// 任务表——调用方随后拿同一份去落盘与投递，三处因此恒为同一帧。调用方持有 mutex。
//
// 派生字段（百分比、速率、ETA）必须在这条出口上算，而不是靠每条写入路径各自记得算：
// 漏掉一处不会有编译错误，后果是那条路径写出的那一帧带着上一帧的陈值。
func (e *taskEngine) writeTaskLocked(task *TaskStatus) {
	enrichTaskProgress(task)
	if e.tasks == nil {
		e.tasks = make(map[string]TaskStatus)
	}
	e.tasks[task.Key] = *task
}

// ---- 启动 ----

// taskPanicMessageCode 是 panic 兜底下发的失败文案码。
//
// panic 兜底是引擎唯一直接面向用户说话的地方，因此也必须只用 i18n 码：一句写死在这里的文案
// 没有任何地方能翻译它，非英文用户看到的就是一句英文。panic 值本身另走 TaskStatus.Error，
// 那是技术串、不面向翻译。
const taskPanicMessageCode = "task.msg.control.panicked"

// runTaskGoroutine 在受停机管辖的后台 goroutine 里执行任务体，并保证任务体一旦 panic，
// key 对应的任务被置为失败态。
//
// 这条兜底是「活动任务不被 pruneTasksLocked 淘汰」的必要配套。panic 的任务体走不到 settleTask，
// 任务将永远停在 running；而活动任务既不被淘汰、也不被 clearTasks 删除，于是那个任务键从此
// 恒定返回 409「已在运行」——同类任务在进程重启前再也发不起来。
// 把 panic 转成一次显式失败，用户至少能看到原因并重试。
func (e *taskEngine) runTaskGoroutine(key string, fn func()) {
	e.runBackground(func() {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("Background task panicked", "task_key", key, "panic", rec, "stack", string(debug.Stack()))
				e.failTask(key, taskPanicMessageCode, nil, fmt.Sprint(rec))
			}
		}()
		fn()
	})
}

// admitTaskLocked 是新任务落地的**唯一**机制：过任务键闸门，然后编号、入表、裁剪、落盘、投递。
// 调用方持有 mutex，并已把这个任务的全部字段填好（Sequence 与 Retryable 除外，由这里补）。
//
// 它与 claimTaskSlot 分家，是为了让「填字段」与「五步顺序」各在一处：后面这几步漏掉任何一步
// 都不会有编译错误，而后果各不相同——漏投递则界面上任务不出现，漏落盘则重启后任务凭空消失，
// 漏裁剪则任务表无限增长。
func (e *taskEngine) admitTaskLocked(task TaskStatus) bool {
	if e.tasks == nil {
		e.tasks = make(map[string]TaskStatus)
	}
	if existing, ok := e.tasks[task.Key]; ok && taskIsActive(existing.Status) {
		return false
	}

	task.Retryable = e.isRetryableTaskType(task.Type)
	e.seq++
	task.Sequence = e.seq

	e.writeTaskLocked(&task)
	e.pruneTasksLocked()
	e.persistTaskStatus(task)
	e.publishTaskStatusLocked(task)
	return true
}

// ---- 进度更新 ----

// mergeTaskParams 是**任务句柄**三条写入通道里的「设**任务参数**」那条。
//
// 判据与另外两条一致，都是**活动态**：**扫描观察者**活得比那次扫描调用久，一份迟到的报文
// 落在已收尾的任务上，会把它的序号推到任务中心最前、再投递一帧已经作废的展示态。
func (e *taskEngine) mergeTaskParams(key string, params map[string]string) {
	if len(params) == 0 {
		return
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()

	task, ok := e.tasks[key]
	if !ok || !taskIsActive(task.Status) {
		return
	}
	if task.Params == nil {
		task.Params = make(map[string]string, len(params))
	}
	for k, v := range params {
		task.Params[k] = v
	}
	task.UpdatedAt = time.Now()
	e.seq++
	task.Sequence = e.seq
	decodeTaskParams(&task)
	e.writeTaskLocked(&task)
	e.persistTaskStatus(task)
	e.publishTaskProgressLocked(task)
}

// mergeActiveTaskMetricSums 是三条写入通道里的「加指标」那条：报文只覆盖全局的一部分
// （逐资料库的扫描指标），全局总量只能在这里累加得出。
//
// 判据必须是**活动态**而不是运行中：**暂停中**与**取消中**的任务体仍在收尾，此刻丢掉的是
// io_wait_ms / paused_ms / hashed_files 这些**累加值**——它们不像整帧那样带着全量当前值，
// 丢一次就永远补不回来，任务面板上的吞吐量从此少一截。
func (e *taskEngine) mergeActiveTaskMetricSums(key string, increments map[string]int64, params map[string]string) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	task, ok := e.tasks[key]
	if !ok || !taskIsActive(task.Status) {
		return
	}
	if task.Params == nil {
		task.Params = make(map[string]string, len(params)+len(increments))
	}
	for k, v := range params {
		if strings.TrimSpace(v) != "" {
			task.Params[k] = v
		}
	}
	for k, inc := range increments {
		if inc == 0 {
			continue
		}
		current, _ := strconv.ParseInt(task.Params[k], 10, 64)
		task.Params[k] = strconv.FormatInt(current+inc, 10)
		if task.Metrics == nil {
			task.Metrics = make(map[string]int64)
		}
		task.Metrics[k] += inc
	}
	task.UpdatedAt = time.Now()
	e.seq++
	task.Sequence = e.seq
	decodeTaskParams(&task)
	e.writeTaskLocked(&task)
	e.persistTaskStatus(task)
	e.publishTaskProgressLocked(task)
}

// absorbTaskPauseLocked 把「这一次暂停」折进累计暂停时长并清掉暂停起点，供离开**已暂停**的每条
// 出口调用：恢复、取消，以及暂停中直接收尾。调用方持有 mutex。
//
// 累计值本身只有一个消费者——速率与 ETA 的分母（见 taskPausedSoFar）。少调一处不会有编译错误，
// 后果是那条出口之后的分母里凭空多出一段没人干活的时间，任务看起来慢了几倍。
func absorbTaskPauseLocked(task *TaskStatus, now time.Time) {
	if task.PausedAt == nil {
		return
	}
	if paused := now.Sub(*task.PausedAt); paused > 0 {
		task.ControlPausedMillis += paused.Milliseconds()
	}
	task.PausedAt = nil
}

// ---- 终态 ----

// finalizeTask 把任务落成 status 指名的那个**终态**，失败态除外（那条走 failTask，
// 它还要记下技术错误串）。
//
// 名字刻意不叫 complete：**完成**只是终态里的一个，**已取消**同样从这里写入。用「完成」命名
// 会让读代码的人以为取消另有一条路径，进而去找一个不存在的函数——CONTEXT.md 点名要避开这处模糊。
//
// **计数推进**只在**完成**那一条上被补到总数。
func (e *taskEngine) finalizeTask(key, status, code string, params map[string]string) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	task, ok := e.tasks[key]
	if !ok {
		return
	}
	now := time.Now()
	task.Status = status
	applyTaskMessage(&task, code, params)
	if status != "failed" {
		task.Error = ""
	}
	task.CanCancel = false
	task.CanPause = false
	task.CanResume = false
	absorbTaskPauseLocked(&task, now)
	task.PauseReason = ""
	// 只有**完成**补齐计数：`rebuild_index`、`cleanup_library`、`ai_grouping` 这类任务把总数
	// 声明成阶段数却从不推进计数，靠这一笔才显示成 1 / 1。**已取消**不跟着补——被取消掉的那些
	// 条目一个都没处理，补上去这个数就答不出「做完了多少」，用户据此以为活已经干完。
	if status == "completed" && task.Total > 0 {
		task.Current = task.Total
	}
	task.UpdatedAt = now
	task.FinishedAt = &now
	e.seq++
	task.Sequence = e.seq
	e.writeTaskLocked(&task)
	delete(e.runtimes, key)
	// 终态清掉水位：同名任务重跑时首帧必须无条件放行，否则会被上一轮的残留水位吞掉。
	delete(e.publishGates, key)
	e.persistTaskStatusFinal(task)
	e.publishTaskStatusLocked(task)
}

// failTask 把任务落成失败态。它与 finalizeTask 分家只为多带一个 taskError：
// 那是给用户看排查线索用的技术错误串，与 code 指名的那句可翻译文案是两条通道。
func (e *taskEngine) failTask(key, code string, params map[string]string, taskError string) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	task, ok := e.tasks[key]
	if !ok {
		return
	}
	now := time.Now()
	task.Status = "failed"
	applyTaskMessage(&task, code, params)
	task.Error = taskError
	task.CanCancel = false
	task.CanPause = false
	task.CanResume = false
	absorbTaskPauseLocked(&task, now)
	task.PauseReason = ""
	task.UpdatedAt = now
	task.FinishedAt = &now
	e.seq++
	task.Sequence = e.seq
	e.writeTaskLocked(&task)
	delete(e.runtimes, key)
	// 终态清掉水位：同名任务重跑时首帧必须无条件放行，否则会被上一轮的残留水位吞掉。
	delete(e.publishGates, key)
	e.persistTaskStatusFinal(task)
	e.publishTaskStatusLocked(task)
}

// pruneTasksLocked 把内存任务表裁到 maxRetainedTasks，**活动任务无条件保留**。
//
// 内存里的这份是活动任务的唯一可写副本：applyTaskProgress 等一律「tasks[key] 查不到就 return」。
// 一个仍在跑的任务被裁掉之后，它后续的全部进度、指标乃至终态更新都会静默失效，
// 任务面板上永远停在最后一次进度、也再不会变成「完成」。
//
// 而按 Sequence 降序排并不能保护它：Sequence 只在有更新时才递增，一个长时间无进度上报的
// 长任务（大库扫描的哈希阶段就是如此）会被后来的大量短任务超过，正好落进被裁的那一段——
// 这恰恰是缺陷最容易触发的情形，所以必须按状态判定而不是靠排序位置。
func (e *taskEngine) pruneTasksLocked() {
	if len(e.tasks) <= maxRetainedTasks {
		return
	}

	next := make(map[string]TaskStatus, maxRetainedTasks+1)
	finished := make([]TaskStatus, 0, len(e.tasks))
	for _, task := range e.tasks {
		if taskIsActive(task.Status) {
			next[task.Key] = task
			continue
		}
		finished = append(finished, task)
	}

	// 配额只在已终结的任务之间分配。活动任务本身就超额时 quota 为 0：
	// 此时全部保留活动任务、丢掉所有历史，也好过丢掉一个还在跑的任务。
	quota := maxRetainedTasks - len(next)
	if quota <= 0 {
		e.tasks = next
		return
	}
	if len(finished) > quota {
		sortTaskStatusesByActivity(finished)
		finished = finished[:quota]
	}
	for _, task := range finished {
		next[task.Key] = task
	}
	e.tasks = next
}

// sortTaskStatusesByActivity 定序任务：**活动态**在前，其后按「最近活动」降序——
// 序号优先，其后依次是更新时间、开始时间、key。
//
// 活动态优先不是展示偏好，而是可用性下限：序号只在有更新时才递增，一个长时间不上报进度的大库扫描
// 会被后来的大量短任务超过；任务表又没有行数上限，而前端只取第一页 50 条。只按序号排的话，
// 用户正等着看的那个任务恰好会掉出第一页——而它是唯一一个「还能变」的任务。
//
// 这里是任务中心顺序的唯一裁决处：DB 侧只按序号取候选历史（database.SqlStore.ListTasks），
// 活动任务不靠那一页捞回来——内存里恒有一份（pruneTasksLocked 无条件保留）。
// 淘汰与列表接口共用同一套定序，保证「被裁掉的」与「排在末尾的」是同一批；
// 淘汰那边的输入已剔除活动任务，故首个比较键在那里是空转。
func sortTaskStatusesByActivity(items []TaskStatus) {
	sort.Slice(items, func(i, j int) bool {
		if activeI, activeJ := taskIsActive(items[i].Status), taskIsActive(items[j].Status); activeI != activeJ {
			return activeI
		}
		if items[i].Sequence == items[j].Sequence {
			if items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
				if items[i].StartedAt.Equal(items[j].StartedAt) {
					return items[i].Key > items[j].Key
				}
				return items[i].StartedAt.After(items[j].StartedAt)
			}
			return items[i].UpdatedAt.After(items[j].UpdatedAt)
		}
		return items[i].Sequence > items[j].Sequence
	})
}

// ---- 查询 ----

// listTaskStatuses 合并 DB 历史记录与内存中的活动任务，返回定序后的快照副本
// （**活动态**在前，其后按最近活动降序，见 sortTaskStatusesByActivity）。
//
// 限流在合并**之后**再截断：DB 那一页按序号取回最近的 Limit 条历史，内存里的活动任务补进来后重排，
// 被截掉的只会是排在最末的历史。
func (e *taskEngine) listTaskStatuses(ctx context.Context, filters database.TaskFilters) ([]TaskStatus, error) {
	records, err := e.store.ListTasks(ctx, filters)
	if err != nil {
		return nil, err
	}
	merged := make(map[string]TaskStatus, len(records))
	for _, record := range records {
		merged[record.Key] = taskStatusFromRecord(record)
	}

	e.mutex.Lock()
	// 进度是异步落盘的，DB 记录最多滞后一个落盘周期。同时存在于内存与 DB 时必须取内存版本，
	// 否则 API 返回的进度会被滞后的 DB 快照盖回去。
	// 克隆：返回的切片会在 Unlock 之后由 listTasks 交给 json.Marshal 遍历，
	// 而运行中的任务仍在持锁原地写 Metrics/Params，共享 map 会导致并发读写 fatal。
	for key, task := range e.tasks {
		merged[key] = cloneTaskStatus(task)
	}
	e.mutex.Unlock()

	// 谓词判在合并**之后**，因此只有这一处：DB 那一页由 SQL 过滤，而内存版盖上去之后状态可能已经
	// 不满足筛选条件（库里那行还是 running，内存里已经完成）。两支各判一遍的话，
	// 新增一条筛选条件只补一处不会有编译错误，后果是筛选对其中一支静默失效。
	items := make([]TaskStatus, 0, len(merged))
	for _, task := range merged {
		if !taskMatchesFilters(task, filters) {
			continue
		}
		items = append(items, task)
	}

	sortTaskStatusesByActivity(items)
	if filters.Limit > 0 && len(items) > filters.Limit {
		items = items[:filters.Limit]
	}
	return items, nil
}

// taskMatchesFilters 是任务列表五条筛选谓词的**唯一**实现，DB 记录与内存快照合并之后统一判一次。
// 关键词与 SQL 侧同口径：键、消息、错误串接起来做大小写无关的子串匹配。
func taskMatchesFilters(task TaskStatus, filters database.TaskFilters) bool {
	if filters.Status != "" && task.Status != filters.Status {
		return false
	}
	if filters.Scope != "" && task.Scope != filters.Scope {
		return false
	}
	if filters.Type != "" && task.Type != filters.Type {
		return false
	}
	if filters.ScopeID != nil && (task.ScopeID == nil || *task.ScopeID != *filters.ScopeID) {
		return false
	}
	if filters.Query != "" {
		haystack := strings.ToLower(task.Key + " " + task.Message + " " + task.Error)
		if !strings.Contains(haystack, strings.ToLower(filters.Query)) {
			return false
		}
	}
	return true
}

// latestTaskByTypes 返回给定类型中最近更新的那个任务的**副本**；无匹配返回 nil。
// 供存储 IO 面板估算扫描/封面速率，不暴露内存里的活 map。
func (e *taskEngine) latestTaskByTypes(types ...string) *TaskStatus {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	var latest *TaskStatus
	for _, task := range e.tasks {
		matched := false
		for _, t := range types {
			if task.Type == t {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		if latest == nil || task.UpdatedAt.After(latest.UpdatedAt) {
			cloned := cloneTaskStatus(task)
			latest = &cloned
		}
	}
	return latest
}

// snapshotForRetry 取回任务快照供重试：优先内存（更新），未命中再查 DB 历史。
func (e *taskEngine) snapshotForRetry(ctx context.Context, key string) (TaskStatus, error) {
	e.mutex.Lock()
	task, ok := e.tasks[key]
	if ok {
		// 克隆后才能带出临界区：relauncher 会读 task.Params，而同名任务若仍在跑，
		// 其进度回调正持锁写同一个 map。
		task = cloneTaskStatus(task)
	}
	e.mutex.Unlock()
	if ok {
		return task, nil
	}

	// DB 这一支是按**任务键**的身份查找，不是搜索：内存表是有上限的缓存（pruneTasksLocked），
	// 重启后更是空的，而**中断**任务恰恰只在库里。TaskFilters 在键上只有 LIKE 一种谓词，
	// 因此这里不设 Limit——任务键互为子串（`scan_series_1` ⊂ `scan_series_1xx`，
	// `scan_library_` / `cleanup_library_` 同型），设了的话目标会被更新的同族键挤出那一页，
	// 用户看着列表里那条「中断，可重试」点重试却得到 404。命中量由 key 是主键约束住：
	// 一个任务键在库里只有一行，回来的只是那几条含它作子串的兄弟键。
	records, err := e.store.ListTasks(ctx, database.TaskFilters{Query: key})
	if err != nil {
		return TaskStatus{}, err
	}
	for _, record := range records {
		if record.Key == key {
			return taskStatusFromRecord(record), nil
		}
	}
	return TaskStatus{}, errTaskNotFound
}

// ---- 控制：清理 / 暂停 / 恢复 / 取消 / 停机 ----

// clear 按过滤条件删除 DB 中的任务记录，并同步清掉内存中对应的**非活动态**任务，返回删除的 DB 行数。
//
// 两步的顺序是**先内存、后库**：内存与待写集合的清空不依赖库，而删库要经 persistMutex 等一批
// 在途落盘写完，那段等待不该压着任务表的锁——进度回调与任务列表都要它。
// 删库排在最后还让「删掉的行不会再被写回来」成立：等待期间在途那批已经写完，此后再无该任务键的快照。
// 删库失败时内存那份已经清掉，但清的都是**终态**任务，库里那行还在，任务列表照常列得出来。
func (e *taskEngine) clear(ctx context.Context, filters database.TaskFilters) (int64, error) {
	e.mutex.Lock()
	if e.tasks == nil {
		e.tasks = make(map[string]TaskStatus)
	}
	for key, task := range e.tasks {
		if !taskMatchesClearFilters(task, filters) {
			continue
		}
		delete(e.tasks, key)
		// 同步清掉待落盘快照：否则异步落盘 goroutine 会把刚被 DeleteTasks 删掉的任务重新 UpsertTask 复活。
		delete(e.persistPending, key)
	}
	e.mutex.Unlock()

	// 与异步落盘串行：一批快照已被换出待写集合、正在锁外逐条 UpsertTask，此刻删掉的行会被它们写回来。
	e.persistMutex.Lock()
	defer e.persistMutex.Unlock()
	return e.store.DeleteTasks(ctx, filters)
}

// taskMatchesClearFilters 判断内存里这条任务是否该被这次清除带走。
//
// 它与列表的 taskMatchesFilters 是两套谓词，不能合并：清除多一条**活动态**豁免，
// 且不认关键词——`clearTasks` 把 Query 清空正是因为「按搜索词删除」不可预期。
// paused / cancelling 与 running 一样，内存里这份是唯一可写副本：删掉之后 resume 会变成 404，
// 而任务体仍卡在**暂停闸门**上永远等不到放行。
func taskMatchesClearFilters(task TaskStatus, filters database.TaskFilters) bool {
	if taskIsActive(task.Status) {
		return false
	}
	if filters.Status != "" && task.Status != filters.Status {
		return false
	}
	if filters.Scope != "" && task.Scope != filters.Scope {
		return false
	}
	if filters.Type != "" && task.Type != filters.Type {
		return false
	}
	if filters.ScopeID != nil && (task.ScopeID == nil || *task.ScopeID != *filters.ScopeID) {
		return false
	}
	return true
}

func (e *taskEngine) pause(key string) error {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	task, ok := e.tasks[key]
	if !ok {
		return errTaskNotFound
	}
	if task.Status != "running" {
		return errTaskNotRunning
	}
	if !task.CanPause {
		return errTaskNotPausable
	}
	runtime := e.runtimes[key]
	if runtime == nil || runtime.PauseGate == nil {
		return errTaskGateUnavailable
	}
	now := time.Now()
	runtime.PauseGate.Pause()
	task.Status = "paused"
	task.CanPause = false
	task.CanResume = true
	task.PausedAt = &now
	task.PauseReason = "manual_pause"
	applyTaskMessage(&task, "task.msg.control.paused", nil)
	task.UpdatedAt = now
	e.seq++
	task.Sequence = e.seq
	e.writeTaskLocked(&task)
	e.persistTaskStatus(task)
	e.publishTaskStatusLocked(task)
	return nil
}

func (e *taskEngine) resume(key string) error {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	task, ok := e.tasks[key]
	if !ok {
		return errTaskNotFound
	}
	if task.Status != "paused" {
		return errTaskNotPaused
	}
	runtime := e.runtimes[key]
	if runtime == nil || runtime.PauseGate == nil {
		return errTaskGateUnavailable
	}
	now := time.Now()
	runtime.PauseGate.Resume()
	task.Status = "running"
	task.CanPause = true
	task.CanResume = false
	absorbTaskPauseLocked(&task, now)
	task.PauseReason = ""
	applyTaskMessage(&task, "task.msg.control.resumed", nil)
	task.UpdatedAt = now
	e.seq++
	task.Sequence = e.seq
	e.writeTaskLocked(&task)
	e.persistTaskStatus(task)
	e.publishTaskStatusLocked(task)
	return nil
}

func (e *taskEngine) cancel(key string) error {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	task, ok := e.tasks[key]
	if !ok {
		return errTaskNotFound
	}
	if task.Status != "running" && task.Status != "paused" {
		return errTaskNotRunning
	}
	if !task.CanCancel {
		return errTaskNotCancelable
	}
	runtime := e.runtimes[key]
	if runtime == nil || runtime.Cancel == nil {
		return errTaskCancelUnavailable
	}

	now := time.Now()
	runtime.Cancel()
	// 一并放行暂停闸门：暂停中的任务卡在 gate 上，只 Cancel 不 Resume 会让它永远收不到取消信号。
	if runtime.PauseGate != nil {
		runtime.PauseGate.Resume()
	}
	task.CanCancel = false
	task.CanPause = false
	task.CanResume = false
	task.Status = "cancelling"
	// 闸门刚被放行，这次暂停到此为止：不在这里结账的话，从暂停直接取消的任务会把整段暂停
	// 留在分母里，一路带进**已取消**那一帧。
	absorbTaskPauseLocked(&task, now)
	task.PauseReason = ""
	applyTaskMessage(&task, "task.msg.control.cancelling", nil)
	task.UpdatedAt = now
	e.seq++
	task.Sequence = e.seq
	e.writeTaskLocked(&task)
	e.persistTaskStatus(task)
	e.publishTaskStatusLocked(task)
	return nil
}

// stopAllRuntimes 在停机时逐个取消任务 ctx 并放行它的**暂停闸门**，顺序与单任务 cancel 一致。
// 先在锁内收集句柄再在锁外调用：Cancel/Resume 会唤醒任务体，而任务体的进度回调要拿同一把锁。
//
// 取消与放行按**每个任务**成对做，而不是先放行全部再取消全部：后者的两段之间有一个窗口，
// 被放行的**已暂停**任务拿着尚未取消的 ctx 回去干活——过可中断点、取**存储令牌**、跑完一整个任务体。
// 任务越多窗口越宽。
//
// 放行本身不能省：**暂停闸门**的等待有一条不带 ctx 的分支（只等放行），而 Cancel 是个可空字段，
// 只取消 ctx 唤不醒这两种等待者。放在自己的取消之后，放行就只剩「让它去看一眼已经取消的 ctx」。
func (e *taskEngine) stopAllRuntimes() {
	e.mutex.Lock()
	runtimes := make([]*TaskRuntime, 0, len(e.runtimes))
	for _, runtime := range e.runtimes {
		if runtime == nil {
			continue
		}
		runtimes = append(runtimes, runtime)
	}
	e.mutex.Unlock()

	for _, runtime := range runtimes {
		if runtime.Cancel != nil {
			runtime.Cancel()
		}
		if runtime.PauseGate != nil {
			runtime.PauseGate.Resume()
		}
	}
}
