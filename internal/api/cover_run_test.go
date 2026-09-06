// 守封面每库一条**运行**在 api 这一侧的四条：批换成一条串联运行、暂停取消只作用在它自己身上、
// 删库按**作用域**把它一起带走、跨批的合计不翻倍。

package api

import (
	"context"
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

// seedCoverRunLibrary 建一个真的能查回来的资料库：封面运行的任务声明要读它的名字与存储画像，
// 读不到就一条运行都发不起来。
func seedCoverRunLibrary(t *testing.T, c *Controller) database.Library {
	t.Helper()
	lib, err := c.store.CreateLibrary(context.Background(), database.CreateLibraryParams{
		Name:         "Main",
		Path:         filepath.Join(t.TempDir(), "main"),
		ScanMode:     "manual",
		ScanInterval: 60,
		ScanFormats:  config.DefaultScanFormatsCSV,
	})
	if err != nil {
		t.Fatalf("建资料库失败: %v", err)
	}
	return lib
}

// coverRunKey 是这个库那条封面运行的**任务键**。
func coverRunKey(libraryID int64) string {
	return "generate_covers_" + strconv.FormatInt(libraryID, 10)
}

// TestCoverBatchBecomesItsOwnChainedRun 走完扫描器交出一批封面之后的那一段：
// 它换成一条**串联**发起、挂在这个库上、可单独暂停与取消的运行，并推进到完成。
//
// 这一条是「扫描完成开始说真话」的另一半：扫描那条运行不再等封面，封面在任务中心自己占一条。
func TestCoverBatchBecomesItsOwnChainedRun(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	lib := seedCoverRunLibrary(t, controller)

	batch, err := controller.scanner.QueueMissingCovers(context.Background(), lib.ID)
	if err != nil {
		t.Fatalf("QueueMissingCovers: %v", err)
	}
	// 同步执行版的后台能力：任务体在发起调用返回前就跑完，终态因此当场落定。
	restore := controller.taskEngine.runBackground
	controller.taskEngine.runBackground = runTaskBodySynchronously
	controller.dispatchCoverBatch(batch)
	controller.taskEngine.runBackground = restore

	run := currentTask(t, controller.taskEngine, coverRunKey(lib.ID))
	if run.Type != "generate_covers" || run.Scope != "library" || run.ScopeID == nil || *run.ScopeID != lib.ID {
		t.Fatalf("封面运行的身份不对：type=%q scope=%q id=%v", run.Type, run.Scope, run.ScopeID)
	}
	if run.Trigger != string(task.TriggerChained) {
		t.Fatalf("封面运行的**发起方**为 %q, want chained —— 它是扫描带起来的", run.Trigger)
	}
	if run.Status != "completed" {
		t.Fatalf("封面运行的状态为 %q, want completed", run.Status)
	}
	if run.ScopeName != lib.Name {
		t.Fatalf("封面运行的作用域名为 %q, want %q", run.ScopeName, lib.Name)
	}
}

// TestScanningALibraryLaunchesItsCoverRun 守装配那一跳：扫描器排出去的封面批确实接到了
// `dispatchCoverBatch` 上。两头各有用例（扫描排出一批、一批换成一条运行），
// 而中间这根线接没接上，只有走一次真扫描才答得出——漏接的表现是封面照生成，任务中心一片空白。
//
// 只断言那条运行**已经落地**：启动入口的准入是同步的，扫描返回时它已经在库里，
// 而任务体在另一条 goroutine 上。它跑到完成由 TestCoverBatchBecomesItsOwnChainedRun 守。
func TestScanningALibraryLaunchesItsCoverRun(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	libraryPath := filepath.Join(rootDir, "Cover Library")
	seriesPath := filepath.Join(libraryPath, "Series Alpha")
	if err := os.MkdirAll(seriesPath, 0o755); err != nil {
		t.Fatalf("mkdir series failed: %v", err)
	}
	if err := writeTestCBZ(filepath.Join(seriesPath, "Alpha 01.cbz"), map[string][]byte{"001.png": png1x1}); err != nil {
		t.Fatalf("write test cbz failed: %v", err)
	}
	lib, err := store.CreateLibrary(context.Background(), database.CreateLibraryParams{
		Name: "Cover Library", Path: libraryPath, ScanMode: "manual", ScanInterval: 60,
		ScanFormats: config.DefaultScanFormatsCSV,
	})
	if err != nil {
		t.Fatalf("建资料库失败: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := controller.scanner.ScanLibrary(ctx, lib.ID, libraryPath, true, nil); err != nil {
		t.Fatalf("ScanLibrary: %v", err)
	}

	run := currentTask(t, controller.taskEngine, coverRunKey(lib.ID))
	if run.Type != "generate_covers" || run.Trigger != string(task.TriggerChained) {
		t.Fatalf("扫描没有带起一条串联的封面运行：type=%q trigger=%q", run.Type, run.Trigger)
	}
}

// TestCoverRunIsControllableOnItsOwn 守用户故事 6：只让出一部分磁盘——按下封面那条运行的
// 暂停或取消，扫描那条不受影响。两条运行各有各的**运行时句柄**，控制按运行 id 寻址。
func TestCoverRunIsControllableOnItsOwn(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	const libraryID int64 = 7
	scanKey := "scan_library_" + strconv.FormatInt(libraryID, 10)
	seedTask(t, controller.taskEngine, taskSeed{
		Key: scanKey, Identity: libraryTask("scan_library", libraryID, variantSole),
		CanCancel: true, CanPause: true,
	})
	seedTask(t, controller.taskEngine, taskSeed{
		Key: coverRunKey(libraryID), Identity: libraryTask("generate_covers", libraryID, variantSole),
		Trigger: task.TriggerChained, CanCancel: true, CanPause: true,
	})

	if err := pauseByKey(controller.taskEngine, coverRunKey(libraryID)); err != nil {
		t.Fatalf("暂停封面运行失败: %v", err)
	}
	if got := currentTask(t, controller.taskEngine, scanKey).Status; got != "running" {
		t.Fatalf("暂停封面之后扫描的状态为 %q, want running —— 两条运行被绑在了一起", got)
	}
	if err := cancelByKey(controller.taskEngine, coverRunKey(libraryID)); err != nil {
		t.Fatalf("取消封面运行失败: %v", err)
	}
	if got := currentTask(t, controller.taskEngine, scanKey).Status; got != "running" {
		t.Fatalf("取消封面之后扫描的状态为 %q, want running", got)
	}
}

// TestDeletingALibraryCancelsEveryRunOnIt 守删库按**作用域**取消而不是按一串**任务键**前缀：
// 封面生成是新加的库级类型，前缀清单漏补它不会有编译错误，只会表现成删库之后
// 队列里那条封面运行自己开跑。
func TestDeletingALibraryCancelsEveryRunOnIt(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	// 三条运行要同时活着才看得出「谁被取消、谁没有」，默认两个**运行槽位**装不下。
	controller.taskEngine.slots = func() int { return 8 }
	const libraryID int64 = 9
	keys := map[string]TaskIdentity{
		"scan_library_9":       libraryTask("scan_library", libraryID, variantSole),
		coverRunKey(libraryID): libraryTask("generate_covers", libraryID, variantSole),
	}
	for key, identity := range keys {
		seedTask(t, controller.taskEngine, taskSeed{Key: key, Identity: identity, CanCancel: true, CanPause: true})
	}
	// 另一个库上的封面运行不该被带走。
	seedTask(t, controller.taskEngine, taskSeed{
		Key: coverRunKey(10), Identity: libraryTask("generate_covers", 10, variantSole), CanCancel: true,
	})

	controller.cancelLibraryScopedTasks(libraryID)

	for key := range keys {
		if got := currentTask(t, controller.taskEngine, key).Status; got == "running" {
			t.Fatalf("删库之后 %q 仍是 running —— 它逃出了取消", key)
		}
	}
	if got := currentTask(t, controller.taskEngine, coverRunKey(10)).Status; got != "running" {
		t.Fatalf("删库带走了另一个库的封面运行：状态 %q, want running", got)
	}
}

// TestCoverRunObserverDoesNotDoubleCountAcrossBatches 守跨批合计：一条封面运行可能认领好几批
// （扫描期间又来一次扫描），每一批报的都是**它自己那一批的全量当前值**。
//
// 定版由任务体在 Drain 返回后做，不由报文自己判「剩余量归零」——扫描还在往批里加的间隙
// 剩余量本来就会归零，按它定版会让下一份报文加在自己刚定版的值上，数字凭空翻倍。
func TestCoverRunObserverDoesNotDoubleCountAcrossBatches(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	const key = "generate_covers_3"
	progress := seedTask(t, controller.taskEngine, taskSeed{
		Key: key, Identity: libraryTask("generate_covers", 3, variantSole), Trigger: task.TriggerChained,
	})
	observer := newCoverRunObserver(progress, "Main")

	observer.begin()
	observer.Progress(scanner.CoverProgressReport{Queued: 4, Generated: 2, Remaining: 2})
	// 这一批的中途剩余量归零：扫描还没收尾，下一份报文仍是这一批的全量值。
	observer.Progress(scanner.CoverProgressReport{Queued: 4, Generated: 4, Remaining: 0})
	observer.fixate()

	observer.begin()
	observer.Progress(scanner.CoverProgressReport{Queued: 3, Generated: 3, Remaining: 0})
	observer.fixate()

	run := currentTask(t, controller.taskEngine, key)
	if run.Metrics["generated_covers"] != 7 || run.Metrics["queued_covers"] != 7 {
		t.Fatalf("跨批合计为 生成 %d / 共 %d, want 7 / 7", run.Metrics["generated_covers"], run.Metrics["queued_covers"])
	}
	if run.Current != 7 || run.Total != 7 {
		t.Fatalf("**计数推进**为 %d/%d, want 7/7", run.Current, run.Total)
	}
	if observer.generated() != 7 {
		t.Fatalf("终态文案要报的生成数为 %d, want 7", observer.generated())
	}

	// 窗口关掉之后迟到的报文（被取消那一批还在飞的作业）不再计入，否则下一批的基线被抬高。
	observer.Progress(scanner.CoverProgressReport{Queued: 3, Generated: 3, Remaining: 0})
	if observer.generated() != 7 {
		t.Fatalf("窗口之外的迟到报文被计入了：%d, want 7", observer.generated())
	}
}

// TestCoverRunProgressCountsSkippedCovers 守**计数推进**数的是「已结算的」而不是「生成 + 失败」：
// 已经有封面的书既不新增一张也不是故障，漏掉它进度条到最后差着几张永远走不满。
func TestCoverRunProgressCountsSkippedCovers(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	const key = "generate_covers_5"
	progress := seedTask(t, controller.taskEngine, taskSeed{
		Key: key, Identity: libraryTask("generate_covers", 5, variantSole), Trigger: task.TriggerChained,
	})
	observer := newCoverRunObserver(progress, "Main")

	observer.begin()
	// 五张：三张生成、一张失败、一张跳过（用户自己设过封面）。
	observer.Progress(scanner.CoverProgressReport{Queued: 5, Generated: 3, Failed: 1, Remaining: 0})

	run := currentTask(t, controller.taskEngine, key)
	if run.Current != 5 || run.Total != 5 {
		t.Fatalf("**计数推进**为 %d/%d, want 5/5 —— 跳过的那张没算进已结算", run.Current, run.Total)
	}
	// duration_ms 必须随每一帧写下来（毫秒级的用例里它就是 0）：缺了这个键，
	// 存储 IO 面板会退回按挂钟算，那格速率在运行收尾之后一路衰减。
	if _, ok := run.Metrics["duration_ms"]; !ok {
		t.Fatalf("封面运行没报 duration_ms：%v", run.Metrics)
	}
}

// TestCoverBatchWithoutARunIsWithdrawnAndDrained 守发起落空时的兜底只收回**自己那一批**：
// 整个库名下抄走一次，会把并发扫描刚挂上、而且发起已经成功的那几批一起带走，
// 认领它们的那条运行认到的是空，而它们跑在一个既不受暂停也不受取消约束的上下文里。
func TestCoverBatchWithoutARunIsWithdrawnAndDrained(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	// 库不存在：封面运行的任务声明读不到它，一条运行都建不出来。
	const missingLibraryID int64 = 4242
	mine, err := controller.scanner.QueueMissingCovers(context.Background(), missingLibraryID)
	if err != nil {
		t.Fatalf("QueueMissingCovers: %v", err)
	}
	lib := seedCoverRunLibrary(t, controller)
	theirs, err := controller.scanner.QueueMissingCovers(context.Background(), lib.ID)
	if err != nil {
		t.Fatalf("QueueMissingCovers: %v", err)
	}
	controller.coverRuns.enqueue(theirs)

	controller.dispatchCoverBatch(mine)

	if controller.coverRuns.withdraw(mine) {
		t.Fatal("发起落空的那一批还挂在名下 —— 没有任何一条运行会来认领它")
	}
	if !controller.coverRuns.withdraw(theirs) {
		t.Fatal("兜底把别人那一批也抄走了 —— 认领它的那条运行会认到空")
	}
}
