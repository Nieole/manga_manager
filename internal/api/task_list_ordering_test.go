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

// taskCenterFirstPage 按前端的真实请求取任务中心第一页。
func taskCenterFirstPage(t *testing.T, c *Controller) []RunSnapshot {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/system/tasks?limit=%d", taskCenterPageSize), nil)
	rec := httptest.NewRecorder()
	c.listTasks(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("任务列表返回 %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var tasks []RunSnapshot
	if err := json.NewDecoder(rec.Body).Decode(&tasks); err != nil {
		t.Fatalf("解析任务列表失败: %v", err)
	}
	if len(tasks) > taskCenterPageSize {
		t.Fatalf("第一页返回 %d 条，超过 limit=%d", len(tasks), taskCenterPageSize)
	}
	return tasks
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

// indexOfKey 找出这一页里属于该**任务键**那条身份的运行排在第几位；不在这一页里即 -1。
func indexOfKey(t *testing.T, page []RunSnapshot, want string) int {
	t.Helper()
	for i := range page {
		if belongsToKey(t, page[i], want) {
			return i
		}
	}
	return -1
}

// firstOfPage 交出这一页最前面的几条，供断言失败时说清楚「页首是谁」。
func firstOfPage(page []RunSnapshot, n int) []RunSnapshot {
	return page[:min(n, len(page))]
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

		page := taskCenterFirstPage(t, reloaded)
		for _, want := range []string{"scan_library_7", "rebuild_index"} {
			if indexOfKey(t, page, want) < 0 {
				t.Fatalf("重启后新起的任务 %q 不在第一页里（页首三条：%+v）", want, firstOfPage(page, 3))
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

		page := taskCenterFirstPage(t, controller)
		if indexOfKey(t, page, activeKey) < 0 {
			t.Fatalf("正在运行的任务 %q 被历史挤出了第一页（页首三条：%+v）", activeKey, firstOfPage(page, 3))
		}
	})

	t.Run("历史任务仍按最近活动降序", func(t *testing.T) {
		controller, store, _, tempDir := newTestController(t)
		seedFinishedHistory(t, controller, taskCenterPageSize+10)
		reloaded := restartController(t, controller, store, tempDir)
		seedTask(t, reloaded.taskEngine, taskSeed{Key: "scan_library_7", Identity: libraryTask("scan_library", 7, variantSole), Total: 100, CanCancel: true, CanPause: true})

		page := taskCenterFirstPage(t, reloaded)
		// 历史部分应当是最近完成的那批，且相对顺序为倒序（scan_series_59, 58, ...）。
		history := make([]RunSnapshot, 0, len(page))
		for _, run := range page {
			if !belongsToKey(t, run, "scan_library_7") {
				history = append(history, run)
			}
		}
		for i := range history {
			want := fmt.Sprintf("scan_series_%d", taskCenterPageSize+10-i)
			if !belongsToKey(t, history[i], want) {
				t.Fatalf("历史任务顺序不对：第 %d 条为 %+v, want %q 那条（整页：%+v）", i, history[i], want, page)
			}
		}
	})
}
