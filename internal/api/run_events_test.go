// 守运行详情那两个今天答不出的问题：「慢在哪一段」由阶段时间线答，「哪些文件失败了、为什么」
// 由失败明细答。外加端点契约（404 / 400）与「原始日志按这一次运行过滤」。

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/task"
)

// runEventsOf 走端点取一条运行的事件流与阶段时间线。
func runEventsOf(t *testing.T, c *Controller, runID int64) RunEventsResponse {
	t.Helper()
	req := requestWithRouteParam(http.MethodGet, "/api/system/runs/1/events", nil, "runID", strconv.FormatInt(runID, 10))
	rec := httptest.NewRecorder()
	c.getRunEvents(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("取事件流返回 %d，想要 200（body=%s）", rec.Code, rec.Body.String())
	}
	var payload RunEventsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("解事件流失败: %v（raw=%s）", err, rec.Body.String())
	}
	return payload
}

// failedItems 挑出事件流里的失败明细。
func failedItems(events []RunEvent) []RunEvent {
	picked := make([]RunEvent, 0, len(events))
	for _, event := range events {
		if event.Kind == string(task.EventItem) {
			picked = append(picked, event)
		}
	}
	return picked
}

// TestScanWithABrokenArchiveListsTheFileAndWhy 走一次真扫描：库里放一个坏掉的归档，
// 扫完之后详情页答得出**是哪个文件、为什么**。
//
// 这一条是本票的正题。两头各有用例（扫描器数得出 failed_archives、事件落得进库），
// 而中间那根线接没接上只有走一次真扫描才答得出——漏接的表现是指标里写着「失败 1」，
// 详情页仍然只有那个数字。
func TestScanWithABrokenArchiveListsTheFileAndWhy(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	libraryPath := filepath.Join(rootDir, "Broken Library")
	seriesPath := filepath.Join(libraryPath, "Series Alpha")
	if err := os.MkdirAll(seriesPath, 0o755); err != nil {
		t.Fatalf("建系列目录失败: %v", err)
	}
	if err := writeTestCBZ(filepath.Join(seriesPath, "Alpha 01.cbz"), map[string][]byte{"001.png": png1x1}); err != nil {
		t.Fatalf("写测试归档失败: %v", err)
	}
	// 一个后缀对得上、内容不是 zip 的文件：扫描器打开它时失败，正是用户嘴里那「7 个文件之一」。
	brokenPath := filepath.Join(seriesPath, "Alpha 02.cbz")
	if err := os.WriteFile(brokenPath, []byte("this is not a zip archive"), 0o644); err != nil {
		t.Fatalf("写坏归档失败: %v", err)
	}
	lib, err := store.CreateLibrary(context.Background(), database.CreateLibraryParams{
		Name: "Broken Library", Path: libraryPath, ScanMode: "manual", ScanInterval: 60,
		ScanFormats: config.DefaultScanFormatsCSV,
	})
	if err != nil {
		t.Fatalf("建资料库失败: %v", err)
	}

	// 任务体照常跑在后台再等它收尾，而不是换成同步执行版：这次扫描途中会自己排出一批封面，
	// 而那一批要另起一条运行来跑——同步版会让扫描在等封面、封面在等扫描收工。
	launched, err := controller.startLibraryScanRun(lib, true, task.TriggerManual)
	if err != nil {
		t.Fatalf("发起扫描失败: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := controller.taskEngine.awaitRunOutcome(ctx, launched.Run.ID); err != nil {
		t.Fatalf("等扫描收尾失败: %v", err)
	}

	run := currentTask(t, controller.taskEngine, "scan_library_"+strconv.FormatInt(lib.ID, 10))
	if run.Metrics["failed_archives"] < 1 {
		t.Fatalf("扫描没有数出失败归档：%v", run.Metrics)
	}

	failures := failedItems(runEventsOf(t, controller, run.RunID).Events)
	if len(failures) == 0 {
		t.Fatal("指标里写着有失败归档，事件流里却一条明细也没有——那个数字仍然无处可查")
	}
	found := false
	for _, failure := range failures {
		if failure.Item == brokenPath {
			found = true
			if strings.TrimSpace(failure.Reason) == "" {
				t.Errorf("失败明细 %q 没带原因：只有文件名答不出「为什么」", failure.Item)
			}
		}
	}
	if !found {
		t.Fatalf("失败明细里没有那个坏归档 %q：%+v", brokenPath, failures)
	}
}

// TestRunEventsGiveAPhaseTimeline 守「慢在哪一段」：详情页拿得到每一段的耗时，
// 而那是相邻两条阶段事件相减出来的，不是另存的一列。
func TestRunEventsGiveAPhaseTimeline(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1700000000, 0)}
	engine, _ := newBackgroundTestEngine(t, runTaskBodySynchronously, nil)
	engine.now = clock.Now
	controller := &Controller{taskEngine: engine}

	handle := seedTask(t, engine, taskSeed{
		Key: "scan_library_7", Identity: libraryTask("scan_library", 7, variantSole), CanPause: true, CanCancel: true,
	})
	runID := currentTask(t, engine, "scan_library_7").RunID

	handle.Phase("discovering", "", nil)
	clock.advance(90 * time.Second)
	handle.Phase("reading_metadata", "", nil)
	clock.advance(30 * time.Minute)
	handle.Phase("ingesting", "", nil)
	clock.advance(10 * time.Second)

	phases := runEventsOf(t, controller, runID).Phases
	if len(phases) != 3 {
		t.Fatalf("时间线上有 %d 段，想要 3 段：%+v", len(phases), phases)
	}
	want := []struct {
		phase    string
		duration int64
	}{
		{"discovering", (90 * time.Second).Milliseconds()},
		{"reading_metadata", (30 * time.Minute).Milliseconds()},
		{"ingesting", (10 * time.Second).Milliseconds()},
	}
	for i, expected := range want {
		if phases[i].Phase != expected.phase || phases[i].DurationMillis != expected.duration {
			t.Errorf("第 %d 段是 %q/%dms，想要 %q/%dms", i+1, phases[i].Phase, phases[i].DurationMillis,
				expected.phase, expected.duration)
		}
	}
	// 最后一段还在跑：它的耗时量到此刻为止，界面据此把它画成「进行中」而不是一个定死的数。
	if !phases[2].Current {
		t.Error("运行还在跑，最后一段却没有标成进行中")
	}
	if phases[0].Current {
		t.Error("已经切走的那一段被标成了进行中")
	}
}

