// 守重启之后**可续跑**的那半边真的自己接着跑、另半边真的停在**中断**，以及这两条判据与
// **可重试**是分开的两列。白名单的判定在 `internal/task` 纯内存测，这里测的是接线：
// 谁被重新发起、发起方记的是不是恢复、原来那条还在不在。

package api

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/task"
)

// restartWithSeededRun 播下一条停在**活动态**的运行，再另起一个 Controller 走一遍重启恢复：
// 转**中断**，白名单内的顺手重新发起。交回重启后的 Controller 与那条运行的**任务键**。
//
// 「重启」是拿同一份存储另起一个 Controller——**运行时句柄**不跨实例，新实例只能从库里读回那条
// 还写着活动态的运行，正是重启恢复要处置的东西。
func restartWithSeededRun(t *testing.T, seed taskSeed) *Controller {
	t.Helper()
	controller, store, _, tempDir := newTestController(t)
	seedTask(t, controller.taskEngine, seed)

	reloaded := restartController(t, controller, store, tempDir)
	reloaded.taskEngine.markInterrupted(context.Background())
	return reloaded
}

// seedScannableLibrary 建一个真的能被扫的资料库：续跑要经**重启函数**把它读回来，
// 读不到库就没有任何东西会被发起，而那时用例的绿是假的。
func seedScannableLibrary(t *testing.T, c *Controller) database.Library {
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

// 白名单内的类型重启后自己接着跑：**发起方记恢复**，而原来那条留在**中断**——
// 「上一次断在哪」是用户回头要看的东西，恢复不该把它改回运行中。
func TestRestartResumesScanAndKeepsTheInterruptedRun(t *testing.T) {
	controller, store, _, tempDir := newTestController(t)
	lib := seedScannableLibrary(t, controller)
	key := "scan_library_" + strconv.FormatInt(lib.ID, 10)
	seedTask(t, controller.taskEngine, taskSeed{
		Key: key, Identity: libraryTask("scan_library", lib.ID, variantSole), Total: 10,
		Metadata: map[string]string{"force": "true"},
	})

	reloaded := restartController(t, controller, store, tempDir)
	reloaded.taskEngine.markInterrupted(context.Background())

	runs := runsForKey(t, reloaded, key)
	if len(runs) != 2 {
		t.Fatalf("重启后这个键下有 %d 条运行, want 2（中断的那条 + 恢复出来的新一条）", len(runs))
	}
	resumed, interrupted := runs[0], runs[1]
	if interrupted.Status != "interrupted" {
		t.Fatalf("原来那条运行的状态为 %q, want interrupted", interrupted.Status)
	}
	if resumed.RunID == interrupted.RunID {
		t.Fatal("恢复改写了原来那条运行 —— 恢复是新一次运行")
	}
	if resumed.Trigger != string(task.TriggerResumed) {
		t.Fatalf("恢复出来的运行发起方为 %q, want resumed —— 用户据此知道这条是重启接上的", resumed.Trigger)
	}
	if resumed.Params["force"] != "true" {
		t.Fatalf("恢复出来的运行入参为 %v, want force=true —— 原来那次的参数没被读回来", resumed.Params)
	}
}

// 白名单外的类型只标**中断**，一条都不自动发起。清理资料库**可重试**（用户点得动），
// 但它删的是记录，无人看着时不该自己再删一遍。
func TestRestartDoesNotResumeWorkOutsideTheWhitelist(t *testing.T) {
	const key = "cleanup_library_3"
	reloaded := restartWithSeededRun(t, taskSeed{
		Key: key, Identity: libraryTask("cleanup_library", 3, variantSole), Total: 1,
	})

	runs := runsForKey(t, reloaded, key)
	if len(runs) != 1 {
		t.Fatalf("重启后这个键下有 %d 条运行, want 1 —— 白名单外的类型不该被自动发起", len(runs))
	}
	if runs[0].Status != "interrupted" {
		t.Fatalf("那条运行的状态为 %q, want interrupted", runs[0].Status)
	}
	if !runs[0].Retryable {
		t.Fatal("中断的清理不可重试了 —— 不可续跑说的是「没人看着时不自己跑」，不是「不许人点」")
	}
}

// 全局开关关掉之后一条都不续跑，而转**中断**照旧：开关关的是「自己接着跑」，不是「记不记这一笔」。
func TestResumeSwitchOffLeavesEverythingInterrupted(t *testing.T) {
	controller, store, _, tempDir := newTestController(t)
	lib := seedScannableLibrary(t, controller)
	key := "scan_library_" + strconv.FormatInt(lib.ID, 10)
	seedTask(t, controller.taskEngine, taskSeed{
		Key: key, Identity: libraryTask("scan_library", lib.ID, variantSole), Total: 10,
	})

	reloaded := restartController(t, controller, store, tempDir)
	disableResume(reloaded)
	reloaded.taskEngine.markInterrupted(context.Background())

	runs := runsForKey(t, reloaded, key)
	if len(runs) != 1 {
		t.Fatalf("关掉续跑之后这个键下有 %d 条运行, want 1", len(runs))
	}
	if runs[0].Status != "interrupted" {
		t.Fatalf("那条运行的状态为 %q, want interrupted —— 关掉的是续跑，不是转写", runs[0].Status)
	}
}

// **可续跑比可重试严格，两者不得合成一个标志**（规格关键决定 7）。这条用例守两件事：
// 存在「可重试但不可续跑」的类型，以及白名单里的每一个都真有重启函数——
// 「白名单里有、却没人发得起」的类型每次重启都静默什么都不做。
func TestResumableIsStricterThanRetryable(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	dispatch := controller.taskEngine.dispatch

	// 可重试但**不可续跑**：它们要么改磁盘内容、要么花钱。
	strictlyRetryable := []taskDispatchKey{
		{Type: "cleanup_library", Variant: variantSole},
		{Type: "scrape", Variant: variantScrapeAllLibraries},
		{Type: "scrape", Variant: variantScrapeOneLibrary},
		{Type: "ai_grouping", Variant: variantSole},
	}
	for _, key := range strictlyRetryable {
		entry, ok := dispatch[key]
		if !ok {
			t.Fatalf("%q/%q 没注册重启函数 —— 它该是可重试的", key.Type, key.Variant)
		}
		if !controller.taskEngine.isRetryableTask(key.Type, key.Variant) {
			t.Fatalf("%q/%q 不可重试了", key.Type, key.Variant)
		}
		if entry.Resumable {
			t.Fatalf("%q/%q 被判成可续跑 —— 它要么改磁盘内容、要么花钱，断电后开机不该自己跑",
				key.Type, key.Variant)
		}
	}

	for key, entry := range dispatch {
		if entry.Resumable && entry.Relaunch == nil {
			t.Fatalf("%q/%q 在白名单里却没有重启函数 —— 每次重启它都会静默什么都不做", key.Type, key.Variant)
		}
	}
}

// 白名单的成员就是规格关键决定 7 那一份清单里**今天已经有运行的那些**，一个不多一个不少：
// 多一个就是无人值守时多一类自己跑起来的活，少一个就是重启后还得有人登录去点。
//
// 清单上的**封面生成**不在这里：它还不是自己的一条运行（票 13），因此没有类型名可写。
// 票 13 建出那条运行时，这份 want 与注册表要一起补上（挂账 D87）。
func TestResumeWhitelistMatchesTheSpec(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	want := map[taskDispatchKey]bool{
		{Type: "scan_library", Variant: variantSole}:                         true,
		{Type: "scan_series", Variant: variantSole}:                          true,
		{Type: "rebuild_thumbnails", Variant: variantSole}:                   true,
		{Type: "cleanup_thumbnails", Variant: variantSole}:                   true,
		{Type: "rebuild_book_hashes", Variant: variantHashRebuildForeground}: true,
		{Type: "rebuild_book_hashes", Variant: variantHashRebuildBackfill}:   true,
		{Type: "rebuild_file_identities", Variant: variantSole}:              true,
		{Type: "reconcile_koreader_progress", Variant: variantSole}:          true,
		{Type: "refresh_koreader_matching", Variant: variantSole}:            true,
	}
	got := map[taskDispatchKey]bool{}
	for key, entry := range controller.taskEngine.dispatch {
		if entry.Resumable {
			got[key] = true
		}
	}

	for key := range want {
		if !got[key] {
			t.Errorf("%q/%q 不在白名单里 —— 重启之后它要等人登录去点", key.Type, key.Variant)
		}
	}
	for key := range got {
		if !want[key] {
			t.Errorf("%q/%q 进了白名单，而规格那份清单里没有它", key.Type, key.Variant)
		}
	}
}
