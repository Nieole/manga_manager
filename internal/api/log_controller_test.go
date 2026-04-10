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
}
