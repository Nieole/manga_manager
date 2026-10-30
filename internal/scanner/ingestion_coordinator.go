package scanner

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// ingestionCoordinator owns change debounce, actual completion and safe cleanup scheduling.
// The filesystem adapter supplies changes; task hooks remain the only admission authority.
type ingestionCoordinator struct {
	mu                 sync.Mutex
	pending            map[int64]time.Time
	pendingCleanup     map[int64]cleanupSchedule
	scansInFlight      map[int64]int
	changeVersion      map[int64]uint64
	stopCh             chan struct{}
	stopping           bool
	baseCtx            context.Context
	cancelBase         context.CancelFunc
	inFlight           sync.WaitGroup
	coordinationLoop   sync.WaitGroup
	scanLibrary        func(context.Context, int64) (ScanOutcome, error)
	cleanupLibrary     func(context.Context, int64, func() bool) error
	libraryScanRunning func(int64) bool
	timings            watcherTimings
}

func newIngestionCoordinator(hooks WatcherHooks) *ingestionCoordinator {
	ctx, cancel := context.WithCancel(context.Background())
	return &ingestionCoordinator{
		pending: make(map[int64]time.Time), pendingCleanup: make(map[int64]cleanupSchedule),
		changeVersion: make(map[int64]uint64), scansInFlight: make(map[int64]int), stopCh: make(chan struct{}), baseCtx: ctx, cancelBase: cancel,
		scanLibrary: hooks.ScanLibrary, cleanupLibrary: hooks.CleanupLibrary, libraryScanRunning: hooks.LibraryScanRunning,
		timings: defaultWatcherTimings(),
	}
}

func (fw *ingestionCoordinator) changedLocked(id int64, removed bool, now time.Time) {
	if fw.stopping {
		return
	}
	fw.pending[id] = now
	fw.changeVersion[id]++
	if removed {
		fw.scheduleCleanupLocked(id, now)
	}
}

func (fw *ingestionCoordinator) requeueScan(id int64) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	if !fw.stopping {
		fw.pending[id] = time.Now()
	}
}

func (fw *ingestionCoordinator) start(publish func(string)) {
	fw.coordinationLoop.Add(1)
	go func() {
		defer fw.coordinationLoop.Done()
		ticker := time.NewTicker(fw.timings.tick)
		defer ticker.Stop()
		for {
			select {
			case <-fw.stopCh:
				return
			case now := <-ticker.C:
				fw.mu.Lock()
				fw.dispatchDueLocked(now, publish)
				fw.mu.Unlock()
			}
		}
	}()
}

// cleanupSchedule 是一次库清理的排期。两个时刻各答一个问题：lastEvent 答「去抖窗口到了没」，
// firstEvent 答「最多还能再推迟多久」——推迟上限必须从**第一个**删除/重命名事件起算，
// 否则持续不断的删除会把 lastEvent 一直往后推，上限永远触发不了。
type cleanupSchedule struct {
	firstEvent time.Time
	lastEvent  time.Time
}

// due 报告这条清理本轮是否该被处理：去抖窗口已过，或已经等过了推迟上限。
func (c cleanupSchedule) due(now time.Time, t watcherTimings) bool {
	return now.Sub(c.lastEvent) >= t.cleanupDebounce || c.overdue(now, t)
}

// overdue 报告这条清理是否已等过推迟上限。它是让扫描**越过自己的去抖窗口**提前派发的唯一理由。
func (c cleanupSchedule) overdue(now time.Time, t watcherTimings) bool {
	return now.Sub(c.firstEvent) >= t.cleanupMaxDeferral
}

