// 业务说明：本文件是业务实现，属于后端 HTTP API 层，负责把前端请求转换为数据库、扫描器、图片处理和元数据服务调用。
// 它承载资料库浏览、阅读器取页、系列维护、任务进度、系统设置和静态资源缓存等对外业务契约。
// 维护时应重点关注请求参数校验、错误语义、缓存头、并发任务状态和前后端字段兼容性。

package api

import (
	"net"
	"net/http"
	"strings"
	"time"
)

type ClientConnectionEndpoint struct {
	Key         string                           `json:"key"`
	Category    string                           `json:"category"`
	ClientType  string                           `json:"client_type"`
	Label       string                           `json:"label"`
	URL         string                           `json:"url"`
	Path        string                           `json:"path"`
	Description string                           `json:"description"`
	Enabled     bool                             `json:"enabled"`
	Health      string                           `json:"health"`
	AuthNote    string                           `json:"auth_note"`
	Diagnostics []string                         `json:"diagnostics"`
	Requests    ClientEndpointRequestDiagnostics `json:"requests"`
}

type ClientEndpointRequestDiagnostics struct {
	Total          int                             `json:"total"`
	Success        int                             `json:"success"`
	Warnings       int                             `json:"warnings"`
	Errors         int                             `json:"errors"`
	Slow           int                             `json:"slow"`
	LastSeen       *time.Time                      `json:"last_seen,omitempty"`
	LastStatus     int                             `json:"last_status"`
	LastDurationMS int64                           `json:"last_duration_ms"`
	LastPath       string                          `json:"last_path"`
	Recent         []ClientEndpointRequestSnapshot `json:"recent"`
}

type ClientEndpointRequestSnapshot struct {
	Time       time.Time `json:"time"`
	Method     string    `json:"method"`
	Path       string    `json:"path"`
	Status     int       `json:"status"`
	DurationMS int64     `json:"duration_ms"`
	RemoteIP   string    `json:"remote_ip"`
}

type ClientConnectionStatus struct {
	OPDSEnabled             bool   `json:"opds_enabled"`
	MihonEnabled            bool   `json:"mihon_enabled"`
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
			Diagnostics: []string{"Catalog root is always available while the server is running.", "Use this URL for OPDS-compatible readers.", "Book entries include OPDS-PSE page streaming links for clients that support comic page streaming."},
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
			Key:         "opds_recent",
			Category:    "catalog",
			ClientType:  "opds",
			Label:       "OPDS Recent",
			Path:        "/opds/v1.2/recent",
			URL:         baseURL + "/opds/v1.2/recent",
			Description: "OPDS feed sorted by recently added series.",
			Enabled:     true,
			Health:      "ready",
			AuthNote:    "No authentication is required by Manga Manager.",
			Diagnostics: []string{"Use this feed when a client supports separate catalog shortcuts.", "Add libraryId as a query parameter to scope the feed to one library."},
		},
		{
			Key:         "opds_reading_lists",
			Category:    "collections",
			ClientType:  "opds",
			Label:       "OPDS Reading Lists",
			Path:        "/opds/v1.2/reading-lists",
			URL:         baseURL + "/opds/v1.2/reading-lists",
			Description: "OPDS navigation feed for ordered reading lists.",
			Enabled:     true,
			Health:      "ready",
			AuthNote:    "No authentication is required by Manga Manager.",
			Diagnostics: []string{"Use this endpoint for curated reading-order lists.", "Each list opens as a paged series feed."},
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
			Key:         "mihon_recent",
			Category:    "catalog",
			ClientType:  "mihon",
			Label:       "Mihon Recently Added",
			Path:        "/api/mihon/v1/recently-added",
			URL:         baseURL + "/api/mihon/v1/recently-added",
			Description: "JSON series page sorted by recently added time.",
			Enabled:     true,
			Health:      "ready",
			AuthNote:    "No authentication is required by Manga Manager.",
			Diagnostics: []string{"Use this endpoint for extension-side recent shelves.", "Supports page, limit, and libraryId query parameters."},
		},
		{
			Key:         "mihon_reading_lists",
			Category:    "collections",
			ClientType:  "mihon",
			Label:       "Mihon Reading Lists",
			Path:        "/api/mihon/v1/reading-lists",
			URL:         baseURL + "/api/mihon/v1/reading-lists",
			Description: "JSON reading-list catalog for Mihon/Tachiyomi style clients.",
			Enabled:     true,
			Health:      "ready",
			AuthNote:    "No authentication is required by Manga Manager.",
			Diagnostics: []string{"Use this endpoint for curated reading-order discovery.", "List members are available at /api/mihon/v1/reading-lists/{id}/series."},
		},
		{
			Key:         "mihon_continue",
			Category:    "sync",
			ClientType:  "mihon",
			Label:       "Mihon Continue",
			Path:        "/api/mihon/v1/continue",
			URL:         baseURL + "/api/mihon/v1/continue",
			Description: "JSON feed of recently read books for compatible clients.",
			Enabled:     true,
			Health:      "ready",
			AuthNote:    "No authentication is required by Manga Manager.",
			Diagnostics: []string{"Use this endpoint to build client-side continue-reading shelves.", "Progress writes still use the existing book progress endpoint."},
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

	endpoints = filterEnabledClientEndpoints(endpoints, cfg.Protocols.OPDS.Enabled, cfg.Protocols.Mihon.Enabled, cfg.KOReader.Enabled)
	attachEndpointRequestDiagnostics(endpoints)

	jsonResponse(w, http.StatusOK, ClientConnectionsResponse{
		BaseURL:   baseURL,
		Endpoints: endpoints,
		Status: ClientConnectionStatus{
			OPDSEnabled:             cfg.Protocols.OPDS.Enabled,
			MihonEnabled:            cfg.Protocols.Mihon.Enabled,
			KOReaderEnabled:         cfg.KOReader.Enabled,
			KOReaderAccountCount:    stats.AccountCount,
			KOReaderEnabledAccounts: stats.EnabledAccountCount,
			KOReaderMatchMode:       cfg.KOReader.MatchMode,
		},
	})
}

