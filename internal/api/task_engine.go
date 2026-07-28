// 业务说明：本文件是后台任务引擎，持有任务子域的**全部可变状态**并独占其锁。
//
// 此前这些状态挂在 taskEngine 上、方法却挂在 Controller 上（`c.taskEngine.xxx` 出现 130+ 次），
// 于是「谁能改任务表」这条边界在 17.9k 行的 api 包里没有任何结构约束——任何 Controller 方法都能
// 顺手锁 mutex 改 tasks。现在状态与方法归一到同一个类型上：引擎只依赖三样外部能力（落盘用的 store、
// 投递 SSE 的 publish、感知停机的 done），Controller 侧退化为 HTTP 层与领域重启函数的持有者。
//
// 并发约定：
//   - 除 relaunchers（装配期写入，之后只读）外，全部字段由 mutex 保护。
//   - 名字带 Locked 后缀的方法要求调用方已持锁；其余方法自行加解锁。
//   - 任何要把 TaskStatus 带出临界区的路径（异步落盘、HTTP 序列化、重试取参）必须先 cloneTaskStatus，
//     否则会与仍在持锁原地写 Metrics/Params 的进度回调撞成 `fatal error: concurrent map read and
//     map write`——那是 runtime throw 而非 panic，recover 与 middleware.Recoverer 都拦不住。
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/taskcontrol"
)

// 任务控制（暂停/恢复/取消）与重试查找的哨兵错误。引擎只表达「为什么不行」，
// 具体的 HTTP 状态码与英文文案由 controller_tasks.go 的映射表决定，避免引擎依赖传输层语义。
var (
	errTaskNotFound          = errors.New("task not found")
	errTaskNotRunning        = errors.New("task is not running")
	errTaskNotPaused         = errors.New("task is not paused")
	errTaskNotPausable       = errors.New("task cannot be paused")
	errTaskNotCancelable     = errors.New("task cannot be cancelled")
	errTaskGateUnavailable   = errors.New("task pause gate is not available")
	errTaskCancelUnavailable = errors.New("task cancellation is not available")
)

// taskEngine 是后台任务引擎：任务表、运行时句柄、序号、异步落盘的待写集合与唤醒信号，以及任务重试注册表。
type taskEngine struct {
	// ---- 外部依赖（装配期注入，之后只读）----

	// store 用于任务快照落盘与查询历史任务；为 nil 时引擎退化为纯内存模式（部分白盒测试如此使用）。
	store database.Store
	// publish 把任务状态变更投递给 SSE 订阅者；为 nil 时不投递。
	publish func(string)
	// done 返回 Controller 的生命周期信号，落盘 goroutine 据此退出前做最后一次刷盘。
	done func() <-chan struct{}

	// ---- 受 mutex 保护的状态 ----

	mutex    sync.Mutex
	tasks    map[string]TaskStatus
	runtimes map[string]*TaskRuntime
	seq      int64
	// persistPending 是待异步落盘的最新任务快照（按 key 合并）。进度更新只写内存 + 入此集合，
	// 专用 goroutine（startTaskPersister）节流批量写 SQLite，避免在临界区内同步写库阻塞任务 API 与系列详情页。
	// persistWake 在终态时唤醒该 goroutine 立即刷，缩短终态落库延迟（缓冲 1）。
	persistPending map[string]TaskStatus
	persistWake    chan struct{}

	// relaunchers 是任务重试的注册表（taskType -> 重启函数），也是「可重试类型」的唯一事实来源。
	// 在 newControllerCore 中一次性填好（重启函数要调 Controller 的领域方法，故由 Controller 构建），
	// 此后只读，不需要持锁。
	relaunchers map[string]taskRelauncher
}

func newTaskEngine(store database.Store, publish func(string), done func() <-chan struct{}) *taskEngine {
	return &taskEngine{
		store:          store,
		publish:        publish,
		done:           done,
		tasks:          make(map[string]TaskStatus),
		runtimes:       make(map[string]*TaskRuntime),
		persistPending: make(map[string]TaskStatus),
		persistWake:    make(chan struct{}, 1),
	}
}

