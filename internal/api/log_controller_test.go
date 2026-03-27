package api

import (
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

	body := rec.Body.String()
	if !(contains(body, `"msg":"third"`) && contains(body, `"msg":"second"`)) {
		t.Fatalf("expected latest two error logs in response, got %s", body)
	}
	if contains(body, `"msg":"first"`) {
		t.Fatalf("did not expect older error log in limited response, got %s", body)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		return stringIndex(s, sub) >= 0
	})()
}

func stringIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
