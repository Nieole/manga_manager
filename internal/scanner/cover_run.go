// 封面每库一条**运行**在扫描器这一侧的样子：一批封面作业的记账、派发与等待，
// 以及这一批交给谁去跑。生成一张封面本身在 scanner.go 的 runCoverJob。

package scanner

import (
	"context"
	"crypto/sha1"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"manga-manager/internal/diskwork"
	"manga-manager/internal/taskcontrol"
)

// CoverObserver 是一条**封面运行**收下报文的对象，由发起那条运行的一方交出。
//
// 它与**扫描观察者**是两条互不相干的通道：封面作业入队时就带上自己那条运行，报文因此直接
// 写进那条运行，不经扫描那一条（ADR 0005）。交出 nil 表示这一批的报文无处可报。
type CoverObserver interface {
	Progress(CoverProgressReport)
	// ItemFailed 报告一本书的封面没生成成：哪个文件、为什么。
	// `failed_covers: 12` 答不出是哪 12 本，这一条才答得出。
	ItemFailed(ItemFailure)
}

// CoverProgressReport 是一条封面运行的一次推进报文。
//
// 它与扫描的报文一样不带身份：一份报文属于哪一次运行，由收下它的 CoverObserver 回答。
type CoverProgressReport struct {
	CurrentItem string
	// Queued 是这一批至今排进来的总数，Remaining 是其中还没有结果的那些；
	// **已结算的那些是 Queued 减 Remaining**，不是 Generated 加 Failed——
	// 还有第三种出口：这本书在封面生成期间已经有了封面，那既不是新增一张也不是故障。
	Queued    int64
	Generated int64
	Failed    int64
	Remaining int64
	// OpenedArchives 数封面构建真的打开过的归档，存储 IO 面板按它估速率。
	OpenedArchives       int64
	IOWaitMillis         int64
	PausedMillis         int64
	ThumbnailWriteMillis int64
}

// CoverRunLauncher 由装配方注册：一次扫描第一次要为某个库排封面作业时调它**一次**，
// 由它去建那条封面运行，并在那条运行的任务体里把这一批跑完（CoverBatch.Drain）。
//
// 它**接手**这一批，扫描器此后不再管它。没有注册时扫描器自己跑完这一批——封面照样生成，
// 只是无处可报，与交出 nil 扫描观察者是同一个形状。
type CoverRunLauncher func(batch *CoverBatch)

// SetCoverRunLauncher 注册封面运行的发起口。
func (s *Scanner) SetCoverRunLauncher(launch CoverRunLauncher) {
	s.coverRuns = launch
}

// bookCoverHash 是一本书自动缩略图的文件名：路径、修改时间与大小的复合哈希，
// 因此文件内容一变，缩略图就换一个名字、必然重生成。
func bookCoverHash(path string, modTime time.Time, size int64) string {
	return fmt.Sprintf("%x", sha1.Sum([]byte(fmt.Sprintf("%s|%d|%d", path, modTime.Unix(), size))))
}

// QueueMissingCovers 为这个库里还缺封面的书排一批封面作业并交回那一批。
//
// 它是**续跑与重试**那条路：进程里那一批随重启一起没了，缺封面的书还在库里。交回的批已关闭，
// 调用方（那条封面运行的任务体）直接 Drain 即可。挑哪一页留给封面构建自己去读——
// 库里读得到书行，读不到归档里的页目录。
func (s *Scanner) QueueMissingCovers(ctx context.Context, libraryID int64) (*CoverBatch, error) {
	batch := s.newCoverBatch(libraryID)
	defer batch.close()

	seriesList, err := s.store.ListSeriesByLibraryLite(ctx, libraryID)
	if err != nil {
		return nil, fmt.Errorf("failed to list series of library %d: %w", libraryID, err)
	}
	for _, series := range seriesList {
		if err := taskcontrol.Wait(ctx); err != nil {
			return nil, err
		}
		books, err := s.store.ListBooksBySeries(ctx, series.ID)
		if err != nil {
			slog.WarnContext(ctx, "Failed to list books while queueing missing covers", "series_id", series.ID, "error", err)
			continue
		}
		var jobs []coverJob
		for _, book := range books {
			if book.CoverPath.Valid && book.CoverPath.String != "" {
				continue
			}
			jobs = append(jobs, coverJob{
				bookID:    book.ID,
				seriesID:  book.SeriesID,
				libraryID: libraryID,
				candidate: coverCandidate{path: book.Path, bookHash: bookCoverHash(book.Path, book.FileModifiedAt, book.Size)},
			})
		}
		batch.add(jobs)
	}
	return batch, nil
}

// coverReportInterval 是封面推进的投递水位：每生成一张就报一次的话，一次首扫要写几万次运行行。
const coverReportInterval = 250 * time.Millisecond

// coverDispatchWindow 是**一批**封面同时在飞的上限。
//
// 不封上限的后果是暂停按不住：Drain 会一口气把上千个作业塞进共享队列，worker 取到之后才发现
// 这条运行的**暂停闸门**关着，于是一条被按下的封面运行占满整个池子，另一个库的封面跟着一起停。
// 闸门因此只在**派发前**问，而派发受这个窗口约束——暂停在几张封面之内生效，
// 且一条运行最多占住这么多个 worker。取值比 worker 池的上限（4）宽一档，
// 池子才不会为了等下一次派发而空转。
const coverDispatchWindow = 8

