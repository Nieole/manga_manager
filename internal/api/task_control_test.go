// 守暂停/恢复/取消三个端点的错误语义：引擎只返回控制哨兵错误，每个哨兵都必须在
// taskControlResponses 里有自己的状态码与文案。
//
// 漏映射一个哨兵不会有编译错误，只会退化成 500 "Task control failed"——任务面板按 404/409
// 分别提示，用户于是只看到一句「操作失败」，无从知道任务是不在了还是状态不对。

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// TestTaskControlErrorMapping 把任务引擎的每个控制哨兵错误钉到它的 HTTP 状态码与响应文案上。
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

// TestRunControlEndpointsRejectUnknownRun 覆盖三个端点在运行不存在时的 404。
func TestRunControlEndpointsRejectUnknownRun(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	endpoints := map[string]http.HandlerFunc{
		"pause":  controller.pauseRun,
		"resume": controller.resumeRun,
		"cancel": controller.cancelRun,
	}

	for name, handler := range endpoints {
		t.Run(name, func(t *testing.T) {
			req := requestWithRouteParam(http.MethodPost, "/api/system/runs/4242/"+name, nil, "runID", "4242")
			rec := httptest.NewRecorder()
			handler(rec, req)

			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404 (body=%s)", rec.Code, rec.Body.String())
			}
		})
	}
}

// TestResumeRunRejectsRunningRun 覆盖 errTaskNotPaused 这条唯一属于 resume 的分支：
// 对着一条正在跑的运行调 resume 必须是 409，而不是把它当成幂等的空操作。
func TestResumeRunRejectsRunningRun(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	taskKey := "rebuild_index"
	seedTask(t, controller.taskEngine, taskSeed{Key: taskKey, Identity: systemTask("rebuild_index", variantSole), Total: 1, CanCancel: true, CanPause: true})
	runID := currentTask(t, controller.taskEngine, taskKey).RunID

	req := requestWithRouteParam(http.MethodPost, "/api/system/runs/1/resume", nil, "runID", strconv.FormatInt(runID, 10))
	rec := httptest.NewRecorder()
	controller.resumeRun(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body=%s)", rec.Code, rec.Body.String())
	}
}

// TestMissingRunIDIsBadRequest 保证三个端点在路由参数不是一个运行 id 时仍是 400，
// 而不是落进控制错误映射。
func TestMissingRunIDIsBadRequest(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	endpoints := map[string]http.HandlerFunc{
		"pause":  controller.pauseRun,
		"resume": controller.resumeRun,
		"cancel": controller.cancelRun,
	}

	for name, handler := range endpoints {
		t.Run(name, func(t *testing.T) {
			req := requestWithRouteParam(http.MethodPost, "/api/system/runs//"+name, nil, "runID", "")
			rec := httptest.NewRecorder()
			handler(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
			}
		})
	}
}
