package runhandle

import (
	"context"
	"sync"

	"manga-manager/internal/diskwork"
	"manga-manager/internal/storageio"
	"manga-manager/internal/taskcontrol"
)

// Frame 是一次上报的载体：只有被显式设置的字段会写进任务。
// 「不改变总数」由「不设 Total」表达，不用哨兵值；播报**阶段**也不必编造一个凑数的计数值。
type Frame struct {
	Current *int
	Total   *int
	Phase   string
	Item    string
	Code    string
	Params  map[string]string
	Metrics map[string]int64
	Labels  map[string]string
}

// IOMetrics 是这个任务至今发起的全部**磁盘作业**在限流上的实况：等待与暂停累加，
// 档位与卷键取**最后一次**有值的那次——一批书可以跨资料库跨卷，这两项报的是最近那次落在哪。
// HashedFiles 只数真正被读过的书，被闸门或令牌挡下的不算。
type IOMetrics struct {
	StorageProfile string
	VolumeKey      string
	IOWaitMillis   int64
	PausedMillis   int64
	HashedFiles    int64
}

// Handle 是交给一次**运行**的**运行句柄**：写它的进度、过它的**暂停闸门**、以它的名义发起**磁盘作业**。
//
// 「谁有资格写某一次运行的进度」由「谁被交给了这个句柄」决定，而不是「谁会拼那个任务键字符串」——
// 前者是结构约束，后者不是。**任务键**因此不出现在句柄上，它由构造期那几个闭包在引擎侧绑定。
//
// 全部方法可从任意 goroutine 调用：上报那几个方法本来就会从任务体之外被调用（**扫描观察者**
// 把封面队列的报文写进来），而 IO 实况是句柄自己持有的可变状态。
type Handle struct {
	writes Writes
	disk   *diskwork.Runner

	mu sync.Mutex
	io IOMetrics
}

// Writes 是一次运行的五条写入通道，一次性交齐。
//
// 收成结构体而不是位置参数：五个都是函数，而其中两个都以两个字符串打头（ItemFailed 与 Warn），
// 位置参数下接反了未必有编译错误——后果是失败的文件名被当成告警码渲染成一句翻译缺失。
type Writes struct {
	// Report 写整帧展示态，MergeParams 按键合并**任务参数**，AddMetrics 按键累加指标。
	Report      func(Frame)
	MergeParams func(map[string]string)
	AddMetrics  func(map[string]int64, map[string]string)
	// ItemFailed 落一条条目失败的**运行事件**：哪个文件、为什么。
	ItemFailed func(item, reason string)
	// Warn 落一条运行级告警的**运行事件**：降级、批量跳过、护栏触发。
	// code 是一个稳定的短码（由渲染方翻译），detail 是给排查用的补充说明，
	// count 是「这一条涉及多少个条目」。
	Warn func(code, detail string, count int64)
}

// New 建一个运行句柄。五条写入通道都不得为 nil。
// disk 只被 Disk 用到：留 nil 的句柄照样能上报与过闸门，而 Disk 会在闸门放行之后 panic。
func New(writes Writes, disk *diskwork.Runner) *Handle {
	return &Handle{writes: writes, disk: disk}
}

// Report 把整帧交给写入函数，一次报完。
//
// 它是上报面的主用法：一个外部事件天然就是一整帧，典型是把扫描器的一份报文翻成一帧进度。
// 那种帧拆成几次报会撕开——写入方的投递水位放行了其中一条中间态，又把后面补齐的那条吞掉，
// 于是同一份载荷里指标已经走到第 N 条、进度条还停在第 N-1 条。
func (h *Handle) Report(f Frame) {
	h.writes.Report(f)
}

// Advance 报告**计数推进**：做完了多少、一共多少。它只动计数与总数，不碰**阶段**。
func (h *Handle) Advance(current, total int, code string, params map[string]string) {
	h.Report(Frame{Current: &current, Total: &total, Code: code, Params: params})
}

// Phase 报告**阶段**：正在做什么。它只动阶段与文案，不碰计数与总数，
// 因此播报阶段不必编造一个凑数的计数值。
//
// 名字用 Phase 而不是 Stage：CONTEXT.md 把 stage 列为这个概念要避开的词。
func (h *Handle) Phase(phase, code string, params map[string]string) {
	h.Report(Frame{Phase: phase, Code: code, Params: params})
}

