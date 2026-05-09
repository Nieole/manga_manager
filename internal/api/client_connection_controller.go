package api

import (
	"net"
	"net/http"
	"strings"
)

type ClientConnectionEndpoint struct {
	Key         string   `json:"key"`
	Category    string   `json:"category"`
	ClientType  string   `json:"client_type"`
	Label       string   `json:"label"`
	URL         string   `json:"url"`
	Path        string   `json:"path"`
	Description string   `json:"description"`
	Enabled     bool     `json:"enabled"`
	Health      string   `json:"health"`
	AuthNote    string   `json:"auth_note"`
	Diagnostics []string `json:"diagnostics"`
}

type ClientConnectionStatus struct {
	KOReaderEnabled         bool   `json:"koreader_enabled"`
	KOReaderAccountCount    int64  `json:"koreader_account_count"`
	KOReaderEnabledAccounts int64  `json:"koreader_enabled_accounts"`
	KOReaderMatchMode       string `json:"koreader_match_mode"`
}

type ClientConnectionsResponse struct {
	BaseURL   string                     `json:"base_url"`
	Endpoints []ClientConnectionEndpoint `json:"endpoints"`
	Status    ClientConnectionStatus     `json:"status"`
}

func (c *Controller) getClientConnections(w http.ResponseWriter, r *http.Request) {
	cfg := c.currentConfig()
	stats, err := c.store.GetKOReaderStats(r.Context())
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to fetch client connection status")
		return
	}

	baseURL := requestBaseURL(r)
	koreaderPath := strings.TrimSpace(cfg.KOReader.BasePath)
	if koreaderPath == "" {
		koreaderPath = "/koreader"
	}
	if !strings.HasPrefix(koreaderPath, "/") {
		koreaderPath = "/" + koreaderPath
	}

	endpoints := []ClientConnectionEndpoint{
		{
			Key:         "opds",
			Category:    "catalog",
			ClientType:  "opds",
			Label:       "OPDS 1.2",
			Path:        "/opds/v1.2/",
			URL:         baseURL + "/opds/v1.2/",
			Description: "OPDS catalog root for compatible comic readers.",
			Enabled:     true,
			Health:      "ready",
			AuthNote:    "No authentication is required by Manga Manager.",
			Diagnostics: []string{"Catalog root is always available while the server is running.", "Use this URL for OPDS-compatible readers."},
		},
		{
			Key:         "opds_search",
			Category:    "catalog",
			ClientType:  "opds",
			Label:       "OpenSearch",
			Path:        "/opds/v1.2/opensearch.xml",
			URL:         baseURL + "/opds/v1.2/opensearch.xml",
			Description: "Search descriptor discoverable by OPDS clients.",
			Enabled:     true,
			Health:      "ready",
			AuthNote:    "No authentication is required by Manga Manager.",
			Diagnostics: []string{"Expose this descriptor only if the OPDS client supports OpenSearch discovery.", "If search does not appear in a client, use the OPDS root URL instead."},
		},
		{
			Key:         "opds_collections",
			Category:    "collections",
			ClientType:  "opds",
			Label:       "OPDS Collections",
			Path:        "/opds/v1.2/collections",
			URL:         baseURL + "/opds/v1.2/collections",
			Description: "Unified OPDS feed for manual, AI, snapshot, and smart collections.",
			Enabled:     true,
			Health:      "ready",
			AuthNote:    "No authentication is required by Manga Manager.",
			Diagnostics: []string{"Use this when the client supports adding multiple catalog feeds.", "Dynamic smart collections are resolved on request."},
		},
		{
			Key:         "mihon",
			Category:    "catalog",
			ClientType:  "mihon",
			Label:       "Mihon API",
			Path:        "/api/mihon/v1",
			URL:         baseURL + "/api/mihon/v1",
			Description: "Private Mihon/Tachiyomi style JSON API root.",
			Enabled:     true,
			Health:      "ready",
			AuthNote:    "No authentication is required by Manga Manager.",
			Diagnostics: []string{"Use the base API root when configuring a compatible extension.", "This endpoint is JSON and is not an OPDS feed."},
		},
		{
			Key:         "mihon_collections",
			Category:    "collections",
			ClientType:  "mihon",
			Label:       "Mihon Collections",
			Path:        "/api/mihon/v1/collections",
			URL:         baseURL + "/api/mihon/v1/collections",
			Description: "JSON collection list for Mihon/Tachiyomi style clients.",
			Enabled:     true,
			Health:      "ready",
			AuthNote:    "No authentication is required by Manga Manager.",
			Diagnostics: []string{"Use this endpoint for client-side collection discovery.", "Manual, AI, snapshot, and smart collection views share this gateway."},
		},
		{
			Key:         "koreader",
			Category:    "sync",
			ClientType:  "koreader",
			Label:       "KOReader Sync",
			Path:        koreaderPath,
			URL:         baseURL + koreaderPath,
			Description: "Custom progress sync server for KOReader.",
			Enabled:     cfg.KOReader.Enabled,
			Health:      koreaderConnectionHealth(cfg.KOReader.Enabled, stats.EnabledAccountCount),
			AuthNote:    "Requires a KOReader account username and sync key from the KOReader settings page.",
			Diagnostics: koreaderConnectionDiagnostics(cfg.KOReader.Enabled, stats.EnabledAccountCount, cfg.KOReader.MatchMode),
		},
	}

	jsonResponse(w, http.StatusOK, ClientConnectionsResponse{
		BaseURL:   baseURL,
		Endpoints: endpoints,
		Status: ClientConnectionStatus{
			KOReaderEnabled:         cfg.KOReader.Enabled,
			KOReaderAccountCount:    stats.AccountCount,
			KOReaderEnabledAccounts: stats.EnabledAccountCount,
			KOReaderMatchMode:       cfg.KOReader.MatchMode,
		},
	})
}

