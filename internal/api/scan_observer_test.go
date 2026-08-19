// 守**资料库扫描**与**系列扫描**任务的进度所有权（模型见 taskScanObserver）。
//
// 这两个**任务键**的拼法在整个包里人尽皆知，「谁能写它们的进度」若不由交出去的那个
// **扫描观察者**决定就等于不受约束；反过来观察者一旦没交出去，整个扫描期间的计数、阶段与
// 当前条目全都不动，任务气泡停在起始文案上。

package api

import (
	"testing"
	"time"

	"manga-manager/internal/scanner"
)

// newScanEventsTestController 手工拼装这条链路唯一需要的组件：任务引擎。
// 引擎仍经它唯一的 seam（newTaskEngine）构造，扫描报文的转译处不碰任何 Controller 字段，
// 因此这里不需要数据库、配置管理器或扫描器。
func newScanEventsTestController(clock *fakeClock) (*Controller, func() []TaskStatus) {
	e, snapshots := newBackgroundTestEngine(runTaskBodySynchronously)
	e.now = clock.Now
	return &Controller{taskEngine: e}, snapshots
}

// startedScanRig 造一条「扫描任务已启动、观察者已交出」的现场，即多数用例的起点。
func startedScanRig(t *testing.T, key, taskType string) (scanner.ScanObserver, func() []TaskStatus, *fakeClock) {
	t.Helper()
	clock := &fakeClock{now: time.Unix(1700000000, 0)}
	c, snapshots := newScanEventsTestController(clock)
	progress := seedTask(t, c.taskEngine, taskSeed{
		Key: key, Type: taskType, CanCancel: true, CanPause: true,
	})
	return newTaskScanObserver(progress), snapshots, clock
}

// TestScanProgressFlowsThroughHandedOverObserver 走完一整条交接：启动任务拿到**任务句柄**、
// 包成观察者交给扫描器、由扫描器一侧驱动、断言投递出去的载荷。
//
// 驱动用的是扫描器真正会调的那两个方法——报文里没有身份，那一侧没有任何办法拼出任务键。
func TestScanProgressFlowsThroughHandedOverObserver(t *testing.T) {
	observer, snapshots, _ := startedScanRig(t, "scan_series_42", "scan_series")

	observer.Progress(scanner.ScanProgressReport{
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

// TestScanMetricsFlowThroughHandedOverObserver 守扫描收尾的那份指标报文落进**任务参数**——
// 存储 IO 面板按参数名读它们（见 taskArchiveOpenRate），走错通道会让面板永远是空的。
func TestScanMetricsFlowThroughHandedOverObserver(t *testing.T) {
	observer, snapshots, _ := startedScanRig(t, "scan_library_7", "scan_library")

	observer.Metrics(scanner.ScanMetricsReport{
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

// TestScanObserverIsNilWithoutHandle 守「有没有交出观察者」就是「这次扫描属不属于某个任务」
// 这个判定本身。守护扫描、watcher 触发的扫描与建库后首扫走的正是这条：拿不到句柄就造不出
// 观察者，扫描器收到的是一个 nil，一条报文也发不出来。
//
// 断言的是 nil 接口值而不是「一个什么都不做的观察者」：带类型的 nil 指针在扫描器那边
// `observer == nil` 判不出来，会在第一条报文上解引用。
func TestScanObserverIsNilWithoutHandle(t *testing.T) {
	if observer := newTaskScanObserver(nil); observer != nil {
		t.Fatalf("没拿到句柄却造出了观察者 %#v —— 写入资格漏出去了", observer)
	}
}

// TestScanFramesArePublishedWholeAndOnce 守一份扫描器报文只投递一条载荷，且那条载荷内部自洽。
// 拆成几次报就会破——投递水位放行其中一条中间态、又吞掉后面补齐的那条。
func TestScanFramesArePublishedWholeAndOnce(t *testing.T) {
	observer, snapshots, clock := startedScanRig(t, "scan_library_7", "scan_library")

	for i := 1; i <= 4; i++ {
		clock.advance(taskProgressPublishInterval + 50*time.Millisecond)
		before := publishedCountFor(snapshots(), "scan_library_7")
		observer.Progress(scanner.ScanProgressReport{
			Phase:       "reading_metadata",
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
