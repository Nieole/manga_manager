// 守任务中心第一页的定序：活动态任务永不被历史挤掉，重启后新起的任务排在旧记录之前。
//
// 这一页是用户观察后台任务的唯一入口，前端固定只取 50 条且原样采用后端顺序——
// 排到 50 名之外等同于「任务没跑起来」，而日志与接口都不会有任何异常迹象。

package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"manga-manager/internal/database"
	"manga-manager/internal/scanner"
)

// taskCenterPageSize 与前端 BackgroundTasks 页固定发出的 limit 一致。
const taskCenterPageSize = 50

// restartController 用同一份存储另起一个 Controller，模拟进程重启：走与生产同源的装配。
// **运行时句柄**不跨实例，新实例只能从库里读回历史——运行本身早已落盘，不需要先刷一遍。
func restartController(t *testing.T, prev *Controller, store database.Store, tempDir string) *Controller {
	t.Helper()
	cfg := prev.config
	reloaded := newControllerCore(store, scanner.NewScanner(store, cfg), cfg,
		filepath.Join(tempDir, "config.yaml"), controllerCacheSizes{
			imageBytes: 8 << 10, page: 8, bookPageSource: 8, progressWrite: 8,
		})
	// 关掉它再让用例收尾：重启恢复会为**可续跑**的类型真的发起一次运行，而那条运行跑在后台
	// goroutine 上。不等它停下，任务体会在存储已经关掉之后还在写。
	t.Cleanup(reloaded.Close)
	return reloaded
}

// taskCenterFirstPage 按前端的真实请求取任务中心第一页，返回任务键。
func taskCenterFirstPage(t *testing.T, c *Controller) []string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/system/tasks?limit=%d", taskCenterPageSize), nil)
	rec := httptest.NewRecorder()
	c.listTasks(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("任务列表返回 %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var tasks []RunStatus
	if err := json.NewDecoder(rec.Body).Decode(&tasks); err != nil {
		t.Fatalf("解析任务列表失败: %v", err)
	}
	if len(tasks) > taskCenterPageSize {
		t.Fatalf("第一页返回 %d 条，超过 limit=%d", len(tasks), taskCenterPageSize)
	}
	keys := make([]string, 0, len(tasks))
	for _, task := range tasks {
		keys = append(keys, task.Key)
	}
	return keys
}

// seedFinishedHistory 灌入 n 条已完成的历史运行，键为 scan_series_<i>。
// 作用域 id 从 1 起：系列级身份没有 0 号系列，四要素的自洽校验会当场拒了它。
func seedFinishedHistory(t *testing.T, c *Controller, n int) {
	t.Helper()
	for i := range n {
		seedTask(t, c.taskEngine, taskSeed{
			Key: fmt.Sprintf("scan_series_%d", i+1), Identity: seriesTask("scan_series", int64(i+1), variantSole),
			Total: 1, Terminal: "completed",
		})
	}
}

func indexOfKey(keys []string, want string) int {
	for i, key := range keys {
		if key == want {
			return i
		}
	}
	return -1
}

// TestTaskCenterFirstPageOrdering 守任务中心第一页在「历史比一页还多」时仍然可用。
func TestTaskCenterFirstPageOrdering(t *testing.T) {
	t.Run("重启后新起的任务出现在第一页", func(t *testing.T) {
		controller, store, _, tempDir := newTestController(t)
		// 历史比一页多：真实库里一次大库扫描就能留下几千条。
		seedFinishedHistory(t, controller, taskCenterPageSize+10)

		reloaded := restartController(t, controller, store, tempDir)
		seedTask(t, reloaded.taskEngine, taskSeed{Key: "rebuild_index", Identity: systemTask("rebuild_index", variantSole), Total: 1, Terminal: "completed"})
		seedTask(t, reloaded.taskEngine, taskSeed{Key: "scan_library_7", Identity: libraryTask("scan_library", 7, variantSole), Total: 100, CanCancel: true, CanPause: true})

		keys := taskCenterFirstPage(t, reloaded)
		for _, want := range []string{"scan_library_7", "rebuild_index"} {
			if indexOfKey(keys, want) < 0 {
				t.Fatalf("重启后新起的任务 %q 不在第一页里（页首三条：%v）", want, keys[:min(3, len(keys))])
			}
		}
	})

	t.Run("活动态任务不被历史挤出第一页", func(t *testing.T) {
		controller, _, _, _ := newTestController(t)

		const activeKey = "scan_library_7"
		seedTask(t, controller.taskEngine, taskSeed{Key: activeKey, Identity: libraryTask("scan_library", 7, variantSole), Total: 100, CanCancel: true, CanPause: true})
		// 活动任务先启动，序号因此最小；之后大量短任务跑完。真实场景是大库扫描的哈希阶段
		// 长时间不上报进度，被后来的短任务全部超过。
		seedFinishedHistory(t, controller, taskCenterPageSize+10)

		keys := taskCenterFirstPage(t, controller)
		if indexOfKey(keys, activeKey) < 0 {
			t.Fatalf("正在运行的任务 %q 被历史挤出了第一页（页首三条：%v）", activeKey, keys[:min(3, len(keys))])
		}
	})

	t.Run("历史任务仍按最近活动降序", func(t *testing.T) {
		controller, store, _, tempDir := newTestController(t)
		seedFinishedHistory(t, controller, taskCenterPageSize+10)
		reloaded := restartController(t, controller, store, tempDir)
		seedTask(t, reloaded.taskEngine, taskSeed{Key: "scan_library_7", Identity: libraryTask("scan_library", 7, variantSole), Total: 100, CanCancel: true, CanPause: true})

		keys := taskCenterFirstPage(t, reloaded)
		// 历史部分应当是最近完成的那批，且相对顺序为倒序（scan_series_59, 58, ...）。
		history := make([]string, 0, len(keys))
		for _, key := range keys {
			if key != "scan_library_7" {
				history = append(history, key)
			}
		}
		want := make([]string, 0, len(history))
		for i := taskCenterPageSize + 10; i > taskCenterPageSize+10-len(history); i-- {
			want = append(want, fmt.Sprintf("scan_series_%d", i))
		}
		for i := range history {
			if history[i] != want[i] {
				t.Fatalf("历史任务顺序不对：第 %d 条为 %q, want %q（整页：%v）", i, history[i], want[i], keys)
			}
		}
	})
}
