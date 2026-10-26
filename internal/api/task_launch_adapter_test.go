package api

import (
	"context"
	"manga-manager/internal/runhandle"
	"manga-manager/internal/task"
	"strings"
)

// RunSpec 是一份运行声明：「这一次运行怎么跑、怎么显示」的完整描述，与身份一起一次性交给引擎。
//
// 身份不在这里而是启动入口的**第一个位置参数**（TaskIdentity）：它是一份声明里唯一「漏了就必须
// 有编译错误」的部分，而结构体字面量漏一个字段只会得到零值——那等于把从任务键反解作用域的猜测
// 换了个地方接着猜。
//
// 整份声明必须原子落地。拆成启动之后的多次补写，会留下一个「任务已经出现在列表里、却还没有
// 作用域名」的窗口——那是任务列表接口能观察到的，而补写的那几帧还会被首帧刚写下的节流水位吞掉。
type RunSpec struct {
	ValidateAccess func() error
	// Key 是这个任务的**任务键**：日志与 URL 靠它给人辨认，也随运行一起落盘。
	// 它由启动点自己拼，与身份的四项**不互相推导**——身份不从它反解，它也不由身份生成。
	//
	// 它**不再用于寻址**：暂停 / 恢复 / 取消按运行 id，重试与禁用按任务 id（队列一开，同一个键
	// 可以有两条仍会变化的运行）。ADR 0007 判的是**键退出寻址、不退出日志**，因此本字段留着——
	// 它是日志行上那个 task_key 的唯一来源；被删掉的是 runs 上那一列与按键筛选的谓词。
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

	// Limits 是这次运行实际生效的并发上限。只有真被某个并发上限管住的运行才填它——顺序逐本处理的
	// 维护任务填了也只是报一个没有对应实物的数。零值表示「这次运行没有上限可报」，不是「上限为 0」；
	// 引擎不会为它凭空造一份全零的上限，任务面板上那块徽章随之整块不出现。
	Limits RunLimits

	// 三条终态分支的**默认**文案码。常规任务因此不必为收尾写任何代码；
	// 「部分成功」「第一阶段失败」这类变体由任务体经 RunResult.Code 覆盖对应的一条。
	CompleteCode string
	CancelCode   string
	FailCode     string
}

// RunResult 是任务体对终态文案的可选修正。零值表示「用任务声明里的默认码」，
// 而不是「把文案清空」——绝大多数任务体返回的正是零值。
type RunResult struct {
	Code   string
	Params map[string]string
}

// taskBody 是任务体：干活，以及经交给它的**运行句柄**上报。
type taskBody func(ctx context.Context, handle *runhandle.Handle) (RunResult, error)

// Run 是启动一个后台任务的唯一入口：一份**身份**、一个**发起方**、一份任务声明、一个任务体。
//
// 发起方与身份一样是**位置参数**而不是 RunSpec 的一个字段：结构体字面量漏一个字段只会得到
// 零值，而一个零值发起方要到运行期才被声明校验拦下——那时启动点已经在生产里跑了。
// 它也不该有默认值：半夜转盘的那条运行到底是定时叫来的还是监听叫来的，正是用户要的答案，
// 猜错比不填更糟。
//
// 返回 nil **不等于**建出了一条运行，落成哪一种要看闸门：槽位有空且这个身份没有活动运行时它
// 当场开跑，否则进**排队中**等放行；这个身份已经排着一条时，本次发起被**合并**进那一条；
// 而**自动**发起（定时 / 监听）撞上**停发**时一条运行都不建。后三种交出去的任务体都不会执行，
// 四种也都不是错误——冲突不再被丢弃，被自己的设置挡下更不是故障。要分辨落成了哪一种，用 start。
// **判据是身份而不是任务键**：键只管寻址，而「同一件事不会同时跑两遍」由库上那条部分唯一索引保证。
//
// 刻意保留的不变量：准入**同步**执行、任务体**异步**执行。Run 返回时运行已在库里、
// 而任务体尚未开跑，HTTP 层才能立即返回 202 而不被任务体阻塞。
func (e *taskEngine) Run(identity TaskIdentity, trigger task.Trigger, spec RunSpec, fn taskBody) error {
	_, err := e.start(identity, trigger, spec, fn)
	return err
}

// start 与 Run 是同一次发起，另外交回这次发起落地成了什么（见 task.Launched）。
//
// 只有「发起完还要等它跑完」的调用方需要它——文件监听器按扫描的成败决定敢不敢接着清理，
// 而被**合并**掉的那次发起，任务体根本不会执行，等在它身上就是永远等下去。
func (e *taskEngine) start(identity TaskIdentity, trigger task.Trigger, spec RunSpec, fn taskBody) (task.Launched, error) {
	ctx := context.Background()
	runSpec := task.RunSpec{
		Identity:       identity.domain(),
		ValidateAccess: spec.ValidateAccess,
		Trigger:        trigger,
		Key:            spec.Key,
		ScopeName:      strings.TrimSpace(spec.ScopeName),
		StartCode:      spec.StartCode,
		StartParams:    spec.StartParams,
		Total:          spec.Total,
		CanCancel:      spec.CanCancel,
		CanPause:       spec.CanPause,
		Args:           spec.Metadata,
		Labels:         spec.Labels,
		CompleteCode:   spec.CompleteCode,
		CancelCode:     spec.CancelCode,
		FailCode:       spec.FailCode,
	}
	if e.libraryIDs != nil {
		ids, err := e.libraryIDs(ctx, identity)
		if err != nil {
			return task.Launched{}, err
		}
		runSpec.LibraryIDs = ids
	}
	if spec.Limits != (RunLimits{}) {
		runSpec.Limits = spec.Limits.domain()
	}

	launched, err := e.runs.Start(ctx, runSpec, func(runCtx context.Context, handle *runhandle.Handle) (task.Result, error) {
		result, err := fn(runCtx, handle)
		return task.Result{Code: result.Code, Params: result.Params}, err
	})
	// **正常路径上这两条都不会再出现**：撞上活动运行的那次发起进**排队中**（返回 nil），
	// 撞上排队运行的那次被**合并**进去（同样返回 nil）。留着这层翻译是为了那种「引擎刚判完
	// 状态就被落盘侧的索引否掉」的竞态——它对调用方仍然是同一个答案「这件事已经在跑了」，
	// 而不是一个 500。
	return launched, taskLaunchError(err)
}
