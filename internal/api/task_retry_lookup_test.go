// 守重试取快照按**任务键**精确命中，而不是「搜一把再从前几条里挑」。
//
// 任务键互为子串（`scan_series_1` ⊂ `scan_series_1xx`），靠 LIKE 取一页挑的话，目标会被更新的
// 同族键挤到页外：用户看着任务中心里那条「中断，可重试」，点重试却被告知任务不存在。
// 接线之后这条谓词是落盘侧的一句 `task_key = ?`，同族键再多也挤不掉它——本用例守它没退回去。

package api

import (
	"context"
	"fmt"
	"testing"
)

// TestRetrySnapshotFindsTaskCrowdedOutByKinKeys 钉住同族键再多也挤不掉目标那一条。
// 重启后满屏中断运行正是高发场景：同一个类型下每个库/系列各一条，键互为前缀。
func TestRetrySnapshotFindsTaskCrowdedOutByKinKeys(t *testing.T) {
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
			// 同族键各占一条运行、序号都比它新：按序号倒序取时先取到的全是它们。
			for i := range 30 {
				kinKey, kinIdentity := tc.kin(i)
				seedTask(t, controller.taskEngine, taskSeed{Key: kinKey, Identity: kinIdentity, Total: 10, Terminal: "failed"})
			}

			task, err := controller.taskEngine.snapshotForRetry(context.Background(), tc.key)
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

// TestRetrySnapshotPrefersTheNewestRun 守同一个任务键有多次运行时，重试拿的是**最近那一次**。
//
// 重试从此不再抹掉上一次，于是同一个键上会堆着一串历史。拿错一条的后果是重试按着一份过时的
// 入参重来——用户以为在重跑刚才失败的那次，实际重跑的是上礼拜那次。
func TestRetrySnapshotPrefersTheNewestRun(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	const key = "scan_library_7"
	seedTask(t, controller.taskEngine, taskSeed{
		Key: key, Identity: libraryTask("scan_library", 7, variantSole), Total: 100,
		Metadata: map[string]string{"force": "false"}, Terminal: "failed",
	})
	seedTask(t, controller.taskEngine, taskSeed{
		Key: key, Identity: libraryTask("scan_library", 7, variantSole), Total: 100,
		Metadata: map[string]string{"force": "true"}, CanCancel: true,
	})

	task, err := controller.taskEngine.snapshotForRetry(context.Background(), key)
	if err != nil {
		t.Fatalf("取 %q 的重试快照失败: %v", key, err)
	}
	if task.Status != "running" {
		t.Fatalf("取回的状态为 %q, want running —— 拿的是上一次运行，重试会放行一个正在跑的任务", task.Status)
	}
	if task.Params["force"] != "true" {
		t.Fatalf("取回的入参为 %v —— 拿的是上一次运行那份，重试会换一套参数重来", task.Params)
	}
}
