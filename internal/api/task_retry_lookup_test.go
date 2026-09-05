// 守重试取快照时的 DB 回退是**按任务键的身份查找**，而不是「搜一把再从前几条里挑」。
//
// 内存表是有上限的缓存，重启后更是空的，而**中断**任务恰恰只在库里。任务键互为子串
// （`scan_series_1` ⊂ `scan_series_1xx`），靠 LIKE 取一页挑的话，目标会被更新的同族键挤到页外：
// 用户看着任务中心里那条「中断，可重试」，点重试却被告知任务不存在。

package api

import (
	"context"
	"fmt"
	"testing"
	"time"

	"manga-manager/internal/database"
)

// upsertHistoricTaskRecord 只往库里落一条历史任务，不碰内存表——重启之后的现场就是这样：
// 任务只剩库里那一行。作用域与生产同源，由任务类型与任务键推出。
func upsertHistoricTaskRecord(t *testing.T, store database.Store, key, taskType, status string, sequence int64) {
	t.Helper()
	scope, scopeID := inferTaskScope(taskType, key)
	now := time.Now()
	if err := store.UpsertTask(context.Background(), database.TaskRecord{
		Key:        key,
		Type:       taskType,
		Scope:      scope,
		ScopeID:    scopeID,
		Status:     status,
		Message:    "任务因服务重启而中断，可重试",
		Retryable:  true,
		Current:    3,
		Total:      10,
		Params:     map[string]string{"force": "true"},
		StartedAt:  now.Add(-time.Hour),
		UpdatedAt:  now,
		FinishedAt: &now,
		Sequence:   sequence,
	}); err != nil {
		t.Fatalf("落一条历史任务 %q 失败: %v", key, err)
	}
}

// TestRetrySnapshotFindsTaskCrowdedOutByKinKeys 钉住同族键再多也挤不掉目标那一条。
// 重启后满屏中断任务正是高发场景：同一个类型下每个库/系列各一条，键互为前缀。
func TestRetrySnapshotFindsTaskCrowdedOutByKinKeys(t *testing.T) {
	cases := []struct {
		name     string
		key      string
		taskType string
		kin      func(int) string
	}{
		{
			name: "系列扫描", key: "scan_series_1", taskType: "scan_series",
			kin: func(i int) string { return fmt.Sprintf("scan_series_1%d", i) },
		},
		{
			name: "资料库清理", key: "cleanup_library_2", taskType: "cleanup_library",
			kin: func(i int) string { return fmt.Sprintf("cleanup_library_2%d", i) },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			controller, store, _, _ := newTestController(t)

			// 目标：重启时被转成**中断**的那条，序号最旧。
			upsertHistoricTaskRecord(t, store, tc.key, tc.taskType, "interrupted", 1)
			// 同族键各占一行、序号都比它新：按序号倒序取时先取到的全是它们。
			for i := range 30 {
				upsertHistoricTaskRecord(t, store, tc.kin(i), tc.taskType, "interrupted", int64(i+2))
			}

			task, err := controller.taskEngine.snapshotForRetry(context.Background(), tc.key)
			if err != nil {
				t.Fatalf("取 %q 的重试快照失败: %v —— 任务中心里明明列着它，点重试却是 404", tc.key, err)
			}
			if task.Key != tc.key {
				t.Fatalf("取回的是 %q, want %q —— 重试会去重启另一条任务", task.Key, tc.key)
			}
			if task.Status != "interrupted" || task.Params["force"] != "true" {
				t.Fatalf("取回的 %q 状态为 %q、入参为 %v, want interrupted + force=true", task.Key, task.Status, task.Params)
			}
		})
	}
}

// TestRetrySnapshotPrefersMemoryOverStore 守内存命中时不查库：活动任务的进度只在内存里是最新的。
func TestRetrySnapshotPrefersMemoryOverStore(t *testing.T) {
	controller, store, _, _ := newTestController(t)

	const key = "scan_library_7"
	upsertHistoricTaskRecord(t, store, key, "scan_library", "failed", 1)
	seedTask(t, controller.taskEngine, taskSeed{Key: key, Type: "scan_library", Total: 100, CanCancel: true})

	task, err := controller.taskEngine.snapshotForRetry(context.Background(), key)
	if err != nil {
		t.Fatalf("取 %q 的重试快照失败: %v", key, err)
	}
	if task.Status != "running" {
		t.Fatalf("取回的状态为 %q, want running —— 拿的是滞后的库记录，重试会放行一个正在跑的任务", task.Status)
	}
}
