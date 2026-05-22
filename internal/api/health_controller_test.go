package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"manga-manager/internal/database"
)

func TestGetHealthReport(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	if _, err := store.(*database.SqlStore).DB().ExecContext(context.Background(), `INSERT INTO koreader_progress (username, document, progress, percentage, device, device_id, book_id) VALUES ('reader', 'missing.cbz', '{}', 0.5, 'device', 'id', NULL)`); err != nil {
		t.Fatalf("insert unmatched progress failed: %v", err)
	}

	rec := httptest.NewRecorder()
	controller.getHealthReport(rec, httptest.NewRequest(http.MethodGet, "/api/health/report?limit=5", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected health report 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var report database.HealthReport
	if err := json.NewDecoder(rec.Body).Decode(&report); err != nil {
		t.Fatalf("decode health report failed: %v", err)
	}
	if report.Limit != 5 {
		t.Fatalf("expected limit 5, got %d", report.Limit)
	}
	if len(report.Summary) == 0 {
		t.Fatal("expected health report summary")
	}
	for _, item := range report.Summary {
		if item.Type == "unmatched_koreader" {
			t.Fatalf("expected disabled KOReader health report to skip unmatched KOReader, got %+v", report.Summary)
		}
	}

	rec = httptest.NewRecorder()
	controller.getHealthReport(rec, httptest.NewRequest(http.MethodGet, "/api/health/report?library_id=bad", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected bad library id 400, got %d", rec.Code)
	}
}
