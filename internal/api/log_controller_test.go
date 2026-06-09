// 业务说明：本文件是业务回归测试，属于后端 HTTP API 层，负责把前端请求转换为数据库、扫描器、图片处理和元数据服务调用。
// 它通过自动化断言保护对应业务场景在扫描、读取、展示或配置变更后仍保持兼容。
// 维护时应让用例名称、测试数据和断言结果直接反映真实用户流程，而不是只覆盖实现细节。

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestGetSystemLogsHonorsFilterAndLimit(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	cfg := controller.currentConfig()
	logPath := filepath.Join(filepath.Dir(cfg.Database.Path), "manga_manager.log")

	content := "" +
		"time=2026-01-01T00:00:00Z level=DEBUG msg=\"trace\"\n" +
		"time=2026-01-01T00:00:00Z level=INFO msg=\"boot\"\n" +
		"time=2026-01-01T00:01:00Z level=ERROR msg=\"first\"\n" +
		"time=2026-01-01T00:02:00Z level=WARN msg=\"warn\"\n" +
		"time=2026-01-01T00:03:00Z level=ERROR msg=\"second\"\n" +
		"time=2026-01-01T00:04:00Z level=ERROR msg=\"third\"\n"
	if err := os.WriteFile(logPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write log file failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/system/logs?level=ERROR&limit=2", nil)
	rec := httptest.NewRecorder()
	controller.getSystemLogs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var response LogsResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode logs response failed: %v", err)
	}
	if len(response.Items) != 2 {
		t.Fatalf("expected 2 log items, got %d", len(response.Items))
	}
	if response.Items[0].Msg != "third" || response.Items[1].Msg != "second" {
		t.Fatalf("expected latest error logs first, got %+v", response.Items)
	}
	if response.Summary.ByLevel["ERROR"] != 3 {
		t.Fatalf("expected error summary count 3, got %+v", response.Summary.ByLevel)
	}
	if response.Summary.ByLevel["DEBUG"] != 1 {
		t.Fatalf("expected debug summary count 1, got %+v", response.Summary.ByLevel)
	}
}

func TestGetSystemLogsTaskKeyFilter(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	cfg := controller.currentConfig()
	logPath := filepath.Join(filepath.Dir(cfg.Database.Path), "manga_manager.log")

	content := "" +
		"time=2026-01-01T00:00:00Z level=ERROR msg=\"unrelated\"\n" +
		"time=2026-01-01T00:01:00Z level=ERROR msg=\"scan failure\" task_key=scan_library_1\n" +
		"time=2026-01-01T00:02:00Z level=ERROR msg=\"scrape failure\" task_key=scrape_library_2\n" +
		"time=2026-01-01T00:03:00Z level=ERROR msg=\"scan retry\" task_key=scan_library_1\n"
	if err := os.WriteFile(logPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write log file failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/system/logs?level=ERROR&task_key=scan_library_1", nil)
	rec := httptest.NewRecorder()
	controller.getSystemLogs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var response LogsResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode logs response failed: %v", err)
	}
	if len(response.Items) != 2 {
		t.Fatalf("expected 2 log items filtered by task_key, got %d", len(response.Items))
	}
	for _, item := range response.Items {
		if item.Msg != "scan failure" && item.Msg != "scan retry" {
			t.Fatalf("unexpected item leaked into task_key filter: %+v", item)
		}
	}
}