// CoverBatch 是一次发起排进封面队列的**那一批**封面作业，也是它自己的记账处。
//
// 计数按批而不是全局：一条封面运行要回答的是「我这一批还剩几张」，而 worker 池是共享的，
// 两个库的封面运行会在里面交错推进。
//
// 它由扫描器建、交给一条封面运行去 Drain；Drain 把作业逐个派进共享 worker 池，
// 因此**槽位与暂停都拦得住它**——还没轮到的封面运行一张都不会开始生成。
type CoverBatch struct {
	scanner   *Scanner
	libraryID int64

	mu       sync.Mutex
	pending  []coverJob
	closed   bool
	observer CoverObserver
	// wake 让 Drain 在「还没关批、此刻却没活」时等着，容量 1 的信号槽即可：
	// 醒来之后它会把 pending 抽干，因此攒下的信号合成一次没有损失。
	wake chan struct{}

	launchOnce sync.Once
	claimed    atomic.Bool

	queued    atomic.Int64
	settled   atomic.Int64
	generated atomic.Int64
	failed    atomic.Int64

	openedArchives       atomic.Int64
	ioWaitMillis         atomic.Int64
	pausedMillis         atomic.Int64
	thumbnailWriteMillis atomic.Int64

	// window 是在飞名额的信号量：派发前占一个，作业结束时归还。inflight 只回答
	// 「还有没有在飞的」，两者一起构成 awaitInflight 等的那件事。
	window   chan struct{}
	inflight sync.WaitGroup
	throttle reportThrottle
}

// newCoverBatch 开一批封面作业。它还没有去处——交给谁跑由 attach 或调用方自己决定。
func (s *Scanner) newCoverBatch(libraryID int64) *CoverBatch {
	return &CoverBatch{
		scanner:   s,
		libraryID: libraryID,
		wake:      make(chan struct{}, 1),
		window:    make(chan struct{}, coverDispatchWindow),
		throttle:  reportThrottle{interval: coverReportInterval},
	}
}

// LibraryID 是这一批封面属于哪个资料库——封面运行每库一条，它就是那条运行的作用域 id。
func (b *CoverBatch) LibraryID() int64 { return b.libraryID }

// Queued 是这一批至今排进来的总数，Remaining 是其中还没有结果的那些。
func (b *CoverBatch) Queued() int64    { return b.queued.Load() }
func (b *CoverBatch) Generated() int64 { return b.generated.Load() }
func (b *CoverBatch) Remaining() int64 { return b.queued.Load() - b.settled.Load() }

// add 把一批作业排进来。批已关闭即无操作——扫描收尾之后不该再往里加。
func (b *CoverBatch) add(jobs []coverJob) {
	if len(jobs) == 0 {
		return
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.pending = append(b.pending, jobs...)
	b.mu.Unlock()
	b.queued.Add(int64(len(jobs)))
	b.signal()
}

// attach 把这一批交给注册的发起方，一批只交一次。
func (b *CoverBatch) attach() {
	b.launchOnce.Do(func() {
		if b.scanner.coverRuns != nil {
			b.scanner.coverRuns(b)
			return
		}
		// 没有发起方接手：扫描器自己把这一批跑完。封面照样生成，只是不建运行、无处可报，
		// 与交出 nil 扫描观察者是同一个形状。
		b.scanner.selfDrained.Add(1)
		go func() {
			defer b.scanner.selfDrained.Done()
			_ = b.Drain(context.Background(), nil)
		}()
	})
}

// waitForCoverQueue 等扫描器**自己接手**的那些批跑完。
//
// 它答不出别人接手的那些——那些批的剩余量归各自那条封面运行（CoverBatch.Remaining），
// 进程级的「封面排空了没有」在按批计数之后不再是一个有意义的问题。
func (s *Scanner) waitForCoverQueue(ctx context.Context) error {
	return awaitWaitGroup(ctx, &s.selfDrained)
}

// close 声明「扫描不再往这一批里加了」。Drain 据此知道抽干就是抽完。
func (b *CoverBatch) close() {
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
	b.signal()
}

func (b *CoverBatch) signal() {
	select {
	case b.wake <- struct{}{}:
	default:
	}
}

// take 取下一个待派发的作业：ok 说这次有没有取到，closed 说这一批还会不会再有新的。
func (b *CoverBatch) take() (job coverJob, ok bool, closed bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.pending) == 0 {
		return coverJob{}, false, b.closed
	}
	job = b.pending[0]
	b.pending[0] = coverJob{}
	b.pending = b.pending[1:]
	return job, true, b.closed
}

