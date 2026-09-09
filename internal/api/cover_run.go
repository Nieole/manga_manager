// 封面每库一条**运行**：扫描把一批封面作业交进来，这里发起那条运行、把批交给它跑完，
// 并把它的推进翻成**一帧**。批与运行的配对规则见 coverRunQueues。

package api

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/runhandle"
	"manga-manager/internal/scanner"
	"manga-manager/internal/task"
)

// coverRunQueues 是「哪一批封面归哪一条运行」的配对处：批先进这里攒着，运行开跑时**认领**
// 攒下的那一组。
//
// 认领而不是在发起时绑定，是因为发起与开跑之间隔着队列：槽位满或这个库已有一条封面运行时，
// 本次发起只落成一条**排队中**，甚至被**合并**进已经排着的那一条。攒着的那些批因此不能挂在
// 某一次发起上——挂上去，被合并掉的那次就把它带进了坟墓，那个库的封面从此静默不再生成。
//
// 同一个库同一时刻只有一条封面运行在跑（身份上那条部分唯一索引保证），认领因此不会撞车：
// 一条运行认走的就是「上一条认领之后攒起来的全部」。
type coverRunQueues struct {
	mu      sync.Mutex
	pending map[int64][]*scanner.CoverBatch
}

func newCoverRunQueues() *coverRunQueues {
	return &coverRunQueues{pending: make(map[int64][]*scanner.CoverBatch)}
}

// enqueue 把一批封面挂到它那个库的名下，等一条运行来认领。
func (q *coverRunQueues) enqueue(batch *scanner.CoverBatch) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.pending[batch.LibraryID()] = append(q.pending[batch.LibraryID()], batch)
}

// claim 认领这个库名下攒着的全部批，并把名下清空。
func (q *coverRunQueues) claim(libraryID int64) []*scanner.CoverBatch {
	q.mu.Lock()
	defer q.mu.Unlock()
	batches := q.pending[libraryID]
	delete(q.pending, libraryID)
	return batches
}

// withdraw 撤回**这一批**，交回它是不是还挂在名下。
//
// 发起不成时只能撤回自己那一批：整个库名下 claim 一次会把并发扫描刚挂上、
// 而且发起已经成功的那几批一起抄走，它们随后会被跑在一个既不受暂停也不受取消约束的
// 上下文里，而认领它们的那条运行认到的是空。
func (q *coverRunQueues) withdraw(batch *scanner.CoverBatch) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	libraryID := batch.LibraryID()
	pending := q.pending[libraryID]
	for i, candidate := range pending {
		if candidate != batch {
			continue
		}
		q.pending[libraryID] = append(pending[:i:i], pending[i+1:]...)
		if len(q.pending[libraryID]) == 0 {
			delete(q.pending, libraryID)
		}
		return true
	}
	return false
}

// dispatchCoverBatch 是交给扫描器的**封面运行**发起口（scanner.CoverRunLauncher）。
//
// 它**接手**这一批：先挂到库名下，再发起一条**串联**的封面运行去认领。发起可能落成排队、
// 也可能被**合并**进已经排着的那一条，两者都不是错误——那条运行开跑时会连同这一批一起认领走。
//
// **不得阻塞**：它跑在扫描的入库协程上，而这一批要到扫描收尾才关。启动入口保证「准入同步、
// 任务体异步」，这条约束与它同源。
//
// 一条运行都没建出来时（发起出错，或这个身份被**停发**挡下）才走兜底：把自己这一批就地跑完，
// 封面照样生成，只是无处可报。丢下不管的话它会一直挂在名下，那个库缺封面的书要等到
// 下一次扫描才有人再排一遍。
func (c *Controller) dispatchCoverBatch(batch *scanner.CoverBatch) {
	libraryID := batch.LibraryID()
	c.coverRuns.enqueue(batch)
	launched, err := c.startCoverRun(libraryID, task.TriggerChained, false)
	if err == nil && launched.Stalled == task.StallNone {
		return
	}
	if err != nil {
		slog.Warn("Failed to launch cover run, generating covers without one", "library_id", libraryID, "error", err)
	}
	if c.coverRuns.withdraw(batch) {
		c.runBackground(func() { _ = batch.Drain(context.Background(), nil) })
	}
}

