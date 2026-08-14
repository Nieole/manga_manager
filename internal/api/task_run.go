// 业务说明：本文件是任务引擎对外的**唯一启动入口**。
//
// 此前启动一个后台任务要手抄五步：申请任务槽位 → 写元数据 → 写并发上限 → 建可取消可暂停的上下文
// → 起后台 goroutine 并在其中手写「取消 / 失败 / 完成」三条收尾分支。这套流程的正确性完全依赖
// 「抄对」，而漏掉任何一步都不会有编译错误——代码里已经留下过多套一层 goroutine 导致任务被静默
// 丢弃的事故记录。
//
// 现在调用方只提交一份任务声明（TaskSpec）与一个任务体，其余全部由引擎承担。任务体只做两件事：
// 干活，以及经进度句柄报告**计数推进**与**阶段**。它不接触**任务键**、不自己判断**终态**、
// 不自己起 goroutine。
//
// 并发约定与 task_engine.go 一致：本文件里带 Locked 后缀之外的方法自行加解锁。

package api

import (
	"context"
	"errors"
	"strings"
	"time"
)

// TaskSpec 是一份任务声明：「这是一个什么任务」的完整描述，一次性交给引擎。
//
// 它把此前四次独立的写入（启动、元数据、作用域名、并发上限）合成一次原子落地，因此不再存在
// 「任务已经出现在列表里、但还没有作用域名」的窗口——那个窗口今天是可以被任务列表接口观察到的。
type TaskSpec struct {
	Key  string
	Type string

	// StartCode 与 StartParams 是起始文案的 i18n 码与占位参数。消息词汇只有 i18n 码一种。
	//
	// 名字必须与 Metadata 拉开距离：它落进 TaskStatus.MessageParams，而 Metadata 才落进
	// TaskStatus.Params。两者接反不会有编译错误，后果是任务参数丢失——**重启函数**从任务参数
	// 里读回原始入参，读不到就静默回落到默认值（AI 分组的语言设置正是这样一处）。
	StartCode   string
	StartParams map[string]string

	Total     int
	CanCancel bool
	CanPause  bool

	// Metadata 是任务参数（**重启函数**从这里读回原始入参），ScopeName 是作用域在界面上的显示名。
	Metadata  map[string]string
	ScopeName string

	// Limits 是该任务实际生效的并发上限。零值表示这个任务没有上限可报（多数维护任务如此），
	// 引擎不会为它凭空造一份全零的上限。
	Limits TaskLimits

	// 三条终态分支的**默认**文案码。常规任务因此不必为收尾写任何代码；
	// 「部分成功」「第一阶段失败」这类变体由任务体经 TaskResult.Code 覆盖对应的一条。
	CompleteCode string
	CancelCode   string
	FailCode     string
}

// TaskResult 是任务体对终态文案的可选修正。零值表示「用任务声明里的默认码」，
// 而不是「把文案清空」——绝大多数任务体返回的正是零值。
type TaskResult struct {
	Code   string
	Params map[string]string
}

// TaskProgress 是任务体唯一的进度上报句柄，已绑定**任务键**。
//
// 「谁有资格写某个任务的进度」因此从「谁会拼那个任务键字符串」变成「谁被交给了这个句柄」——
// 这是一条结构约束，而拼字符串不是。任务体内部也不再重复出现同一个键。
type TaskProgress struct {
	engine *taskEngine
	key    string
}

// Run 是启动一个后台任务的唯一入口。
//
// 返回 nil 表示已启动；返回 errTaskAlreadyRunning 表示同一**任务键**已有**活动态**任务
// （含**取消中**），此时任务体一步都不会执行。
//
// 刻意保留的不变量：槽位闸门**同步**执行、任务体**异步**执行。Run 返回时任务已在列表里、
// 而任务体尚未开跑，HTTP 层才能立即返回 202 而不被任务体阻塞。
func (e *taskEngine) Run(spec TaskSpec, fn func(ctx context.Context, tp *TaskProgress) (TaskResult, error)) error {
	if !e.claimTaskSlot(spec) {
		return errTaskAlreadyRunning
	}

	taskCtx, releaseRuntime := e.newTaskContext(spec.Key)
	progress := &TaskProgress{engine: e, key: spec.Key}

	e.runTaskGoroutine(spec.Key, func() {
		// 用 defer 归还**运行时句柄**，好处是它不依赖任务体走哪条出口：panic 也会经这里归还，
		// 之后才由 runTaskGoroutine 的兜底把任务置为失败态。手写这条延迟清理时漏写或写成裸调用，
		// 任务体一 panic 就泄漏一份 ctx 与**暂停闸门**——`launchLowPriorityBookHashBackfillTask` 曾如此。
		defer releaseRuntime()
		result, err := fn(taskCtx, progress)
		e.settleTask(spec, result, err)
	})

	return nil
}

