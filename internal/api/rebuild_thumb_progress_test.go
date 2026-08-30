// 守「重建缩略图」任务的进度所有权模型（模型本身见 rebuildThumbAggregator）。
//
// 破了的后果分两头：拿不到**扫描观察者**却仍能写，则「谁能改这个任务」重新变成谁都行；
// 拿到了观察者却写不进去，则重建期间任务气泡上的进度、阶段与当前条目全部不动。

package api

import (
	"fmt"
	"testing"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/scanner"
	"manga-manager/internal/taskrun"
)

// newRebuildThumbTestController 手工拼装出这条链路需要的两个组件：任务引擎与聚合器。
// 引擎仍经它唯一的 seam（newTaskEngine）构造，扫描事件转译处不碰其余任何 Controller 字段，
// 因此这里不需要数据库、配置管理器或扫描器。
func newRebuildThumbTestController(clock *fakeClock) (*Controller, func() []TaskStatus) {
	e, snapshots := newBackgroundTestEngine(runTaskBodySynchronously, nil)
	e.now = clock.Now
	return &Controller{taskEngine: e, rebuildThumbAgg: newRebuildThumbAggregator()}, snapshots
}

// startedRebuildThumbRig 造一条「任务已启动、句柄已交给聚合器」的现场，
// 即多数用例的起点：句柄交接本身只有 TestRebuildThumbWritersAreInertWithoutHandle 不做。
func startedRebuildThumbRig(t *testing.T, totalLibraries int) (*Controller, func() []TaskStatus, *fakeClock) {
	t.Helper()
	clock := &fakeClock{now: time.Unix(1700000000, 0)}
	c, snapshots := newRebuildThumbTestController(clock)
	c.initRebuildThumbAggregator(seedRebuildThumbTask(t, c), totalLibraries)
	t.Cleanup(c.releaseRebuildThumbAggregator)
	return c, snapshots, clock
}

const rebuildThumbTestKey = "rebuild_thumbnails"

func seedRebuildThumbTask(t *testing.T, c *Controller) *taskrun.Handle {
	t.Helper()
	return seedTask(t, c.taskEngine, taskSeed{
		Key: rebuildThumbTestKey, Type: "rebuild_thumbnails",
		StartCode: "task.msg.rebuild_thumbnails.start",
		CanCancel: true, CanPause: true,
	})
}

func rebuildThumbTestLibrary() database.Library {
	return database.Library{ID: 7, Name: "Main", Path: "/srv/main"}
}

// beginTestLibrary 开一个资料库并取回交给这次扫描的观察者；取不到即断言失败，
// 因为后续每一条断言都建立在「确实拿到了写入资格」之上。
func beginTestLibrary(t *testing.T, c *Controller, lib database.Library, total int) scanner.ScanObserver {
	t.Helper()
	observer := c.beginRebuildThumbLibrary(lib, total)
	if observer == nil {
		t.Fatal("没拿到扫描观察者 —— 这次扫描的报文无处可报")
	}
	return observer
}

// TestRebuildThumbProgressFlowsThroughHandedOverHandle 走完一整条交接：启动任务拿到句柄、
// 交给聚合器、由扫描器一侧驱动、断言投递出去的载荷。
//
// 驱动用的是生产的扫描器回调入口本身——那一侧只认得聚合器，没有任何办法拼出任务键。
func TestRebuildThumbProgressFlowsThroughHandedOverHandle(t *testing.T) {
	c, snapshots, _ := startedRebuildThumbRig(t, 2)
	observer := beginTestLibrary(t, c, rebuildThumbTestLibrary(), 2)
	observer.Progress(scanner.ScanProgressReport{
		Phase:       "reading_metadata",
		CurrentItem: "/srv/main/vol01.cbz",
		Metrics: map[string]int64{
			"discovered_archives": 10,
			"processed_archives":  4,
			"queued_covers":       6,
			"generated_covers":    2,
		},
	})

	task := lastPublishedTask(t, snapshots(), rebuildThumbTestKey)
	// 两阶段进度：归档 4/10 加封面 2/6。
	if task.Current != 6 || task.Total != 16 {
		t.Fatalf("**计数推进**为 %d/%d, want 6/16", task.Current, task.Total)
	}
	if task.Phase != "reading_metadata" {
		t.Fatalf("**阶段**为 %q, want reading_metadata", task.Phase)
	}
	if task.CurrentItem != "/srv/main/vol01.cbz" {
		t.Fatalf("当前条目为 %q, want /srv/main/vol01.cbz", task.CurrentItem)
	}
	if task.Metrics["discovered_archives"] != 10 || task.Metrics["generated_covers"] != 2 {
		t.Fatalf("指标没有随快照落地：%v", task.Metrics)
	}
	if task.Labels["current_library"] != "Main" {
		t.Fatalf("当前资料库标签为 %q, want Main", task.Labels["current_library"])
	}
	if task.MessageCode != "task.msg.rebuild_thumbnails.rebuilding_item_in_library" {
		t.Fatalf("文案码为 %q, want ...rebuilding_item_in_library", task.MessageCode)
	}
}

