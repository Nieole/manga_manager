// 守身份四要素由启动点声明：任务落地时带的是声明的那一份，**变体**读得回来，重试按它分发。
//
// 挑的是任务类型名与**任务键**看着最像能推出作用域、其实推不出的那几条。判错不会有任何报错，
// 只是那个任务落到了别的作用域下，而那个库或那个系列的任务列表里从此看不到它。

package api

import (
	"fmt"
	"testing"

	"manga-manager/internal/database"
	"manga-manager/internal/task"
)

// TestExternalLibraryTasksDeclareTheirLibraryScope 钉住外部库扫描与传输都落在**资料库**作用域
// 上、带着自己那个库的 id——而不是会话 id，也不是系统作用域。
func TestExternalLibraryTasksDeclareTheirLibraryScope(t *testing.T) {
	cases := []struct {
		name   string
		launch func(t *testing.T, rig *externalRig, sessionID string) string
	}{
		{
			name: "扫描",
			launch: func(t *testing.T, rig *externalRig, sessionID string) string {
				t.Helper()
				if err := rig.c.launchExternalLibraryScanTask(rig.libraryID, sessionID); err != nil {
					t.Fatalf("启动外部库扫描失败: %v", err)
				}
				return externalLibraryScanTaskKey(rig.libraryID, sessionID)
			},
		},
		{
			name: "传输",
			launch: func(t *testing.T, rig *externalRig, sessionID string) string {
				t.Helper()
				if err := rig.c.launchExternalLibraryTransferTask(rig.libraryID, sessionID, rig.plan(t, sessionID)); err != nil {
					t.Fatalf("启动外部库传输失败: %v", err)
				}
				return externalLibraryTransferTaskKey(rig.libraryID, sessionID)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rig := newExternalRig(t, 2, frozenClock(), runTaskBodySynchronously)
			sessionID := rig.readySession(t)

			key := tc.launch(t, rig, sessionID)

			task := firstPublishedTask(t, rig.snapshots(), key)
			if task.Scope != taskScopeLibrary || task.ScopeID == nil || *task.ScopeID != rig.libraryID {
				t.Fatalf("作用域为 %q / %v, want library + %d —— 这个任务在任务中心挂到了别的地方",
					task.Scope, task.ScopeID, rig.libraryID)
			}
		})
	}
}

// TestAIGroupingDeclaresItsLibraryScope 守 AI 分组落在它那个资料库上。
//
// 它从前落在**系统**作用域却带着库 id：类型名 `ai_grouping` 里没有 library 二字，而键的末段
// 恰好是库 id。后台能力只登记不执行——观测的是任务诞生那一帧，任务体（要调 LLM）跑不跑无关。
func TestAIGroupingDeclaresItsLibraryScope(t *testing.T) {
	const libraryID = int64(3)
	engine, snapshots := newBackgroundTestEngine(t, func(func()) {}, nil)
	c := &Controller{
		taskEngine: engine,
		store:      &externalTaskStore{lib: database.Library{ID: libraryID, Name: "Library C"}},
	}

	if err := c.launchAIGroupingTask(libraryID, "zh-CN", task.TriggerManual); err != nil {
		t.Fatalf("启动 AI 分组失败: %v", err)
	}

	task := firstPublishedTask(t, snapshots(), fmt.Sprintf("ai_grouping_library_%d", libraryID))
	if task.Scope != taskScopeLibrary || task.ScopeID == nil || *task.ScopeID != libraryID {
		t.Fatalf("作用域为 %q / %v, want library + %d", task.Scope, task.ScopeID, libraryID)
	}
	if task.ScopeName != "Library C" {
		t.Fatalf("作用域名为 %q, want Library C", task.ScopeName)
	}
}