func koreaderConnectionHealth(enabled bool, enabledAccounts int64) string {
	if !enabled {
		return "disabled"
	}
	if enabledAccounts <= 0 {
		return "needs_account"
	}
	return "ready"
}

func koreaderConnectionDiagnostics(enabled bool, enabledAccounts int64, matchMode string) []string {
	if !enabled {
		return []string{"Enable the KOReader sync service before configuring devices.", "Existing account keys stay stored while the service is disabled."}
	}
	if enabledAccounts <= 0 {
		return []string{"Create or enable at least one KOReader account before pairing a device.", "The endpoint is reachable, but authentication will fail without an enabled account."}
	}
	return []string{"Pair KOReader with an enabled account username and sync key.", "Current progress matching mode: " + strings.TrimSpace(matchMode) + "."}
}

func requestBaseURL(r *http.Request) string {
	proto := firstHeaderValue(r, "X-Forwarded-Proto")
	if proto == "" {
		proto = firstHeaderValue(r, "X-Forwarded-Scheme")
	}
	if proto == "" && r.TLS != nil {
		proto = "https"
	}
	if proto == "" {
		proto = "http"
	}

	host := firstHeaderValue(r, "X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	if host == "" {
		host = "localhost"
	}
	if forwardedPort := firstHeaderValue(r, "X-Forwarded-Port"); forwardedPort != "" {
		if _, _, err := net.SplitHostPort(host); err != nil && !strings.Contains(host, "]") {
			host = host + ":" + forwardedPort
		}
	}

	return strings.TrimRight(proto+"://"+host, "/")
}

func firstHeaderValue(r *http.Request, key string) string {
	value := strings.TrimSpace(r.Header.Get(key))
	if value == "" {
		return ""
	}
	if idx := strings.Index(value, ","); idx >= 0 {
		value = value[:idx]
	}
	return strings.TrimSpace(value)
}
