// 守封面每库一条**运行**在 api 这一侧的四条：批换成一条串联运行、暂停取消只作用在它自己身上、
// 删库按**作用域**把它一起带走、跨批的合计不翻倍。

package api

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"

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

// TestCoverRunSinkDoesNotDoubleCountAcrossBatches 守跨批合计：一条封面运行可能认领好几批
// （扫描期间又来一次扫描），每一批报的都是**它自己那一批的全量当前值**。
//
// 定版由任务体在 Drain 返回后做，不由报文自己判「剩余量归零」——扫描还在往批里加的间隙
// 剩余量本来就会归零，按它定版会让下一份报文加在自己刚定版的值上，数字凭空翻倍。
func TestCoverRunSinkDoesNotDoubleCountAcrossBatches(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	const key = "generate_covers_3"
	progress := seedTask(t, controller.taskEngine, taskSeed{
		Key: key, Identity: libraryTask("generate_covers", 3, variantSole), Trigger: task.TriggerChained,
	})
	sink := &coverRunSink{progress: progress, libraryName: "Main"}

	sink.begin()
	sink.Progress(scanner.CoverProgressReport{Queued: 4, Generated: 2, Remaining: 2})
	// 这一批的中途剩余量归零：扫描还没收尾，下一份报文仍是这一批的全量值。
	sink.Progress(scanner.CoverProgressReport{Queued: 4, Generated: 4, Remaining: 0})
	sink.fixate()

	sink.begin()
	sink.Progress(scanner.CoverProgressReport{Queued: 3, Generated: 3, Remaining: 0})
	sink.fixate()

	run := currentTask(t, controller.taskEngine, key)
	if run.Metrics["generated_covers"] != 7 || run.Metrics["queued_covers"] != 7 {
		t.Fatalf("跨批合计为 生成 %d / 共 %d, want 7 / 7", run.Metrics["generated_covers"], run.Metrics["queued_covers"])
	}
	if run.Current != 7 || run.Total != 7 {
		t.Fatalf("**计数推进**为 %d/%d, want 7/7", run.Current, run.Total)
	}
	if sink.generated() != 7 {
		t.Fatalf("终态文案要报的生成数为 %d, want 7", sink.generated())
	}

	// 窗口关掉之后迟到的报文（被取消那一批还在飞的作业）不再计入，否则下一批的基线被抬高。
	sink.Progress(scanner.CoverProgressReport{Queued: 3, Generated: 3, Remaining: 0})
	if sink.generated() != 7 {
		t.Fatalf("窗口之外的迟到报文被计入了：%d, want 7", sink.generated())
	}
}