// claimTaskSlot 同步申领任务槽位：同一任务键已有活动态任务时返回 false，
// 否则把整份任务声明一次性落成任务行，并投递一帧完整的首帧。
func (e *taskEngine) claimTaskSlot(spec TaskSpec) bool {
	now := time.Now()
	scope, scopeID := inferTaskScope(spec.Type, spec.Key)
	task := TaskStatus{
		Key:           spec.Key,
		Type:          spec.Type,
		Scope:         scope,
		ScopeID:       scopeID,
		Status:        "running",
		MessageCode:   spec.StartCode,
		MessageParams: spec.StartParams,
		Total:         spec.Total,
		CanCancel:     spec.CanCancel,
		CanPause:      spec.CanPause,
		Params:        spec.Metadata,
		StartedAt:     now,
		UpdatedAt:     now,
	}
	if strings.TrimSpace(spec.ScopeName) != "" {
		task.ScopeName = spec.ScopeName
	}
	if spec.Limits != (TaskLimits{}) {
		limits := spec.Limits
		task.EffectiveLimit = &limits
	}

	e.mutex.Lock()
	defer e.mutex.Unlock()
	return e.admitTaskLocked(task)
}

// settleTask 是三条终态分支的唯一裁决处：**任务体返回的错误**决定进哪一条，TaskResult 只能改文案。
//
// 不提供「任务体自行调用收尾方法」的第二条路径——那正是本次要消灭的隐式约定：
// 两条路径并存时，「忘了收尾」与「收了两次」都不会有编译错误。
func (e *taskEngine) settleTask(spec TaskSpec, result TaskResult, err error) {
	switch {
	case err == nil:
		e.finalizeTaskCore(spec.Key, "completed", "", firstNonEmptyTaskValue(result.Code, spec.CompleteCode), result.Params)
	case errors.Is(err, context.Canceled):
		e.finalizeTaskCore(spec.Key, "cancelled", "", firstNonEmptyTaskValue(result.Code, spec.CancelCode), result.Params)
	default:
		e.failTaskCore(spec.Key, "", firstNonEmptyTaskValue(result.Code, spec.FailCode), result.Params, err.Error())
	}
}

// Advance 报告**计数推进**：做完了多少、一共多少。它只动计数与总数，不碰**阶段**。
func (tp *TaskProgress) Advance(current, total int, code string, params map[string]string) {
	tp.engine.applyTaskProgress(tp.key, taskProgressUpdate{
		current: &current,
		total:   &total,
		code:    code,
		params:  params,
	})
}

// Phase 报告**阶段**：正在做什么。它只动阶段与文案，不碰计数——
// 此前这要靠给九参数方法的总数传 -1 哨兵来表达「别动总数」，且必须编造一个凑数的计数值。
//
// 名字用 Phase 而不是 Stage：CONTEXT.md 把 stage 列为这个概念要避开的词。
func (tp *TaskProgress) Phase(phase, code string, params map[string]string) {
	tp.engine.applyTaskProgress(tp.key, taskProgressUpdate{
		phase:  phase,
		code:   code,
		params: params,
	})
}

// Metrics 合并任务指标（已哈希文件数、IO 等待毫秒等），按键覆盖。
func (tp *TaskProgress) Metrics(m map[string]int64) {
	tp.engine.applyTaskProgress(tp.key, taskProgressUpdate{metrics: m})
}

// Item 报告当前正在处理的条目名。
func (tp *TaskProgress) Item(name string) {
	tp.engine.applyTaskProgress(tp.key, taskProgressUpdate{item: name})
}

// taskProgressUpdate 是进度句柄四个方法的共同载体：只有被显式设置的字段才会写进任务。
// 「不改变总数」由「不传总数」表达，哨兵值随之消失。
type taskProgressUpdate struct {
	current *int
	total   *int
	phase   string
	item    string
	code    string
	params  map[string]string
	metrics map[string]int64
}

// applyTaskProgress 把一次上报写进任务表并按节流水位投递。
//
// 任务已进入终态后一律忽略：扫描器的进度回调不在任务体的调用栈上，晚一拍很常见，
// 放行会把一个已经收尾的任务在界面上拽回运行中。
func (e *taskEngine) applyTaskProgress(key string, update taskProgressUpdate) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	task, ok := e.tasks[key]
	if !ok || !taskIsActive(task.Status) {
		return
	}
	if update.current != nil {
		task.Current = *update.current
	}
	if update.total != nil {
		task.Total = *update.total
	}
	applyTaskMessage(&task, "", update.code, update.params)
	if update.phase != "" {
		task.Phase = update.phase
	}
	if update.item != "" {
		task.CurrentItem = update.item
	}
	if len(update.metrics) > 0 {
		if task.Metrics == nil {
			task.Metrics = make(map[string]int64, len(update.metrics))
		}
		for k, v := range update.metrics {
			task.Metrics[k] = v
		}
	}
	task.UpdatedAt = time.Now()
	e.seq++
	task.Sequence = e.seq
	enrichTaskProgress(&task)
	e.tasks[key] = task
	e.persistTaskStatus(task)
	e.publishTaskProgressLocked(task)
}
