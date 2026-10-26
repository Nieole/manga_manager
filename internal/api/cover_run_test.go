// 守封面运行的 API 装配、独立控制与删除资料库时的作用域取消。

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

// coverRunKey 是这个库那条封面运行的**任务键**，并顺手登记它指的身份。
//
// 登记是必需的：契约上不再有键（ADR 0007），装置的取数按身份走，见 task_seed_test.go 的登记表。
func coverRunKey(libraryID int64) string {
	key := "generate_covers_" + strconv.FormatInt(libraryID, 10)
	rememberTaskKey(key, libraryTask("generate_covers", libraryID, variantSole))
	return key
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
	controller.dispatchCoverBatch(context.Background(), batch)
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
