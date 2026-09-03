// 任务引擎对外的**唯一启动入口**：调用方提交一份任务声明（TaskSpec）与一个任务体，槽位申领、
// 上下文与运行时句柄、后台 goroutine、三条终态分支全部由引擎承担。
// 任务体只做两件事——干活，以及经交给它的**任务句柄**（taskrun.Handle）上报；它不接触**任务键**、
// 不自己判断**终态**、不自己起 goroutine。
// 并发约定与 taskEngine 一致：带 Locked 后缀之外的方法自行加解锁。

package api

import (
	"context"
	"errors"
	"strings"
	"time"

	"manga-manager/internal/taskcontrol"
	"manga-manager/internal/taskrun"
)

// TaskSpec 是一份任务声明：「这是一个什么任务」的完整描述，一次性交给引擎。
//
// 整份声明必须原子落地。拆成启动之后的多次补写，会留下一个「任务已经出现在列表里、却还没有
// 作用域名」的窗口——那是任务列表接口能观察到的，而补写的那几帧还会被首帧刚写下的节流水位吞掉。
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

	// Labels 是启动时就已知、整个任务期间不变的展示标签（刮削源名等），落进 TaskStatus.Labels。
	//
	// 它与 Metadata 只隔一层编码：标签落盘时被编码成 `label.` 前缀的任务参数、读回时再解码，
	// 因此塞进 Metadata 也「能用」——代价是任务声明里出现一份编码后的形状，接反了不会有编译错误。
	// 开跑之后才变的标签走 taskrun.Frame.Labels，两条路都是按键合并。
	Labels map[string]string

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

// taskFailure 给一个失败原因配上它专属的文案码，取消除外。
//
// 一个任务体的各道工序常各有各的失败文案，而取消同样以 ctx.Err() 的形式从这些调用里返回；
// TaskResult 的文案覆盖对 settleTask 裁决出的每条分支一视同仁，无条件带上码的话，
// 用户按下取消看到的会是「清空封面索引失败」而不是「已取消」。
func taskFailure(code string, err error) TaskResult {
	if errors.Is(err, context.Canceled) {
		return TaskResult{}
	}
	return TaskResult{Code: code}
}

// Run 是启动一个后台任务的唯一入口。
//
// 返回 nil 表示已启动；返回 errTaskAlreadyRunning 表示同一**任务键**已有**活动态**任务
// （含**取消中**），此时任务体一步都不会执行。
//
// 刻意保留的不变量：槽位闸门**同步**执行、任务体**异步**执行。Run 返回时任务已在列表里、
// 而任务体尚未开跑，HTTP 层才能立即返回 202 而不被任务体阻塞。
func (e *taskEngine) Run(spec TaskSpec, fn func(ctx context.Context, tp *taskrun.Handle) (TaskResult, error)) error {
	taskCtx, releaseRuntime, claimed := e.claimTaskSlot(spec)
	if !claimed {
		return errTaskAlreadyRunning
	}
	progress := e.newTaskHandle(spec.Key)

	e.runTaskGoroutine(spec.Key, func() {
		// 必须用 defer 归还**运行时句柄**：它因此不依赖任务体走哪条出口，panic 也会经这里归还，
		// 之后才由 runTaskGoroutine 的兜底把任务置为失败态。写成裸调用的话，任务体一 panic
		// 就泄漏一份 ctx 与**暂停闸门**，那个任务键从此暂停不了也取消不了。
		defer releaseRuntime()
		result, err := fn(taskCtx, progress)
		e.settleTask(spec, result, err)
	})

	return nil
}

// newTaskHandle 把这个任务的三条写入通道包成闭包，交出它的**任务句柄**。
//
// **任务键**在这里一次性绑定，此后不出现在句柄上：给句柄开一个 key 形参等于把「谁有资格写由谁
// 拿到句柄决定，而不是谁会拼那个字符串」这条结构约束重新打开。
func (e *taskEngine) newTaskHandle(key string) *taskrun.Handle {
	return taskrun.New(
		func(frame taskrun.Frame) { e.applyTaskProgress(key, frame) },
		func(params map[string]string) { e.mergeTaskParams(key, params) },
		func(increments map[string]int64, params map[string]string) {
			e.mergeActiveTaskMetricSums(key, increments, params)
		},
		e.diskWork,
	)
}