// isRetryableTaskType 由注册表派生：注册了 relauncher 的类型即可重试，消除第二份硬编码清单。
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
func (e *taskEngine) flushTaskPersist() {
	if e.store == nil {
		return
	}
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

// ---- 启动 ----

func (e *taskEngine) startTask(key, taskType, message string, total int) bool {
	return e.startTaskWithOptionsCore(key, taskType, message, "", nil, total, false, false)
}

func (e *taskEngine) startCancelableTask(key, taskType, message string, total int) bool {
	return e.startTaskWithOptionsCore(key, taskType, message, "", nil, total, true, false)
}

func (e *taskEngine) startPausableCancelableTask(key, taskType, message string, total int) bool {
	return e.startTaskWithOptionsCore(key, taskType, message, "", nil, total, true, true)
}

// startTaskMsg 等是启动方法的 i18n 版：初始消息用稳定码 + 占位参数。
func (e *taskEngine) startTaskMsg(key, taskType, code string, params map[string]string, total int) bool {
	return e.startTaskWithOptionsCore(key, taskType, "", code, params, total, false, false)
}

func (e *taskEngine) startPausableCancelableTaskMsg(key, taskType, code string, params map[string]string, total int) bool {
	return e.startTaskWithOptionsCore(key, taskType, "", code, params, total, true, true)
}

func (e *taskEngine) startTaskWithOptionsCore(key, taskType, message, code string, params map[string]string, total int, canCancel bool, canPause bool) bool {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	if e.tasks == nil {
		e.tasks = make(map[string]TaskStatus)
	}

	if existing, ok := e.tasks[key]; ok && taskIsActive(existing.Status) {
		return false
	}

	now := time.Now()
	e.seq++
	scope, scopeID := inferTaskScope(taskType, key)
	task := TaskStatus{
		Key:           key,
		Type:          taskType,
		Scope:         scope,
		ScopeID:       scopeID,
		Status:        "running",
		Message:       message,
		MessageCode:   code,
		MessageParams: params,
		Current:       0,
		Total:         total,
		CanCancel:     canCancel,
		CanPause:      canPause,
		Retryable:     e.isRetryableTaskType(taskType),
		StartedAt:     now,
		UpdatedAt:     now,
		Sequence:      e.seq,
	}
	e.tasks[key] = task
	e.pruneTasksLocked()
	e.persistTaskStatus(task)
	e.publishTaskStatusLocked(task)
	return true
}

// newTaskContext 为任务体建立可取消 + 可暂停的 ctx，并登记运行时句柄供暂停/恢复/取消接口操作。
// 返回的 cleanup 必须在任务体退出时调用，否则运行时句柄会一直留在表里。
func (e *taskEngine) newTaskContext(key string) (context.Context, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	gate := taskcontrol.NewPauseGate()
	taskCtx := taskcontrol.WithPauseGate(ctx, gate)

	e.mutex.Lock()
	if e.runtimes == nil {
		e.runtimes = make(map[string]*TaskRuntime)
	}
	e.runtimes[key] = &TaskRuntime{
		Context:   taskCtx,
		Cancel:    cancel,
		PauseGate: gate,
		StartedAt: time.Now(),
	}
	e.mutex.Unlock()

	cleanup := func() {
		e.mutex.Lock()
		delete(e.runtimes, key)
		e.mutex.Unlock()
	}

	return taskCtx, cleanup
}

// ---- 进度更新 ----

func (e *taskEngine) updateTask(key string, current, total int, message string) {
	e.updateTaskCore(key, current, total, message, "", nil)
}

// updateTaskMsg 是 updateTask 的 i18n 版：只发稳定消息码 + 占位参数，由前端本地化渲染。
func (e *taskEngine) updateTaskMsg(key string, current, total int, code string, params map[string]string) {
	e.updateTaskCore(key, current, total, "", code, params)
}

func (e *taskEngine) updateTaskCore(key string, current, total int, message, code string, params map[string]string) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	task, ok := e.tasks[key]
	if !ok {
		return
	}
	task.Current = current
	if total >= 0 {
		task.Total = total
	}
	applyTaskMessage(&task, message, code, params)
	task.UpdatedAt = time.Now()
	e.seq++
	task.Sequence = e.seq
	enrichTaskProgress(&task)
	e.tasks[key] = task
	e.persistTaskStatus(task)
	e.publishTaskStatusLocked(task)
}

