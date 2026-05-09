package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetClientConnections(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)
	if _, err := controller.koreader.CreateAccount(context.Background(), "reader"); err != nil {
		t.Fatalf("create KOReader account failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/system/client-connections", nil)
	req.Host = "manga.local:8080"
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()

	controller.getClientConnections(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp ClientConnectionsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode client connections failed: %v", err)
	}
	if resp.BaseURL != "https://manga.local:8080" {
		t.Fatalf("unexpected base URL: %s", resp.BaseURL)
	}
	if len(resp.Endpoints) != 4 {
		t.Fatalf("expected 4 endpoints, got %+v", resp.Endpoints)
	}
	if resp.Endpoints[0].URL != "https://manga.local:8080/opds/v1.2/" {
		t.Fatalf("unexpected OPDS URL: %+v", resp.Endpoints[0])
	}
	if resp.Status.KOReaderAccountCount != 1 || resp.Status.KOReaderEnabledAccounts != 1 {
		t.Fatalf("unexpected KOReader account status: %+v", resp.Status)
	}
}

func TestRequestBaseURL(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://internal/api/system/client-connections", nil)
	req.Header.Set("X-Forwarded-Proto", "https,http")
	req.Header.Set("X-Forwarded-Host", "public.example.com, proxy.local")

	if got := requestBaseURL(req); got != "https://public.example.com" {
		t.Fatalf("unexpected forwarded base URL: %s", got)
	}
}