// watcherTimings 是去抖判定用到的四个时长。
type watcherTimings struct {
	// tick 是去抖判定的轮询周期。
	tick time.Duration
	// scanDebounce 是最后一次文件变动之后、触发扫描之前的静默期。
	scanDebounce time.Duration
	// cleanupDebounce 是最后一次删除/重命名之后、触发库清理之前的静默期。
	cleanupDebounce time.Duration
	// cleanupMaxDeferral 是一条清理从首个删除/重命名事件起最多被推迟多久。
	//
	// 清理要等「该库没有未了结的改名重连」，而持续写入的库里扫描的去抖窗口一直后移，
	// 这个条件可以永远不成立——删掉的书就永远以幽灵记录挂在库里。等过上限之后不再干等，
	// 改为主动为该库派发一次扫描、把清理接在它之后串行：安全性不变（清理仍在一次**成功**的
	// 扫描之后），代价只是这次扫描可能读到一个还在拷贝中的半成品归档，下一次扫描自愈。
	cleanupMaxDeferral time.Duration
}

func defaultWatcherTimings() watcherTimings {
	return watcherTimings{
		tick:               2 * time.Second,
		scanDebounce:       5 * time.Second,
		cleanupDebounce:    5 * time.Second,
		cleanupMaxDeferral: 2 * time.Minute,
	}
}

// scheduleCleanupLocked 排期（或续期）一条库清理。调用方须持有 fw.mu。
// firstEvent 只在这条排期首次建立时写：它是推迟上限的起算点，续期不该把它往后推。
func (fw *ingestionCoordinator) scheduleCleanupLocked(libID int64, now time.Time) {
	sched := fw.pendingCleanup[libID]
	if sched.firstEvent.IsZero() {
		sched.firstEvent = now
	}
	sched.lastEvent = now
	fw.pendingCleanup[libID] = sched
}

// requeueCleanupAfterFailedScan 在同轮扫描没跑成时把清理放回排期，并**同时**重新排期一次扫描。
//
// 清理只有跟在一次成功的扫描之后才安全。只放回清理而不放回扫描，下一个去抖窗口会因为
// 「这个库没有排期中的扫描」而判定安全并直接删行——那正是改名重连还没发生的时刻。
// 两个时刻都重置为现在：推迟上限是用来打破「去抖窗口一直后移」这个死结的，
// 而扫描失败是另一回事，按正常去抖节奏重试即可，不该退化成每个 tick 顶一次扫描。
func (fw *ingestionCoordinator) requeueCleanupAfterFailedScan(libID int64) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	if fw.stopping {
		return
	}
	now := time.Now()
	fw.pendingCleanup[libID] = cleanupSchedule{firstEvent: now, lastEvent: now}
	fw.pending[libID] = now
}

// rehomeUnsettledLocked 报告该库此刻是否还有**未了结的改名重连**，非空即为「清理必须继续等」的理由。
// 调用方须持有 fw.mu。
//
// 「安全」的实质不是「本轮有没有扫描要跑」，而是「这个库当前有没有改名还没被重连上」。
// 三条信号缺一不可，因为它们各自只看得见一段时间：
//
//   - pending 里还挂着这个库 —— 有文件变动排了扫描但还没派发。改名的 Create(新名) 半边一定会
//     排一次扫描，所以「排了但去抖窗口还没到」正是改名待重连的时刻。光靠「本轮有没有扫描要跑」
//     判不出这一条：持续写入（一边改名一边往库里拷新书）会让扫描的窗口一直后移，两条排期就此分叉。
//   - 本 watcher 有已派发未返回的该库扫描 —— 重连是扫描末尾才写的，扫描没返回就不算落地。
//   - 别处发起的扫描正在跑 —— 任务面板的「扫描资料库」同样在末尾写重连。
func (fw *ingestionCoordinator) rehomeUnsettledLocked(libID int64) string {
	if _, ok := fw.pending[libID]; ok {
		return "scan_pending"
	}
	if fw.scansInFlight[libID] > 0 {
		return "scan_in_flight"
	}
	if fw.libraryScanRunning(libID) {
		return "scan_running_elsewhere"
	}
	return ""
}