func (e *taskEngine) updateTaskDetails(key string, current, total int, message, phase, currentItem string, metrics map[string]int64, labels map[string]string) {
	e.updateTaskDetailsCore(key, current, total, message, "", nil, phase, currentItem, metrics, labels)
}

// updateTaskDetailsMsg 是 updateTaskDetails 的 i18n 版：消息改为稳定码 + 占位参数，其余（phase/currentItem/
// metrics/labels）语义不变。
func (e *taskEngine) updateTaskDetailsMsg(key string, current, total int, code string, params map[string]string, phase, currentItem string, metrics map[string]int64, labels map[string]string) {
	e.updateTaskDetailsCore(key, current, total, "", code, params, phase, currentItem, metrics, labels)
}

func (e *taskEngine) updateTaskDetailsCore(key string, current, total int, message, code string, params map[string]string, phase, currentItem string, metrics map[string]int64, labels map[string]string) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	task, ok := e.tasks[key]
	if !ok {
		return
	}
	if !taskIsActive(task.Status) {
		return
	}
	task.Current = current
	if total >= 0 {
		task.Total = total
	}
	applyTaskMessage(&task, message, code, params)
	if phase != "" {
		task.Phase = phase
	}
	if currentItem != "" {
		task.CurrentItem = currentItem
	}
	if len(metrics) > 0 {
		if task.Metrics == nil {
			task.Metrics = make(map[string]int64, len(metrics))
		}
		for k, v := range metrics {
			task.Metrics[k] = v
		}
	}
	if len(labels) > 0 {
		if task.Labels == nil {
			task.Labels = make(map[string]string, len(labels))
		}
		for k, v := range labels {
			task.Labels[k] = v
		}
	}
	task.UpdatedAt = time.Now()
	e.seq++
	task.Sequence = e.seq
	enrichTaskProgress(&task)
	e.tasks[key] = task
	e.persistTaskStatus(task)
	e.publishTaskStatusLocked(task)
}

func (e *taskEngine) setTaskMetadata(key string, params map[string]string, scopeName string) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	task, ok := e.tasks[key]
	if !ok {
		return
	}
	task.Params = params
	if strings.TrimSpace(scopeName) != "" {
		task.ScopeName = scopeName
	}
	e.seq++
	task.Sequence = e.seq
	hydrateTaskStatusDerivedFields(&task)
	e.tasks[key] = task
	e.persistTaskStatus(task)
	e.publishTaskStatusLocked(task)
}

func (e *taskEngine) mergeTaskParams(key string, params map[string]string) {
	if len(params) == 0 {
		return
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()

	task, ok := e.tasks[key]
	if !ok {
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
	hydrateTaskStatusDerivedFields(&task)
	e.tasks[key] = task
	e.persistTaskStatus(task)
	e.publishTaskStatusLocked(task)
}

func (e *taskEngine) mergeRunningTaskMetricSums(key string, increments map[string]int64, params map[string]string) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	task, ok := e.tasks[key]
	if !ok || task.Status != "running" {
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
	hydrateTaskStatusDerivedFields(&task)
	e.tasks[key] = task
	e.persistTaskStatus(task)
	e.publishTaskStatusLocked(task)
}

func (e *taskEngine) setTaskEffectiveLimit(key string, limit TaskLimits) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	task, ok := e.tasks[key]
	if !ok {
		return
	}
	task.EffectiveLimit = &limit
	task.UpdatedAt = time.Now()
	e.seq++
	task.Sequence = e.seq
	hydrateTaskStatusDerivedFields(&task)
	e.tasks[key] = task
	e.persistTaskStatus(task)
	e.publishTaskStatusLocked(task)
}

// ---- 终态 ----

func (e *taskEngine) finishTask(key, message string) {
	e.completeTaskCore(key, "completed", message, "", nil)
}

// finishTaskMsg 是 finishTask 的 i18n 版：只发稳定消息码 + 占位参数。
func (e *taskEngine) finishTaskMsg(key, code string, params map[string]string) {
	e.completeTaskCore(key, "completed", "", code, params)
}

func (e *taskEngine) failTask(key, message string) {
	e.failTaskCore(key, message, "", nil, message)
}

// completeTaskMsg 是 completeTask 的 i18n 版（多用于取消态等终态）。
func (e *taskEngine) completeTaskMsg(key, status, code string, params map[string]string) {
	e.completeTaskCore(key, status, "", code, params)
}

func (e *taskEngine) completeTaskCore(key, status, message, code string, params map[string]string) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	task, ok := e.tasks[key]
	if !ok {
		return
	}
	now := time.Now()
	task.Status = status
	applyTaskMessage(&task, message, code, params)
	if status != "failed" {
		task.Error = ""
	}
	task.CanCancel = false
	task.CanPause = false
	task.CanResume = false
	task.PausedAt = nil
	task.PauseReason = ""
	if task.Total > 0 {
		task.Current = task.Total
	}
	task.UpdatedAt = now
	task.FinishedAt = &now
	e.seq++
	task.Sequence = e.seq
	e.tasks[key] = task
	delete(e.runtimes, key)
	e.persistTaskStatusFinal(task)
	e.publishTaskStatusLocked(task)
}

