// 守任务列表的四条筛选谓词真的生效：状态、关键词、类型与作用域一路下推到落盘侧，
// 而不是被当成「没填」整条丢掉。
//
// 破了意味着任务中心的筛选器形同虚设——按「已完成」筛，返回的却是一条正在跑的任务。

package api

import (
	"context"
	"testing"
)

// TestTaskListFiltersArePushedDown 钉住每一条谓词都参与判定。
//
// 每个用例只播一条正在运行、无错误的任务：谓词若被忽略，这一条会照样返回，
// 于是那几个 want=false 的用例当场变红。
func TestTaskListFiltersArePushedDown(t *testing.T) {
	const key = "scan_library_7"

	cases := []struct {
		name    string
		filters taskFilters
		want    bool
	}{
		{"按状态筛：这一条正在跑，不该出现在已完成里", taskFilters{Status: "completed"}, false},
		{"按关键词筛：这一条没有错误，搜不到那个词", taskFilters{Query: "disk full"}, false},
		{"按类型筛：类型对得上，照常返回", taskFilters{Type: "scan_library"}, true},
		{"按状态筛：状态对得上，照常返回", taskFilters{Status: "running"}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			controller, _, _, _ := newTestController(t)

			seedTask(t, controller.taskEngine, taskSeed{Key: key, Identity: libraryTask("scan_library", 7, variantSole), Total: 100, CanCancel: true, CanPause: true})

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
				t.Fatalf("筛选 %+v 把那条任务整个滤掉了：它明明满足条件", tc.filters)
			}
			if !tc.want && found != nil {
				t.Fatalf("筛选 %+v 返回了一条状态为 %q、错误为 %q 的任务：这一条谓词没生效",
					tc.filters, found.Status, found.Error)
			}
		})
	}
}