// scanFinished 记一次派发出去的扫描已返回。
func (fw *ingestionCoordinator) scanFinished(libID int64) {
	fw.mu.Lock()
	if fw.scansInFlight[libID] <= 1 {
		delete(fw.scansInFlight, libID)
	} else {
		fw.scansInFlight[libID]--
	}
	fw.mu.Unlock()
}

// dispatchDueLocked 派发本轮到期的扫描与清理。调用方须持有 fw.mu。
func (fw *ingestionCoordinator) dispatchDueLocked(now time.Time, publishEvent func(string)) {
	// 停机已经开始：此刻派出去的工作只会拿着已取消的 baseCtx 立刻返回，而那次 Add 会与
	// Stop 的 Wait 撞在一起。见 stopping。
	if fw.stopping {
		return
	}
	for libID, lastChange := range fw.pending {
		if fw.scansInFlight[libID] > 0 {
			continue
		}
		// 去抖：最后一次文件变动距今超过 scanDebounce 才触发扫描。例外是该库有一条清理
		// 已经等过了推迟上限——那条清理必须跟在一次扫描之后才敢跑，而持续写入会让这个
		// 窗口一直后移，只能由它反过来把扫描顶出去（见 cleanupMaxDeferral）。
		sched, hasCleanup := fw.pendingCleanup[libID]
		forcedByCleanup := hasCleanup && sched.overdue(now, fw.timings)
		if now.Sub(lastChange) < fw.timings.scanDebounce && !forcedByCleanup {
			continue
		}
		delete(fw.pending, libID)
		// 同一个库若也有一条到期的清理，把它接在这次扫描之后串行跑。改名一本书会产生
		// Rename(旧名)+Create(新名) 两个事件，分别排期清理与扫描；清理只做 stat，
		// 比要走完整棵树、末尾才写 RehomeBookPath 的扫描快得多，并发派发时必然抢在
		// 改名重连之前把旧行当成"文件已消失"删掉，阅读进度、书签、合集归属、阅读清单
		// 条目随 ON DELETE CASCADE 一起没。
		cleanupAfterScan := hasCleanup && sched.due(now, fw.timings)
		if cleanupAfterScan {
			delete(fw.pendingCleanup, libID)
		}
		slog.Info("Hot reload triggered by file watcher", "library_id", libID, "forced_by_cleanup", forcedByCleanup)
		if publishEvent != nil {
			publishEvent("hot_reload:")
		}
		fw.scansInFlight[libID]++
		version := fw.changeVersion[libID]
		fw.inFlight.Add(1)
		go func(id int64, cleanup bool, version uint64) {
			defer fw.inFlight.Done()
			// 计数在**清理之后**才归零：多留这一会儿只会让别的清理更保守地多等一轮。
			defer fw.scanFinished(id)
			outcome, err := fw.scanLibrary(fw.baseCtx, id)
			if err == nil {
				err = outcome.Err()
			}
			switch {
			case errors.Is(err, context.Canceled):
				// 停机取消，不是故障。
			case errors.Is(err, ErrScanSuppressed):
				// 这个库的自动发起被停发挡下：本轮不扫，跟在后面的清理也一并放弃——
				// 清理只有跟在一次成功的扫描之后才安全，而这次扫描根本没发生。
				// 不重排：条件不会因为再等一个去抖窗口而改变，重排就是每隔几秒再被挡一次。
				slog.Info("Hot reload scan suppressed by task backoff or disable", "library_id", id)
				return
			case errors.Is(err, ErrScanCoalesced):
				// 这个库上已经排着一次等价的扫描，本次被**合并**进去了：那一次会看到这批新文件，
				// 无需重试也不必报错。它还没跑完，因此下面那条清理仍要重新排期。
				slog.Info("Hot reload scan coalesced into a queued scan", "library_id", id)
			case err != nil:
				slog.Error("Hot reload scan failed", "library_id", id, "error", err)
			}
			if !cleanup {
				if err != nil && !errors.Is(err, context.Canceled) {
					fw.requeueScan(id)
				}
				return
			}
			// 扫描没跑成就不能清理：改名重连可能还没发生，此刻删行就是丢数据。
			// 连同扫描一起重新排期，交给下一个去抖窗口。
			if err != nil {
				fw.requeueCleanupAfterFailedScan(id)
				return
			}
			fw.cleanupAfterScan(id, version)
		}(libID, cleanupAfterScan, version)
	}

	// 去抖后单独触发库清理，清除删除/重命名遗留的幽灵记录。同轮有扫描的已在上面被取走、
	// 接在扫描之后串行；走到这里的还必须再过一道闸：该库不能有未了结的改名重连。
	// CleanupLibrary 自带根目录探测与占比熔断，存储离线时不会误删。
	for libID, sched := range fw.pendingCleanup {
		if !sched.due(now, fw.timings) {
			continue
		}
		if reason := fw.rehomeUnsettledLocked(libID); reason != "" {
			// 留在排期里，时间戳原样不动：下一轮再判。什么时候能跑得看闸何时放开——
			// 在飞的扫描必然结束；排期中的扫描则由 cleanupMaxDeferral 兜底顶出去。
			slog.Debug("Watcher-triggered cleanup deferred", "library_id", libID, "reason", reason)
			continue
		}
		delete(fw.pendingCleanup, libID)
		version := fw.changeVersion[libID]
		fw.inFlight.Add(1)
		go func(id int64) {
			defer fw.inFlight.Done()
			fw.cleanupLibraryNow(id, version, false)
		}(libID)
	}
}