// TestSeededTaskCarriesDeclaredIdentity 守播种脚手架把整份**身份**交下去，而不是只挑其中几项。
// 漏掉哪一项都不会有编译错误，后果是消费方以别的理由变红——按作用域筛选的用例首当其冲。
func TestSeededTaskCarriesDeclaredIdentity(t *testing.T) {
	e, snapshots := newBackgroundTestEngine(t, func(func()) {}, nil)

	const key = "background_book_hash_backfill"
	seedTask(t, e, taskSeed{Key: key, Identity: systemTask("rebuild_book_hashes", variantHashRebuildBackfill), Total: 1})

	task := firstPublishedTask(t, snapshots(), key)
	if task.Type != "rebuild_book_hashes" || task.Scope != taskScopeSystem || task.ScopeID != nil {
		t.Fatalf("播下的类型与作用域为 %q / %q / %v", task.Type, task.Scope, task.ScopeID)
	}
	if task.Variant != variantHashRebuildBackfill {
		t.Fatalf("播下的变体为 %q, want %q —— 重启函数会按另一个变体分发", task.Variant, variantHashRebuildBackfill)
	}
}

// TestVariantSurvivesTheRoundTripThroughTheStore 守**变体**读得回来。
//
// 它上一版守的是「变体只随任务参数走、要能从那个 TEXT 堆里解回来」。变体如今是身份行上的真列，
// 往返仍要守：**中断**的运行重启之后只剩库里那一行，读不回变体就等于回到按类型分发——
// 用户对低优先级回填按下的重试会起出一条前台档。
//
// 上一版另有一条守「落盘的 retryable 列不得照抄」的用例，随本次接线删除：那一列不存在了，
// 可重试从来只由注册表派生（见 isRetryableTask），结构上已经没有第二份清单可抄。
func TestVariantSurvivesTheRoundTripThroughTheStore(t *testing.T) {
	e, _ := newBackgroundTestEngine(t, runTaskBodySynchronously, nil)

	const key = "background_book_hash_backfill"
	seedTask(t, e, taskSeed{
		Key: key, Identity: systemTask("rebuild_book_hashes", variantHashRebuildBackfill),
		Terminal: "failed",
	})

	readBack := currentTask(t, e, key)
	if readBack.Variant != variantHashRebuildBackfill {
		t.Fatalf("读回的变体为 %q, want %q", readBack.Variant, variantHashRebuildBackfill)
	}
	if readBack.Type != "rebuild_book_hashes" || readBack.Scope != taskScopeSystem || readBack.ScopeID != nil {
		t.Fatalf("读回的另外三项不对：type=%q scope=%q id=%v", readBack.Type, readBack.Scope, readBack.ScopeID)
	}
}

// TestBackfillRetryDispatchesByVariantNotKey 守重试分发认的是（类型，**变体**）。
//
// 它与 TestRetryRestartsTheSameVariant 的差别在于故意换掉**任务键**：分发若还在看键，
// 这条用例会掉进「不支持的重试目标」，而按变体分发对键叫什么毫不关心。
func TestBackfillRetryDispatchesByVariantNotKey(t *testing.T) {
	c, snapshots := newHashRebuildRetryRig(t)

	const key = "some_other_key_for_the_same_variant"
	seedTask(t, c.taskEngine, taskSeed{
		Key: key, Identity: systemTask("rebuild_book_hashes", variantHashRebuildBackfill), Total: 1,
		Terminal: "failed",
	})

	done := lastPublishedTask(t, snapshots(), key)
	relaunch, ok := c.taskEngine.relauncherFor(done.Type, done.Variant)
	if !ok {
		t.Fatal("低优先级回填没有重启函数 —— 界面上那个重试按钮点下去是 400")
	}
	if err := relaunch(t.Context(), done, task.TriggerManual); err != nil {
		t.Fatalf("重启低优先级回填失败: %v", err)
	}

	backfill := lastPublishedTask(t, snapshots(), lowPriorityBookHashTaskKey)
	if backfill.Params["profile"] != "full_hash_low_priority" {
		t.Fatalf("重启出来的档位为 %q, want full_hash_low_priority —— 它跑成了前台档那套抢盘跑法",
			backfill.Params["profile"])
	}
}
