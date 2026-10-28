// 守删库成功后的缓存交接，以及占用拒绝时保留资料库与运行。
// 缓存不清会继续显示旧统计和推荐；访问未退出时不能提前删除。

package api

import (
	"context"
	"manga-manager/internal/recommendation"
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
	cacheKey := recommendation.Key(locale, 0)
	controller.recommendations.StoreAt(controller.recommendations.SnapshotGen(), cacheKey,
		[]recommendation.Item{{SeriesID: 1, Title: "stale pick"}})
	if got := controller.recommendations.Cached(cacheKey); len(got) != 1 {
		t.Fatalf("推荐缓存预热失败，got %+v", got)
	}

	req := requestWithRouteParam(http.MethodDelete, "/api/libraries/1", nil, "libraryId", strconv.FormatInt(lib.ID, 10))
	rec := httptest.NewRecorder()
	controller.deleteLibrary(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("deleteLibrary = %d, body=%s", rec.Code, rec.Body.String())
	}
	if got := controller.recommendations.Cached(cacheKey); got != nil {
		t.Fatalf("删库后 AI 推荐缓存仍在（TTL 24h，不清就是一整天的陈旧推荐）: %+v", got)
	}
}

// TestDeleteLibraryWaitsForScopedTask keeps records while cancelled work holds access.
func TestDeleteLibraryWaitsForScopedTask(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	ctx := context.Background()

	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{
		Name: "busy", Path: t.TempDir(), ScanMode: "none", ScanInterval: 60,
	})
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}

	taskKey := "scan_library_" + strconv.FormatInt(lib.ID, 10)
	seedTask(t, controller.taskEngine, taskSeed{Key: taskKey, Identity: libraryTask("scan_library", lib.ID, variantSole), Total: 100, CanCancel: true, CanPause: true})

	req := requestWithRouteParam(http.MethodDelete, "/api/libraries/1", nil, "libraryId", strconv.FormatInt(lib.ID, 10))
	rec := httptest.NewRecorder()
	controller.deleteLibrary(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("deleteLibrary = %d, body=%s", rec.Code, rec.Body.String())
	}

	task := currentTask(t, controller.taskEngine, taskKey)
	if task.Status != "cancelling" {
		t.Fatalf("occupied deletion run status = %q, want cancelling", task.Status)
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
