// 守「历史再多也不会打掉还在跑的运行」（running / paused / cancelling 一视同仁）。
//
// 上一版这里守的是内存任务表的淘汰：那张表当年是活动任务的唯一可写副本，一个仍在跑的任务被裁掉
// 之后，它后续的全部进度乃至终态更新都会静默失效。表没有了，但它防的那件事还在——现在换成
// 两条：清除不得带走仍会变化的运行（判据写死在落盘侧），以及历史再多也不把活动运行挤出第一页。

package api

import (
	"context"
	"fmt"
	"testing"

	"manga-manager/internal/runhandle"
)

// floodFinishedRuns 灌入 n 条已完成的运行，把历史堆到比任务中心一页还多。
func floodFinishedRuns(t *testing.T, engine *taskEngine, n int) {
	t.Helper()
	for i := range n {
		seedTask(t, engine, taskSeed{
			Key: fmt.Sprintf("filler_%d", i), Identity: seriesTask("filler", int64(i+1), variantSole),
			Total: 1, Terminal: "completed",
		})
	}
}

func TestHistoryDoesNotDisplaceActiveRuns(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, e *taskEngine, key string) *runhandle.Handle
	}{
		{
			name: "running 运行不被历史淹没",
			setup: func(t *testing.T, e *taskEngine, key string) *runhandle.Handle {
				return seedTask(t, e, taskSeed{Key: key, Identity: libraryTask("scan_library", 1, variantSole), Total: 100, CanCancel: true, CanPause: true})
			},
		},
		{
			name: "paused 运行不被历史淹没",
			setup: func(t *testing.T, e *taskEngine, key string) *runhandle.Handle {
				progress := seedTask(t, e, taskSeed{Key: key, Identity: libraryTask("scan_library", 1, variantSole), Total: 100, CanCancel: true, CanPause: true})
				if err := pauseByKey(e, key); err != nil {
					t.Fatalf("pause: %v", err)
				}
				return progress
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			controller, _, _, _ := newTestController(t)
			engine := controller.taskEngine

			const activeKey = "scan_library_1"
			progress := tc.setup(t, engine, activeKey)

			// 关键点：活动运行先起，它的序号因此最小。定序若只看序号，它必然排在最后——
			// 真实场景里一个长时间无进度上报的大库扫描正是如此。
			floodFinishedRuns(t, engine, taskCenterPageSize+10)

			keys := taskCenterFirstPage(t, controller)
			if indexOfKey(keys, activeKey) < 0 {
				t.Fatalf("活动运行被历史挤出了第一页（页首三条：%v）—— 用户看不到自己刚发起的那一条",
					keys[:min(3, len(keys))])
			}

			// 更新仍然生效，说明那条运行还在、还认得它的**运行句柄**。
			progress.Advance(42, 100, "", nil)
			if updated := currentTask(t, engine, activeKey); updated.Current != 42 {
				t.Fatalf("活动运行的进度更新没生效：Current = %d, want 42（状态 %q）", updated.Current, updated.Status)
			}
		})
	}
}

// TestClearTasksKeepsPausedTask 守卫清除的判活。
// paused/cancelling 与 running 一样仍会变化：删掉之后 resume 变成 404，
// 而任务体仍卡在**暂停闸门**上永远等不到放行。这条判据写死在落盘侧，不经调用方。
func TestClearTasksKeepsPausedTask(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	engine := controller.taskEngine

	const key = "scan_library_7"
	seedTask(t, engine, taskSeed{Key: key, Identity: libraryTask("scan_library", 7, variantSole), Total: 100, CanCancel: true, CanPause: true})
	if err := pauseByKey(engine, key); err != nil {
		t.Fatalf("pause: %v", err)
	}

	if _, err := engine.clear(context.Background(), taskFilters{}); err != nil {
		t.Fatalf("clear: %v", err)
	}

	if !taskExists(t, engine, key) {
		t.Fatal("clear 删掉了暂停中的运行 —— resume 会变成 404，任务体永远卡在暂停闸门上")
	}
	if err := resumeByKey(engine, key); err != nil {
		t.Fatalf("clear 之后 resume 失败: %v", err)
	}
}
