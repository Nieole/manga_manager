// 守「查看日志」按钮点开有内容：任务体沿途的日志经任务 ctx 自动带上**任务键**，按它过滤拿得到非空结果。
// 破了的表现是那个按钮对绝大多数任务返回空列表。
// 另一半是边界：不属于任何任务的扫描（守护 / watcher / 首扫）日志不带任务键，那是对的。

package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/logger"
	"manga-manager/internal/runhandle"
	"manga-manager/internal/scanner"
	"manga-manager/internal/task"
)

// scanFailingStore 让资料库扫描在「加载已入库文件快照」那一步失败。
//
// 挑这一步是因为它在扫描器内部、而且返回错误就直接中止整次扫描：任务因此真的以**失败**收尾，
// 同时那条 slog.WarnContext 是从任务体深处而不是启动点发出的——要守的正是这段路。
type scanFailingStore struct {
	database.Store
	err error
}

func (s scanFailingStore) ListBooksByLibrary(context.Context, int64) ([]database.ListBooksByLibraryRow, error) {
	return nil, s.err
}

// captureLogsInto 把全局 logger 接到 path，用与生产同一层 ctx handler 包着，用完还原。
//
// 不调 logger.Init：它会把包级的日志文件路径钉在这个用例的临时目录上，之后同包别的用例
// 读到的就是一个已被删掉的路径。查看接口在没有 Init 时按数据目录推导，正好落到这里。
func captureLogsInto(t *testing.T, path string) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("建日志文件失败: %v", err)
	}
	t.Cleanup(func() { _ = file.Close() })

	previous := slog.Default()
	slog.SetDefault(slog.New(logger.NewContextHandler(
		slog.NewTextHandler(file, &slog.HandlerOptions{Level: slog.LevelDebug}),
	)))
	t.Cleanup(func() { slog.SetDefault(previous) })
}

// newScanFailureRig 装一个「扫描必定失败」的控制器：任务体同步执行，日志落到查看接口会读的那个文件。
func newScanFailureRig(t *testing.T) (*Controller, string) {
	t.Helper()

	controller, store, _, _ := newTestController(t)
	controller.taskEngine.runBackground = runTaskBodySynchronously
	controller.scanner = scanner.NewScanner(
		scanFailingStore{Store: store, err: errors.New("library rows unreadable")},
		controller.config,
	)

	logPath := filepath.Join(filepath.Dir(controller.currentConfig().Database.Path), "manga_manager.log")
	captureLogsInto(t, logPath)

	return controller, logPath
}

// queryLogsByTaskKey 走查看接口按任务键过滤，口径与「查看日志」按钮完全一致。
func queryLogsByTaskKey(t *testing.T, controller *Controller, taskKey string) LogsResponse {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/api/system/logs?level=ALL&task_key="+taskKey, nil)
	rec := httptest.NewRecorder()
	controller.getSystemLogs(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("日志接口返回 %d: %s", rec.Code, rec.Body.String())
	}

	var response LogsResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("解析日志响应失败: %v", err)
	}
	return response
}

func TestFailedLibraryScanLogsAreFilterableByTaskKey(t *testing.T) {
	controller, _ := newScanFailureRig(t)

	lib := database.Library{ID: 7, Name: "Main", Path: filepath.Join(t.TempDir(), "main")}
	if err := controller.launchLibraryScanTask(lib, true, task.TriggerManual); err != nil {
		t.Fatalf("启动资料库扫描失败: %v", err)
	}

	const key = "scan_library_7"
	status := currentTask(t, controller.taskEngine, key).Status
	if status != "failed" {
		t.Fatalf("扫描任务终态为 %q, want failed —— 这个用例要守的是一次**失败**的扫描", status)
	}

	response := queryLogsByTaskKey(t, controller, key)
	if len(response.Items) == 0 {
		t.Fatal("按任务键过滤日志得到空列表：「查看日志」按钮点开还是没有内容")
	}
	for _, item := range response.Items {
		if !strings.Contains(item.Raw, logger.TaskKeyAttr+"="+key) {
			t.Fatalf("过滤结果里混进了不带该任务键的行: %q", item.Raw)
		}
	}
}