// discardCoverBatches 丢掉这个库名下还没被认领的批。删库那条路径要它：库都没了，
// 那些封面不必再生成，而挂着的批没有任何一条运行会来认领它们。
func (c *Controller) discardCoverBatches(libraryID int64) {
	c.coverRuns.claim(libraryID)
}

// launchCoverRun 发起一条封面运行，走引擎的启动入口——槽位与准入因此对它一视同仁：
// 还没轮到的封面运行一张封面都不会开始生成。
//
// backfill 为真表示这一次要自己去找活干：重启后的续跑与用户点重试都拿不到进程里那一批
// （它随重启一起没了），改为把这个库里**还缺封面的书**重新排一遍。
func (c *Controller) launchCoverRun(libraryID int64, trigger task.Trigger, backfill bool) error {
	_, err := c.startCoverRun(libraryID, trigger, backfill)
	return err
}

// startCoverRun 与 launchCoverRun 是同一次发起，另外交回这次发起落地成了什么（见 task.Launched）。
// 只有交出批的那一方需要它：一条运行都没建出来时，那一批得由发起方自己收场。
func (c *Controller) startCoverRun(libraryID int64, trigger task.Trigger, backfill bool) (task.Launched, error) {
	lib, err := c.store.GetLibrary(context.Background(), libraryID)
	if err != nil {
		return task.Launched{}, err
	}
	policy := config.ResolveStoragePolicy(c.currentConfig(), lib.Path)
	spec := RunSpec{
		Key:         fmt.Sprintf("generate_covers_%d", libraryID),
		StartCode:   "task.msg.generate_covers.start",
		StartParams: map[string]string{"name": lib.Name},
		CanCancel:   true,
		CanPause:    true,
		ScopeName:   lib.Name,
		Metadata: map[string]string{
			"storage_profile":   policy.StorageProfile,
			"volume_key":        policy.VolumeKey,
			"cover_concurrency": strconv.Itoa(policy.IOPolicy.CoverConcurrency),
		},
		CompleteCode: "task.msg.generate_covers.complete",
		CancelCode:   "task.msg.generate_covers.cancelled",
		FailCode:     "task.msg.generate_covers.failed",
	}

	return c.taskEngine.start(libraryTask("generate_covers", libraryID, variantSole), trigger, spec,
		func(ctx context.Context, handle *runhandle.Handle) (TaskResult, error) {
			batches := c.coverRuns.claim(libraryID)
			if backfill {
				missing, err := c.scanner.QueueMissingCovers(ctx, libraryID)
				if err != nil {
					return taskFailure("task.msg.generate_covers.scan_failed", err), err
				}
				batches = append(batches, missing)
			}
			observer := newCoverRunObserver(handle, lib.Name)
			for _, batch := range batches {
				observer.begin()
				err := batch.Drain(ctx, observer)
				// 定版必须在两条出口上都做：被取消的那一批也已经生成了它那部分，
				// 只算前面几批会让终态文案报出一个比实际小的数。
				observer.fixate()
				if err != nil {
					return TaskResult{Params: map[string]string{"name": lib.Name}}, err
				}
			}
			c.PublishEvent("refresh_thumbnails")
			return TaskResult{Params: map[string]string{
				"name":      lib.Name,
				"generated": strconv.FormatInt(observer.generated(), 10),
			}}, nil
		})
}

// coverRunObserver 把封面推进的报文写进它那条运行的**运行句柄**（实现 scanner.CoverObserver）。
//
// 它只是句柄的一层翻译加一个跨批的累计：一条封面运行可能认领了好几批（扫描期间又来一次扫描），
// 而每一批只报自己那份，界面上要的是这条运行至今一共生成了多少。
type coverRunObserver struct {
	progress    *runhandle.Handle
	libraryName string
	// startedAt 是这条运行开跑的时刻：存储 IO 面板按 duration_ms 估速率，
	// 缺了它那格会在运行收尾之后按挂钟一路衰减。
	startedAt time.Time

	mu sync.Mutex
	// accepting 划出「手上正跑着哪一批」的窗口：窗口之外的报文一律丢掉。
	// 被取消的那一批还有几个作业在飞，它们迟到的报文会落在下一批的基线上，把数字加一倍。
	accepting bool
	base      coverRunTotals
	latest    coverRunTotals
}

func newCoverRunObserver(progress *runhandle.Handle, libraryName string) *coverRunObserver {
	return &coverRunObserver{progress: progress, libraryName: libraryName, startedAt: time.Now()}
}

