package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGetClientConnections(t *testing.T) {
	requestDiagnostics.reset()
	t.Cleanup(requestDiagnostics.reset)

	controller, store, _, rootDir := newTestController(t)
	seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)
	if _, err := controller.koreader.CreateAccount(context.Background(), "reader"); err != nil {
		t.Fatalf("create KOReader account failed: %v", err)
	}
	cfg := controller.currentConfig()
	cfg.Protocols.OPDS.Enabled = true
	cfg.Protocols.Mihon.Enabled = true
	cfg.KOReader.Enabled = true
	controller.config.Replace(&cfg)
	requestDiagnostics.record(RequestDiagnosticEvent{
		Time:       testNow(),
		Method:     http.MethodGet,
		Path:       "/opds/v1.2/recent",
		Status:     http.StatusOK,
		DurationMS: 12,
		Bytes:      128,
		RemoteIP:   "192.0.2.10:50000",
	})
	requestDiagnostics.record(RequestDiagnosticEvent{
		Time:       testNow(),
		Method:     http.MethodGet,
		Path:       "/api/mihon/v1/continue",
		Status:     http.StatusBadGateway,
		DurationMS: slowRequestThreshold.Milliseconds() + 1,
		Bytes:      64,
		RemoteIP:   "192.0.2.11:50000",
	})

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
	if len(resp.Endpoints) != 11 {
		t.Fatalf("expected 11 endpoints, got %+v", resp.Endpoints)
	}
	if resp.Endpoints[0].URL != "https://manga.local:8080/opds/v1.2/" {
		t.Fatalf("unexpected OPDS URL: %+v", resp.Endpoints[0])
	}
	byKey := make(map[string]ClientConnectionEndpoint, len(resp.Endpoints))
	for _, endpoint := range resp.Endpoints {
		byKey[endpoint.Key] = endpoint
	}
	if byKey["opds_collections"].URL != "https://manga.local:8080/opds/v1.2/collections" || byKey["opds_collections"].Category != "collections" {
		t.Fatalf("unexpected OPDS collections endpoint: %+v", byKey["opds_collections"])
	}
	if byKey["opds_collections"].ClientType != "opds" || byKey["opds_collections"].Health != "ready" || len(byKey["opds_collections"].Diagnostics) == 0 {
		t.Fatalf("unexpected OPDS collections diagnostics: %+v", byKey["opds_collections"])
	}
	if byKey["mihon_collections"].URL != "https://manga.local:8080/api/mihon/v1/collections" || byKey["mihon_collections"].Category != "collections" {
		t.Fatalf("unexpected Mihon collections endpoint: %+v", byKey["mihon_collections"])
	}
	if byKey["opds_recent"].URL != "https://manga.local:8080/opds/v1.2/recent" || byKey["opds_recent"].ClientType != "opds" {
		t.Fatalf("unexpected OPDS recent endpoint: %+v", byKey["opds_recent"])
	}
	if byKey["opds_recent"].Requests.Total != 1 || byKey["opds_recent"].Requests.LastStatus != http.StatusOK {
		t.Fatalf("unexpected OPDS recent request diagnostics: %+v", byKey["opds_recent"].Requests)
	}
	if byKey["opds_reading_lists"].URL != "https://manga.local:8080/opds/v1.2/reading-lists" || byKey["opds_reading_lists"].Category != "collections" {
		t.Fatalf("unexpected OPDS reading lists endpoint: %+v", byKey["opds_reading_lists"])
	}
	if byKey["mihon_recent"].URL != "https://manga.local:8080/api/mihon/v1/recently-added" || byKey["mihon_recent"].Category != "catalog" {
		t.Fatalf("unexpected Mihon recent endpoint: %+v", byKey["mihon_recent"])
	}
	if byKey["mihon_reading_lists"].URL != "https://manga.local:8080/api/mihon/v1/reading-lists" || byKey["mihon_reading_lists"].Category != "collections" {
		t.Fatalf("unexpected Mihon reading lists endpoint: %+v", byKey["mihon_reading_lists"])
	}
	if byKey["mihon_continue"].URL != "https://manga.local:8080/api/mihon/v1/continue" || byKey["mihon_continue"].Category != "sync" {
		t.Fatalf("unexpected Mihon continue endpoint: %+v", byKey["mihon_continue"])
	}
	if byKey["mihon_continue"].Requests.Total != 1 || byKey["mihon_continue"].Requests.Errors != 1 || byKey["mihon_continue"].Requests.Slow != 1 {
		t.Fatalf("unexpected Mihon continue request diagnostics: %+v", byKey["mihon_continue"].Requests)
	}
	if byKey["koreader"].Category != "sync" {
		t.Fatalf("unexpected KOReader endpoint category: %+v", byKey["koreader"])
	}
	if byKey["koreader"].Health != "ready" {
		t.Fatalf("unexpected KOReader health with enabled service: %+v", byKey["koreader"])
	}
	if !resp.Status.OPDSEnabled || !resp.Status.MihonEnabled || !resp.Status.KOReaderEnabled || resp.Status.KOReaderAccountCount != 1 || resp.Status.KOReaderEnabledAccounts != 1 {
		t.Fatalf("unexpected client connection status: %+v", resp.Status)
	}
}

func TestGetClientConnectionsHidesDisabledProtocolEndpoints(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	rec := httptest.NewRecorder()
	controller.getClientConnections(rec, httptest.NewRequest(http.MethodGet, "/api/system/client-connections", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp ClientConnectionsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode client connections failed: %v", err)
	}
	if len(resp.Endpoints) != 0 {
		t.Fatalf("expected no disabled protocol endpoints, got %+v", resp.Endpoints)
	}
	if resp.Status.OPDSEnabled || resp.Status.MihonEnabled || resp.Status.KOReaderEnabled {
		t.Fatalf("expected all external protocols disabled by default, got %+v", resp.Status)
	}
}

func testNow() time.Time {
	return time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
}

func TestKOReaderConnectionHealth(t *testing.T) {
	tests := []struct {
		name            string
		enabled         bool
		enabledAccounts int64
		want            string
	}{
		{name: "disabled", enabled: false, enabledAccounts: 1, want: "disabled"},
		{name: "needs account", enabled: true, enabledAccounts: 0, want: "needs_account"},
		{name: "ready", enabled: true, enabledAccounts: 1, want: "ready"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := koreaderConnectionHealth(tt.enabled, tt.enabledAccounts); got != tt.want {
				t.Fatalf("unexpected health: got %s want %s", got, tt.want)
			}
		})
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