// TestRebuildThumbMetricsReportAccumulatesThroughHandle 守跨库累计：
// 每份报文只带本库这一段，全局总量由引擎累加。
func TestRebuildThumbMetricsReportAccumulatesThroughHandle(t *testing.T) {
	c, snapshots, clock := startedRebuildThumbRig(t, 2)

	for i, report := range []scanner.ScanMetricsReport{
		{OpenedArchives: 3, ThumbnailWriteMillis: 40, DurationMillis: 60000},
		{OpenedArchives: 2, ThumbnailWriteMillis: 10, DurationMillis: 30000},
	} {
		lib := database.Library{ID: int64(7 + i), Name: fmt.Sprintf("Lib%d", i), Path: fmt.Sprintf("/srv/lib%d", i)}
		observer := beginTestLibrary(t, c, lib, 2)
		// 两份报文的展示态一模一样，不推开时钟的话第二帧会被节流水位吞掉，
		// 断言就会对着第一帧下结论。
		clock.advance(taskProgressPublishInterval * 2)
		observer.Metrics(report)
	}

	task := lastPublishedTask(t, snapshots(), rebuildThumbTestKey)
	if task.Metrics["opened_archives"] != 5 || task.Metrics["thumbnail_write_ms"] != 50 {
		t.Fatalf("跨库指标没有累加：%v", task.Metrics)
	}
	// duration_ms 不在聚合器的 baseline 里，只有累加这条通道会写它；存储 IO 面板按参数名读它。
	if task.Params["duration_ms"] != "90000" || task.Params["opened_archives"] != "5" {
		t.Fatalf("累计值没有落进任务参数：%v", task.Params)
	}
}

// TestRebuildThumbCountsLibrariesByFixation 守「已完成几个库」只有一个来源：已定版指标的库数。
//
// 它此前来自库切换的通知，而一个库定版发生在切到下一个库**之前**，于是完成第一个库时
// 界面上写的是「已完成 0 / 2」。同一个数若再有第二个来源，这个差一就会回来。
func TestRebuildThumbCountsLibrariesByFixation(t *testing.T) {
	c, snapshots, _ := startedRebuildThumbRig(t, 2)

	observer := beginTestLibrary(t, c, rebuildThumbTestLibrary(), 2)
	if task := lastPublishedTask(t, snapshots(), rebuildThumbTestKey); task.MessageParams["done"] != "1" {
		t.Fatalf("开工第一个库时报的是第 %q 个, want 1", task.MessageParams["done"])
	}

	observer.Metrics(scanner.ScanMetricsReport{OpenedArchives: 3, DurationMillis: 60000})

	task := lastPublishedTask(t, snapshots(), rebuildThumbTestKey)
	if task.MessageCode != "task.msg.rebuild_thumbnails.libraries_completed" {
		t.Fatalf("定版后的文案码为 %q, want ...libraries_completed", task.MessageCode)
	}
	if task.MessageParams["done"] != "1" || task.MessageParams["total"] != "2" {
		t.Fatalf("第一个库扫完时报的是 %q / %q, want 1 / 2", task.MessageParams["done"], task.MessageParams["total"])
	}
}

// TestRebuildThumbWritersAreInertWithoutHandle 守「有没有拿到句柄」就是「重建是否在进行中」
// 这个判定本身：任务确实在跑、任务键也人尽皆知，但句柄没交出去，外部写入点就都写不进去。
func TestRebuildThumbWritersAreInertWithoutHandle(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1700000000, 0)}
	c, snapshots := newRebuildThumbTestController(clock)

	seedRebuildThumbTask(t, c)
	lib := rebuildThumbTestLibrary()
	before := publishedCountFor(snapshots(), rebuildThumbTestKey)

	// 每一处外部写入点都试一遍。观察者根本造不出来——写入资格在结构上就发不出去。
	if observer := c.beginRebuildThumbLibrary(lib, 2); observer != nil {
		t.Fatal("没拿到句柄却造出了扫描观察者 —— 写入资格漏出去了")
	}
	c.refreshRebuildThumbTaskFromAggregator(lib)
	c.refreshRebuildThumbTaskMessage("task.msg.rebuild_thumbnails.waiting_cover_queue", nil, "queueing_covers")

	if got := publishedCountFor(snapshots(), rebuildThumbTestKey) - before; got != 0 {
		t.Fatalf("没拿到句柄却投递了 %d 条 —— 写入资格又回到了「谁会拼那个任务键」", got)
	}
	task := lastPublishedTask(t, snapshots(), rebuildThumbTestKey)
	if task.Phase != "" || task.Current != 0 {
		t.Fatalf("没拿到句柄却改动了任务：phase=%q current=%d", task.Phase, task.Current)
	}
}

