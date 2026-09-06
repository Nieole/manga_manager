// 守任务列表的筛选谓词真的下推到落盘侧：状态、关键词、类型与作用域各自都参与判定，
// 而且是**双向**的——该留的留下，该滤的滤掉。
//
// 破了意味着任务中心的筛选器形同虚设：按「已完成」筛，返回的却是一条正在跑的任务。

package api

import (
	"context"
	"slices"
	"testing"
)

// TestTaskListFiltersArePushedDown 每个用例都断言两条任务的去留，单向的谓词漏判逃不掉。
//
// 播两条：一条已失败、带着「disk full」这条线索，一条还在跑、没有错误。任何一条谓词若被
// 整条忽略，返回的就是两条；若被判反，返回的就是另一条。
func TestTaskListFiltersArePushedDown(t *testing.T) {
	const failedKey = "scan_library_7"
	const runningKey = "scan_library_8"
	runningScopeID := int64(8)

	cases := []struct {
		name    string
		filters taskFilters
		want    []string
	}{
		{"按状态筛：只留已失败那条", taskFilters{Status: "failed"}, []string{failedKey}},
		{"按状态筛：只留正在跑那条", taskFilters{Status: "running"}, []string{runningKey}},
		{"按关键词筛：错误串只落在失败那条上", taskFilters{Query: "disk full"}, []string{failedKey}},
		{"按类型筛：两条同类型，都留下", taskFilters{Type: "scan_library"}, []string{failedKey, runningKey}},
		{"按类型筛：换个类型，一条都不剩", taskFilters{Type: "scrape_series"}, nil},
		{"按作用域 id 筛：只留那个库的", taskFilters{Scope: "library", ScopeID: &runningScopeID}, []string{runningKey}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			controller, _, _, _ := newTestController(t)

			seedTask(t, controller.taskEngine, taskSeed{
				Key: failedKey, Identity: libraryTask("scan_library", 7, variantSole),
				Total: 100, Terminal: "failed", FailError: "disk full",
			})
			seedTask(t, controller.taskEngine, taskSeed{
				Key: runningKey, Identity: libraryTask("scan_library", 8, variantSole),
				Total: 100, CanCancel: true, CanPause: true,
			})

			items, err := controller.taskEngine.listTaskStatuses(context.Background(), tc.filters)
			if err != nil {
				t.Fatalf("列任务失败: %v", err)
			}

			for _, key := range tc.want {
				if !slices.ContainsFunc(items, func(item TaskStatus) bool { return item.Key == key }) {
					t.Errorf("筛选 %+v 把 %s 滤掉了：它满足条件", tc.filters, key)
				}
			}
			for _, item := range items {
				if !slices.Contains(tc.want, item.Key) {
					t.Errorf("筛选 %+v 返回了 %s（状态 %q、错误 %q）：这一条谓词没生效",
						tc.filters, item.Key, item.Status, item.Error)
				}
			}
		})
	}
}
