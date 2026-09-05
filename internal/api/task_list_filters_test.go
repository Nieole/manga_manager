// 守任务列表的筛选谓词对**两个来源**一视同仁：库记录与盖在它上面的内存快照。
//
// 同键同时存在于内存与库时取内存版（进度要新），而那一版的状态可能已经不满足筛选条件——
// 按「已完成」筛出一条 running 的任务，用户看到的是筛选坏了。

package api

import (
	"context"
	"testing"

	"manga-manager/internal/database"
)

// TestTaskListFiltersApplyToMemoryOverride 钉住内存覆盖那一支同样过筛选。
func TestTaskListFiltersApplyToMemoryOverride(t *testing.T) {
	const key = "scan_library_7"

	cases := []struct {
		name    string
		filters database.TaskFilters
		want    bool
	}{
		{"按状态筛：库里那行是已完成，内存里正在跑", database.TaskFilters{Status: "completed"}, false},
		{"按关键词筛：错误串只留在库里那行上", database.TaskFilters{Query: "disk full"}, false},
		{"按类型筛：内存版类型不变，照常返回", database.TaskFilters{Type: "scan_library"}, true},
		{"内存版仍满足条件时照常返回", database.TaskFilters{Status: "running"}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			controller, store, _, _ := newTestController(t)

			// 库里那行是上一轮留下的：已完成、带着当时的失败线索。
			if err := store.UpsertTask(context.Background(), database.TaskRecord{
				Key:      key,
				Type:     "scan_library",
				Scope:    "library",
				Status:   "completed",
				Error:    "disk full",
				Sequence: 1,
			}); err != nil {
				t.Fatalf("落一条历史任务失败: %v", err)
			}
			// 内存里是同一条键新起的那次：正在跑，没有错误。
			seedTask(t, controller.taskEngine, taskSeed{Key: key, Type: "scan_library", Total: 100, CanCancel: true, CanPause: true})

			items, err := controller.taskEngine.listTaskStatuses(context.Background(), tc.filters)
			if err != nil {
				t.Fatalf("列任务失败: %v", err)
			}

			var found *TaskStatus
			for i := range items {
				if items[i].Key == key {
					found = &items[i]
				}
			}
			if tc.want && found == nil {
				t.Fatalf("筛选 %+v 把内存里那条任务整个滤掉了：它明明满足条件", tc.filters)
			}
			if !tc.want && found != nil {
				t.Fatalf("筛选 %+v 返回了一条状态为 %q、错误为 %q 的任务：筛选对内存覆盖那一支没生效",
					tc.filters, found.Status, found.Error)
			}
		})
	}
}
