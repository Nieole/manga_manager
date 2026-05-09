package api

import (
	"net"
	"net/http"
	"strings"
)

type ClientConnectionEndpoint struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	URL         string `json:"url"`
	Path        string `json:"path"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
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
			Label:       "OPDS 1.2",
			Path:        "/opds/v1.2/",
			URL:         baseURL + "/opds/v1.2/",
			Description: "OPDS catalog root for compatible comic readers.",
			Enabled:     true,
		},
		{
			Key:         "opds_search",
			Label:       "OpenSearch",
			Path:        "/opds/v1.2/opensearch.xml",
			URL:         baseURL + "/opds/v1.2/opensearch.xml",
			Description: "Search descriptor discoverable by OPDS clients.",
			Enabled:     true,
		},
		{
			Key:         "mihon",
			Label:       "Mihon API",
			Path:        "/api/mihon/v1",
			URL:         baseURL + "/api/mihon/v1",
			Description: "Private Mihon/Tachiyomi style JSON API root.",
			Enabled:     true,
		},
		{
			Key:         "koreader",
			Label:       "KOReader Sync",
			Path:        koreaderPath,
			URL:         baseURL + koreaderPath,
			Description: "Custom progress sync server for KOReader.",
			Enabled:     cfg.KOReader.Enabled,
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