func (e *taskEngine) failTaskWithError(key, message, taskError string) {
	e.failTaskCore(key, message, "", nil, taskError)
}

// failTaskErrMsg 是 failTaskWithError 的 i18n 版：显示消息用稳定码，taskError 保留原始技术错误串（诊断用，不翻译）。
func (e *taskEngine) failTaskErrMsg(key, code string, params map[string]string, taskError string) {
	e.failTaskCore(key, "", code, params, taskError)
}

func (e *taskEngine) failTaskCore(key, message, code string, params map[string]string, taskError string) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	task, ok := e.tasks[key]
	if !ok {
		return
	}
	now := time.Now()
	task.Status = "failed"
	applyTaskMessage(&task, message, code, params)
	task.Error = taskError
	task.CanCancel = false
	task.CanPause = false
	task.CanResume = false
	task.PausedAt = nil
	task.UpdatedAt = now
	task.FinishedAt = &now
	e.seq++
	task.Sequence = e.seq
	e.tasks[key] = task
	delete(e.runtimes, key)
	e.persistTaskStatusFinal(task)
	e.publishTaskStatusLocked(task)
}

// pruneTasksLocked 把内存任务表裁到 maxRetainedTasks，**活动任务无条件保留**。
//
// 内存里的这份是活动任务的唯一可写副本：updateTaskCore 等一律「tasks[key] 查不到就 return」。
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
		sortTaskStatusesByRecency(finished)
		finished = finished[:quota]
	}
	for _, task := range finished {
		next[task.Key] = task
	}
	e.tasks = next
}

