// 落盘端口：领域声明它要什么，taskstore 实现它。
// 依赖方向恒为领域 → 端口 → 适配器，端口不得长成表的形状。

package task

import (
	"context"
	"errors"
	"time"
)

// 准入与查找的哨兵错误。适配器把数据库那两条部分唯一约束的违例翻成前两个，
// 引擎据此分辨「已有活动运行」与「已有排队运行」——这是**准入只剩一处**的落点。
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
)

// RunFilter 是运行查询的谓词。零值表示不筛：全部任务、全部状态、不限条数。
type RunFilter struct {
	// TaskID 为 0 表示不按任务筛。
	TaskID int64
	// Statuses 为空表示不按状态筛。
	Statuses []RunStatus
	Order    RunOrder
	Limit    int
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
	// SaveTaskAttributes 写回身份的长期属性。**退避**与禁用的规则不在本包实现，只经这里落盘。
	SaveTaskAttributes(ctx context.Context, taskID int64, attrs TaskAttributes) error

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

	// PruneRuns 按分层保留裁剪历史，返回各层清掉的行数。
	// **活动态与排队中的运行永不被选中**，实现方不得让它可配。
	PruneRuns(ctx context.Context, policy RetentionPolicy) (PruneResult, error)
}