func filterEnabledClientEndpoints(endpoints []ClientConnectionEndpoint, opdsEnabled, mihonEnabled, koreaderEnabled bool) []ClientConnectionEndpoint {
	visible := make([]ClientConnectionEndpoint, 0, len(endpoints))
	for _, endpoint := range endpoints {
		switch endpoint.ClientType {
		case "opds":
			if !opdsEnabled {
				continue
			}
		case "mihon":
			if !mihonEnabled {
				continue
			}
		case "koreader":
			if !koreaderEnabled {
				continue
			}
		}
		visible = append(visible, endpoint)
	}
	return visible
}

func attachEndpointRequestDiagnostics(endpoints []ClientConnectionEndpoint) {
	events := requestDiagnostics.snapshot()
	for index := range endpoints {
		endpoints[index].Requests = summarizeEndpointRequests(endpoints[index], events)
	}
}

func summarizeEndpointRequests(endpoint ClientConnectionEndpoint, events []RequestDiagnosticEvent) ClientEndpointRequestDiagnostics {
	summary := ClientEndpointRequestDiagnostics{
		Recent: make([]ClientEndpointRequestSnapshot, 0, 5),
	}
	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		if !requestMatchesEndpoint(endpoint, event.Path) {
			continue
		}
		summary.Total++
		switch {
		case event.Status >= 500:
			summary.Errors++
		case event.Status >= 400:
			summary.Warnings++
		default:
			summary.Success++
		}
		if event.DurationMS >= slowRequestThreshold.Milliseconds() {
			summary.Slow++
		}
		if summary.LastSeen == nil {
			seen := event.Time
			summary.LastSeen = &seen
			summary.LastStatus = event.Status
			summary.LastDurationMS = event.DurationMS
			summary.LastPath = event.Path
		}
		if len(summary.Recent) < 5 {
			summary.Recent = append(summary.Recent, ClientEndpointRequestSnapshot{
				Time:       event.Time,
				Method:     event.Method,
				Path:       event.Path,
				Status:     event.Status,
				DurationMS: event.DurationMS,
				RemoteIP:   event.RemoteIP,
			})
		}
	}
	return summary
}

func requestMatchesEndpoint(endpoint ClientConnectionEndpoint, path string) bool {
	prefix := strings.TrimRight(endpoint.Path, "/")
	if prefix == "" {
		prefix = endpoint.Path
	}
	if endpoint.Path == "/opds/v1.2/" || endpoint.Path == "/api/mihon/v1" {
		return path == prefix || strings.HasPrefix(path, prefix+"/")
	}
	if endpoint.ClientType == "koreader" {
		return path == prefix || strings.HasPrefix(path, prefix+"/")
	}
	return path == endpoint.Path || strings.HasPrefix(path, prefix+"/")
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
