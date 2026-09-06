// 任务引擎对外的**唯一启动入口**：调用方提交一份**身份**、一份运行声明（RunSpec）与一个任务体，
// 准入、上下文与**运行时句柄**、后台 goroutine、四条终态分支全部由 `internal/task` 的领域引擎承担。
// 任务体只做两件事——干活，以及经交给它的**运行句柄**（runhandle.Handle）上报；它不接触**任务键**、
// 不自己判断**终态**、不自己起 goroutine。
//
// 本文件不含状态：翻译完就把整份声明交给领域引擎，准入的判据在数据库那两条部分唯一索引上。

package api

import (
	"context"
	"errors"
	"strings"

	"manga-manager/internal/runhandle"
	"manga-manager/internal/task"
)

// TaskVariant 是**变体**：同一类工作在同一个作用域上的第二个身份。
//
// 它是独立类型而不是 string，为的是让身份构造函数里那两个都叫「一串字符」的位置——任务类型与
// 变体——接反时有编译错误。
type TaskVariant string

const (
	// variantSole 是「这个类型在一个作用域上只有一个身份」。它是一次显式声明，不是省略：
	// 身份四要素没有「不填」这个选项，见 TaskIdentity。
	variantSole TaskVariant = ""

	// 书哈希重建的两个变体：前台重建大批次、无停顿；低优先级回填压低批次、批间停顿、
	// 匹配模式钉死二进制哈希。**重启函数**按（类型，变体）分发，两条跑法因此各自重启回自己。
	variantHashRebuildForeground TaskVariant = "foreground"
	variantHashRebuildBackfill   TaskVariant = "low_priority_backfill"

	// 刮削的两个变体：全库那条挂在系统作用域，单库那条带库作用域。
	variantScrapeAllLibraries TaskVariant = "all_libraries"
	variantScrapeOneLibrary   TaskVariant = "one_library"
)

// 任务的三个**作用域**。它们同时是身份行 scope 列的取值与任务列表的筛选值。
const (
	taskScopeSystem  = "system"
	taskScopeLibrary = "library"
	taskScopeSeries  = "series"
)

// TaskIdentity 是一个任务的**身份**：类型、**作用域**、作用域 id 与**变体**四项唯一确定它。
// 四项由启动点声明，引擎不推导其中任何一项——包括不从**任务键**的字符串里反解作用域。
//
// 字段不导出，因此建它只有 systemTask / libraryTask / seriesTask 三条路。三者都把四项收成
// 位置参数，少填一项是**编译错误**：结构体字面量漏一个字段只会得到零值，而一个默默判成系统级、
// 或默默丢掉作用域 id 的任务不会有任何报错，只会挂在任务中心里错的那个作用域下。
type TaskIdentity struct {
	taskType string
	scope    string
	scopeID  *int64
	variant  TaskVariant
}

// systemTask 声明一个系统级任务的身份。
func systemTask(taskType string, variant TaskVariant) TaskIdentity {
	return TaskIdentity{taskType: taskType, scope: taskScopeSystem, variant: variant}
}

// libraryTask 声明一个资料库级任务的身份。
//
// 作用域 id 是位置参数而不是一个可以留 nil 的字段：库级任务没有「不知道是哪个库」这种状态，
// 启动点调它的那一刻手里就握着 lib.ID。
func libraryTask(taskType string, libraryID int64, variant TaskVariant) TaskIdentity {
	return TaskIdentity{taskType: taskType, scope: taskScopeLibrary, scopeID: &libraryID, variant: variant}
}

// seriesTask 声明一个系列级任务的身份，作用域 id 的道理同 libraryTask。
func seriesTask(taskType string, seriesID int64, variant TaskVariant) TaskIdentity {
	return TaskIdentity{taskType: taskType, scope: taskScopeSeries, scopeID: &seriesID, variant: variant}
}

