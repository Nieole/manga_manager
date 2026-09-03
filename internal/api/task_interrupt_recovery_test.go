// 守服务重启把**活动态**任务转成**中断**时，任务身上不再留着上一轮的展示态：那句「因服务重启
// 而中断」是**中断**任务唯一该说的话，而上一轮的文案码优先级高于它，会把它整句挡掉。
//
// 反面同样要守：**中断**是可重试的终态，进度、阶段与**重启函数**要读的入参一个都不能跟着清掉，
// 否则用户既看不到断在哪，重试也会回落到默认参数。

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"manga-manager/internal/database"
)

// interruptRecoveredTask 落一条活动态任务、跑一次重启恢复，再从任务列表接口读回它。
// 全程真库、真 SQL、真列表接口：params 列被 SQL 那笔批量 UPDATE 漏掉这件事只有走到这里才看得见。
func interruptRecoveredTask(t *testing.T, status string, params map[string]string) TaskStatus {
	t.Helper()
	controller, store, _, _ := newTestController(t)
	ctx := context.Background()

	libraryID := int64(1)
	startedAt := time.Now().Add(-30 * time.Minute)
	if err := store.UpsertTask(ctx, database.TaskRecord{
		Key:       "scan_library_1",
		Type:      "scan_library",
		Scope:     "library",
		ScopeID:   &libraryID,
		Status:    status,
		Current:   600,
		Total:     10000,
		CanCancel: true,
		Retryable: true,
		Params:    params,
		StartedAt: startedAt,
		UpdatedAt: startedAt.Add(10 * time.Minute),
		Sequence:  1,
	}); err != nil {
		t.Fatalf("落一条 %q 的任务失败: %v", status, err)
	}

	controller.recoverInterruptedTasks()

	rec := httptest.NewRecorder()
	controller.listTasks(rec, httptest.NewRequest(http.MethodGet, "/api/system/tasks", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("列任务返回 %d, body=%s", rec.Code, rec.Body.String())
	}
	var tasks []TaskStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &tasks); err != nil {
		t.Fatalf("解析任务列表失败: %v", err)
	}
	if len(tasks) != 1 || tasks[0].Status != "interrupted" {
		t.Fatalf("读回 %+v, want 一条 interrupted 任务", tasks)
	}
	return tasks[0]
}

// lastActiveDisplayParams 是一条被暂停中的扫描任务落盘时带着的全套展示态，外加**重启函数**
// 要读回的入参与用户要看的进度线索。
func lastActiveDisplayParams() map[string]string {
	return map[string]string{
		"message_code":           "task.msg.control.paused",
		"msgparam.file":          "vol01.zip",
		"pause_reason":           "manual_pause",
		"paused_at":              time.Now().Add(-20 * time.Minute).Format(time.RFC3339Nano),
		"can_pause":              "false",
		"can_resume":             "true",
		"phase":                  "hashing",
		"current_item":           "vol01.zip",
		"force":                  "true",
		"metric.processed_books": "600",
		"io_wait_ms":             "1200",
	}
}

// TestInterruptedTaskDropsLastActiveDisplayState 钉住上一轮的展示态被清干净。
func TestInterruptedTaskDropsLastActiveDisplayState(t *testing.T) {
	cases := []struct{ name, status string }{
		{"运行中被中断", "running"},
		{"已暂停被中断", "paused"},
		{"取消中被中断", "cancelling"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			task := interruptRecoveredTask(t, tc.status, lastActiveDisplayParams())

			if task.MessageCode != "" {
				t.Fatalf("中断任务带着上一轮的文案码 %q —— 前端码优先，「任务因服务重启而中断，可重试」永远走不到",
					task.MessageCode)
			}
			if task.Message == "" {
				t.Fatal("中断任务一句话都没有 —— 码清掉了就得让那句已渲染文案露出来")
			}
			if len(task.MessageParams) != 0 {
				t.Fatalf("中断任务带着上一轮的文案占位参数 %v —— 码都没了，它们没有去处", task.MessageParams)
			}
			if task.PauseReason != "" || task.PausedAt != nil {
				t.Fatalf("中断任务带着暂停原因 %q / 暂停时刻 %v —— 详情面板会渲染成「暂停原因：手动暂停」",
					task.PauseReason, task.PausedAt)
			}
			if task.CanPause || task.CanResume || task.CanCancel {
				t.Fatalf("中断任务还带着控制能力：pause=%v resume=%v cancel=%v —— 它已经不占运行槽位了",
					task.CanPause, task.CanResume, task.CanCancel)
			}
			for _, key := range []string{"message_code", "pause_reason", "paused_at", "can_pause", "can_resume", "msgparam.file"} {
				if _, ok := task.Params[key]; ok {
					t.Fatalf("任务参数里还留着 %q：%v", key, task.Params)
				}
			}
		})
	}
}

// TestInterruptedTaskKeepsProgressAndRetryInputs 钉住那笔清理没有连有用的一起清掉：**中断**可重试，
// 用户要知道断在哪，**重启函数**要读回原始入参。
func TestInterruptedTaskKeepsProgressAndRetryInputs(t *testing.T) {
	task := interruptRecoveredTask(t, "paused", lastActiveDisplayParams())

	if task.Current != 600 || task.Total != 10000 {
		t.Fatalf("中断任务的计数为 %d / %d, want 600 / 10000", task.Current, task.Total)
	}
	if task.Percent == nil || *task.Percent != 6 {
		t.Fatalf("中断任务的百分比为 %v, want 6", task.Percent)
	}
	if !task.Retryable {
		t.Fatal("中断任务不可重试了 —— 它没出错，只是没跑完")
	}
	if task.Phase != "hashing" || task.CurrentItem != "vol01.zip" {
		t.Fatalf("阶段 / 当前条目为 %q / %q, want hashing / vol01.zip —— 用户靠它们知道断在哪", task.Phase, task.CurrentItem)
	}
	if task.Params["force"] != "true" {
		t.Fatalf("重启函数要读的入参没了：%v", task.Params)
	}
	if task.Metrics["processed_books"] != 600 || task.Params["io_wait_ms"] != "1200" {
		t.Fatalf("累计指标被一起清掉了：metrics=%v params=%v", task.Metrics, task.Params)
	}
}