// cleanupLibraryNow 跑一次库清理并按结果记一笔。
//
// 停机取消、被**合并**与被**停发**挡下都不是故障：合并意味着这个库上已经排着一条等价的清理，
// 停发意味着用户或退避不要它自己跑。两个派发点共用它，「什么算故障」因此只有一处答案——
// 否则同一件事在一处记 Error、另一处不记。
func (fw *ingestionCoordinator) cleanupLibraryNow(libraryID int64, version uint64, ownScan bool) {
	eligible := func() bool {
		fw.mu.Lock()
		defer fw.mu.Unlock()
		maxFlights := 0
		if ownScan {
			maxFlights = 1
		}
		_, pending := fw.pending[libraryID]
		return !fw.stopping && fw.changeVersion[libraryID] == version && !pending && fw.scansInFlight[libraryID] <= maxFlights && !fw.libraryScanRunning(libraryID)
	}
	switch err := fw.cleanupLibrary(fw.baseCtx, libraryID, eligible); {
	case err == nil, errors.Is(err, context.Canceled):
	case errors.Is(err, ErrCleanupDeferred):
		fw.requeueCleanupAfterFailedScan(libraryID)
	case errors.Is(err, ErrScanSuppressed):
		slog.Info("Watcher-triggered cleanup suppressed by task backoff or disable", "library_id", libraryID)
	case errors.Is(err, ErrScanCoalesced):
		slog.Info("Watcher-triggered cleanup coalesced into a queued cleanup", "library_id", libraryID)
	default:
		slog.Error("Watcher-triggered cleanup failed", "library_id", libraryID, "error", err)
	}
}

// cleanupAfterScan requires that the covering scan did not miss a newer delivered change.
func (fw *ingestionCoordinator) cleanupAfterScan(id int64, version uint64) {
	fw.mu.Lock()
	if fw.stopping {
		fw.mu.Unlock()
		return
	}
	if fw.changeVersion[id] != version {
		now := time.Now()
		fw.scheduleCleanupLocked(id, now)
		if _, queued := fw.pending[id]; !queued {
			fw.pending[id] = now
		}
		fw.mu.Unlock()
		return
	}
	fw.mu.Unlock()
	fw.cleanupLibraryNow(id, version, true)
}
