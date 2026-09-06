// 守服务重启把仍会变化的运行（**活动态**与**排队中**）转成**中断**时，运行身上不再留着上一轮的
// 展示态：那句「因服务重启而中断」是**中断**运行唯一该说的话，而上一轮的文案码会把它整句挡掉。
//
// 反面同样要守：**中断**是可重试的终态，进度、阶段与**重启函数**要读的入参一个都不能跟着清掉，
// 否则用户既看不到断在哪，重试也会回落到默认参数。
//
// 全程真库、真列表接口：转写漏掉哪一列这件事只有走到这里才看得见。

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"manga-manager/internal/database"
	"manga-manager/internal/runhandle"
)

// interruptRecoveredTask 起一条活动态运行、模拟一次进程重启，再从任务列表接口读回它。
//
// 「重启」是拿同一份存储另起一个 Controller：**运行时句柄**不跨实例，新实例只能从库里读回那条
// 还写着活动态的运行——正是重启恢复要处置的东西。
func interruptRecoveredTask(t *testing.T, prepare func(handle *runhandle.Handle)) RunStatus {
	t.Helper()
	controller, store, _, tempDir := newTestController(t)

	handle := seedTask(t, controller.taskEngine, taskSeed{
		Key:      "scan_library_1",
		Identity: libraryTask("scan_library", 1, variantSole),
		Total:    10000,
		Metadata: map[string]string{"force": "true"},
		// 上一轮停在「已暂停」，因此带着一整套控制能力与暂停期的展示态。
		CanCancel: true,
		CanPause:  true,
	})
	prepare(handle)
	if err := controller.taskEngine.pause("scan_library_1"); err != nil {
		t.Fatalf("暂停失败: %v", err)
	}

	tasks, _ := restartAndListInterrupted(t, controller, store, tempDir)
	return tasks[0]
}

// restartAndListInterrupted 另起一个 Controller、把上一轮留下的活动运行转成**中断**，再按前端的
// 真实请求把任务列表读回来。解析结果与原始载荷一起交出：某个字段在不在，只有载荷答得出。
//
// 断言库里此刻只剩那一条运行：多出来的那条会让调用方的下标断言落在别的运行上，而它们都只看 [0]。
func restartAndListInterrupted(t *testing.T, prev *Controller, store database.Store, tempDir string) ([]RunStatus, string) {
	t.Helper()
	reloaded := restartController(t, prev, store, tempDir)
	reloaded.taskEngine.markInterrupted(context.Background())

	rec := httptest.NewRecorder()
	reloaded.listTasks(rec, httptest.NewRequest(http.MethodGet, "/api/system/tasks", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("列任务返回 %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	var tasks []RunStatus
	if err := json.Unmarshal([]byte(body), &tasks); err != nil {
		t.Fatalf("解析任务列表失败: %v", err)
	}
	if len(tasks) != 1 || tasks[0].Status != "interrupted" {
		t.Fatalf("读回 %+v, want 一条 interrupted 运行", tasks)
	}
	return tasks, body
}

// lastActiveFrame 是一条扫描运行被按下暂停之前报出的最后一帧：进度、阶段、当前条目与累计指标。
func lastActiveFrame(handle *runhandle.Handle) {
	current := 600
	total := 10000
	handle.Report(runhandle.Frame{
		Current: &current,
		Total:   &total,
		Phase:   "hashing",
		Item:    "vol01.zip",
		Code:    "task.msg.scan_library.progress",
		Params:  map[string]string{"file": "vol01.zip"},
		Metrics: map[string]int64{"processed_books": 600},
	})
	handle.AddMetrics(map[string]int64{"io_wait_ms": 1200}, map[string]string{"io_wait_ms": "1200"})
}

// TestInterruptedTaskDropsLastActiveDisplayState 钉住上一轮的展示态被换成中断那一句。
func TestInterruptedTaskDropsLastActiveDisplayState(t *testing.T) {
	task := interruptRecoveredTask(t, lastActiveFrame)

	if task.MessageCode != taskInterruptedMessageCode {
		t.Fatalf("中断运行的文案码为 %q, want %q —— 留着上一轮的码，「因服务重启而中断」永远走不到",
			task.MessageCode, taskInterruptedMessageCode)
	}
	if len(task.MessageParams) != 0 {
		t.Fatalf("中断运行带着上一轮的文案占位参数 %v —— 码都换了，它们没有去处", task.MessageParams)
	}
	if task.Message != "" {
		t.Fatalf("中断运行带着一句已渲染文案 %q —— 文案只有 i18n 码一种，写死的那句翻译不了", task.Message)
	}
	if task.PausedAt != nil {
		t.Fatalf("中断运行带着暂停时刻 %v —— 它已经不占运行槽位了", task.PausedAt)
	}
	if task.CanPause || task.CanResume || task.CanCancel {
		t.Fatalf("中断运行还带着控制能力：pause=%v resume=%v cancel=%v —— 闸门随进程一起没了",
			task.CanPause, task.CanResume, task.CanCancel)
	}
}

// TestInterruptedTaskKeepsProgressAndRetryInputs 钉住那笔转写没有连有用的一起清掉：**中断**可重试，
// 用户要知道断在哪，**重启函数**要读回原始入参。
func TestInterruptedTaskKeepsProgressAndRetryInputs(t *testing.T) {
	task := interruptRecoveredTask(t, lastActiveFrame)

	if task.Current != 600 || task.Total != 10000 {
		t.Fatalf("中断运行的计数为 %d / %d, want 600 / 10000", task.Current, task.Total)
	}
	if task.Percent == nil || *task.Percent != 6 {
		t.Fatalf("中断运行的百分比为 %v, want 6", task.Percent)
	}
	if !task.Retryable {
		t.Fatal("中断运行不可重试了 —— 它没出错，只是没跑完")
	}
	if task.Phase != "hashing" || task.CurrentItem != "vol01.zip" {
		t.Fatalf("阶段 / 当前条目为 %q / %q, want hashing / vol01.zip —— 用户靠它们知道断在哪", task.Phase, task.CurrentItem)
	}
	if task.Params["force"] != "true" {
		t.Fatalf("重启函数要读的入参没了：%v", task.Params)
	}
	if task.Metrics["processed_books"] != 600 || task.Metrics["io_wait_ms"] != 1200 {
		t.Fatalf("累计指标被一起清掉了：%v", task.Metrics)
	}
}

// TestInterruptedRunIsNotListedTwice 守重启恢复不会把同一条运行既算成活动的又算成中断的：
// 转写改的是那一行本身，不是另起一行。
func TestInterruptedRunIsNotListedTwice(t *testing.T) {
	controller, store, _, tempDir := newTestController(t)
	seedTask(t, controller.taskEngine, taskSeed{
		Key: "scan_library_1", Identity: libraryTask("scan_library", 1, variantSole), Total: 10,
	})

	reloaded := restartController(t, controller, store, tempDir)
	reloaded.taskEngine.markInterrupted(context.Background())

	tasks, err := reloaded.taskEngine.listRunStatuses(context.Background(), taskFilters{})
	if err != nil {
		t.Fatalf("列任务失败: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("读回 %d 条运行, want 1：转写把同一条运行复制成了两行", len(tasks))
	}
}
