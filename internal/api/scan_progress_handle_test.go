// 守**资料库扫描**与**系列扫描**任务的进度所有权（保管处见 scanProgressHandles）。
//
// 这两个**任务键**的拼法在整个包里人尽皆知，「谁能写它们的进度」若不由句柄决定就等于不受约束；
// 反过来句柄一旦登记不上，整个扫描期间的计数、阶段与当前条目全都不动，任务气泡停在起始文案上。

package api

import (
	"testing"
	"time"

	"manga-manager/internal/scanner"
)

// newScanEventsTestController 手工拼装这条链路需要的两个组件：任务引擎与句柄保管处。
// 引擎仍经它唯一的 seam（newTaskEngine）构造，扫描事件转译处不碰其余任何 Controller 字段，
// 因此这里不需要数据库、配置管理器或扫描器。
func newScanEventsTestController(clock *fakeClock) (*Controller, func() []TaskStatus) {
	e, snapshots := newBackgroundTestEngine(runTaskBodySynchronously)
	e.now = clock.Now
	return &Controller{taskEngine: e, scanProgress: newScanProgressHandles()}, snapshots
}

// startedScanRig 造一条「扫描任务已启动、句柄已登记」的现场，即多数用例的起点。
func startedScanRig(t *testing.T, key, taskType string, target scanTarget) (*Controller, func() []TaskStatus, *fakeClock) {
	t.Helper()
	clock := &fakeClock{now: time.Unix(1700000000, 0)}
	c, snapshots := newScanEventsTestController(clock)
	progress := seedTask(t, c.taskEngine, taskSeed{
		Key: key, Type: taskType, CanCancel: true, CanPause: true,
	})
	t.Cleanup(c.scanProgress.track(target, progress))
	return c, snapshots, clock
}

// TestScanProgressFlowsThroughRegisteredHandle 走完一整条交接：启动任务拿到句柄、按扫描对象
// 登记、由扫描器一侧驱动、断言投递出去的载荷。
//
// 驱动用的是生产的扫描器回调入口本身——那一侧只认得扫描对象，没有任何办法拼出任务键。
func TestScanProgressFlowsThroughRegisteredHandle(t *testing.T) {
	c, snapshots, _ := startedScanRig(t, "scan_series_42", "scan_series", seriesScanTarget(42))

	c.handleScannerProgressEvent(scanner.ScanProgressReport{
		Scope:       "series",
		ID:          42,
		Phase:       "reading_metadata",
		CurrentItem: "/srv/main/Alpha/vol01.cbz",
		Current:     3,
		Total:       9,
		Metrics:     map[string]int64{"processed_archives": 3},
	})

	task := lastPublishedTask(t, snapshots(), "scan_series_42")
	if task.Current != 3 || task.Total != 9 {
		t.Fatalf("**计数推进**为 %d/%d, want 3/9", task.Current, task.Total)
	}
	if task.Phase != "reading_metadata" || task.CurrentItem != "/srv/main/Alpha/vol01.cbz" {
		t.Fatalf("阶段/当前条目没落地：phase=%q item=%q", task.Phase, task.CurrentItem)
	}
	if task.Metrics["processed_archives"] != 3 {
		t.Fatalf("指标没有随帧落地：%v", task.Metrics)
	}
	if task.MessageCode != "task.msg.scan.scanning_item" || task.MessageParams["item"] != "vol01.cbz" {
		t.Fatalf("文案码/占位参数为 %q %v, want scanning_item + 文件名", task.MessageCode, task.MessageParams)
	}
}

// TestScanMetricsFlowThroughRegisteredHandle 守扫描收尾的那份指标报文落进**任务参数**——
// 存储 IO 面板按参数名读它们（见 taskArchiveOpenRate），走错通道会让面板永远是空的。
func TestScanMetricsFlowThroughRegisteredHandle(t *testing.T) {
	c, snapshots, _ := startedScanRig(t, "scan_library_7", "scan_library", libraryScanTarget(7))

	c.handleScannerMetricsEvent(scanner.ScanMetricsReport{
		Scope:          "library",
		ID:             7,
		StorageProfile: "hdd_external",
		OpenedArchives: 5,
		HashedFiles:    2,
		IOWaitMillis:   123,
	})

	task := lastPublishedTask(t, snapshots(), "scan_library_7")
	if task.Params["opened_archives"] != "5" || task.Params["hashed_files"] != "2" || task.Params["io_wait_ms"] != "123" {
		t.Fatalf("扫描指标没有落进任务参数：%v", task.Params)
	}
}

// TestScanWritersAreInertWithoutHandle 守「有没有登记句柄」就是「这次扫描属不属于某个任务」
// 这个判定本身：任务确实在跑、任务键也人尽皆知，但句柄没登记，扫描器的报文就写不进去。
//
// 这一条同时是守护扫描、watcher 触发的扫描与建库后首扫的真实形状——它们不属于任何任务。
func TestScanWritersAreInertWithoutHandle(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1700000000, 0)}
	c, snapshots := newScanEventsTestController(clock)
	seedTask(t, c.taskEngine, taskSeed{Key: "scan_library_7", Type: "scan_library", CanCancel: true, CanPause: true})
	before := publishedCountFor(snapshots(), "scan_library_7")

	c.handleScannerProgressEvent(scanner.ScanProgressReport{
		Scope: "library", ID: 7, Phase: "reading_metadata", Current: 3, Total: 9,
	})
	c.handleScannerMetricsEvent(scanner.ScanMetricsReport{Scope: "library", ID: 7, OpenedArchives: 5})

	if got := publishedCountFor(snapshots(), "scan_library_7") - before; got != 0 {
		t.Fatalf("没登记句柄却投递了 %d 条 —— 写入资格又回到了「谁会拼那个任务键」", got)
	}
	task := lastPublishedTask(t, snapshots(), "scan_library_7")
	if task.Phase != "" || task.Current != 0 || len(task.Params) != 0 {
		t.Fatalf("没登记句柄却改动了任务：phase=%q current=%d params=%v", task.Phase, task.Current, task.Params)
	}
}

