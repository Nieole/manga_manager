// 守顶部那对按钮的端点契约与诊断接口那个暂停字段的**语义**：全部暂停作用在**运行**上，
// 因此不按**任务键**寻址、没有 404；而 `paused` 回答的是「有没有运行被暂停」，
// 不再是 `storageio` 后台暂停那个开关。

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// bulkControl 打一次全部暂停 / 全部恢复，返回响应里那个计数。
func bulkControl(t *testing.T, handler http.HandlerFunc, path, countField string) int {
	t.Helper()
	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodPost, path, nil))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("%s 的状态码为 %d, want 202 (body=%s)", path, rec.Code, rec.Body.String())
	}
	var payload map[string]int
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("解 %s 的响应失败: %v (raw=%s)", path, err, rec.Body.String())
	}
	return payload[countField]
}

// diagnosticsPaused 取诊断接口那个暂停字段。
func diagnosticsPaused(t *testing.T, controller *Controller) bool {
	t.Helper()
	rec := httptest.NewRecorder()
	controller.getStorageIODiagnostics(rec, httptest.NewRequest(http.MethodGet, "/api/system/storage-io", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("诊断接口的状态码为 %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var response StorageIODiagnosticsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("解诊断响应失败: %v (raw=%s)", err, rec.Body.String())
	}
	return response.Paused
}

// TestPauseAllTurnsRunningIntoPaused 守按下全部暂停之后状态**如实**变成已暂停，
// 而不是继续写着运行中却一动不动——那正是本票要消灭的那一幕。
func TestPauseAllTurnsRunningIntoPaused(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	const pausableKey = "scan_library_1"
	const stubbornKey = "write_comicinfo_2"
	seedTask(t, controller.taskEngine, taskSeed{
		Key: pausableKey, Identity: libraryTask("scan_library", 1, variantSole), Total: 100, CanPause: true, CanCancel: true,
	})
	seedTask(t, controller.taskEngine, taskSeed{
		Key: stubbornKey, Identity: libraryTask("write_comicinfo", 2, variantSole), Total: 100,
	})

	if diagnosticsPaused(t, controller) {
		t.Fatal("一条都没暂停时诊断接口就说有运行被暂停了")
	}

	paused := bulkControl(t, controller.pauseAllTasks, "/api/system/tasks/pause-all", "paused")
	if paused != 1 {
		t.Fatalf("按下的条数为 %d, want 1（不可暂停的那条不受影响）", paused)
	}
	if got := currentTask(t, controller.taskEngine, pausableKey); got.Status != "paused" {
		t.Fatalf("可暂停的运行状态为 %q, want paused", got.Status)
	}
	// 不可暂停的运行在界面上另有说明，这里守的是它一动不动。
	if got := currentTask(t, controller.taskEngine, stubbornKey); got.Status != "running" {
		t.Fatalf("不可暂停的运行状态为 %q, want running", got.Status)
	}
	if !diagnosticsPaused(t, controller) {
		t.Fatal("有运行被暂停，诊断接口的暂停字段却说没有")
	}

	resumed := bulkControl(t, controller.resumeAllTasks, "/api/system/tasks/resume-all", "resumed")
	if resumed != 1 {
		t.Fatalf("放行的条数为 %d, want 1", resumed)
	}
	if got := currentTask(t, controller.taskEngine, pausableKey); got.Status != "running" {
		t.Fatalf("恢复后状态为 %q, want running", got.Status)
	}
	if diagnosticsPaused(t, controller) {
		t.Fatal("全部恢复之后诊断接口仍说有运行被暂停")
	}
}

// TestPauseAllOnAnIdleSystemIsNotAnError 守没有活动运行时全部暂停照样是 202、条数为 0：
// 它不是「按不下去」，只是没有可按的。
func TestPauseAllOnAnIdleSystemIsNotAnError(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	if paused := bulkControl(t, controller.pauseAllTasks, "/api/system/tasks/pause-all", "paused"); paused != 0 {
		t.Fatalf("空系统上按下的条数为 %d, want 0", paused)
	}
	if resumed := bulkControl(t, controller.resumeAllTasks, "/api/system/tasks/resume-all", "resumed"); resumed != 0 {
		t.Fatalf("空系统上放行的条数为 %d, want 0", resumed)
	}
}

// TestPauseAllReportsWhoPressedIt 守对外的 `pause_reason` 重新有话可说：
// 全部暂停按下的那些写 pause_all，单条暂停写 manual，恢复之后一个字都不剩。
func TestPauseAllReportsWhoPressedIt(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	const bulkKey = "scan_library_1"
	const singleKey = "scan_library_2"
	seedTask(t, controller.taskEngine, taskSeed{
		Key: bulkKey, Identity: libraryTask("scan_library", 1, variantSole), Total: 100, CanPause: true,
	})
	seedTask(t, controller.taskEngine, taskSeed{
		Key: singleKey, Identity: libraryTask("scan_library", 2, variantSole), Total: 100, CanPause: true,
	})

	if err := controller.taskEngine.pause(singleKey); err != nil {
		t.Fatalf("单条暂停失败: %v", err)
	}
	bulkControl(t, controller.pauseAllTasks, "/api/system/tasks/pause-all", "paused")

	if got := currentTask(t, controller.taskEngine, bulkKey).PauseReason; got != "pause_all" {
		t.Fatalf("全部暂停按下的那条暂停原因为 %q, want pause_all", got)
	}
	if got := currentTask(t, controller.taskEngine, singleKey).PauseReason; got != "manual" {
		t.Fatalf("单条暂停原因为 %q, want manual", got)
	}

	bulkControl(t, controller.resumeAllTasks, "/api/system/tasks/resume-all", "resumed")
	for _, key := range []string{bulkKey, singleKey} {
		if got := currentTask(t, controller.taskEngine, key).PauseReason; got != "" {
			t.Fatalf("恢复之后任务 %q 仍带着暂停原因 %q", key, got)
		}
	}
}