// TestAutomaticScanLogsCarryTaskKeyAndRunID 守自动发起的扫描不再是「无归属」的那一类：
// 守护扫描跑在它自己那条运行的 ctx 上，因此扫描器深处那行日志同样带着**任务键**与运行标识。
//
// 这是这段路唯一的判据。同一段扫描器代码，带不带只由跑在谁的 ctx 上决定——守护扫描退回
// context.Background() 的话，用户半夜听见盘响，「查看日志」按运行过滤出来仍是空的。
func TestAutomaticScanLogsCarryTaskKeyAndRunID(t *testing.T) {
	controller, logPath := newScanFailureRig(t)

	lib, err := controller.store.CreateLibrary(context.Background(), database.CreateLibraryParams{
		Name:         "Main",
		Path:         filepath.Join(t.TempDir(), "main"),
		ScanMode:     "interval",
		ScanInterval: 60,
		ScanFormats:  config.DefaultScanFormatsCSV,
	})
	if err != nil {
		t.Fatalf("建资料库失败: %v", err)
	}

	controller.dispatchScheduledScans(context.Background(), time.Now(), map[int64]time.Time{})

	key := "scan_library_" + strconv.FormatInt(lib.ID, 10)
	run := currentTask(t, controller.taskEngine, key)
	if run.Trigger != string(task.TriggerScheduled) {
		t.Fatalf("守护扫描的发起方为 %q, want scheduled", run.Trigger)
	}

	written, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatalf("读日志文件失败: %v", readErr)
	}
	if !strings.Contains(string(written), "Failed to load existing books cache") {
		t.Fatalf("扫描失败那条日志没落盘，用例什么也没守到:\n%s", written)
	}
	if !strings.Contains(string(written), logger.TaskKeyAttr+"="+key) {
		t.Fatalf("守护扫描的日志不带任务键:\n%s", written)
	}
	if !strings.Contains(string(written), logger.RunIDAttr+"="+strconv.FormatInt(run.RunID, 10)) {
		t.Fatalf("守护扫描的日志不带运行标识:\n%s", written)
	}
}

// TestTaskBodyContextCarriesTaskKey 守启动入口那一下：任务体拿到的 ctx 带着自己的**任务键**。
// 它是三类任务共用的那一处——任务体沿途每一行带 ctx 的日志都从这里取键，掉了就全都不带。
func TestTaskBodyContextCarriesTaskKey(t *testing.T) {
	cases := []struct {
		name     string
		identity TaskIdentity
		key      string
	}{
		{"资料库扫描", libraryTask("scan_library", 1, variantSole), "scan_library_1"},
		{"重建缩略图", systemTask("rebuild_thumbnails", variantSole), "rebuild_thumbnails"},
		{"刮削", libraryTask("scrape", 2, variantScrapeOneLibrary), "scrape_library_2"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			engine, _ := newBackgroundTestEngine(t, runTaskBodySynchronously, nil)

			var seen string
			var seenRunID int64
			err := engine.Run(tc.identity, task.TriggerManual, RunSpec{Key: tc.key}, func(ctx context.Context, _ *runhandle.Handle) (TaskResult, error) {
				seen = logger.TaskKeyFrom(ctx)
				seenRunID = logger.RunIDFrom(ctx)
				return TaskResult{}, nil
			})
			if err != nil {
				t.Fatalf("启动入口返回了 %v，应为 nil", err)
			}
			if seen != tc.key {
				t.Fatalf("任务体的 ctx 里任务键为 %q, want %q", seen, tc.key)
			}
			// 运行标识与任务键走同一个 handler：同一个任务连着跑三次，只按键过滤会把三次混在一起。
			if seenRunID <= 0 {
				t.Fatalf("任务体的 ctx 里没有运行标识（%d）—— 按运行过滤原始日志就无从谈起", seenRunID)
			}
		})
	}
}