// claimTaskSlot 同步申领任务槽位：同一任务键已有活动态任务时返回 false，
// 否则把整份任务声明一次性落成任务行、投递一帧完整的首帧，并建好**运行时句柄**。
//
// 句柄与任务行必须在同一次持锁内一起落地。分成两步的话，两步之间存在一个「任务已经出现在
// 列表里、却还没有 ctx 与**暂停闸门**」的窗口——用户在那一瞬按下取消会吃一条
// errTaskCancelUnavailable。反过来，把建句柄挪到闸门之前则更糟：被闸门挡下的那次启动会把
// 正在跑的那个任务的句柄换成一份没人持有的，那个任务从此暂停不了也取消不了。
//
// 任务声明里的三份 map 一律克隆：引擎此后持锁原地写它们，而调用方在锁外仍握着自己那份，
// 共享同一个 map header 迟早撞成 taskEngine 符号 doc 里写的那种 fatal error。
//
// 返回的 release 必须在任务体退出时调用，否则句柄会一直留在表里。
func (e *taskEngine) claimTaskSlot(spec TaskSpec) (context.Context, func(), bool) {
	now := time.Now()
	scope, scopeID := inferTaskScope(spec.Type, spec.Key)
	task := TaskStatus{
		Key:           spec.Key,
		Type:          spec.Type,
		Scope:         scope,
		ScopeID:       scopeID,
		Status:        "running",
		MessageCode:   spec.StartCode,
		MessageParams: cloneStringMap(spec.StartParams),
		Total:         spec.Total,
		CanCancel:     spec.CanCancel,
		CanPause:      spec.CanPause,
		Params:        cloneStringMap(spec.Metadata),
		Labels:        cloneStringMap(spec.Labels),
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
	if !e.admitTaskLocked(task) {
		return nil, nil, false
	}
	taskCtx, release := e.newTaskRuntimeLocked(spec.Key, now)
	return taskCtx, release, true
}

// newTaskRuntimeLocked 为任务体建立可取消 + 可暂停的 ctx，并登记**运行时句柄**供暂停/恢复/取消
// 接口操作。调用方持有 mutex。
//
// 它只被 claimTaskSlot 调用，因此「拿得到一份**运行时句柄**」等价于「刚刚成功申领到一个任务槽位」。
// 单独暴露出去就等于开了一条给任意任务键凭空造句柄的路，包括那些根本没有任务行的键。
func (e *taskEngine) newTaskRuntimeLocked(key string, startedAt time.Time) (context.Context, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	gate := taskcontrol.NewPauseGate()
	taskCtx := taskcontrol.WithPauseGate(ctx, gate)

	runtime := &TaskRuntime{
		Context:   taskCtx,
		Cancel:    cancel,
		PauseGate: gate,
		StartedAt: startedAt,
	}
	if e.runtimes == nil {
		e.runtimes = make(map[string]*TaskRuntime)
	}
	e.runtimes[key] = runtime

	// 只归还**自己那份**句柄。终态写入本身也会清掉这一项，于是一个走 defer 清理的任务体在
	// 收尾之后才执行清理；此间同名任务若已重新启动并登记了自己的句柄，无差别 delete 会把
	// 新任务的 ctx 与**暂停闸门**一起抹掉——那个任务从此暂停不了也取消不了，直到进程重启。
	release := func() {
		e.mutex.Lock()
		if e.runtimes[key] == runtime {
			delete(e.runtimes, key)
		}
		e.mutex.Unlock()
	}

	return taskCtx, release
}

// settleTask 是三条终态分支的唯一裁决处：**任务体返回的错误**决定进哪一条，TaskResult 只能改文案。
//
// 不得再开「任务体自行调用收尾方法」的第二条路径：两条路径并存时，
// 「忘了收尾」与「收了两次」都不会有编译错误。
func (e *taskEngine) settleTask(spec TaskSpec, result TaskResult, err error) {
	switch {
	case err == nil:
		e.finalizeTask(spec.Key, "completed", firstNonEmptyTaskValue(result.Code, spec.CompleteCode), result.Params)
	case errors.Is(err, context.Canceled):
		e.finalizeTask(spec.Key, "cancelled", firstNonEmptyTaskValue(result.Code, spec.CancelCode), result.Params)
	default:
		e.failTask(spec.Key, firstNonEmptyTaskValue(result.Code, spec.FailCode), result.Params, err.Error())
	}
}

// applyTaskProgress 把一帧上报写进任务表并按节流水位投递。
//
// 任务已进入终态后一律忽略：扫描器的进度回调不在任务体的调用栈上，晚一拍很常见，
// 放行会把一个已经收尾的任务在界面上拽回运行中。
func (e *taskEngine) applyTaskProgress(key string, frame taskrun.Frame) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	task, ok := e.tasks[key]
	if !ok || !taskIsActive(task.Status) {
		return
	}
	if frame.Current != nil {
		task.Current = *frame.Current
	}
	if frame.Total != nil {
		task.Total = *frame.Total
	}
	applyTaskMessage(&task, frame.Code, frame.Params)
	if frame.Phase != "" {
		task.Phase = frame.Phase
	}
	if frame.Item != "" {
		task.CurrentItem = frame.Item
	}
	if len(frame.Metrics) > 0 {
		if task.Metrics == nil {
			task.Metrics = make(map[string]int64, len(frame.Metrics))
		}
		for k, v := range frame.Metrics {
			task.Metrics[k] = v
		}
	}
	if len(frame.Labels) > 0 {
		if task.Labels == nil {
			task.Labels = make(map[string]string, len(frame.Labels))
		}
		for k, v := range frame.Labels {
			task.Labels[k] = v
		}
	}
	task.UpdatedAt = time.Now()
	e.seq++
	task.Sequence = e.seq
	e.writeTaskLocked(&task)
	e.persistTaskStatus(task)
	e.publishTaskProgressLocked(task)
}