// sortTaskStatusesByRecency 按「最近活动」降序排：序号优先，其后依次是更新时间、开始时间、key。
// 淘汰与列表接口共用同一套定序，保证「被裁掉的」与「排在末尾的」是同一批。
func sortTaskStatusesByRecency(items []TaskStatus) {
	sort.Slice(items, func(i, j int) bool {
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

// listTaskStatuses 合并 DB 历史记录与内存中的活动任务，返回按最近活动降序排列的快照副本。
func (e *taskEngine) listTaskStatuses(ctx context.Context, filters database.TaskFilters) ([]TaskStatus, error) {
	records, err := e.store.ListTasks(ctx, filters)
	if err != nil {
		return nil, err
	}
	e.mutex.Lock()
	if e.tasks == nil {
		e.tasks = make(map[string]TaskStatus)
	}
	items := make([]TaskStatus, 0, len(records)+len(e.tasks))
	seen := make(map[string]bool, len(records))
	for _, record := range records {
		task := taskStatusFromRecord(record)
		// 进度改为异步落盘后，活动任务的内存快照比 DB 记录更新（DB 可能滞后最多一个落盘周期）。
		// 同时存在于内存与 DB 时用内存版本，避免 API 返回被滞后的 DB 进度覆盖。
		if memTask, ok := e.tasks[task.Key]; ok {
			// 克隆：返回的切片会在 Unlock 之后由 listTasks 交给 json.Marshal 遍历，
			// 而运行中的任务仍在持锁原地写 Metrics/Params，共享 map 会导致并发读写 fatal。
			task = cloneTaskStatus(memTask)
		}
		items = append(items, task)
		seen[task.Key] = true
	}
	for _, task := range e.tasks {
		if seen[task.Key] {
			continue
		}
		if filters.Status != "" && task.Status != filters.Status {
			continue
		}
		if filters.Scope != "" && task.Scope != filters.Scope {
			continue
		}
		if filters.Type != "" && task.Type != filters.Type {
			continue
		}
		if filters.ScopeID != nil && (task.ScopeID == nil || *task.ScopeID != *filters.ScopeID) {
			continue
		}
		if filters.Query != "" {
			haystack := strings.ToLower(task.Key + " " + task.Message + " " + task.Error)
			if !strings.Contains(haystack, strings.ToLower(filters.Query)) {
				continue
			}
		}
		items = append(items, cloneTaskStatus(task))
	}
	e.mutex.Unlock()

	sortTaskStatusesByRecency(items)
	if filters.Limit > 0 && len(items) > filters.Limit {
		items = items[:filters.Limit]
	}
	return items, nil
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

	records, err := e.store.ListTasks(ctx, database.TaskFilters{Query: key, Limit: 20})
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
func (e *taskEngine) clear(ctx context.Context, filters database.TaskFilters) (int64, error) {
	removed, err := e.store.DeleteTasks(ctx, filters)
	if err != nil {
		return 0, err
	}

	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.tasks == nil {
		e.tasks = make(map[string]TaskStatus)
	}
	for key, task := range e.tasks {
		if filters.Status != "" && task.Status != filters.Status {
			continue
		}
		if filters.Scope != "" && task.Scope != filters.Scope {
			continue
		}
		if filters.Type != "" && task.Type != filters.Type {
			continue
		}
		if filters.ScopeID != nil && (task.ScopeID == nil || *task.ScopeID != *filters.ScopeID) {
			continue
		}
		// paused / cancelling 与 running 一样，内存里这份是唯一可写副本：删掉之后
		// resume 会变成 404，而任务体仍卡在 PauseGate 上永远等不到放行。
		if taskIsActive(task.Status) {
			continue
		}
		delete(e.tasks, key)
		// 同步清掉待落盘快照：否则异步落盘 goroutine 会把刚被 DeleteTasks 删掉的任务重新 UpsertTask 复活。
		delete(e.persistPending, key)
	}
	return removed, nil
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
	applyTaskMessage(&task, "", "task.msg.control.paused", nil)
	task.UpdatedAt = now
	e.seq++
	task.Sequence = e.seq
	enrichTaskProgress(&task)
	e.tasks[key] = task
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
	runtime.PauseGate.Resume()
	task.Status = "running"
	task.CanPause = true
	task.CanResume = false
	task.PausedAt = nil
	task.PauseReason = ""
	applyTaskMessage(&task, "", "task.msg.control.resumed", nil)
	task.UpdatedAt = time.Now()
	e.seq++
	task.Sequence = e.seq
	enrichTaskProgress(&task)
	e.tasks[key] = task
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

	runtime.Cancel()
	// 一并放行暂停闸门：暂停中的任务卡在 gate 上，只 Cancel 不 Resume 会让它永远收不到取消信号。
	if runtime.PauseGate != nil {
		runtime.PauseGate.Resume()
	}
	task.CanCancel = false
	task.CanPause = false
	task.CanResume = false
	task.Status = "cancelling"
	applyTaskMessage(&task, "", "task.msg.control.cancelling", nil)
	task.UpdatedAt = time.Now()
	e.seq++
	task.Sequence = e.seq
	e.tasks[key] = task
	e.persistTaskStatus(task)
	e.publishTaskStatusLocked(task)
	return nil
}

// stopAllRuntimes 在停机时放行所有暂停闸门并取消所有任务 ctx。
// 先在锁内收集句柄再在锁外调用：Cancel/Resume 会唤醒任务体，而任务体的进度回调要拿同一把锁。
func (e *taskEngine) stopAllRuntimes() {
	e.mutex.Lock()
	cancels := make([]context.CancelFunc, 0, len(e.runtimes))
	pauses := make([]*taskcontrol.PauseGate, 0, len(e.runtimes))
	for _, runtime := range e.runtimes {
		if runtime == nil {
			continue
		}
		if runtime.PauseGate != nil {
			pauses = append(pauses, runtime.PauseGate)
		}
		if runtime.Cancel != nil {
			cancels = append(cancels, runtime.Cancel)
		}
	}
	e.mutex.Unlock()

	for _, gate := range pauses {
		gate.Resume()
	}
	for _, cancel := range cancels {
		cancel()
	}
}
