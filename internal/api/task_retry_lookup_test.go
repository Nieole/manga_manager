// 守重试取快照按**任务 id** 精确命中，而不是「搜一把再从前几条里挑」。
//
// 挑得出来的做法有好几种，错的那几种长得都一样：目标被更新的邻居挤到页外，用户看着任务中心里
// 那条「中断，可重试」，点重试却被告知没有可重试的运行。重启后满屏中断运行正是高发场景——
// 同一个类型下每个库 / 系列各一条。接线之后这条谓词是落盘侧的一句 `task_id = ?`，
// 邻居再多也挤不掉它——本用例守它没退回去。

package api

import (
	"context"
	"fmt"
	"testing"
)

// TestRetrySnapshotFindsTaskCrowdedOutByKinTasks 钉住同类型的邻居任务再多也挤不掉目标那一条。
func TestRetrySnapshotFindsTaskCrowdedOutByKinTasks(t *testing.T) {
	cases := []struct {
		name     string
		key      string
		identity TaskIdentity
		kin      func(int) (string, TaskIdentity)
	}{
		{
			name: "系列扫描", key: "scan_series_1", identity: seriesTask("scan_series", 1, variantSole),
			kin: func(i int) (string, TaskIdentity) {
				return fmt.Sprintf("scan_series_1%d", i), seriesTask("scan_series", int64(10+i), variantSole)
			},
		},
		{
			name: "资料库清理", key: "cleanup_library_2", identity: libraryTask("cleanup_library", 2, variantSole),
			kin: func(i int) (string, TaskIdentity) {
				return fmt.Sprintf("cleanup_library_2%d", i), libraryTask("cleanup_library", int64(20+i), variantSole)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			controller, _, _, _ := newTestController(t)

			// 目标：失败在先，序号最旧。
			seedTask(t, controller.taskEngine, taskSeed{
				Key: tc.key, Identity: tc.identity, Total: 10,
				Metadata: map[string]string{"force": "true"},
				Terminal: "failed",
			})
			taskID := taskIDForKey(t, controller.taskEngine, tc.key)

			// 邻居任务各占一条运行、序号都比它新：按序号倒序取时先取到的全是它们。
			for i := range 30 {
				kinKey, kinIdentity := tc.kin(i)
				seedTask(t, controller.taskEngine, taskSeed{Key: kinKey, Identity: kinIdentity, Total: 10, Terminal: "failed"})
			}

			task, err := controller.taskEngine.snapshotForRetry(context.Background(), taskID)
			if err != nil {
				t.Fatalf("取 %q 的重试快照失败: %v —— 任务中心里明明列着它，点重试却是 404", tc.key, err)
			}
			if task.Key != tc.key {
				t.Fatalf("取回的是 %q, want %q —— 重试会去重启另一条任务", task.Key, tc.key)
			}
			if task.Status != "failed" || task.Params["force"] != "true" {
				t.Fatalf("取回的 %q 状态为 %q、入参为 %v, want failed + force=true", task.Key, task.Status, task.Params)
			}
		})
	}
}

// TestRetrySnapshotPrefersTheNewestTerminalRun 守同一个任务跑过多次时，重试拿的是**最近那一次终态**。
//
// 重试从此不再抹掉上一次，于是同一个任务下面会堆着一串历史。拿错一条的后果是重试按着一份过时的
// 入参重来——用户以为在重跑刚才失败的那次，实际重跑的是上礼拜那次。
func TestRetrySnapshotPrefersTheNewestTerminalRun(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	const key = "scan_library_7"
	identity := libraryTask("scan_library", 7, variantSole)
	seedTask(t, controller.taskEngine, taskSeed{
		Key: key, Identity: identity, Total: 100,
		Metadata: map[string]string{"force": "false"}, Terminal: "completed",
	})
	seedTask(t, controller.taskEngine, taskSeed{
		Key: key, Identity: identity, Total: 100,
		Metadata: map[string]string{"force": "true"}, Terminal: "failed",
	})

	task, err := controller.taskEngine.snapshotForRetry(context.Background(), taskIDForKey(t, controller.taskEngine, key))
	if err != nil {
		t.Fatalf("取 %q 的重试快照失败: %v", key, err)
	}
	if task.Status != "failed" {
		t.Fatalf("取回的状态为 %q, want failed —— 拿的是上一次运行，用户看着的是刚失败的那条", task.Status)
	}
	if task.Params["force"] != "true" {
		t.Fatalf("取回的入参为 %v —— 拿的是上一次运行那份，重试会换一套参数重来", task.Params)
	}
}