// RunSpec 是一份运行声明：「这一次运行怎么跑、怎么显示」的完整描述，与身份一起一次性交给引擎。
//
// 身份不在这里而是启动入口的**第一个位置参数**（TaskIdentity）：它是一份声明里唯一「漏了就必须
// 有编译错误」的部分，而结构体字面量漏一个字段只会得到零值——那等于把从任务键反解作用域的猜测
// 换了个地方接着猜。
//
// 整份声明必须原子落地。拆成启动之后的多次补写，会留下一个「任务已经出现在列表里、却还没有
// 作用域名」的窗口——那是任务列表接口能观察到的，而补写的那几帧还会被首帧刚写下的节流水位吞掉。
type RunSpec struct {
	// Key 是这个任务的**任务键**：日志、URL 与六个控制端点都按它寻址，也随运行一起落盘。
	// 它由启动点自己拼，与身份的四项**不互相推导**——身份不从它反解，它也不由身份生成。
	//
	// 它是**过渡期**的寻址方式：控制端点改成按对象（运行、任务）寻址之后整块消失。
	Key string

	// StartCode 与 StartParams 是起始文案的 i18n 码与占位参数。消息词汇只有 i18n 码一种。
	//
	// 名字必须与 Metadata 拉开距离：它落进运行行的文案占位参数，而 Metadata 落进重启入参那张
	// 侧表。两者接反不会有编译错误，后果是**重启函数**读不回原始入参，静默回落到默认值
	// （AI 分组的语言设置正是这样一处）。
	StartCode   string
	StartParams map[string]string

	Total     int
	CanCancel bool
	CanPause  bool

	// Metadata 是重启入参（**重启函数**从这里读回原始入参），ScopeName 是作用域在界面上的显示名。
	Metadata  map[string]string
	ScopeName string

	// Labels 是启动时就已知、整个任务期间不变的展示标签（刮削源名等），落进它自己那张侧表。
	//
	// 它与 Metadata 从此是两张表而不是一个命名空间里的两组前缀键：接反的后果因此从「读不回来」
	// 变成「显示在了另一处」。开跑之后才变的标签走 runhandle.Frame.Labels，两条路都是按键合并。
	Labels map[string]string

	// Limits 是该任务实际生效的并发上限。只有真被某个并发上限管住的任务才填它——顺序逐本处理的
	// 维护任务填了也只是报一个没有对应实物的数。零值表示「这个任务没有上限可报」，不是「上限为 0」；
	// 引擎不会为它凭空造一份全零的上限，任务面板上那块徽章随之整块不出现。
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
// TaskResult 的文案覆盖对引擎裁决出的每条分支一视同仁，无条件带上码的话，
// 用户按下取消看到的会是「清空封面索引失败」而不是「已取消」。
func taskFailure(code string, err error) TaskResult {
	if errors.Is(err, context.Canceled) {
		return TaskResult{}
	}
	return TaskResult{Code: code}
}

// Run 是启动一个后台任务的唯一入口：一份**身份**、一份任务声明、一个任务体。
//
// 返回 nil 表示已启动；返回 errTaskAlreadyRunning 表示这个**身份**已有**活动态**运行
// （含**取消中**），此时任务体一步都不会执行。**判据是身份而不是任务键**：键只管寻址，
// 而「同一件事不会同时跑两遍」由库上那条部分唯一索引保证。
//
// 刻意保留的不变量：准入**同步**执行、任务体**异步**执行。Run 返回时运行已在库里、
// 而任务体尚未开跑，HTTP 层才能立即返回 202 而不被任务体阻塞。
func (e *taskEngine) Run(identity TaskIdentity, spec RunSpec, fn func(ctx context.Context, tp *runhandle.Handle) (TaskResult, error)) error {
	ctx := context.Background()
	// 先把身份取出来（不存在就建一条）：投递首帧那一刻在领域引擎的临界区里，补不了身份，
	// 而首帧要带着类型与作用域出去——它是任务在界面上诞生的那一帧。
	owner, err := e.runStore.EnsureTask(ctx, identity.domain())
	if err != nil {
		return err
	}
	e.rememberIdentity(owner.ID, identity)

	runSpec := task.RunSpec{
		Identity: identity.domain(),
		// 今天这 17 个启动点都是有人（或某个前台动作）当场叫起来的。定时、监听与串联那几种
		// **发起方**要等定时守护扫描、watcher 派生扫描与串联的工作也建运行，那时才有第二个取值。
		Trigger:      task.TriggerManual,
		Key:          spec.Key,
		ScopeName:    strings.TrimSpace(spec.ScopeName),
		StartCode:    spec.StartCode,
		StartParams:  spec.StartParams,
		Total:        spec.Total,
		CanCancel:    spec.CanCancel,
		CanPause:     spec.CanPause,
		Args:         spec.Metadata,
		Labels:       spec.Labels,
		CompleteCode: spec.CompleteCode,
		CancelCode:   spec.CancelCode,
		FailCode:     spec.FailCode,
	}
	if spec.Limits != (TaskLimits{}) {
		runSpec.Limits = spec.Limits.domain()
	}

	_, err = e.engine.Start(ctx, runSpec, func(runCtx context.Context, handle *runhandle.Handle) (task.Result, error) {
		result, err := fn(runCtx, handle)
		return task.Result{Code: result.Code, Params: result.Params}, err
	})
	// 已有活动运行与已有排队运行在本层是同一个答案：调用方要知道的只是「这件事已经在跑了」。
	// 今天排队开不出来（槽位不设上限），但准入哨兵两条都翻：将来放开槽位时这里不会静默变成 500。
	if errors.Is(err, task.ErrRunAlreadyActive) || errors.Is(err, task.ErrRunAlreadyQueued) {
		return errTaskAlreadyRunning
	}
	return err
}