// coverRunTotals 是若干批封面的合计。每一项都是可加的：合计 = 已收工那几批的定版值 +
// 手上这一批的最新值。
type coverRunTotals struct {
	queued               int64
	generated            int64
	failed               int64
	remaining            int64
	openedArchives       int64
	ioWaitMillis         int64
	pausedMillis         int64
	thumbnailWriteMillis int64
}

func (t *coverRunTotals) add(other coverRunTotals) {
	t.queued += other.queued
	t.generated += other.generated
	t.failed += other.failed
	t.remaining += other.remaining
	t.openedArchives += other.openedArchives
	t.ioWaitMillis += other.ioWaitMillis
	t.pausedMillis += other.pausedMillis
	t.thumbnailWriteMillis += other.thumbnailWriteMillis
}

// Progress 把一批封面的一份报文并进这条运行的合计，再整帧写出去。
//
// 报文带的是**那一批的全量当前值**，因此不能相加：合计 = 已收工的批的定版值 + 手上这一批的最新值。
func (o *coverRunObserver) Progress(report scanner.CoverProgressReport) {
	merged, ok := o.absorb(coverRunTotals{
		queued:               report.Queued,
		generated:            report.Generated,
		failed:               report.Failed,
		remaining:            report.Remaining,
		openedArchives:       report.OpenedArchives,
		ioWaitMillis:         report.IOWaitMillis,
		pausedMillis:         report.PausedMillis,
		thumbnailWriteMillis: report.ThumbnailWriteMillis,
	})
	if !ok {
		return
	}

	// 已结算的那些是 Queued 减 Remaining，不是「生成 + 失败」：还有第三种出口——
	// 这本书已经有封面了（用户自己设的，或另一次扫描抢先写下）。漏掉它，
	// 进度条到最后差着几张永远走不满。
	current := int(merged.queued - merged.remaining)
	total := int(merged.queued)
	o.progress.Report(runhandle.Frame{
		Current: &current,
		Total:   &total,
		Phase:   "generating_covers",
		Item:    report.CurrentItem,
		Code:    "task.msg.generate_covers.progress",
		Params: map[string]string{
			"name":      o.libraryName,
			"generated": strconv.FormatInt(merged.generated, 10),
			"total":     strconv.FormatInt(merged.queued, 10),
		},
		Metrics: map[string]int64{
			"queued_covers":      merged.queued,
			"generated_covers":   merged.generated,
			"failed_covers":      merged.failed,
			"remaining_covers":   merged.remaining,
			"opened_archives":    merged.openedArchives,
			"io_wait_ms":         merged.ioWaitMillis,
			"paused_ms":          merged.pausedMillis,
			"thumbnail_write_ms": merged.thumbnailWriteMillis,
			"duration_ms":        time.Since(o.startedAt).Milliseconds(),
		},
	})
}

// ItemFailed 把一本书的封面失败落成这条运行的**运行事件**：`failed_covers: 12` 答不出
// 是哪 12 本，这一条才答得出。
//
// 它不受那扇「手上正跑着哪一批」的窗口约束：窗口挡的是会被重复累加的**合计**，
// 而一条失败明细是一件独立发生过的事，迟到一拍照样属于这条运行。
func (o *coverRunObserver) ItemFailed(failure scanner.ItemFailure) {
	o.progress.ItemFailed(failure.Path, failure.Reason)
}

// absorb 收下手上这一批的最新值，交回这条运行至今的合计；窗口之外的报文即 false。
func (o *coverRunObserver) absorb(latest coverRunTotals) (coverRunTotals, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.accepting {
		return coverRunTotals{}, false
	}
	o.latest = latest
	merged := o.base
	merged.add(latest)
	return merged, true
}

// begin 开一批的窗口，fixate 关掉它并把这一批定版进合计。两者由任务体在 Drain 前后调用。
//
// 定版不由报文自己判「剩余量归零」：扫描还在往批里加的间隙剩余量本来就会归零，
// 按它定版会让下一份报文加在自己刚定版的值上，界面上的数字凭空翻倍。
func (o *coverRunObserver) begin() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.accepting = true
}

func (o *coverRunObserver) fixate() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.accepting = false
	o.base.add(o.latest)
	o.latest = coverRunTotals{}
}

func (o *coverRunObserver) generated() int64 {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.base.generated + o.latest.generated
}
