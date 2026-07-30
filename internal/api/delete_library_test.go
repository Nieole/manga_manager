// 业务说明：本文件守卫删库时的收尾动作。
//
// deleteLibrary 会级联删掉整个库的数据，但三份进程内缓存（看板统计、阅读路径的归档来源/页清单、
// AI 推荐）都是以「库里有哪些书」为前提算出来的。不失效它们，用户在删库后看到的仍是旧数字与
// 旧推荐，且没有任何自愈路径——统计缓存有 TTL，推荐缓存的 TTL 是 24 小时。

package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"manga-manager/internal/database"
)

func TestDeleteLibraryInvalidatesCaches(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	ctx := context.Background()

	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{
		Name: "to-delete", Path: t.TempDir(), ScanMode: "none", ScanInterval: 60,
	})
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}

	// 预热三份缓存。
	const locale = "zh-CN"
	cacheKey := recommendationCacheKey(locale, 0)
	controller.recommendations.storeAt(controller.recommendations.snapshotGen(), cacheKey,
		[]AIRecommendationResponse{{SeriesID: 1, Title: "stale pick"}})
	if got := controller.cachedRecommendations(cacheKey); len(got) != 1 {
		t.Fatalf("推荐缓存预热失败，got %+v", got)
	}

	req := requestWithRouteParam(http.MethodDelete, "/api/libraries/1", nil, "libraryId", strconv.FormatInt(lib.ID, 10))
	rec := httptest.NewRecorder()
	controller.deleteLibrary(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("deleteLibrary = %d, body=%s", rec.Code, rec.Body.String())
	}
	if got := controller.cachedRecommendations(cacheKey); got != nil {
		t.Fatalf("删库后 AI 推荐缓存仍在（TTL 24h，不清就是一整天的陈旧推荐）: %+v", got)
	}
}

// TestDeleteLibraryCancelsScopedTask 覆盖「删库前取消该库在跑的扫描任务」。
func TestDeleteLibraryCancelsScopedTask(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	ctx := context.Background()

	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{
		Name: "busy", Path: t.TempDir(), ScanMode: "none", ScanInterval: 60,
	})
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}

	taskKey := "scan_library_" + strconv.FormatInt(lib.ID, 10)
	if !controller.taskEngine.startPausableCancelableTask(taskKey, "scan_library", "scanning", 100) {
		t.Fatal("任务未能启动")
	}
	controller.taskEngine.newTaskContext(taskKey) // 登记 runtime，cancel 才有 ctx 可取消

	req := requestWithRouteParam(http.MethodDelete, "/api/libraries/1", nil, "libraryId", strconv.FormatInt(lib.ID, 10))
	rec := httptest.NewRecorder()
	controller.deleteLibrary(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("deleteLibrary = %d, body=%s", rec.Code, rec.Body.String())
	}

	controller.taskEngine.mutex.Lock()
	task := controller.taskEngine.tasks[taskKey]
	controller.taskEngine.mutex.Unlock()
	if task.Status != "cancelling" {
		t.Fatalf("该库的扫描任务状态 = %q, want cancelling（删库应先请求取消，缩短往已删库写入的窗口）", task.Status)
	}
}

// TestDeleteLibraryWithoutActiveTaskStillSucceeds 保证没有可取消任务时删库不受影响。
func TestDeleteLibraryWithoutActiveTaskStillSucceeds(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	ctx := context.Background()

	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{
		Name: "idle", Path: t.TempDir(), ScanMode: "none", ScanInterval: 60,
	})
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}

	req := requestWithRouteParam(http.MethodDelete, "/api/libraries/1", nil, "libraryId", strconv.FormatInt(lib.ID, 10))
	rec := httptest.NewRecorder()
	controller.deleteLibrary(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("deleteLibrary = %d, body=%s", rec.Code, rec.Body.String())
	}
	if _, err := store.GetLibrary(ctx, lib.ID); err == nil {
		t.Fatal("资料库行仍然存在")
	}
}
