// 守自动发起的工作也建**运行**：守护扫描、监听器派生的扫描与清理、建库后的首扫各带自己的
// **发起方**，与手动发起的一样可暂停可取消；毫秒级的内存活仍然不建运行。
//
// 这里守的是**接线**：发起方接到了对外契约上、撞车走的是队列而不是丢弃、监听器那条出口
// 真的等到了扫描跑完。领域侧的排队与合并规则由 internal/task 的用例守。

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/scanner"
	"manga-manager/internal/task"
)

// blockingScanStore 让一次扫描停在「加载已入库文件快照」那一步，直到用例放行或 ctx 被取消。
//
// 挑这一步与 scanFailingStore 同理：它在扫描器内部，因此停在这里的运行是一条**真的在跑**的
// 运行——暂停、取消与「监听器要等它」这几条断言都需要它真的还没收尾。
type blockingScanStore struct {
	database.Store
	release <-chan struct{}
}

func (s blockingScanStore) ListBooksByLibrary(ctx context.Context, libraryID int64) ([]database.ListBooksByLibraryRow, error) {
	select {
	case <-s.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return s.Store.ListBooksByLibrary(ctx, libraryID)
}

// newAutomaticScanRig 造一台扫描会停在可控点上的 Controller，并建好一个 interval 模式的资料库。
// 返回的 release 放行那次扫描；用例结束时自动放行一次，卡住的运行不会拖着停机。
func newAutomaticScanRig(t *testing.T) (*Controller, database.Library, func()) {
	t.Helper()

	controller, store, _, _ := newTestController(t)
	release := make(chan struct{})
	var releaseOnce bool
	releaseFn := func() {
		if !releaseOnce {
			releaseOnce = true
			close(release)
		}
	}
	t.Cleanup(releaseFn)
	controller.scanner = scanner.NewScanner(blockingScanStore{Store: store, release: release}, controller.config)

	libPath := filepath.Join(t.TempDir(), "library")
	if err := os.MkdirAll(libPath, 0o755); err != nil {
		t.Fatalf("建库目录失败: %v", err)
	}
	lib, err := store.CreateLibrary(context.Background(), database.CreateLibraryParams{
		Name:         "Main",
		Path:         libPath,
		ScanMode:     "interval",
		ScanInterval: 60,
		ScanFormats:  config.DefaultScanFormatsCSV,
	})
	if err != nil {
		t.Fatalf("建资料库失败: %v", err)
	}
	return controller, lib, releaseFn
}

func scanKeyFor(lib database.Library) string {
	return "scan_library_" + strconv.FormatInt(lib.ID, 10)
}

// TestScheduledScanCreatesAVisibleRun 守半夜硬盘在转时任务中心里有东西：守护扫描建一条运行、
// 标出**发起方**，并且停得下来。交出 nil **扫描观察者**的守护扫描不建任何行，
// 用户听见盘响却什么也看不见、也没有任何东西可以暂停或取消。
func TestScheduledScanCreatesAVisibleRun(t *testing.T) {
	controller, lib, _ := newAutomaticScanRig(t)

	controller.dispatchScheduledScans(context.Background(), time.Now(), map[int64]time.Time{})

	run := currentTask(t, controller.taskEngine, scanKeyFor(lib))
	if run.Trigger != string(task.TriggerScheduled) {
		t.Fatalf("守护扫描的发起方为 %q, want scheduled —— 用户答不出「这活是谁叫来的」", run.Trigger)
	}
	if run.Status != "running" {
		t.Fatalf("守护扫描的运行状态为 %q, want running", run.Status)
	}
	if !run.CanPause || !run.CanCancel {
		t.Fatalf("守护扫描停不下来：pause=%v cancel=%v", run.CanPause, run.CanCancel)
	}
}

// TestScheduledScanIsPausableAndCancellable 守自动发起的运行与手动发起的没有区别：
// 用户要用机器时按得停它（用户故事 3）。
func TestScheduledScanIsPausableAndCancellable(t *testing.T) {
	controller, lib, _ := newAutomaticScanRig(t)
	controller.dispatchScheduledScans(context.Background(), time.Now(), map[int64]time.Time{})

	key := scanKeyFor(lib)
	runID := currentTask(t, controller.taskEngine, key).RunID
	if err := controller.taskEngine.pauseRun(runID); err != nil {
		t.Fatalf("暂停守护扫描失败: %v", err)
	}
	if got := currentTask(t, controller.taskEngine, key).Status; got != "paused" {
		t.Fatalf("暂停之后状态为 %q, want paused", got)
	}
	if err := controller.taskEngine.resumeRun(runID); err != nil {
		t.Fatalf("恢复守护扫描失败: %v", err)
	}
	if err := controller.taskEngine.cancelRun(runID); err != nil {
		t.Fatalf("取消守护扫描失败: %v", err)
	}
}

// TestScheduledScanQueuesBehindAManualScan 守撞车不靠丢弃：守护扫描撞上手动扫描时排在队里，
// 界面上看得见。被静默跳过、等下一个 tick 的那种处置，用户永远不知道那次自动扫描没跑。
func TestScheduledScanQueuesBehindAManualScan(t *testing.T) {
	controller, lib, _ := newAutomaticScanRig(t)
	key := scanKeyFor(lib)

	// 手动扫描先占住这个身份。
	seedTask(t, controller.taskEngine, taskSeed{
		Key: key, Identity: libraryTask("scan_library", lib.ID, variantSole),
		Total: 100, CanCancel: true, CanPause: true,
	})

	controller.dispatchScheduledScans(context.Background(), time.Now(), map[int64]time.Time{})

	queued := currentTask(t, controller.taskEngine, key)
	if queued.Status != "queued" {
		t.Fatalf("撞上手动扫描的守护扫描状态为 %q, want queued —— 它又被丢掉了", queued.Status)
	}
	if queued.Trigger != string(task.TriggerScheduled) {
		t.Fatalf("排队那条的发起方为 %q, want scheduled", queued.Trigger)
	}

	// 再来一轮：合并进那条排队的，而不是堆出第二条一模一样的运行。
	controller.dispatchScheduledScans(context.Background(), time.Now().Add(2*time.Hour), map[int64]time.Time{})
	if got := currentTask(t, controller.taskEngine, key); got.RunID != queued.RunID || got.CoalescedCount != 1 {
		t.Fatalf("第二轮守护扫描没有并进排队那条：run_id=%d(want %d) coalesced=%d(want 1)",
			got.RunID, queued.RunID, got.CoalescedCount)
	}
}

// TestScheduledScanSkipsLibrariesBeforeTheirInterval 守节拍没变：每分钟看一眼，
// 到没到间隔由每个库自己说了算。
func TestScheduledScanSkipsLibrariesBeforeTheirInterval(t *testing.T) {
	controller, lib, _ := newAutomaticScanRig(t)

	now := time.Now()
	lastScan := map[int64]time.Time{lib.ID: now.Add(-30 * time.Minute)} // 间隔 60 分钟
	controller.dispatchScheduledScans(context.Background(), now, lastScan)

	if taskExists(t, controller.taskEngine, scanKeyFor(lib)) {
		t.Fatal("还没到间隔就发起了守护扫描")
	}
}

// TestWatchedScanWaitsForTheScanToFinish 守监听器那条出口**等到扫描跑完才返回**。
//
// 它不是顺手为之：监听器按扫描的成败决定敢不敢接着清理，而改名重连写在扫描末尾。
// 发起完就返回的话，那条清理会在扫描还排着队时跑起来，把待重连的行当成「文件已消失」删掉——
// 阅读进度、书签、合集归属、阅读清单条目随 CASCADE 一起没。
func TestWatchedScanWaitsForTheScanToFinish(t *testing.T) {
	controller, lib, release := newAutomaticScanRig(t)

	returned := make(chan error, 1)
	go func() { returned <- controller.runWatchedLibraryScan(context.Background(), lib.ID) }()

	// 扫描卡在可控点上，出口因此不该返回。
	select {
	case err := <-returned:
		t.Fatalf("扫描还没跑完，监听器那条出口就返回了（err=%v）", err)
	case <-time.After(200 * time.Millisecond):
	}

	run := currentTask(t, controller.taskEngine, scanKeyFor(lib))
	if run.Trigger != string(task.TriggerWatch) {
		t.Fatalf("监听扫描的发起方为 %q, want watch", run.Trigger)
	}

	release()
	select {
	case err := <-returned:
		if err != nil {
			t.Fatalf("放行之后监听器那条出口返回了错误: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("扫描跑完了，监听器那条出口还没返回")
	}
}

// TestWatchedScanReportsCoalescedInsteadOfWaitingForever 守被**合并**掉的那次发起不等：
// 它的任务体不会执行（排在前面那条跑的是同一件事），等在它身上就是把监听器的 goroutine 吊死。
func TestWatchedScanReportsCoalescedInsteadOfWaitingForever(t *testing.T) {
	controller, lib, _ := newAutomaticScanRig(t)
	key := scanKeyFor(lib)

	seed := taskSeed{Key: key, Identity: libraryTask("scan_library", lib.ID, variantSole), Total: 100}
	seedTask(t, controller.taskEngine, seed)
	if _, err := trySeedTask(t, controller.taskEngine, seed); err != errSeededRunQueued {
		t.Fatalf("第二次播种返回 %v, want 停在排队中", err)
	}

	returned := make(chan error, 1)
	go func() { returned <- controller.runWatchedLibraryScan(context.Background(), lib.ID) }()
	select {
	case err := <-returned:
		if err != scanner.ErrScanCoalesced {
			t.Fatalf("被合并掉的那次发起返回 %v, want ErrScanCoalesced", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("被合并掉的那次发起把监听器吊死了")
	}
}

// TestWatchedScanStopsWaitingWhenTheQueuedRunIsCancelled 守等待的判据是**运行收尾了没有**，
// 不是「任务体返回了没有」。
//
// 排队中的运行被取消时任务体根本不会执行——而自动发起的运行可取消正是本票的一条验收，
// 用户在实况区按下那张排队卡片上的取消就是这一种。只等任务体的话，监听器那条 goroutine
// 会一直挂到停机，`scansInFlight` 也一直不归零：那个库的幽灵记录从此再没人清。
func TestWatchedScanStopsWaitingWhenTheQueuedRunIsCancelled(t *testing.T) {
	controller, lib, _ := newAutomaticScanRig(t)
	key := scanKeyFor(lib)

	// 另一条运行占住这个身份，监听器发起的那次因此进排队中。
	seedTask(t, controller.taskEngine, taskSeed{
		Key: key, Identity: libraryTask("scan_library", lib.ID, variantSole), Total: 100,
	})

	returned := make(chan error, 1)
	go func() { returned <- controller.runWatchedLibraryScan(context.Background(), lib.ID) }()

	// 等那条排队运行落地（发起是同步的，但它跑在另一条 goroutine 上）。
	var queuedID int64
	for i := 0; i < 200 && queuedID == 0; i++ {
		if run := currentTask(t, controller.taskEngine, key); run.Status == "queued" {
			queuedID = run.RunID
		} else {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if queuedID == 0 {
		t.Fatal("监听器发起的那次扫描没有进排队中")
	}

	if err := controller.taskEngine.cancelRun(queuedID); err != nil {
		t.Fatalf("取消排队中的监听扫描失败: %v", err)
	}
	select {
	case err := <-returned:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("排队中被取消之后监听器那条出口返回 %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("排队中的运行被取消了，监听器那条出口还挂着 —— 它会一直等到停机")
	}
}

// TestWatchedCleanupCreatesARunWithTheWatchTrigger 守监听器派生的**清理**同样建运行。
func TestWatchedCleanupCreatesARunWithTheWatchTrigger(t *testing.T) {
	controller, lib, _ := newAutomaticScanRig(t)

	if err := controller.runWatchedLibraryCleanup(context.Background(), lib.ID); err != nil {
		t.Fatalf("监听器派生的清理失败: %v", err)
	}

	run := currentTask(t, controller.taskEngine, "cleanup_library_"+strconv.FormatInt(lib.ID, 10))
	if run.Trigger != string(task.TriggerWatch) {
		t.Fatalf("监听清理的发起方为 %q, want watch", run.Trigger)
	}
	if run.Status != "completed" {
		t.Fatalf("监听清理的终态为 %q, want completed —— 出口返回时它本该已经跑完", run.Status)
	}
}

// TestInitialLibraryScanIsVisible 守建库后的首扫也在任务中心里（用户故事 4）。
// 它记**手动**：那是用户刚按下「添加资料库」的直接后果，他正等着看它扫到哪了。
func TestInitialLibraryScanIsVisible(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	libPath := filepath.Join(t.TempDir(), "library")
	if err := os.MkdirAll(libPath, 0o755); err != nil {
		t.Fatalf("建库目录失败: %v", err)
	}

	payload, err := json.Marshal(map[string]string{"name": "Main", "path": libPath})
	if err != nil {
		t.Fatalf("序列化建库请求失败: %v", err)
	}
	rec := httptest.NewRecorder()
	controller.createLibrary(rec, httptest.NewRequest(http.MethodPost, "/api/libraries", bytes.NewReader(payload)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("建库返回 %d: %s", rec.Code, rec.Body.String())
	}

	var created database.Library
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("解析建库响应失败: %v", err)
	}
	run := currentTask(t, controller.taskEngine, scanKeyFor(created))
	if run.Trigger != string(task.TriggerManual) {
		t.Fatalf("建库首扫的发起方为 %q, want manual", run.Trigger)
	}
}

// TestInMemoryWorkCreatesNoRun 守边界的另一头：毫秒级的内存活不建运行。
//
// 作品群重建与仪表板统计预热不动磁盘，为它们各建一条运行只会把实况区刷屏，
// 而用户要回答的是「我的盘现在在干什么」。
func TestInMemoryWorkCreatesNoRun(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	if err := controller.RebuildFranchiseCollections(context.Background()); err != nil {
		t.Fatalf("作品群重建失败: %v", err)
	}
	controller.warmDashboardStatsCacheAsync("test")
	controller.invalidateDashboardStatsCache("test")

	runs, err := controller.taskEngine.listRunStatuses(context.Background(), taskFilters{Limit: 50})
	if err != nil {
		t.Fatalf("列运行失败: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("毫秒级的内存活建出了 %d 条运行：%+v", len(runs), runs)
	}
}

// TestLiveAreaPutsManualRunsFirst 守实况区的定序：用户刚发起的那一条不被自动运行淹没
// （用户故事 22）。序号在每一次可见变化时递增，聒噪的守护扫描每报一帧就把自己顶到最前。
func TestLiveAreaPutsManualRunsFirst(t *testing.T) {
	e, _ := newBackgroundTestEngine(t, func(func()) {}, nil)

	// 先手动、后自动：自动那条序号更大，纯按序号排它会在前面。
	seedTask(t, e, taskSeed{
		Key: "scan_library_1", Identity: libraryTask("scan_library", 1, variantSole),
		Trigger: task.TriggerManual,
	})
	seedTask(t, e, taskSeed{
		Key: "scan_library_2", Identity: libraryTask("scan_library", 2, variantSole),
		Trigger: task.TriggerScheduled,
	})

	frame, err := e.live(context.Background())
	if err != nil {
		t.Fatalf("取实况帧失败: %v", err)
	}
	if len(frame.Runs) != 2 {
		t.Fatalf("实况区里有 %d 条运行, want 2", len(frame.Runs))
	}
	if frame.Runs[0].Trigger != string(task.TriggerManual) {
		t.Fatalf("实况区第一条的发起方为 %q, want manual —— 用户刚点的那一条被自动运行顶下去了", frame.Runs[0].Trigger)
	}
}
