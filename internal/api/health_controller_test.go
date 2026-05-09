package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"manga-manager/internal/database"
)

func TestGetHealthReport(t *testing.T) {
	controller, _, _, _ := newTestController(t)

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

	rec = httptest.NewRecorder()
	controller.getHealthReport(rec, httptest.NewRequest(http.MethodGet, "/api/health/report?library_id=bad", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected bad library id 400, got %d", rec.Code)
	}
}