// TestPhaseTimelineNeverGoesNegative 守段长夹在零以上：两个写入方与一次系统时钟回拨都能让
// 相邻两条的时刻倒过来，而一段负的耗时在界面上没有任何念法。
func TestPhaseTimelineNeverGoesNegative(t *testing.T) {
	at := time.Unix(1700000000, 0)
	spans := phaseTimeline([]task.Event{
		{At: at, Kind: task.EventPhase, Phase: "first"},
		{At: at.Add(-time.Minute), Kind: task.EventPhase, Phase: "second"},
	}, at.Add(-2*time.Minute), false)

	if len(spans) != 2 {
		t.Fatalf("时间线上有 %d 段，想要 2 段", len(spans))
	}
	for _, span := range spans {
		if span.DurationMillis < 0 {
			t.Errorf("%q 这一段算出 %dms", span.Phase, span.DurationMillis)
		}
	}
}

// TestRunEventsEndpointContract 钉端点的两条错误分支：运行不存在是 404，路径参数不是运行 id 是 400。
func TestRunEventsEndpointContract(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	t.Run("运行不存在", func(t *testing.T) {
		req := requestWithRouteParam(http.MethodGet, "/api/system/runs/4242/events", nil, "runID", "4242")
		rec := httptest.NewRecorder()
		controller.getRunEvents(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404（body=%s）", rec.Code, rec.Body.String())
		}
	})

	t.Run("路径参数不是运行 id", func(t *testing.T) {
		req := requestWithRouteParam(http.MethodGet, "/api/system/runs//events", nil, "runID", "")
		rec := httptest.NewRecorder()
		controller.getRunEvents(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400（body=%s）", rec.Code, rec.Body.String())
		}
	})
}

