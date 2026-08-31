package taskrun

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

// Handle 是一个任务的**任务句柄**：写它的进度、过它的**暂停闸门**、以它的名义发起**磁盘作业**。
//
// 「谁有资格写某个任务的进度」由「谁被交给了这个句柄」决定，而不是「谁会拼那个任务键字符串」——
// 前者是结构约束，后者不是。**任务键**因此不出现在句柄上，它由构造期那三个闭包在引擎侧绑定。
//
// 全部方法可从任意 goroutine 调用：上报那几个方法本来就会从任务体之外被调用（**扫描观察者**
// 把封面队列的报文写进来），而 IO 实况是句柄自己持有的可变状态。
type Handle struct {
	report      func(Frame)
	mergeParams func(map[string]string)
	addMetrics  func(map[string]int64, map[string]string)
	disk        *diskwork.Runner

	mu sync.Mutex
	io IOMetrics
}

// New 建一个任务句柄。三个函数值分别是这个任务的整帧写入、**任务参数**合并与指标累加，都不得为 nil。
// disk 只被 Disk 用到：留 nil 的句柄照样能上报与过闸门，而 Disk 会在闸门放行之后 panic。
func New(
	report func(Frame),
	mergeParams func(map[string]string),
	addMetrics func(map[string]int64, map[string]string),
	disk *diskwork.Runner,
) *Handle {
	return &Handle{report: report, mergeParams: mergeParams, addMetrics: addMetrics, disk: disk}
}

// Report 把整帧交给写入函数，一次报完。
//
// 它是上报面的主用法：一个外部事件天然就是一整帧，典型是把扫描器的一份报文翻成一帧进度。
// 那种帧拆成几次报会撕开——写入方的投递水位放行了其中一条中间态，又把后面补齐的那条吞掉，
// 于是同一份载荷里指标已经走到第 N 条、进度条还停在第 N-1 条。
func (h *Handle) Report(f Frame) {
	h.report(f)
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

// MergeParams 按键合并**任务参数**：**重启函数**读回入参、存储 IO 面板读取扫描计数用的正是这一份。
// 上报方手里握着这些参数的全量当前值时用它。
//
// 它与 Frame.Params 只差一个字，去处却不同——那一路是文案占位参数。接反不会有编译错误，
// 后果是任务参数丢失，或者文案把占位符原样渲染出来。
func (h *Handle) MergeParams(params map[string]string) {
	h.mergeParams(params)
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
	h.addMetrics(increments, params)
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
