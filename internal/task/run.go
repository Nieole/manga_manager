// 一次执行这一半：状态机的取值与三条判定、运行行本身，以及挂在运行上的侧数据
// （上限、**运行事件**、**采样**）。身份那一半在 identity.go。

package task

import "time"

// RunStatus 是一次**运行**所处的状态。取值就是落盘那一列的取值，两侧不得各写一份。
type RunStatus string

const (
	// StatusQueued 是**排队中**：已被登记、还没拿到运行槽位。它既不是**活动态**也不是**终态**。
	StatusQueued     RunStatus = "queued"
	StatusRunning    RunStatus = "running"
	StatusPaused     RunStatus = "paused"
	StatusCancelling RunStatus = "cancelling"

	StatusCompleted   RunStatus = "completed"
	StatusCancelled   RunStatus = "cancelled"
	StatusFailed      RunStatus = "failed"
	StatusInterrupted RunStatus = "interrupted"
)

// activeStatuses 与 liveStatuses 是三条判定各自的取值集合，供落盘端口按状态筛选。
var (
	activeStatuses = []RunStatus{StatusRunning, StatusPaused, StatusCancelling}
	liveStatuses   = []RunStatus{StatusQueued, StatusRunning, StatusPaused, StatusCancelling}
)

// ActiveStatuses 返回**活动态**的三种取值，供落盘端口拼出「同一任务只有一次活动运行」那条约束。
func ActiveStatuses() []RunStatus { return append([]RunStatus(nil), activeStatuses...) }

// LiveStatuses 返回仍会变化的四种取值（**活动态**加**排队中**），供重启时的批量转**中断**
// 与保留裁剪的排除集合使用。
func LiveStatuses() []RunStatus { return append([]RunStatus(nil), liveStatuses...) }

// IsActive 判断这次运行是否仍占着运行槽位：运行中、已暂停、**取消中**三种。
//
// **排队中**不算：它不占槽位，却仍会开跑。每一处判活动态都要单独想一遍排队中该不该算进去，
// 多数不该——算进去等于让排队的运行白占一个槽位，队列因此永远放不出人。
func (s RunStatus) IsActive() bool {
	return s == StatusRunning || s == StatusPaused || s == StatusCancelling
}

// IsTerminal 判断这次运行是否已进入**终态**：完成、已取消、失败、中断四种。
// **排队中**的运行被取消也进这里（进的是已取消）。
func (s RunStatus) IsTerminal() bool {
	return s == StatusCompleted || s == StatusCancelled || s == StatusFailed || s == StatusInterrupted
}

// IsLive 判断这次运行是否还会变化：**活动态**或**排队中**。
//
// 保留裁剪认的是这一条而不是 IsActive——排队中的运行还没开跑，被清掉就等于那次发起凭空消失。
func (s RunStatus) IsLive() bool { return s.IsActive() || s == StatusQueued }

// Trigger 是**发起方**：这次运行是被什么叫来的。它不改变运行怎么跑，只回答「这活是谁叫来的」。
type Trigger string

const (
	TriggerManual    Trigger = "manual"
	TriggerScheduled Trigger = "scheduled"
	TriggerWatch     Trigger = "watch"
	TriggerChained   Trigger = "chained"
	TriggerResumed   Trigger = "resumed"
)

// Run 是一次**运行**：库里的一行，一次执行一行，重试是同一个任务的新一行而不是覆盖上一行。
//
// 展示态（阶段、当前条目、计数）必须是这一行的真列，不得编码进一个通用的键值堆；累计指标、
// 并发上限、重启入参、**运行事件**与**采样**各有自己的去处，经 Store 端口写入。
type Run struct {
	ID     int64
	TaskID int64

	// Key 是**过渡期**字段：今天的六个控制端点、重试查找与对外契约仍按**任务键**寻址，
	// 而新模型按运行 id 与身份四要素寻址。键**怎么拼**仍归 api，本包只原样携带它。
	//
	// 它落在运行上而不是任务上：外部库那两类的键带着会话 id，同一身份的两次运行键并不相同。
	// 控制端点改成按对象寻址、对外契约不再带任务键之后，本字段连同它那一列一起删。
	Key string
	// ScopeName 是作用域在界面上的显示名，同样是**过渡期**字段，本包不解释它。
	//
	// 它落在运行上是因为今天没有别的地方放得下：多数取值是资料库名（可由作用域 id 解析），
	// 但刮削全库那条写的是「全库」——一个挂在系统作用域上的字面量，解析不出来。
	// 最终归属（身份行上的一列，还是渲染时按作用域解析）等任务清单成形时再定。
	ScopeName string

	Trigger Trigger
	// NthRun 是这个任务的第几次运行，从 1 起。
	NthRun int
	Status RunStatus

	Phase       string
	CurrentItem string
	Current     int
	Total       int

	// PausedAt 是当前这一次**已暂停**的起点，离开暂停时折进 ControlPausedMillis 后清掉。
	PausedAt *time.Time
	// ControlPausedMillis 是这次运行至今在已暂停里待过的累计毫秒数。它只有一个用途：
	// 从速率与 ETA 的分母里扣掉——暂停期间一条都没处理，算成在干活会让两个数一路失真到终态。
	ControlPausedMillis int64
	// CoalescedCount 是**合并**进这条排队运行的发起次数，没有合并过就是 0。
	CoalescedCount int

	// MessageCode 与 MessageParams 是面向用户的文案：只有 i18n 码一种，本包不认识具体的码。
	MessageCode   string
	MessageParams map[string]string
	// Error 是失败时的技术错误串，给排查线索用，不面向翻译。
	Error string

	// StartedAt 是这次运行进入运行中的时刻；**排队中**的运行还没开跑，它是零值。
	// 速率的分母取的就是它——把入队时刻写进来的话，排了一小时队的运行会被算成跑了一小时。
	StartedAt time.Time
	// UpdatedAt 是最后一次变化的时刻，进度落盘本身就是心跳。
	UpdatedAt  time.Time
	FinishedAt *time.Time
	// Sequence 是引擎单调发放的序号，也是任务中心的主排序键。
	// 时间列不是：两个写入方会写出两种文本格式，而 SQL 比的是文本。
	Sequence int64
}