// Drain 把这一批逐个派进共享 worker 池，等它们全部有了结果之后返回。这是一条封面运行的任务体。
//
// 扫描还在往里加时它等着，扫描收尾关批之后派完最后一个就收工。ctx 是那条封面运行自己的
// 上下文：**暂停闸门**在派发前问一次，取消当场停止派发——还没派出去的那些不再生成，
// 这正是「只让出一部分磁盘」要的效果。
//
// 一批只由一条运行跑：重复 Drain 是无操作，否则同一份作业会被派两遍。
func (b *CoverBatch) Drain(ctx context.Context, observer CoverObserver) error {
	if !b.claimed.CompareAndSwap(false, true) {
		return nil
	}
	b.mu.Lock()
	b.observer = observer
	b.mu.Unlock()

	for {
		job, ok, closed := b.take()
		if ok {
			if err := taskcontrol.Wait(ctx); err != nil {
				return err
			}
			if err := b.scanner.dispatchCover(ctx, b, job); err != nil {
				return err
			}
			continue
		}
		if closed {
			break
		}
		select {
		case <-b.wake:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err := b.awaitInflight(ctx); err != nil {
		return err
	}
	b.report("", true)
	return nil
}

// reserve 占一个在飞名额，窗口满时等着；ctx 结束即放弃。
func (b *CoverBatch) reserve(ctx context.Context) error {
	select {
	case b.window <- struct{}{}:
		b.inflight.Add(1)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// release 归还一个在飞名额。派出去的每一个作业都要经它，否则窗口会一格格漏光、
// 这一批从此一张也派不出去。
func (b *CoverBatch) release() {
	<-b.window
	b.inflight.Done()
}

// awaitInflight 等已派出去的那些跑完；ctx 结束时不等——它们自己也会在下一个检查点退出。
func (b *CoverBatch) awaitInflight(ctx context.Context) error {
	return awaitWaitGroup(ctx, &b.inflight)
}

// awaitWaitGroup 等一个 WaitGroup 归零，ctx 先结束即交回它的错误。
//
// `sync.WaitGroup.Wait` 等不了 ctx，因此这里必须多起一条 goroutine；它在 WaitGroup 归零时
// 自己退出，等待方走不走得掉都一样。
func awaitWaitGroup(ctx context.Context, wg *sync.WaitGroup) error {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// report 把这一批的当前记账报给它的观察者。force 为假时受投递水位约束。
func (b *CoverBatch) report(currentItem string, force bool) {
	b.mu.Lock()
	observer := b.observer
	b.mu.Unlock()
	if observer == nil || !b.throttle.allow(force) {
		return
	}
	observer.Progress(CoverProgressReport{
		CurrentItem:          currentItem,
		Queued:               b.queued.Load(),
		Generated:            b.generated.Load(),
		Failed:               b.failed.Load(),
		Remaining:            b.Remaining(),
		OpenedArchives:       b.openedArchives.Load(),
		IOWaitMillis:         b.ioWaitMillis.Load(),
		PausedMillis:         b.pausedMillis.Load(),
		ThumbnailWriteMillis: b.thumbnailWriteMillis.Load(),
	})
}

// absorbDiskWork 把一次**磁盘作业**的等待与暂停耗时折进这一批。
func (b *CoverBatch) absorbDiskWork(stats diskwork.Stats) {
	if stats.Wait > 0 {
		b.ioWaitMillis.Add(stats.Wait.Milliseconds())
	}
	if stats.PausedWait > 0 {
		b.pausedMillis.Add(stats.PausedWait.Milliseconds())
	}
}

func (b *CoverBatch) countOpenedArchive() { b.openedArchives.Add(1) }

func (b *CoverBatch) countThumbnailWrite(d time.Duration) {
	if d > 0 {
		b.thumbnailWriteMillis.Add(d.Milliseconds())
	}
}

// 派出去的每一个作业都要经这三条出口之一结算，否则 Remaining 永远归不了零，
// 等这一批排空的那条封面运行会一直挂着。三条分开写是因为「没生成」有两种，
// 只有其中一种是故障：跳过说的是这本书已经有封面了。
func (b *CoverBatch) settleGenerated() {
	b.generated.Add(1)
	b.settled.Add(1)
}

// settleFailed 结算一次失败，并把「哪个文件、为什么」交给观察者。
//
// 原因由调用方给：三条失败出口各有各的因由（缩略图生成失败、生成了却没有路径、写回封面路径失败），
// 只报一句「封面生成失败」的话，详情页列出的是一串没有线索的文件名。
func (b *CoverBatch) settleFailed(path, reason string) {
	b.failed.Add(1)
	b.settled.Add(1)
	b.mu.Lock()
	observer := b.observer
	b.mu.Unlock()
	if observer == nil {
		return
	}
	observer.ItemFailed(ItemFailure{Path: path, Reason: reason})
}

func (b *CoverBatch) settleSkipped() { b.settled.Add(1) }

// reportThrottle 是「同一份展示态别每次都投出去」的水位，扫描进度与封面推进共用一份实现。
type reportThrottle struct {
	interval time.Duration

	mu       sync.Mutex
	lastSent time.Time
}

// allow 判这一次该不该报。force 无条件放行，并把水位重新压下去。
func (t *reportThrottle) allow(force bool) bool {
	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	if !force && now.Sub(t.lastSent) < t.interval {
		return false
	}
	t.lastSent = now
	return true
}