// TestOmittedFailuresAnswerWhileTheRunIsStillGoing 守用户最想问「还有多少条」的那一刻答得出：
// 跑着的运行溢出计数就发得出去，不必等它收尾（用户故事 11）。
//
// 收尾之后它换了来源（内存里那份被结算成一条告警事件），而两个来源相加不会把同一批数两遍。
func TestOmittedFailuresAnswerWhileTheRunIsStillGoing(t *testing.T) {
	const overflow = 9
	engine, _ := newBackgroundTestEngine(t, runTaskBodySynchronously, nil)
	controller := &Controller{taskEngine: engine}
	handle := seedTask(t, engine, taskSeed{
		Key: "scan_library_7", Identity: libraryTask("scan_library", 7, variantSole), CanPause: true, CanCancel: true,
	})
	runID := currentTask(t, engine, "scan_library_7").RunID

	for i := 0; i < task.MaxItemFailureEvents+overflow; i++ {
		handle.ItemFailed("/lib/vol"+strconv.Itoa(i)+".cbz", "corrupted")
	}

	running := runEventsOf(t, controller, runID)
	if len(failedItems(running.Events)) != task.MaxItemFailureEvents {
		t.Fatalf("列出了 %d 条失败明细，想要恰好 %d 条", len(failedItems(running.Events)), task.MaxItemFailureEvents)
	}
	if running.OmittedFailures != overflow {
		t.Fatalf("跑着的运行报出 %d 条未列出，想要 %d —— 那正是用户此刻要问的数",
			running.OmittedFailures, overflow)
	}

	seededRunFor(t, engine, "scan_library_7").settle(nil)
	if got := runEventsOf(t, controller, runID).OmittedFailures; got != overflow {
		t.Fatalf("收尾之后报出 %d 条未列出，想要仍是 %d —— 换了来源，不该换答案", got, overflow)
	}
}

// TestTruncatedEventsDoNotInventAPhase 守截断时时间线不说谎。
//
// 事件撞上取回上限时被切掉的那一段里可能还有几次阶段切换。把最后一条**列出来的**阶段一路拉到
// 运行结束时刻、再标成「当前」，界面上就会长出一段几小时的、根本没发生过的阶段。
func TestTruncatedEventsDoNotInventAPhase(t *testing.T) {
	at := time.Unix(1700000000, 0)
	events := []task.Event{
		{At: at, Kind: task.EventPhase, Phase: "discovering"},
		{At: at.Add(time.Minute), Kind: task.EventItem, Item: "/lib/vol01.cbz"},
	}
	// 截断之后收口取的是**已知的最后一条事件**，而不是「此刻」——两者差着几小时。
	spans := phaseTimeline(events, events[len(events)-1].At, false)
	if len(spans) != 1 {
		t.Fatalf("时间线上有 %d 段，想要 1 段", len(spans))
	}
	if spans[0].Current {
		t.Error("被截断的时间线把最后一段标成了当前——切掉的那些里可能还有别的阶段")
	}
	if want := time.Minute.Milliseconds(); spans[0].DurationMillis != want {
		t.Errorf("最后一段算出 %dms，想要量到已知的最后一条事件为止的 %dms", spans[0].DurationMillis, want)
	}
}

// TestRunEventsAreNotPushed 守规格边界：事件不进推送通道，详情页按需拉。
//
// 推送通道上的载荷只有全量运行快照与**实况汇总**两种；事件挤进去等于让每个开着页面的浏览器
// 替所有人收下整条事件流，而那正是这份契约里没有它的原因。
func TestRunEventsAreNotPushed(t *testing.T) {
	engine, frames := newBackgroundTestEngine(t, runTaskBodySynchronously, nil)
	handle := seedTask(t, engine, taskSeed{
		Key: "scan_library_7", Identity: libraryTask("scan_library", 7, variantSole), CanPause: true, CanCancel: true,
	})
	before := len(frames())

	for i := 0; i < 20; i++ {
		handle.ItemFailed("/lib/vol"+strconv.Itoa(i)+".cbz", "corrupted")
	}

	if after := len(frames()); after != before {
		t.Fatalf("落了 20 条失败明细，推送通道上多出 %d 帧", after-before)
	}
}
