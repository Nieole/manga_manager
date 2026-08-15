// 任务引擎对外的**唯一启动入口**：调用方提交一份任务声明（TaskSpec）与一个任务体，槽位申领、
// 上下文与运行时句柄、后台 goroutine、三条终态分支全部由引擎承担。
// 任务体只做两件事——干活，以及经 TaskProgress 报告**计数推进**与**阶段**；它不接触**任务键**、
// 不自己判断**终态**、不自己起 goroutine。
// 并发约定与 taskEngine 一致：带 Locked 后缀之外的方法自行加解锁。

package api

import (
	"context"
	"errors"
	"strings"
	"time"
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
	// 开跑之后才变的标签走 TaskProgress.Labels，两条路都是按键合并。
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

// TaskProgress 是任务体唯一的进度上报句柄，已绑定**任务键**。
//
// 「谁有资格写某个任务的进度」由「谁被交给了这个句柄」决定，而不是「谁会拼那个任务键字符串」——
// 前者是结构约束，后者不是。任务体内部因此也不必重复出现同一个键。
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
		// 必须用 defer 归还**运行时句柄**：它因此不依赖任务体走哪条出口，panic 也会经这里归还，
		// 之后才由 runTaskGoroutine 的兜底把任务置为失败态。写成裸调用的话，任务体一 panic
		// 就泄漏一份 ctx 与**暂停闸门**，那个任务键从此暂停不了也取消不了。
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
		Labels:        spec.Labels,
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
// 不得再开「任务体自行调用收尾方法」的第二条路径：两条路径并存时，
// 「忘了收尾」与「收了两次」都不会有编译错误。
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
	tp.Report(TaskFrame{Current: &current, Total: &total, Code: code, Params: params})
}

// Phase 报告**阶段**：正在做什么。它只动阶段与文案，不碰计数与总数，
// 因此播报阶段不必编造一个凑数的计数值。
//
// 名字用 Phase 而不是 Stage：CONTEXT.md 把 stage 列为这个概念要避开的词。
func (tp *TaskProgress) Phase(phase, code string, params map[string]string) {
	tp.Report(TaskFrame{Phase: phase, Code: code, Params: params})
}

// Metrics 合并任务指标（已哈希文件数、IO 等待毫秒等），按键**覆盖**。
// 上报方手里握着该指标的全量当前值时用它。
func (tp *TaskProgress) Metrics(m map[string]int64) {
	tp.Report(TaskFrame{Metrics: m})
}

// AddMetrics 按键**累加**指标增量，并顺带补齐随同一份报文而来的描述性参数（存储画像、卷标识等）。
//
// 与 Metrics 的分工在于上报方看得见多少：跨资料库的任务收到的每份报文只覆盖其中一个库，
// 全局总量只能由引擎累加得出。挑错一个不会有编译错误，后果是指标要么翻倍、要么只剩最后一份报文。
//
// 它刻意不走 TaskFrame：那条路是「设」，这条是「加」；而且这里的参数落进 TaskStatus.Params
// 而非 MessageParams——存储 IO 面板按参数名读这些累计值（见 taskArchiveOpenRate）。
// 代价是它与同一次事件里的那一帧各自投递一次，因此只给逐库报文这种低频路径用。
func (tp *TaskProgress) AddMetrics(increments map[string]int64, params map[string]string) {
	tp.engine.mergeRunningTaskMetricSums(tp.key, increments, params)
}

// MergeParams 按键合并**任务参数**（TaskStatus.Params）：**重启函数**读回入参、存储 IO 面板
// 读取扫描计数用的正是这一份。上报方手里握着这些参数的全量当前值时用它。
//
// 它与 TaskFrame.Params 只差一个字，去处却不同——那一路是文案占位参数（TaskStatus.MessageParams）。
// 接反不会有编译错误，后果是任务参数丢失，或者文案把占位符原样渲染出来。
func (tp *TaskProgress) MergeParams(params map[string]string) {
	tp.engine.mergeTaskParams(tp.key, params)
}

// Item 报告当前正在处理的条目名。
func (tp *TaskProgress) Item(name string) {
	tp.Report(TaskFrame{Item: name})
}

// Labels 合并任务标签（刮削源名、当前系列名等展示用的字符串），按键覆盖。
// 与 Metrics 的区别只在值的类型：标签给人看，指标要参与计算。
func (tp *TaskProgress) Labels(l map[string]string) {
	tp.Report(TaskFrame{Labels: l})
}

// TaskFrame 是一次上报的载体：只有被显式设置的字段会写进任务。
// 「不改变总数」由「不设 Total」表达，不用哨兵值；播报**阶段**也不必编造一个凑数的计数值。
type TaskFrame struct {
	Current *int
	Total   *int
	Phase   string
	Item    string
	Code    string
	Params  map[string]string
	Metrics map[string]int64
	Labels  map[string]string
}

// Report 把一帧**原子地**写进任务：一次加锁、一次投递。
//
// 日常上报请用 Advance / Phase / Item / Metrics / Labels——它们各自只回答一个问题，
// 是这个句柄的主用法。Report 留给那些「一个外部事件天然就是一整帧」的写入者，
// 典型是把扫描器的一份报文翻成一帧进度：那种帧拆成几次报会撕开——
// 投递水位放行了其中一条中间态，又把后面补齐的那条吞掉，于是同一份载荷里
// 指标已经走到第 N 条、进度条还停在第 N-1 条。
func (tp *TaskProgress) Report(frame TaskFrame) {
	tp.engine.applyTaskProgress(tp.key, frame)
}

// applyTaskProgress 把一帧上报写进任务表并按节流水位投递。
//
// 任务已进入终态后一律忽略：扫描器的进度回调不在任务体的调用栈上，晚一拍很常见，
// 放行会把一个已经收尾的任务在界面上拽回运行中。
func (e *taskEngine) applyTaskProgress(key string, frame TaskFrame) {
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
	applyTaskMessage(&task, "", frame.Code, frame.Params)
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
	enrichTaskProgress(&task)
	e.tasks[key] = task
	e.persistTaskStatus(task)
	e.publishTaskProgressLocked(task)
}
