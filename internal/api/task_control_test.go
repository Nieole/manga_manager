package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestTaskControlErrorMapping 把任务引擎的每个控制哨兵错误钉到它的 HTTP 状态码与响应文案上。
//
// 暂停/恢复/取消三个端点原本各自内联了一串 jsonError 分支；现在引擎只返回哨兵错误，
// 由 taskControlResponses 做映射。这张表是那次收口的回归护栏：漏映射某个哨兵会退化成
// 500 "Task control failed"，而客户端（任务面板）是按 404/409 分别提示的。
func TestTaskControlErrorMapping(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantBody   string
	}{
		{"not found", errTaskNotFound, http.StatusNotFound, "Task not found"},
		{"not running", errTaskNotRunning, http.StatusConflict, "Task is not running"},
		{"not paused", errTaskNotPaused, http.StatusConflict, "Task is not paused"},
		{"not pausable", errTaskNotPausable, http.StatusConflict, "Task cannot be paused"},
		{"not cancelable", errTaskNotCancelable, http.StatusConflict, "Task cannot be cancelled"},
		{"gate unavailable", errTaskGateUnavailable, http.StatusConflict, "Task pause gate is not available"},
		{"cancel unavailable", errTaskCancelUnavailable, http.StatusConflict, "Task cancellation is not available"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			writeTaskControlError(rec, tc.err)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			var payload map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode body: %v (raw=%s)", err, rec.Body.String())
			}
			if payload["error"] != tc.wantBody {
				t.Fatalf("error = %q, want %q", payload["error"], tc.wantBody)
			}
		})
	}
}

// TestTaskControlEndpointsRejectUnknownTask 覆盖三个端点在任务不存在时的 404。
// 此前只有「不可暂停/不可取消」两条 409 路径有用例，404 分支无覆盖。
func TestTaskControlEndpointsRejectUnknownTask(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	endpoints := map[string]http.HandlerFunc{
		"pause":  controller.pauseTask,
		"resume": controller.resumeTask,
		"cancel": controller.cancelTask,
	}

	for name, handler := range endpoints {
		t.Run(name, func(t *testing.T) {
			req := requestWithRouteParam(http.MethodPost, "/api/system/tasks/nope/"+name, nil, "taskKey", "nope")
			rec := httptest.NewRecorder()
			handler(rec, req)

			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404 (body=%s)", rec.Code, rec.Body.String())
			}
		})
	}
}

// TestResumeTaskRejectsRunningTask 覆盖 errTaskNotPaused 这条唯一属于 resume 的分支：
// 对着一个正在跑的任务调 resume 必须是 409，而不是把它当成幂等的空操作。
func TestResumeTaskRejectsRunningTask(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	taskKey := "rebuild_index"
	if !controller.taskEngine.startPausableCancelableTask(taskKey, "rebuild_index", "running", 1) {
		t.Fatal("expected task to start")
	}

	req := requestWithRouteParam(http.MethodPost, "/api/system/tasks/rebuild_index/resume", nil, "taskKey", taskKey)
	rec := httptest.NewRecorder()
	controller.resumeTask(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body=%s)", rec.Code, rec.Body.String())
	}
}

// TestMissingTaskKeyIsBadRequest 保证三个端点在缺少路由参数时仍是 400 而非落进控制错误映射。
func TestMissingTaskKeyIsBadRequest(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	endpoints := map[string]http.HandlerFunc{
		"pause":  controller.pauseTask,
		"resume": controller.resumeTask,
		"cancel": controller.cancelTask,
	}

	for name, handler := range endpoints {
		t.Run(name, func(t *testing.T) {
			req := requestWithRouteParam(http.MethodPost, "/api/system/tasks//"+name, nil, "taskKey", "")
			rec := httptest.NewRecorder()
			handler(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
			}
		})
	}
}
