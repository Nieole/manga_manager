// 守删库的收尾动作：三份进程内缓存（看板统计、阅读路径的归档来源/页清单、AI 推荐）必须
// 随之失效，跑在该库上的任务必须先被请求取消，缩短往已删库继续写入的窗口。
// 缓存不清，用户删库后看到的还是旧数字与旧推荐，而推荐缓存的 TTL 是 24 小时，没有更快的自愈路径。

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
	seedTask(t, controller.taskEngine, taskSeed{Key: taskKey, Type: "scan_library", Total: 100, CanCancel: true, CanPause: true})

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