// TestScanProgressHandlesSeparateScopes 守资料库与系列共用同一个数字 ID 时互不串写：
// 隔开它们的是 scanTarget 里的**作用域**，光凭 ID 认不出是哪一个。
func TestScanProgressHandlesSeparateScopes(t *testing.T) {
	c, snapshots, _ := startedScanRig(t, "scan_library_7", "scan_library", libraryScanTarget(7))
	seriesProgress := seedTask(t, c.taskEngine, taskSeed{Key: "scan_series_7", Type: "scan_series"})
	t.Cleanup(c.scanProgress.track(seriesScanTarget(7), seriesProgress))

	c.handleScannerProgressEvent(scanner.ScanProgressReport{
		Scope: "series", ID: 7, Phase: "hashing", Current: 4, Total: 4,
	})

	if task := lastPublishedTask(t, snapshots(), "scan_series_7"); task.Phase != "hashing" || task.Current != 4 {
		t.Fatalf("系列扫描没收到自己的进度：phase=%q current=%d", task.Phase, task.Current)
	}
	if task := lastPublishedTask(t, snapshots(), "scan_library_7"); task.Phase != "" || task.Current != 0 {
		t.Fatalf("系列扫描的报文串写进了同号资料库的任务：phase=%q current=%d", task.Phase, task.Current)
	}
}

// TestScanProgressHandleGoesInertAfterRelease 守任务体退出后交回句柄：封面队列这类异步阶段
// 还会继续上报，放行会把一个已经收尾的任务在界面上拽回进行中。
func TestScanProgressHandleGoesInertAfterRelease(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1700000000, 0)}
	c, snapshots := newScanEventsTestController(clock)
	progress := seedTask(t, c.taskEngine, taskSeed{Key: "scan_library_7", Type: "scan_library"})
	release := c.scanProgress.track(libraryScanTarget(7), progress)

	release()
	before := publishedCountFor(snapshots(), "scan_library_7")
	c.handleScannerProgressEvent(scanner.ScanProgressReport{
		Scope: "library", ID: 7, Phase: "queueing_covers", Current: 9, Total: 9,
	})

	if got := publishedCountFor(snapshots(), "scan_library_7") - before; got != 0 {
		t.Fatalf("句柄交回之后仍投递了 %d 条", got)
	}
}

// TestScanProgressHandleReleaseKeepsTheNextScansHandle 守交回的是**自己那份**句柄。
// 无差别删除会在「同一个资料库刚被重新扫描」时把新任务的句柄一起抹掉，
// 那次扫描从此一条进度也报不出来。
func TestScanProgressHandleReleaseKeepsTheNextScansHandle(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1700000000, 0)}
	c, snapshots := newScanEventsTestController(clock)

	first := seedTask(t, c.taskEngine, taskSeed{Key: "scan_library_7", Type: "scan_library"})
	releaseFirst := c.scanProgress.track(libraryScanTarget(7), first)
	settleSeededTask(c.taskEngine, "scan_library_7", nil)

	second := seedTask(t, c.taskEngine, taskSeed{Key: "scan_library_7", Type: "scan_library"})
	t.Cleanup(c.scanProgress.track(libraryScanTarget(7), second))
	releaseFirst()

	c.handleScannerProgressEvent(scanner.ScanProgressReport{
		Scope: "library", ID: 7, Phase: "reading_metadata", Current: 1, Total: 9,
	})

	if task := lastPublishedTask(t, snapshots(), "scan_library_7"); task.Phase != "reading_metadata" || task.Current != 1 {
		t.Fatalf("上一轮的交回抹掉了新任务的句柄：phase=%q current=%d", task.Phase, task.Current)
	}
}

// TestScanFramesArePublishedWholeAndOnce 守一份扫描器报文只投递一条载荷，且那条载荷内部自洽。
// 拆成几次报就会破——投递水位放行其中一条中间态、又吞掉后面补齐的那条。
func TestScanFramesArePublishedWholeAndOnce(t *testing.T) {
	c, snapshots, clock := startedScanRig(t, "scan_library_7", "scan_library", libraryScanTarget(7))

	for i := 1; i <= 4; i++ {
		clock.advance(taskProgressPublishInterval + 50*time.Millisecond)
		before := publishedCountFor(snapshots(), "scan_library_7")
		c.handleScannerProgressEvent(scanner.ScanProgressReport{
			Scope: "library", ID: 7, Phase: "reading_metadata",
			CurrentItem: "/srv/main/vol.cbz",
			Current:     int64(i), Total: 9,
			Metrics: map[string]int64{"processed_archives": int64(i)},
		})

		if got := publishedCountFor(snapshots(), "scan_library_7") - before; got != 1 {
			t.Fatalf("第 %d 份报文投递了 %d 条载荷, want 1", i, got)
		}
		task := lastPublishedTask(t, snapshots(), "scan_library_7")
		if task.Current != i || task.Metrics["processed_archives"] != int64(i) {
			t.Fatalf("第 %d 帧撕开了：计数=%d 指标=%d，不是同一个事件",
				i, task.Current, task.Metrics["processed_archives"])
		}
	}
}
