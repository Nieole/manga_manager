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
	if len(resp.Endpoints) != 6 {
		t.Fatalf("expected 6 endpoints, got %+v", resp.Endpoints)
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
	if byKey["koreader"].Category != "sync" {
		t.Fatalf("unexpected KOReader endpoint category: %+v", byKey["koreader"])
	}
	if byKey["koreader"].Health != "disabled" {
		t.Fatalf("unexpected KOReader health before enabling service: %+v", byKey["koreader"])
	}
	if resp.Status.KOReaderAccountCount != 1 || resp.Status.KOReaderEnabledAccounts != 1 {
		t.Fatalf("unexpected KOReader account status: %+v", resp.Status)
	}
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
