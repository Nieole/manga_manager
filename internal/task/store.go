// 落盘端口：领域声明它要什么，taskstore 实现它。
// 依赖方向恒为领域 → 端口 → 适配器，端口不得长成表的形状。

package task

import (
	"context"
	"errors"
	"time"
)

// 准入与查找的哨兵错误。适配器把数据库那两条部分唯一约束的违例翻成前两个——
// 这是**准入只剩一处**的落点：引擎撞上「已有排队运行」就把这次发起**合并**进那一条，
// 撞上「已有活动运行」则说明它刚刚判定的状态已经过时（同一件事不会同时跑两遍）。
var (
	ErrRunAlreadyActive = errors.New("task already has an active run")
	ErrRunAlreadyQueued = errors.New("task already has a queued run")
	ErrRunNotFound      = errors.New("run not found")
)

// RunOrder 是列表的定序方式。序号是唯一的排序主键，时间列不是。
type RunOrder string

const (
	// OrderSequenceAsc 由旧到新，队列放行按它取最先排上的那一条。
	OrderSequenceAsc RunOrder = "sequence_asc"
	// OrderSequenceDesc 由新到旧，任务中心的列表按它取最近活动的那一页。
	OrderSequenceDesc RunOrder = "sequence_desc"
	// OrderLiveFirst 是任务中心第一页的定序：仍会变化的运行（**活动态**与**排队中**）在前，
	// 其后按序号降序。
	//
	// 它不是展示偏好而是可用性下限：序号只在有更新时才递增，一个长时间不上报进度的大库扫描
	// 会被后来的大量短任务超过，而前端只取第一页——只按序号排的话，用户正等着看的那一条
	// 恰好会掉出第一页，而它是唯一一个还能变的。
	OrderLiveFirst RunOrder = "live_first"
	// OrderManualFirst 是**实况区**的定序：用户**手动**发起的运行在前，其余按序号降序。
	//
	// 它答的是「我刚点的那一条在哪」。序号在每一次可见变化时递增，而自动发起的扫描每报一帧
	// 就把自己顶到最前——一屏里挤着几条定时与监听的运行时，用户刚按下的那次会一路下沉。
	// 同一区里因此有两种定序：先按发起方分两组，组内仍是「最近有动静的在最上面」。
	OrderManualFirst RunOrder = "manual_first"
)

// RunFilter 是运行查询的谓词。零值表示不筛：全部任务、全部状态、不限条数。
//
// 身份那几项（类型、作用域、作用域 id）判的是运行所属**任务**行上的列，实现方因此要连上身份表；
// 它们不是运行行上的列，这正是**作用域是列而不是从任务键里猜出来的**那条要求的落点。
type RunFilter struct {
	// TaskID 为 0 表示不按任务筛。
	TaskID int64
	// Statuses 为空表示不按状态筛。
	Statuses []RunStatus

	// Types 为空表示不按任务类型筛；给多个即取并集。
	Types []Type
	// Scope 为空表示不按作用域筛。
	Scope Scope
	// ScopeID 为 nil 表示不按作用域 id 筛。它是指针而不是 0 值哨兵：系统级身份的作用域 id
	// **就是** 0，用 0 表示「不筛」会让「只看系统级」这条筛选无从表达。
	ScopeID *int64

	// Key 是**过渡期**谓词：按**任务键**精确匹配。控制端点与重试今天按它寻址，见 Run.Key。
	Key string
	// Query 是任务中心搜索框那条谓词：对键、文案码与错误串做大小写无关的子串匹配。
	// 它判在落盘侧而不是取回内存再滤，否则 Limit 截断的会是过滤前的那一页。
	Query string

	Order RunOrder
	Limit int
}

// TaskFilter 是**任务清单**的谓词。零值表示不筛：全部任务、不限条数。
//
// 前三项判在任务行上，后两项判在这个任务**最近一次运行**上——清单那一栏「上次结果」说的就是它。
// 两类谓词收在同一个结构体里是刻意的：界面上它们是同一排筛选器，而「筛的是任务还是运行」
// 这个问题必须在这里就答死，留给每个调用方各自解释就会长出两套口径。
type TaskFilter struct {
	// Types 为空表示不按任务类型筛；给多个即取并集。
	Types []Type
	// Scope 为空表示不按作用域筛。
	Scope Scope
	// ScopeID 为 nil 表示不按作用域 id 筛，理由同 RunFilter.ScopeID。
	ScopeID *int64

	// LastRunStatuses 判在最近一次运行的状态上；为空表示不筛。
	// 一次运行都没有的任务不满足其中任何一条——它没有「上次」。
	LastRunStatuses []RunStatus
	// LastRunQuery 同样判在最近一次运行上：对**任务键**、文案码与错误串做大小写无关的子串匹配。
	LastRunQuery string

	Limit int
}

// RetentionPolicy 是分层保留的三个阈值。**活动态**与**排队中**的运行永不被裁剪带走，
// 这条不是策略而是前提，因此不在这里配。
type RetentionPolicy struct {
	// RunsPerTask 是每个任务保留的最近**终态**运行条数。
	RunsPerTask int
	// TerminalAge 是终态运行的最长保留时长，与 RunsPerTask 取先到者。
	TerminalAge time.Duration
	// SampleAge 是**采样**点的最长保留时长，比运行本身短：运行还在，曲线没了。
	SampleAge time.Duration
}

// PruneResult 是一次保留裁剪清掉的行数，清理运行据此报出「清了多少」。
type PruneResult struct {
	Runs    int64
	Events  int64
	Samples int64
}