// MergeParams 按键合并**任务参数**：**重启函数**读回入参用的正是这一份。
// 上报方手里握着这些参数的全量当前值时用它。
//
// 可聚合的计数与时长不走这里——它们走指标（判据见 api.taskScanObserver.Metrics）。
//
// 它与 Frame.Params 只差一个字，去处却不同——那一路是文案占位参数。接反不会有编译错误，
// 后果是任务参数丢失，或者文案把占位符原样渲染出来。
func (h *Handle) MergeParams(params map[string]string) {
	h.writes.MergeParams(params)
}

// AddMetrics 按键**累加**指标增量，并顺带补齐随同一份报文而来的描述性参数（存储画像、卷标识等）。
//
// 与整帧上报里的 Metrics 的分工在于上报方看得见多少：那一路是「设」，上报方握着全量当前值；
// 这一路是「加」，跨资料库的任务收到的每份报文只覆盖其中一个库，全局总量只能由引擎累加得出。
// 挑错一个不会有编译错误，后果是指标要么翻倍、要么只剩最后一份报文。
//
// 它刻意不走 Frame：这里的参数落进**任务参数**而非文案占位参数。代价是它与同一次事件里的那一帧
// 各自投递一次，因此只给逐库报文这种低频路径用。
func (h *Handle) AddMetrics(increments map[string]int64, params map[string]string) {
	h.writes.AddMetrics(increments, params)
}

// ItemFailed 落一条条目失败的**运行事件**：这个文件没处理成，原因是这个。
//
// 它答的是详情页那个「哪些文件失败了、为什么」，因此**必须显式调用**——不做「日志自动转事件」
// 的推导，绝大多数 slog.Warn 是技术噪音，自动转会把它们当用户信息推出去。
// 同一次运行最多留下若干条，超出的只累加计数（见 task.MaxItemFailureEvents）。
func (h *Handle) ItemFailed(item, reason string) {
	h.writes.ItemFailed(item, reason)
}

// Warn 落一条运行级告警的**运行事件**：降级、批量跳过、护栏触发。
//
// 它与 ItemFailed 的分界是「这条说的是某一个条目，还是整次运行」：一整批被丢掉、
// 某项能力降级、某道护栏拦下了后续的东西，都属于这里。
//
// count 是「这一条涉及多少个条目」，不知道或不适用时给 0。它单独成一格而不是拼进 detail：
// 拼进去的话，渲染方只能把一句英文错误串连同一个数字原样甩给用户。
func (h *Handle) Warn(code, detail string, count int64) {
	h.writes.Warn(code, detail, count)
}

// Checkpoint 在一个可中断点上问一句**暂停闸门**：暂停期间阻塞，恢复后放行。
//
// 闸门在未暂停时返回上下文错误，因此它同时是取消检查——任务体不需要另写一次 ctx.Err()。
func (h *Handle) Checkpoint(ctx context.Context) error {
	return taskcontrol.Wait(ctx)
}

// Disk 以这个任务的名义发起一次**磁盘作业**，并在返回前把这次作业的实况折进 IOMetrics。
//
// 返回的 error 是闸门错、令牌错或 fn 自己的返回值，三者分不出来；「fn 有没有执行」分得出来，
// 「已哈希文件数」这条计数规则因此压在后者上：工种是整文件读取**且 fn 真的跑了**才 +1。
// 被闸门或令牌挡下、一个字节都没读的书不计入，慢盘上这个数字才与实物对得上。
func (h *Handle) Disk(ctx context.Context, w diskwork.Work, fn func() error) error {
	ran := false
	stats, err := h.disk.Do(ctx, w, func() error {
		ran = true
		return fn()
	})
	h.absorb(stats, ran && w.Kind == storageio.WorkKindIdentityHash)
	return err
}

// IOMetrics 返回至今吸收到的 IO 实况快照。何时把它报出去由任务体决定：句柄只负责它不会漏记。
func (h *Handle) IOMetrics() IOMetrics {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.io
}

// absorb 把一次磁盘作业的实况折进累加器。
//
// 档位与卷键只认有值的那一次：闸门拦下的作业根本没解析策略，交回的是零值实况，照抄会把
// 「没有这回事」写成「实况为空」，抹掉上一次作业真实报出的那一档。
func (h *Handle) absorb(stats diskwork.Stats, hashedFile bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.io.IOWaitMillis += stats.Wait.Milliseconds()
	h.io.PausedMillis += stats.PausedWait.Milliseconds()
	if stats.StorageProfile != "" {
		h.io.StorageProfile = stats.StorageProfile
	}
	if stats.VolumeKey != "" {
		h.io.VolumeKey = stats.VolumeKey
	}
	if hashedFile {
		h.io.HashedFiles++
	}
}
