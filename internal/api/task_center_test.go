// 守任务中心两层结构各自的端点契约：实况区取的是仍会变化的那些运行、不看筛选，任务清单一个任务
// 一行、不带历次运行，展开走运行列表的 task_id 谓词。破了的表现是两层读同一份数据，
// 那正是「手动发起那条被自动运行淹掉」的老路。

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"manga-manager/internal/config"
)

// taskSummaries 打一次任务清单接口。
func taskSummaries(t *testing.T, controller *Controller, query string) []TaskSummary {
	t.Helper()
	rec := httptest.NewRecorder()
	controller.listTaskSummaries(rec, httptest.NewRequest(http.MethodGet, "/api/system/tasks/summary"+query, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("任务清单接口的状态码为 %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var summaries []TaskSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &summaries); err != nil {
		t.Fatalf("解任务清单失败: %v (raw=%s)", err, rec.Body.String())
	}
	return summaries
}

// runsOf 打一次运行列表接口，返回取回的那一页。
func runsOf(t *testing.T, controller *Controller, query string) []RunStatus {
	t.Helper()
	rec := httptest.NewRecorder()
	controller.listTasks(rec, httptest.NewRequest(http.MethodGet, "/api/system/tasks"+query, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("运行列表接口的状态码为 %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var runs []RunStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &runs); err != nil {
		t.Fatalf("解运行列表失败: %v (raw=%s)", err, rec.Body.String())
	}
	return runs
}

// TestTaskSummaryIsOneRowPerTaskWithItsLastRun 守清单一个任务一行：同一个任务跑过三次，
// 清单上仍只有一行，行上写的是最近那一次。破了的表现正是平铺列表——重试三次刷出三行。
func TestTaskSummaryIsOneRowPerTaskWithItsLastRun(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	for i := 0; i < 3; i++ {
		seedTask(t, controller.taskEngine, taskSeed{
			Key: "scan_library_1", Identity: libraryTask("scan_library", 1, variantSole),
			Total: 1, ScopeName: "主库", Terminal: "completed",
		})
	}
	seedTask(t, controller.taskEngine, taskSeed{
		Key: "scan_library_1", Identity: libraryTask("scan_library", 1, variantSole),
		Total: 1, Terminal: "failed", FailError: "boom",
	})

	summaries := taskSummaries(t, controller, "")
	if len(summaries) != 1 {
		t.Fatalf("清单条数为 %d, want 1（四次运行同属一个任务）", len(summaries))
	}
	summary := summaries[0]
	if summary.TaskID == 0 {
		t.Fatal("清单行没有 task_id，展开时无从按需取历次运行")
	}
	if summary.LastRun == nil {
		t.Fatal("清单行没有「上次结果」")
	}
	if summary.LastRun.Status != "failed" {
		t.Fatalf("上次结果为 %q, want failed（最近那一次是失败）", summary.LastRun.Status)
	}
	// 长期属性今天没有写入方，行上因此恒为零值——界面据此整块不显示，而不是画一个「连败 0」。
	if summary.Disabled || summary.FailStreak != 0 || summary.BackoffUntil != nil {
		t.Fatalf("停发那一组属性凭空有了值: %+v", summary)
	}
}

// TestTaskSummaryFiltersOnTheLastRun 守筛选语义只有一个：状态判在这个任务最近一次运行上。
// 判成「有没有哪一次是这个状态」的话，重试成功的库会一直躺在失败清单里。
func TestTaskSummaryFiltersOnTheLastRun(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	seedTask(t, controller.taskEngine, taskSeed{
		Key: "scan_library_1", Identity: libraryTask("scan_library", 1, variantSole),
		Total: 1, Terminal: "failed", FailError: "boom",
	})
	seedTask(t, controller.taskEngine, taskSeed{
		Key: "scan_library_1", Identity: libraryTask("scan_library", 1, variantSole),
		Total: 1, Terminal: "completed",
	})
	seedTask(t, controller.taskEngine, taskSeed{
		Key: "rebuild_index", Identity: systemTask("rebuild_index", variantSole),
		Total: 1, Terminal: "failed", FailError: "boom",
	})

	failed := taskSummaries(t, controller, "?status=failed")
	if len(failed) != 1 || failed[0].Type != "rebuild_index" {
		t.Fatalf("上次失败的任务为 %+v, want 只有 rebuild_index", failed)
	}
	scoped := taskSummaries(t, controller, "?scope=library&scope_id=1")
	if len(scoped) != 1 || scoped[0].Type != "scan_library" {
		t.Fatalf("按作用域筛出 %+v, want 只有 1 号库那个任务", scoped)
	}
}

// TestTaskRunsAreFetchedPerTask 守展开按需取：清单接口一条运行都不带回来，历次运行由
// 运行列表接口带上 task_id 单独取，而且只取到这一个任务的。
func TestTaskRunsAreFetchedPerTask(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	for i := 0; i < 2; i++ {
		seedTask(t, controller.taskEngine, taskSeed{
			Key: "scan_library_1", Identity: libraryTask("scan_library", 1, variantSole), Total: 1, Terminal: "completed",
		})
	}
	seedTask(t, controller.taskEngine, taskSeed{
		Key: "rebuild_index", Identity: systemTask("rebuild_index", variantSole), Total: 1, Terminal: "completed",
	})

	var scanTaskID int64
	for _, summary := range taskSummaries(t, controller, "") {
		if summary.Type == "scan_library" {
			scanTaskID = summary.TaskID
		}
	}
	if scanTaskID == 0 {
		t.Fatal("清单里没有那个扫描任务")
	}

	runs := runsOf(t, controller, "?task_id="+strconv.FormatInt(scanTaskID, 10))
	if len(runs) != 2 {
		t.Fatalf("展开取回 %d 条运行, want 2（只有这个任务的）", len(runs))
	}
	for _, run := range runs {
		if run.Type != "scan_library" {
			t.Fatalf("展开取回了别的任务的运行: %+v", run)
		}
	}
}

// TestLiveFrameIgnoresListFilters 守实况区常驻置顶且不看筛选：它答的是「盘现在在干什么」，
// 那是个全局问题，而顶部那对全部暂停 / 全部恢复同样作用于全体运行。
func TestLiveFrameIgnoresListFilters(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	seedTask(t, controller.taskEngine, taskSeed{
		Key: "scan_library_1", Identity: libraryTask("scan_library", 1, variantSole), Total: 100, CanPause: true,
	})
	seedTask(t, controller.taskEngine, taskSeed{
		Key: "rebuild_index", Identity: systemTask("rebuild_index", variantSole), Total: 1, Terminal: "completed",
	})

	rec := httptest.NewRecorder()
	// 带上一份足以把那条活动运行筛掉的条件：实况帧不该受它影响。
	controller.getTaskCenterLive(rec, httptest.NewRequest(http.MethodGet, "/api/system/tasks/live?status=completed&scope=system", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("实况接口的状态码为 %d, want 200", rec.Code)
	}
	var frame RunLive
	if err := json.Unmarshal(rec.Body.Bytes(), &frame); err != nil {
		t.Fatalf("解实况帧失败: %v (raw=%s)", err, rec.Body.String())
	}

	if len(frame.Runs) != 1 || frame.Runs[0].Key != "scan_library_1" {
		t.Fatalf("实况区里的运行为 %+v, want 只有那条还在跑的", frame.Runs)
	}
	if frame.Active != 1 || frame.Queued != 0 {
		t.Fatalf("实况汇总为 active=%d queued=%d, want 1/0", frame.Active, frame.Queued)
	}
	// 槽位上限恒是一个真的会被撞上的数：界面上那格「槽位 n/N」的分母就是它。
	if frame.Slots != config.DefaultRunSlots {
		t.Fatalf("槽位上限为 %d, want %d", frame.Slots, config.DefaultRunSlots)
	}
	if frame.PausedAll {
		t.Fatal("没人按过全部暂停，闸门却报着关上")
	}
}