// Store 是任务与运行的落盘端口。实现方不得把领域概念反向塞进来：这里声明的是领域要什么，
// 不是表有哪些列。
//
// 并发要求：全部方法可从任意 goroutine 调用。引擎会在自己的临界区内调用它们，
// 因此实现方不得回调进引擎，也不得长时间阻塞。
type Store interface {
	// EnsureTask 按身份四要素取回任务，不存在就建一条。重复发起不会建出第二条身份。
	EnsureTask(ctx context.Context, id Identity) (Task, error)
	// LoadTasks 按 id 批量取回任务，查不到的 id 不出现在结果里。
	//
	// 批量而不是逐条：任务中心一页有几十条运行，逐条取身份就是几十次查询，而它们绝大多数
	// 指向同一批任务。
	LoadTasks(ctx context.Context, taskIDs []int64) (map[int64]Task, error)
	// SaveTaskAttributes 写回身份的长期属性。**退避**与禁用的规则不在本包实现，只经这里落盘。
	SaveTaskAttributes(ctx context.Context, taskID int64, attrs TaskAttributes) error
	// ListTasks 按谓词取任务清单，「最近有过动静的」排在前（末次运行的序号降序），
	// 一次运行都没有的排在最后。
	//
	// 它与 ListRuns 是任务中心两层结构各自的取数：这一条一个任务一行，那一条一次运行一行。
	ListTasks(ctx context.Context, filter TaskFilter) ([]Task, error)
	// LatestRuns 批量取这批任务各自**最近一次运行**，一次运行都没有的任务不出现在结果里。
	// 「最近」判的是序号，与列表定序同一把尺子。批量的理由同 LoadTasks。
	LatestRuns(ctx context.Context, taskIDs []int64) (map[int64]Run, error)

	// CreateRun 落一条新运行并回填它的 id。
	//
	// **准入由这里保证**：同一任务已有活动运行返回 ErrRunAlreadyActive，已有排队运行返回
	// ErrRunAlreadyQueued。判据在落盘侧而不是在内存表里，因此「什么叫已经在跑」只有一个答案。
	CreateRun(ctx context.Context, run Run) (Run, error)
	// SaveRun 覆盖写一条已存在的运行；运行不存在返回 ErrRunNotFound。
	SaveRun(ctx context.Context, run Run) error
	// LoadRun 按 id 取一条运行；不存在返回 ErrRunNotFound。
	LoadRun(ctx context.Context, runID int64) (Run, error)
	// ListRuns 按谓词取运行，定序由 RunFilter.Order 指定。
	ListRuns(ctx context.Context, filter RunFilter) ([]Run, error)
	// CountRuns 数满足谓词的运行条数，槽位占用由它得出。
	CountRuns(ctx context.Context, filter RunFilter) (int, error)
	// MaxRunSequence 返回已用掉的最大序号，供引擎跨重启接着往下发。
	MaxRunSequence(ctx context.Context) (int64, error)
	// MaxNthRun 返回这个任务用掉的最大「第几次」，没有任何运行时返回 0。
	//
	// 「第几次」必须由最大值得出而不是由行数，因为保留裁剪会删掉旧的**终态**运行：
	// 按行数算的话，裁完之后的下一次运行会拿到一个已经用过的编号，与历史行撞号，
	// 而用户看到的「第几次」会倒着走。
	MaxNthRun(ctx context.Context, taskID int64) (int, error)

	// SaveRunLimits 记下这次运行实际生效的并发上限。
	SaveRunLimits(ctx context.Context, runID int64, limits Limits) error
	// MergeRunArgs 按键合并重启入参：**重启函数**读回原始入参读的就是它。
	MergeRunArgs(ctx context.Context, runID int64, args map[string]string) error
	// MergeRunLabels 按键合并展示标签（刮削源名等）。
	MergeRunLabels(ctx context.Context, runID int64, labels map[string]string) error
	// SetRunMetrics 按键**设**指标：上报方握着全量当前值时走这一路。
	SetRunMetrics(ctx context.Context, runID int64, values map[string]int64) error
	// AddRunMetrics 按键**累加**指标增量：跨资料库的运行收到的每份报文只覆盖其中一个库，
	// 全局总量只能加出来。两路挑错一个不会有编译错误，后果是指标要么翻倍、要么只剩最后一份报文。
	AddRunMetrics(ctx context.Context, runID int64, increments map[string]int64) error
	// AppendRunEvents 追加**运行事件**。事件必须显式发出，本端口不做日志转事件的推导。
	AppendRunEvents(ctx context.Context, runID int64, events []Event) error
	// AppendRunSamples 追加**采样**点。
	AppendRunSamples(ctx context.Context, runID int64, samples []Sample) error

	// LoadRunSideData 批量读回这批运行的侧数据，一条侧数据都没有的运行不出现在结果里。
	// 批量的理由同 LoadTasks。
	LoadRunSideData(ctx context.Context, runIDs []int64) (map[int64]SideData, error)

	// DeleteRuns 按谓词删除运行，返回删掉的条数。事件、采样与四张侧表随之级联删除。
	//
	// **仍会变化的运行（活动态与排队中）永不被删**，与 PruneRuns 同理：删掉一条还在跑的运行，
	// 它后续的每一次上报都会落空，而任务体仍在动磁盘。这条不是策略，实现方不得让它可配。
	DeleteRuns(ctx context.Context, filter RunFilter) (int64, error)

	// PruneRuns 按分层保留裁剪历史，返回各层清掉的行数。
	// **活动态与排队中的运行永不被选中**，实现方不得让它可配。
	PruneRuns(ctx context.Context, policy RetentionPolicy) (PruneResult, error)
}