// Limits 是一次运行实际生效的并发上限，落进它自己那张侧表的真列。
//
// 刻意不是键值 map：加一个上限字段就该改 schema，而「往 map 里塞一个键」正是指标至今
// 不可聚合查询的原因（ADR 0004）。本包只承载这些值，限流规则属于 diskwork 与 storageio。
type Limits struct {
	ScanProfile                string
	ScannerWorkersConfigured   int
	ScannerWorkersEffective    int
	StorageProfile             string
	VolumeKey                  string
	ScanConcurrency            int
	ArchiveOpenConcurrency     int
	CoverConcurrency           int
	HashConcurrency            int
	PauseBackgroundWhenReading bool
	IdleOnlyHeavyTasks         bool
	DisableSameDiskPageCache   bool
}

// EventKind 是**运行事件**的种类，是一个**封闭枚举**：不做「日志自动转事件」的魔法，
// 否则技术噪音会被当成用户信息推出去。
type EventKind string

const (
	// EventPhase 是**阶段**切换，相邻两条相减即是那一段的耗时。
	EventPhase EventKind = "phase"
	// EventItem 是条目失败：哪个文件、为什么。
	EventItem EventKind = "item"
	// EventControl 是控制动作：暂停、恢复、取消、被**合并**。
	EventControl EventKind = "control"
	// EventWarn 是运行级告警：降级、批量跳过、护栏触发。
	EventWarn EventKind = "warn"
)

// Event 是一次运行途中值得事后回看的一个瞬间。Payload 的内部形状由发出事件的那一方约定，
// 本包只保证种类是封闭枚举。
type Event struct {
	At      time.Time
	Kind    EventKind
	Payload string
}

// Sample 是按固定间隔对一次运行的计数与速率取的一个点，连起来是吞吐曲线。
// 它只用来看趋势：计数的事实来源是运行行本身，任何判断都不得依赖采样。
type Sample struct {
	At            time.Time
	Current       int
	RatePerMinute float64
}

// SideData 是一次运行的侧数据读回来的形状：四张键值侧表各占一格。
//
// 读回面合成一个结构体而不是四个方法，是因为它的四个消费方（任务列表、运行详情、**重启函数**、
// 存储 IO 面板）要的从来都是全套——分成四次调用只会让列表接口对着一页运行发出四倍的查询。
// 写入面仍是分开的：那几条各有各的合并语义（设 / 累加 / 按键合并），合成一条会把它们抹平。
type SideData struct {
	// Args 是**重启函数**读回的原始入参，Labels 是展示标签。
	Args   map[string]string
	Labels map[string]string
	// Metrics 是累计指标。
	Metrics map[string]int64
	// Limits 为 nil 表示这次运行没有上限可报，不是「上限全为 0」。
	Limits *Limits
}

// Capabilities 是一次运行此刻能接受哪些控制动作。
//
// 它由引擎按运行的活性派生，不是运行行上的列：一次运行能不能暂停取决于它的**暂停闸门**
// 此刻还在不在，而闸门活在进程里。落成列的话，重启后那几个布尔值会集体说谎。
type Capabilities struct {
	CanPause  bool
	CanResume bool
	CanCancel bool
}

// Snapshot 是投递出去的一帧：运行行、它此刻的控制能力，加上挂在它上面的侧数据。
//
// 侧数据必须随帧一起出去，不能让订阅方自己回头去取：一帧是一份**全量**快照，缺了指标与上限的
// 那几帧会把订阅方已经显示出来的数字抹掉，直到下一次整表轮询才长回来。
type Snapshot struct {
	Run          Run
	Capabilities Capabilities
	Side         SideData
}

func cloneStrings(src map[string]string) map[string]string {
	if src == nil {
		return nil
	}
	dst := make(map[string]string, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

// applyMessage 在运行上设置文案。空码是无操作，用于「这一帧不改文案」。
func applyMessage(run *Run, message Result) {
	if message.Code == "" {
		return
	}
	run.MessageCode = message.Code
	run.MessageParams = cloneStrings(message.Params)
}

// absorbPause 把「这一次暂停」折进累计并清掉起点，供离开**已暂停**的每条出口调用：
// 恢复、取消，以及暂停中直接收尾。
//
// 累计值只有一个消费者——速率与 ETA 的分母。少调一处不会有编译错误，后果是那条出口之后的
// 分母里凭空多出一段没人干活的时间，运行看起来慢了几倍。
func absorbPause(run *Run, now time.Time) {
	if run.PausedAt == nil {
		return
	}
	if paused := now.Sub(*run.PausedAt); paused > 0 {
		run.ControlPausedMillis += paused.Milliseconds()
	}
	run.PausedAt = nil
}

// cloneRun 深拷贝一条运行：引用类型字段一个都不能漏，否则调用方读到的是仍在被写入的活 map。
func cloneRun(run Run) Run {
	run.MessageParams = cloneStrings(run.MessageParams)
	if run.PausedAt != nil {
		pausedAt := *run.PausedAt
		run.PausedAt = &pausedAt
	}
	if run.FinishedAt != nil {
		finishedAt := *run.FinishedAt
		run.FinishedAt = &finishedAt
	}
	return run
}