// TestRebuildThumbFramesArePublishedWholeAndOnce 守一份扫描器报文只投递一条载荷，
// 且那条载荷内部自洽：指标、两阶段计数与当前条目都来自同一个事件。
//
// 拆成几次报就会破——投递水位放行其中一条中间态、又吞掉后面补齐的那条，
// 于是同一份载荷里指标已经走到第 N 条、进度条还停在第 N-1 条。
func TestRebuildThumbFramesArePublishedWholeAndOnce(t *testing.T) {
	c, snapshots, clock := startedRebuildThumbRig(t, 1)
	observer := beginTestLibrary(t, c, rebuildThumbTestLibrary(), 1)

	for i := 1; i <= 4; i++ {
		// 推过节流窗口，让每份报文都不是「被水位吞掉」那种情况。
		clock.advance(taskProgressPublishInterval + 50*time.Millisecond)
		item := fmt.Sprintf("/srv/main/vol%02d.cbz", i)
		before := publishedCountFor(snapshots(), rebuildThumbTestKey)
		observer.Progress(scanner.ScanProgressReport{
			Phase: "reading_metadata", CurrentItem: item,
			Metrics: map[string]int64{"discovered_archives": 10, "processed_archives": int64(i)},
		})

		if got := publishedCountFor(snapshots(), rebuildThumbTestKey) - before; got != 1 {
			t.Fatalf("第 %d 份报文投递了 %d 条载荷, want 1", i, got)
		}
		task := lastPublishedTask(t, snapshots(), rebuildThumbTestKey)
		if task.Current != i || task.Total != 10 {
			t.Fatalf("第 %d 帧的计数为 %d/%d, want %d/10", i, task.Current, task.Total, i)
		}
		if task.CurrentItem != item || task.Metrics["processed_archives"] != int64(i) {
			t.Fatalf("第 %d 帧撕开了：条目=%q 指标=%d，与计数 %d 不是同一个事件",
				i, task.CurrentItem, task.Metrics["processed_archives"], task.Current)
		}
	}
}

// TestRebuildThumbPhaseTransitionsSurviveThrottle 守重建期间的**阶段**跃迁不被节流水位吞掉。
// 时钟一动不动，三帧全在同一个节流窗口内，靠的正是水位除时间外还记着已发布的展示态。
func TestRebuildThumbPhaseTransitionsSurviveThrottle(t *testing.T) {
	c, snapshots, _ := startedRebuildThumbRig(t, 1)
	observer := beginTestLibrary(t, c, rebuildThumbTestLibrary(), 1)

	drive := func(phase string, processed int64) {
		observer.Progress(scanner.ScanProgressReport{
			Phase:       phase,
			CurrentItem: "/srv/main/vol01.cbz",
			Metrics:     map[string]int64{"discovered_archives": 10, "processed_archives": processed},
		})
	}

	// 先把水位顶到「刚刚发布过」的状态，后面三帧才全都落在窗口内。
	drive("reading_metadata", 1)
	before := publishedCountFor(snapshots(), rebuildThumbTestKey)

	for i, phase := range []string{"hashing", "queueing_covers", "reading_metadata"} {
		drive(phase, int64(i+2))
	}

	if got := publishedCountFor(snapshots(), rebuildThumbTestKey) - before; got != 3 {
		t.Fatalf("三次**阶段**跃迁只投递了 %d 条 —— 任务气泡会停在过期的阶段名上", got)
	}
}

// TestRebuildThumbCountsHoldStillBeforeAnyDenominator 守两阶段进度都还没有分母时不乱报总数：
// 「别动总数」由「干脆不报**计数推进**」表达，写下一个 0 会把进度条按 0/0 重置。
func TestRebuildThumbCountsHoldStillBeforeAnyDenominator(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1700000000, 0)}
	c, snapshots := newRebuildThumbTestController(clock)

	progress := seedTask(t, c.taskEngine, taskSeed{
		Key: rebuildThumbTestKey, Type: "rebuild_thumbnails", Total: 120,
		CanCancel: true, CanPause: true,
	})
	c.initRebuildThumbAggregator(progress, 1)
	t.Cleanup(c.releaseRebuildThumbAggregator)

	c.refreshRebuildThumbTaskMessage("task.msg.rebuild_thumbnails.waiting_cover_queue", nil, "queueing_covers")

	task := lastPublishedTask(t, snapshots(), rebuildThumbTestKey)
	if task.Total != 120 || task.Current != 0 {
		t.Fatalf("无分母时的一帧改动了计数：current=%d total=%d, want 0/120", task.Current, task.Total)
	}
	if task.Phase != "queueing_covers" {
		t.Fatalf("**阶段**没有播报出去：%q", task.Phase)
	}
}
