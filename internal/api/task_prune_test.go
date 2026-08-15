// 守「内存任务表的淘汰与清理都不会打掉还在跑的任务」（running / paused / cancelling 一视同仁）。
//
// 内存里的这份是活动任务的唯一可写副本：applyTaskProgress 等一律「tasks[key] 查不到就 return」。
// 一个仍在跑的任务被裁掉之后，它后续的全部进度乃至终态更新都会静默失效——任务面板上
// 永远停在最后一次进度，也再不会变成「完成」，而日志里不会有任何迹象。

package api

import (
	"context"
	"fmt"
	"testing"

	"manga-manager/internal/database"
)

// floodFinishedTasks 灌入 n 个已完成任务，把内存表推过 maxRetainedTasks。
func floodFinishedTasks(t *testing.T, engine *taskEngine, n int) {
	t.Helper()
	for i := range n {
		seedTask(t, engine, taskSeed{Key: fmt.Sprintf("filler_%d", i), Type: "filler", Total: 1, Terminal: "completed"})
	}
}

func TestPruneKeepsActiveTasks(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, e *taskEngine, key string) *TaskProgress
	}{
		{
			name: "running 任务不被淘汰",
			setup: func(t *testing.T, e *taskEngine, key string) *TaskProgress {
				return seedTask(t, e, taskSeed{Key: key, Type: "scan_library", Total: 100, CanCancel: true, CanPause: true})
			},
		},
		{
			name: "paused 任务不被淘汰",
			setup: func(t *testing.T, e *taskEngine, key string) *TaskProgress {
				progress := seedTask(t, e, taskSeed{Key: key, Type: "scan_library", Total: 100, CanCancel: true, CanPause: true})
				if err := e.pause(key); err != nil {
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

			// 关键点：活动任务先启动，它的 Sequence 因此最小。淘汰若只按「最近活动」排序，
			// 它必然排在最后被裁掉——真实场景里一个长时间无进度上报的大库扫描正是如此。
			floodFinishedTasks(t, engine, maxRetainedTasks+50)

			engine.mutex.Lock()
			task, stillThere := engine.tasks[activeKey]
			total := len(engine.tasks)
			engine.mutex.Unlock()

			if !stillThere {
				t.Fatalf("活动任务被淘汰了（表内剩 %d 条）——它后续的所有状态更新都会静默失效", total)
			}
			if total > maxRetainedTasks {
				t.Fatalf("淘汰后仍有 %d 条，超过上限 %d", total, maxRetainedTasks)
			}

			// 更新仍然生效，说明它确实还是那份可写副本。
			progress.Advance(42, 100, "", nil)
			engine.mutex.Lock()
			updated := engine.tasks[activeKey]
			engine.mutex.Unlock()
			if updated.Current != 42 {
				t.Fatalf("活动任务的进度更新没生效：Current = %d, want 42（原状态 %q）", updated.Current, task.Status)
			}
		})
	}
}

// TestClearTasksKeepsPausedTask 守卫 clear 的判活。
// paused/cancelling 与 running 一样，内存里这份是唯一可写副本：删掉之后 resume 变成 404，
// 而任务体仍卡在 PauseGate 上永远等不到放行。
func TestClearTasksKeepsPausedTask(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	engine := controller.taskEngine

	const key = "scan_library_7"
	seedTask(t, engine, taskSeed{Key: key, Type: "scan_library", Total: 100, CanCancel: true, CanPause: true})
	if err := engine.pause(key); err != nil {
		t.Fatalf("pause: %v", err)
	}

	if _, err := engine.clear(context.Background(), database.TaskFilters{}); err != nil {
		t.Fatalf("clear: %v", err)
	}

	engine.mutex.Lock()
	_, stillThere := engine.tasks[key]
	engine.mutex.Unlock()
	if !stillThere {
		t.Fatal("clear 删掉了暂停中的任务 —— resume 会变成 404，任务体永远卡在 PauseGate 上")
	}
	if err := engine.resume(key); err != nil {
		t.Fatalf("clear 之后 resume 失败: %v", err)
	}
}
