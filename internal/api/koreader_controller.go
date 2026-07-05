// 业务说明：本文件是业务实现，属于后端 HTTP API 层，负责把前端请求转换为数据库、扫描器、图片处理和元数据服务调用。
// 它承载资料库浏览、阅读器取页、系列维护、任务进度、系统设置和静态资源缓存等对外业务契约。
// 维护时应重点关注请求参数校验、错误语义、缓存头、并发任务状态和前后端字段兼容性。

package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
	ksvc "manga-manager/internal/koreader"

	"github.com/go-chi/chi/v5"
)

type KOReaderSystemResponse struct {
	Enabled             bool                   `json:"enabled"`
	BasePath            string                 `json:"base_path"`
	AllowRegistration   bool                   `json:"allow_registration"`
	MatchMode           string                 `json:"match_mode"`
	PathIgnoreExtension bool                   `json:"path_ignore_extension"`
	PathMatchDepth      int                    `json:"path_match_depth"`
	AccountCount        int64                  `json:"account_count"`
	EnabledAccountCount int64                  `json:"enabled_account_count"`
	LatestError         string                 `json:"latest_error,omitempty"`
	Stats               database.KOReaderStats `json:"stats"`
}

type UpdateKOReaderSettingsRequest struct {
	Enabled             bool   `json:"enabled"`
	BasePath            string `json:"base_path"`
	AllowRegistration   bool   `json:"allow_registration"`
	MatchMode           string `json:"match_mode"`
	PathIgnoreExtension bool   `json:"path_ignore_extension"`
}

type CreateKOReaderAccountRequest struct {
	Username string `json:"username"`
}

type ToggleKOReaderAccountRequest struct {
	Enabled bool `json:"enabled"`
}

// KOReaderRegisterRequest 是 kosync POST /users/create 的请求体；password 为用户密钥的 md5 十六进制串。
type KOReaderRegisterRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type KOReaderAccountResponse struct {
	ID          int64   `json:"id"`
	Username    string  `json:"username"`
	SyncKey     string  `json:"sync_key"`
	Enabled     bool    `json:"enabled"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
	LastUsedAt  *string `json:"last_used_at,omitempty"`
	LatestError string  `json:"latest_error,omitempty"`
}

type KOReaderUnmatchedItem struct {
	ID            int64   `json:"id"`
	Document      string  `json:"document"`
	NormalizedKey string  `json:"normalized_key"`
	Device        string  `json:"device"`
	DeviceID      string  `json:"device_id"`
	Percentage    float64 `json:"percentage"`
	UpdatedAt     string  `json:"updated_at"`
	Suggestion    string  `json:"suggestion"`
}

type KOReaderDeviceDiagnosticsResponse struct {
	Summary   KOReaderDeviceDiagnosticsSummary `json:"summary"`
	Devices   []KOReaderDeviceItem             `json:"devices"`
	Conflicts []KOReaderDeviceConflictItem     `json:"conflicts"`
}

type KOReaderDeviceDiagnosticsSummary struct {
	DeviceCount        int   `json:"device_count"`
	HealthyDevices     int   `json:"healthy_devices"`
	AttentionDevices   int   `json:"attention_devices"`
	TotalRecords       int64 `json:"total_records"`
	MatchedRecords     int64 `json:"matched_records"`
	UnmatchedRecords   int64 `json:"unmatched_records"`
	ConflictCount      int   `json:"conflict_count"`
	ErrorConflictCount int   `json:"error_conflict_count"`
}

type KOReaderDeviceItem struct {
	Key              string                    `json:"key"`
	Username         string                    `json:"username"`
	Device           string                    `json:"device"`
	DeviceID         string                    `json:"device_id"`
	Health           string                    `json:"health"`
	TotalRecords     int64                     `json:"total_records"`
	MatchedRecords   int64                     `json:"matched_records"`
	UnmatchedRecords int64                     `json:"unmatched_records"`
	LatestSyncAt     string                    `json:"latest_sync_at,omitempty"`
	LatestDocument   string                    `json:"latest_document"`
	LatestMatchedBy  string                    `json:"latest_matched_by"`
	LatestError      string                    `json:"latest_error,omitempty"`
	MatchMethods     []KOReaderMatchMethodItem `json:"match_methods"`
	Suggestion       string                    `json:"suggestion"`
}

type KOReaderMatchMethodItem struct {
	Method string `json:"method"`
	Count  int64  `json:"count"`
}

type KOReaderDeviceConflictItem struct {
	ID            int64   `json:"id"`
	Type          string  `json:"type"`
	Severity      string  `json:"severity"`
	Username      string  `json:"username"`
	Device        string  `json:"device"`
	DeviceID      string  `json:"device_id"`
	Document      string  `json:"document"`
	NormalizedKey string  `json:"normalized_key"`
	BookID        *int64  `json:"book_id,omitempty"`
	MatchedBy     string  `json:"matched_by"`
	Status        string  `json:"status"`
	Message       string  `json:"message"`
	Percentage    float64 `json:"percentage"`
	UpdatedAt     string  `json:"updated_at"`
	Suggestion    string  `json:"suggestion"`
}

func (c *Controller) SetupKOReaderRoutes(r chi.Router) {
	basePath := c.currentConfig().KOReader.BasePath
	if strings.TrimSpace(basePath) == "" {
		basePath = "/koreader"
	}
	r.Route(basePath, func(r chi.Router) {
		r.Get("/healthcheck", c.koreaderHealthcheck)
		r.Get("/healthstatus", c.koreaderHealthcheck)
		r.Get("/robots.txt", c.koreaderRobots)
		r.Post("/users/create", c.koreaderRegister)
		r.Get("/users/auth", c.koreaderAuth)
		r.Put("/syncs/progress", c.koreaderUpdateProgress)
		r.Get("/syncs/progress/{document}", c.koreaderGetProgress)
	})
}

func (c *Controller) getKOReaderSettings(w http.ResponseWriter, r *http.Request) {
	stats, err := c.store.GetKOReaderStats(r.Context())
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to fetch KOReader settings")
		return
	}
	latestError := ""
	if event, failureErr := c.store.GetLatestKOReaderFailure(r.Context()); failureErr == nil {
		latestError = strings.TrimSpace(event.Message)
	}
	if indexed, err := c.koreader.IndexedBookCount(r.Context()); err == nil {
		stats.HashedBooks = indexed
	}
	cfg := c.currentConfig()
	jsonResponse(w, http.StatusOK, KOReaderSystemResponse{
		Enabled:             cfg.KOReader.Enabled,
		BasePath:            cfg.KOReader.BasePath,
		AllowRegistration:   cfg.KOReader.AllowRegistration,
		MatchMode:           cfg.KOReader.MatchMode,
		PathIgnoreExtension: cfg.KOReader.PathIgnoreExtension,
		PathMatchDepth:      config.KOReaderPathMatchDepth,
		AccountCount:        stats.AccountCount,
		EnabledAccountCount: stats.EnabledAccountCount,
		LatestError:         latestError,
		Stats:               stats,
	})
}

func (c *Controller) listKOReaderUnmatched(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 200 {
			limit = parsed
		}
	}

	items, err := c.store.ListUnmatchedKOReaderProgress(r.Context(), limit)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to load unmatched KOReader progress")
		return
	}

	cfg := c.currentConfig()
	locale := requestLocale(r)
	result := make([]KOReaderUnmatchedItem, 0, len(items))
	for _, item := range items {
		suggestion := koreaderUnmatchedSuggestion(locale, cfg)
		result = append(result, KOReaderUnmatchedItem{
			ID:            item.ID,
			Document:      item.Document,
			NormalizedKey: ksvc.NormalizeDocumentForMatch(item.Document, cfg.KOReader.MatchMode, cfg.KOReader.PathIgnoreExtension),
			Device:        item.Device,
			DeviceID:      item.DeviceID,
			Percentage:    item.Percentage,
			UpdatedAt:     item.UpdatedAt.Format(time.RFC3339),
			Suggestion:    suggestion,
		})
	}
	if result == nil {
		result = []KOReaderUnmatchedItem{}
	}
	jsonResponse(w, http.StatusOK, result)
}

func (c *Controller) getKOReaderDeviceDiagnostics(w http.ResponseWriter, r *http.Request) {
	limit := 30
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 200 {
			limit = parsed
		}
	}

	devices, err := c.store.ListKOReaderDeviceDiagnostics(r.Context())
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to load KOReader device diagnostics")
		return
	}
	methods, err := c.store.ListKOReaderDeviceMatchMethods(r.Context())
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to load KOReader device match methods")
		return
	}
	conflicts, err := c.store.ListKOReaderDeviceConflicts(r.Context(), limit)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to load KOReader device conflicts")
		return
	}

	cfg := c.currentConfig()
	methodsByDevice := make(map[string][]KOReaderMatchMethodItem)
	for _, method := range methods {
		key := koreaderDeviceKey(method.Username, method.Device, method.DeviceID)
		methodsByDevice[key] = append(methodsByDevice[key], KOReaderMatchMethodItem{
			Method: method.MatchedBy,
			Count:  method.Count,
		})
	}

	response := KOReaderDeviceDiagnosticsResponse{
		Devices:   make([]KOReaderDeviceItem, 0, len(devices)),
		Conflicts: make([]KOReaderDeviceConflictItem, 0, len(conflicts)),
	}
	for _, device := range devices {
		key := koreaderDeviceKey(device.Username, device.Device, device.DeviceID)
		health := "ready"
		if device.UnmatchedRecords > 0 {
			health = "needs_reconcile"
		}
		if strings.TrimSpace(device.LatestError) != "" {
			health = "error"
		}
		if health == "ready" {
			response.Summary.HealthyDevices++
		} else {
			response.Summary.AttentionDevices++
		}
		response.Summary.TotalRecords += device.TotalRecords
		response.Summary.MatchedRecords += device.MatchedRecords
		response.Summary.UnmatchedRecords += device.UnmatchedRecords

		latestSyncAt := ""
		if device.LatestSyncAt.Valid {
			latestSyncAt = device.LatestSyncAt.Time.Format(time.RFC3339)
		}
		response.Devices = append(response.Devices, KOReaderDeviceItem{
			Key:              key,
			Username:         device.Username,
			Device:           firstNonEmpty(device.Device, "Unknown device"),
			DeviceID:         device.DeviceID,
			Health:           health,
			TotalRecords:     device.TotalRecords,
			MatchedRecords:   device.MatchedRecords,
			UnmatchedRecords: device.UnmatchedRecords,
			LatestSyncAt:     latestSyncAt,
			LatestDocument:   device.LatestDocument,
			LatestMatchedBy:  device.LatestMatchedBy,
			LatestError:      strings.TrimSpace(device.LatestError),
			MatchMethods:     methodsByDevice[key],
			Suggestion:       koreaderDeviceSuggestion(requestLocale(r), health, cfg, device.UnmatchedRecords),
		})
	}
	response.Summary.DeviceCount = len(response.Devices)

	for _, conflict := range conflicts {
		item := KOReaderDeviceConflictItem{
			ID:            conflict.ID,
			Type:          conflict.Type,
			Severity:      conflict.Severity,
			Username:      conflict.Username,
			Device:        firstNonEmpty(conflict.Device, "Unknown device"),
			DeviceID:      conflict.DeviceID,
			Document:      conflict.Document,
			NormalizedKey: ksvc.NormalizeDocumentForMatch(conflict.Document, cfg.KOReader.MatchMode, cfg.KOReader.PathIgnoreExtension),
			MatchedBy:     conflict.MatchedBy,
			Status:        conflict.Status,
			Message:       conflict.Message,
			Percentage:    conflict.Percentage,
			UpdatedAt:     conflict.UpdatedAt.Format(time.RFC3339),
			Suggestion:    koreaderConflictSuggestion(requestLocale(r), conflict, cfg),
		}
		if conflict.BookID.Valid {
			bookID := conflict.BookID.Int64
			item.BookID = &bookID
		}
		if item.Severity == "error" {
			response.Summary.ErrorConflictCount++
		}
		response.Conflicts = append(response.Conflicts, item)
	}
	response.Summary.ConflictCount = len(response.Conflicts)

	jsonResponse(w, http.StatusOK, response)
}

func koreaderDeviceKey(username, device, deviceID string) string {
	return strings.TrimSpace(username) + "|" + strings.TrimSpace(device) + "|" + strings.TrimSpace(deviceID)
}

// 以下 KOReader 建议文案 helper 按 locale 生成中/英文本（含 %d 参数的分支用常量格式串按 locale
// 选择，满足 go vet 非常量格式检查）。这些建议随设备诊断/未匹配列表响应直接下发、前端只能原样展示。
func koreaderDeviceSuggestion(locale, health string, cfg config.Config, unmatched int64) string {
	en := locale == "en-US"
	switch health {
	case "error":
		if en {
			return "Recent sync or authentication errors were detected. Check the account Sync Key and the device server address first."
		}
		return "最近存在同步或认证错误，请先检查账号 Sync Key 和设备端服务器地址。"
	case "needs_reconcile":
		if cfg.KOReader.MatchMode == config.KOReaderMatchModeFilePath {
			if en {
				return fmt.Sprintf("This device still has %d unmatched records. Ensure the path KOReader reports matches the local file name within %d parent path levels.", unmatched, config.KOReaderPathMatchDepth)
			}
			return fmt.Sprintf("该设备还有 %d 条未匹配记录，请确认 KOReader 上报路径与本地文件名及向上 %d 层路径一致。", unmatched, config.KOReaderPathMatchDepth)
		}
		if en {
			return fmt.Sprintf("This device still has %d unmatched records. Rebuild the binary hash index and reconcile again.", unmatched)
		}
		return fmt.Sprintf("该设备还有 %d 条未匹配记录，请先重建二进制哈希索引再重关联。", unmatched)
	default:
		if en {
			return "All recent sync records for this device are mapped to local books."
		}
		return "该设备最近同步记录均已映射到本地书籍。"
	}
}

func koreaderConflictSuggestion(locale string, conflict database.KOReaderDeviceConflict, cfg config.Config) string {
	en := locale == "en-US"
	if strings.HasPrefix(conflict.Status, "auth_failed") {
		if en {
			return "Authentication failures usually come from a mismatched username or Sync Key. Re-copy the account's original Sync Key to the device."
		}
		return "认证失败通常由用户名或 Sync Key 不一致导致，请重新复制账号的原始 Sync Key 到设备。"
	}
	if conflict.Type == "unmatched_progress" {
		if cfg.KOReader.MatchMode == config.KOReaderMatchModeFilePath {
			if en {
				return fmt.Sprintf("Path matching is in use. Compare the normalized key with the local file name within %d parent path levels.", config.KOReaderPathMatchDepth)
			}
			return fmt.Sprintf("当前使用路径匹配，请比较归一化键与本地文件名及向上 %d 层路径。", config.KOReaderPathMatchDepth)
		}
		if en {
			return "Binary hash matching is in use. Make sure the KOReader index has been rebuilt for local books."
		}
		return "当前使用二进制哈希匹配，请确认已为本地书籍重建 KOReader 索引。"
	}
	if en {
		return "Review the status code and message, then retry the sync. If it recurs, confirm in the connection center that requests hit the correct path."
	}
	return "查看状态码和消息后重试同步；如果反复出现，可先在连接中心确认请求是否命中正确路径。"
}

// koreaderUnmatchedSuggestion 生成未匹配记录列表项的建议文案（按 locale 选中/英）。
func koreaderUnmatchedSuggestion(locale string, cfg config.Config) string {
	en := locale == "en-US"
	if cfg.KOReader.MatchMode == config.KOReaderMatchModeFilePath {
		if en {
			s := fmt.Sprintf("Ensure the path KOReader reports can map to a local book within the file name and %d parent path levels.", config.KOReaderPathMatchDepth)
			if cfg.KOReader.PathIgnoreExtension {
				s += " The extension is currently ignored."
			}
			return s
		}
		s := fmt.Sprintf("请确认 KOReader 上报路径在文件名及向上 %d 层路径范围内可对应本地书籍。", config.KOReaderPathMatchDepth)
		if cfg.KOReader.PathIgnoreExtension {
			s += " 当前已忽略扩展名。"
		}
		return s
	}
	if en {
		return "Confirm KOReader is currently using binary hash matching, and rebuild the match index first."
	}
	return "请确认 KOReader 当前使用的是二进制哈希匹配，并先重建匹配索引。"
}

func mapKOReaderAccountResponse(account database.KOReaderAccount) KOReaderAccountResponse {
	resp := KOReaderAccountResponse{
		ID:        account.ID,
		Username:  account.Username,
		SyncKey:   account.SyncKey,
		Enabled:   account.Enabled,
		CreatedAt: account.CreatedAt.Format(time.RFC3339),
		UpdatedAt: account.UpdatedAt.Format(time.RFC3339),
	}
	if account.LastUsedAt.Valid {
		value := account.LastUsedAt.Time.Format(time.RFC3339)
		resp.LastUsedAt = &value
	}
	if account.LatestError.Valid {
		resp.LatestError = strings.TrimSpace(account.LatestError.String)
	}
	return resp
}

func (c *Controller) listKOReaderAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := c.koreader.ListAccounts(r.Context())
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to load KOReader accounts")
		return
	}
	result := make([]KOReaderAccountResponse, 0, len(accounts))
	for _, account := range accounts {
		result = append(result, mapKOReaderAccountResponse(account))
	}
	if result == nil {
		result = []KOReaderAccountResponse{}
	}
	jsonResponse(w, http.StatusOK, result)
}

func (c *Controller) createKOReaderAccount(w http.ResponseWriter, r *http.Request) {
	var req CreateKOReaderAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid KOReader account payload")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" {
		jsonResponse(w, http.StatusUnprocessableEntity, map[string]interface{}{
			"error": "KOReader account validation failed",
			"validation": config.ValidationResult{
				Valid: false,
				Issues: []config.ValidationIssue{
					{Field: "koreader.accounts.username", Message: apiText(requestLocale(r), "koreader.validation.username_required"), Severity: "error"},
				},
			},
		})
		return
	}
	account, err := c.koreader.CreateAccount(r.Context(), req.Username)
	if err != nil {
		switch {
		case errors.Is(err, ksvc.ErrAlreadyConfigured):
			jsonResponse(w, http.StatusConflict, map[string]string{"error": apiText(requestLocale(r), "koreader.account.username_taken")})
		case errors.Is(err, ksvc.ErrUnauthorized):
			jsonResponse(w, http.StatusUnprocessableEntity, map[string]interface{}{
				"error": "KOReader account validation failed",
				"validation": config.ValidationResult{
					Valid: false,
					Issues: []config.ValidationIssue{
						{Field: "koreader.accounts.username", Message: apiText(requestLocale(r), "koreader.validation.username_required"), Severity: "error"},
					},
				},
			})
		default:
			jsonError(w, http.StatusInternalServerError, "Failed to create KOReader account")
		}
		return
	}
	// 管理员创建的账户归属该管理员（多用户：谁创建谁拥有，其同步进度记到该用户名下）。
	if uid := c.currentUserID(r); uid > 0 {
		if err := c.store.SetKOReaderAccountUser(r.Context(), account.ID, uid); err != nil {
			slog.Warn("Failed to assign KOReader account to creator", "account_id", account.ID, "user_id", uid, "error", err)
		}
	}
	_ = c.store.CreateKOReaderSyncEvent(r.Context(), database.CreateKOReaderSyncEventParams{
		Direction: "system",
		Username:  account.Username,
		Status:    "account_created",
		Message:   "KOReader 账号已创建",
	})
	jsonResponse(w, http.StatusCreated, mapKOReaderAccountResponse(account))
}

func (c *Controller) rotateKOReaderAccountKey(w http.ResponseWriter, r *http.Request) {
	accountID, err := strconv.ParseInt(chi.URLParam(r, "accountId"), 10, 64)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid KOReader account ID")
		return
	}
	account, err := c.koreader.RotateAccountKey(r.Context(), accountID)
	if err != nil {
		switch {
		case errors.Is(err, ksvc.ErrAccountNotFound):
			jsonError(w, http.StatusNotFound, "KOReader account not found")
		default:
			jsonError(w, http.StatusInternalServerError, "Failed to rotate KOReader Sync Key")
		}
		return
	}
	_ = c.store.CreateKOReaderSyncEvent(r.Context(), database.CreateKOReaderSyncEventParams{
		Direction: "system",
		Username:  account.Username,
		Status:    "account_rotated",
		Message:   "KOReader Sync Key 已轮换",
	})
	jsonResponse(w, http.StatusOK, mapKOReaderAccountResponse(account))
}

func (c *Controller) toggleKOReaderAccount(w http.ResponseWriter, r *http.Request) {
	accountID, err := strconv.ParseInt(chi.URLParam(r, "accountId"), 10, 64)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid KOReader account ID")
		return
	}
	var req ToggleKOReaderAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid KOReader account payload")
		return
	}
	account, err := c.koreader.SetAccountEnabled(r.Context(), accountID, req.Enabled)
	if err != nil {
		switch {
		case errors.Is(err, ksvc.ErrAccountNotFound):
			jsonError(w, http.StatusNotFound, "KOReader account not found")
		default:
			jsonError(w, http.StatusInternalServerError, "Failed to update KOReader account")
		}
		return
	}
	status := "account_disabled"
	message := "KOReader 账号已停用"
	if account.Enabled {
		status = "account_enabled"
		message = "KOReader 账号已启用"
	}
	_ = c.store.CreateKOReaderSyncEvent(r.Context(), database.CreateKOReaderSyncEventParams{
		Direction: "system",
		Username:  account.Username,
		Status:    status,
		Message:   message,
	})
	jsonResponse(w, http.StatusOK, mapKOReaderAccountResponse(account))
}

func (c *Controller) deleteKOReaderAccount(w http.ResponseWriter, r *http.Request) {
	accountID, err := strconv.ParseInt(chi.URLParam(r, "accountId"), 10, 64)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid KOReader account ID")
		return
	}
	if err := c.koreader.DeleteAccount(r.Context(), accountID); err != nil {
		switch {
		case errors.Is(err, ksvc.ErrAccountNotFound):
			jsonError(w, http.StatusNotFound, "KOReader account not found")
		default:
			jsonError(w, http.StatusInternalServerError, "Failed to delete KOReader account")
		}
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"message": apiText(requestLocale(r), "koreader.account.deleted")})
}

func (c *Controller) resetKOReaderProgress(w http.ResponseWriter, r *http.Request) {
	progressID, err := strconv.ParseInt(chi.URLParam(r, "progressId"), 10, 64)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid KOReader progress ID")
		return
	}
	record, err := c.koreader.ResetProgress(r.Context(), progressID)
	if err != nil {
		switch {
		case errors.Is(err, ksvc.ErrProgressNotFound):
			jsonError(w, http.StatusNotFound, "KOReader progress record not found")
		default:
			jsonError(w, http.StatusInternalServerError, "Failed to reset KOReader progress")
		}
		return
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"message":  apiText(requestLocale(r), "koreader.progress.reset"),
		"id":       record.ID,
		"username": record.Username,
		"document": record.Document,
	})
}

func (c *Controller) updateKOReaderSettings(w http.ResponseWriter, r *http.Request) {
	var req UpdateKOReaderSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid KOReader settings payload")
		return
	}

	var issues []config.ValidationIssue
	req.BasePath = strings.TrimSpace(req.BasePath)
	if req.BasePath == "" {
		req.BasePath = "/koreader"
	}
	if !strings.HasPrefix(req.BasePath, "/") {
		issues = append(issues, config.ValidationIssue{Field: "koreader.base_path", Message: apiText(requestLocale(r), "koreader.validation.base_path_slash"), Severity: "error"})
	}
	req.MatchMode = strings.TrimSpace(strings.ToLower(req.MatchMode))
	if req.MatchMode == "" {
		req.MatchMode = config.KOReaderMatchModeBinaryHash
	}
	switch req.MatchMode {
	case config.KOReaderMatchModeBinaryHash, config.KOReaderMatchModeFilePath:
	default:
		issues = append(issues, config.ValidationIssue{Field: "koreader.match_mode", Message: apiText(requestLocale(r), "koreader.validation.match_mode"), Severity: "error"})
	}
	if len(issues) > 0 {
		jsonResponse(w, http.StatusUnprocessableEntity, map[string]interface{}{
			"error": "KOReader settings validation failed",
			"validation": config.ValidationResult{
				Valid:  false,
				Issues: issues,
			},
		})
		return
	}

	cfg := c.currentConfig()
	cfg.KOReader.Enabled = req.Enabled
	cfg.KOReader.BasePath = req.BasePath
	cfg.KOReader.AllowRegistration = req.AllowRegistration
	cfg.KOReader.MatchMode = req.MatchMode
	cfg.KOReader.PathIgnoreExtension = req.PathIgnoreExtension
	if err := c.persistConfig(&cfg); err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to persist KOReader configuration")
		return
	}

	c.getKOReaderSettings(w, r)
}

func (c *Controller) rebuildKOReaderHashes(w http.ResponseWriter, r *http.Request) {
	if err := c.launchRebuildBookHashesTask(); err != nil {
		jsonError(w, http.StatusConflict, err.Error())
		return
	}
	jsonResponse(w, http.StatusAccepted, map[string]string{"message": apiText(requestLocale(r), "koreader.task.index_rebuild_started")})
}

func (c *Controller) applyKOReaderMatching(w http.ResponseWriter, r *http.Request) {
	if err := c.launchRefreshKOReaderMatchingTask(); err != nil {
		jsonError(w, http.StatusConflict, err.Error())
		return
	}
	jsonResponse(w, http.StatusAccepted, map[string]string{"message": apiText(requestLocale(r), "koreader.task.match_apply_started")})
}

func (c *Controller) reconcileKOReaderProgress(w http.ResponseWriter, r *http.Request) {
	if err := c.launchReconcileKOReaderProgressTask(); err != nil {
		jsonError(w, http.StatusConflict, err.Error())
		return
	}
	jsonResponse(w, http.StatusAccepted, map[string]string{"message": apiText(requestLocale(r), "koreader.task.reconcile_started")})
}

func (c *Controller) launchRebuildBookHashesTask() error {
	key := "rebuild_book_hashes"
	cfg := c.currentConfig()
	if !c.startPausableCancelableTaskMsg(key, "rebuild_book_hashes", "task.msg.koreader_rebuild_hashes.start", nil, 0) {
		return errTaskAlreadyRunning
	}
	c.setTaskMetadata(key, map[string]string{
		"match_mode":            cfg.KOReader.MatchMode,
		"path_ignore_extension": strconv.FormatBool(cfg.KOReader.PathIgnoreExtension),
	}, "")
	c.setTaskEffectiveLimit(key, c.taskLimitsForPath("", true))
	taskCtx, cleanupCancel := c.newTaskContext(key)

	c.runBackground(func() {
		defer cleanupCancel()
		updated, total, err := c.koreader.RebuildBookIdentities(taskCtx, 500, func(current, total int, _ string) {
			c.updateTaskDetailsMsg(key, current, total, "task.msg.koreader_rebuild_hashes.progress", map[string]string{"updated": strconv.Itoa(current), "total": strconv.Itoa(total)}, "hashing", "", map[string]int64{
				"processed_books": int64(current),
			}, nil)
		})
		if errors.Is(err, context.Canceled) {
			c.completeTaskMsg(key, "cancelled", "task.msg.koreader_rebuild_hashes.cancelled", nil)
			return
		}
		if err != nil {
			c.failTaskErrMsg(key, "task.msg.koreader_rebuild_hashes.failed", nil, err.Error())
			return
		}
		c.finishTaskMsg(key, "task.msg.koreader_rebuild_hashes.complete", map[string]string{"updated": strconv.Itoa(updated), "total": strconv.Itoa(total)})
	})
	return nil
}

func (c *Controller) launchReconcileKOReaderProgressTask() error {
	key := "reconcile_koreader_progress"
	if !c.startPausableCancelableTaskMsg(key, "reconcile_koreader_progress", "task.msg.reconcile_koreader_progress.start", nil, 0) {
		return errTaskAlreadyRunning
	}
	cfg := c.currentConfig()
	c.setTaskMetadata(key, map[string]string{
		"match_mode":            cfg.KOReader.MatchMode,
		"path_ignore_extension": strconv.FormatBool(cfg.KOReader.PathIgnoreExtension),
	}, "")
	taskCtx, cleanupCancel := c.newTaskContext(key)

	c.runBackground(func() {
		defer cleanupCancel()
		updated, total, err := c.koreader.ReconcileProgress(taskCtx, 500, func(current, total int, _ string) {
			c.updateTaskDetailsMsg(key, current, total, "task.msg.reconcile_koreader_progress.progress", map[string]string{"processed": strconv.Itoa(current), "total": strconv.Itoa(total)}, "reconciling_progress", "", map[string]int64{
				"processed_progress": int64(current),
			}, nil)
		})
		if errors.Is(err, context.Canceled) {
			c.completeTaskMsg(key, "cancelled", "task.msg.reconcile_koreader_progress.cancelled", nil)
			return
		}
		if err != nil {
			c.failTaskErrMsg(key, "task.msg.reconcile_koreader_progress.failed", nil, err.Error())
			return
		}
		c.finishTaskMsg(key, "task.msg.reconcile_koreader_progress.complete", map[string]string{"updated": strconv.Itoa(updated), "total": strconv.Itoa(total)})
	})
	return nil
}

func (c *Controller) launchRefreshKOReaderMatchingTask() error {
	key := "refresh_koreader_matching"
	if !c.startPausableCancelableTaskMsg(key, "refresh_koreader_matching", "task.msg.refresh_koreader_matching.start", nil, 2) {
		return errTaskAlreadyRunning
	}
	cfg := c.currentConfig()
	c.setTaskMetadata(key, map[string]string{
		"match_mode":            cfg.KOReader.MatchMode,
		"path_ignore_extension": strconv.FormatBool(cfg.KOReader.PathIgnoreExtension),
	}, "")
	c.setTaskEffectiveLimit(key, c.taskLimitsForPath("", true))
	taskCtx, cleanupCancel := c.newTaskContext(key)

	c.runBackground(func() {
		defer cleanupCancel()
		c.updateTaskDetailsMsg(key, 0, 2, "task.msg.refresh_koreader_matching.rebuild_start", nil, "hashing", "", nil, nil)
		updatedBooks, totalBooks, err := c.koreader.RebuildBookIdentities(taskCtx, 500, func(current, total int, _ string) {
			c.updateTaskDetailsMsg(key, 0, 2, "task.msg.koreader_rebuild_hashes.progress", map[string]string{"updated": strconv.Itoa(current), "total": strconv.Itoa(total)}, "hashing", "", map[string]int64{"processed_books": int64(current)}, nil)
		})
		if errors.Is(err, context.Canceled) {
			c.completeTaskMsg(key, "cancelled", "task.msg.refresh_koreader_matching.cancelled", nil)
			return
		}
		if err != nil {
			c.failTaskErrMsg(key, "task.msg.refresh_koreader_matching.rebuild_failed", nil, err.Error())
			return
		}

		c.updateTaskDetailsMsg(key, 1, 2, "task.msg.refresh_koreader_matching.reconcile_start", map[string]string{"updated": strconv.Itoa(updatedBooks), "total": strconv.Itoa(totalBooks)}, "reconciling_progress", "", nil, nil)
		updatedProgress, totalProgress, err := c.koreader.ReconcileProgress(taskCtx, 500, func(current, total int, _ string) {
			c.updateTaskDetailsMsg(key, 1, 2, "task.msg.reconcile_koreader_progress.progress", map[string]string{"processed": strconv.Itoa(current), "total": strconv.Itoa(total)}, "reconciling_progress", "", map[string]int64{"processed_progress": int64(current)}, nil)
		})
		if errors.Is(err, context.Canceled) {
			c.completeTaskMsg(key, "cancelled", "task.msg.refresh_koreader_matching.cancelled", nil)
			return
		}
		if err != nil {
			c.failTaskErrMsg(key, "task.msg.refresh_koreader_matching.reconcile_failed", nil, err.Error())
			return
		}

		c.finishTaskMsg(key, "task.msg.refresh_koreader_matching.complete", map[string]string{"updatedBooks": strconv.Itoa(updatedBooks), "totalBooks": strconv.Itoa(totalBooks), "updatedProgress": strconv.Itoa(updatedProgress), "totalProgress": strconv.Itoa(totalProgress)})
	})
	return nil
}

func (c *Controller) koreaderHealthcheck(w http.ResponseWriter, r *http.Request) {
	writeKOReaderJSON(w, r, http.StatusOK, map[string]string{"state": "OK"})
}

func (c *Controller) koreaderRobots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("User-agent: *\nDisallow: /\n"))
}

func (c *Controller) koreaderRegister(w http.ResponseWriter, r *http.Request) {
	cfg := c.currentConfig()
	if !cfg.KOReader.Enabled {
		jsonError(w, http.StatusServiceUnavailable, "KOReader sync is disabled")
		return
	}
	if !cfg.KOReader.AllowRegistration {
		writeKOReaderJSON(w, r, http.StatusForbidden, map[string]interface{}{
			"code":    http.StatusForbidden,
			"message": "Registration is disabled. Create KOReader accounts from the admin UI.",
		})
		return
	}

	var req KOReaderRegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeKOReaderJSON(w, r, http.StatusBadRequest, map[string]interface{}{
			"code":    http.StatusBadRequest,
			"message": "Invalid request",
		})
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.Password = strings.TrimSpace(req.Password)

	account, err := c.koreader.RegisterDevice(r.Context(), req.Username, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, ksvc.ErrAlreadyConfigured):
			writeKOReaderJSON(w, r, http.StatusPaymentRequired, map[string]interface{}{
				"code":    http.StatusPaymentRequired,
				"message": "Username is already registered.",
			})
		case errors.Is(err, ksvc.ErrUnauthorized):
			writeKOReaderJSON(w, r, http.StatusBadRequest, map[string]interface{}{
				"code":    http.StatusBadRequest,
				"message": "Invalid request",
			})
		default:
			slog.Error("KOReader self-registration failed", "username", req.Username, "error", err)
			writeKOReaderJSON(w, r, http.StatusInternalServerError, map[string]interface{}{
				"code":    http.StatusInternalServerError,
				"message": "Unknown server error",
			})
		}
		return
	}

	_ = c.store.CreateKOReaderSyncEvent(r.Context(), database.CreateKOReaderSyncEventParams{
		Direction: "system",
		Username:  account.Username,
		Status:    "account_created",
		Message:   "KOReader 设备自助注册创建账号",
	})
	slog.Info("KOReader self-registration succeeded",
		"username", account.Username,
		"client_ip", requestClientIP(r),
	)
	writeKOReaderJSON(w, r, http.StatusCreated, map[string]string{"username": account.Username})
}

func (c *Controller) koreaderAuth(w http.ResponseWriter, r *http.Request) {
	if !c.currentConfig().KOReader.Enabled {
		slog.Warn("KOReader auth request rejected: service disabled",
			"username", strings.TrimSpace(r.Header.Get("x-auth-user")),
			"client_ip", requestClientIP(r),
			"user_agent", r.UserAgent(),
		)
		jsonError(w, http.StatusServiceUnavailable, "KOReader sync is disabled")
		return
	}
	creds := readKOReaderCredentials(r)
	slog.Info("KOReader auth request received",
		"username", creds.Username,
		"client_key_prefix", authKeyPreview(creds.Key),
		"client_ip", requestClientIP(r),
		"user_agent", r.UserAgent(),
		"accept", r.Header.Get("Accept"),
	)
	_, err := c.koreader.Authenticate(r.Context(), creds)
	if err != nil {
		c.logKOReaderAuthFailure(r.Context(), creds, "", err)
		writeKOReaderAuthError(w, r, err)
		return
	}
	writeKOReaderJSON(w, r, http.StatusOK, map[string]string{"state": "OK", "authorized": "OK"})
}

func (c *Controller) koreaderUpdateProgress(w http.ResponseWriter, r *http.Request) {
	if !c.currentConfig().KOReader.Enabled {
		slog.Warn("KOReader progress push rejected: service disabled",
			"username", strings.TrimSpace(r.Header.Get("x-auth-user")),
			"client_ip", requestClientIP(r),
			"user_agent", r.UserAgent(),
		)
		jsonError(w, http.StatusServiceUnavailable, "KOReader sync is disabled")
		return
	}
	var payload ksvc.ProgressPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request")
		return
	}

	creds := readKOReaderCredentials(r)
	slog.Info("KOReader progress push request received",
		"username", creds.Username,
		"client_key_prefix", authKeyPreview(creds.Key),
		"document", strings.TrimSpace(payload.Document),
		"device", strings.TrimSpace(payload.Device),
		"device_id", strings.TrimSpace(payload.DeviceID),
		"client_ip", requestClientIP(r),
	)
	result, err := c.koreader.SaveProgress(r.Context(), creds, payload)
	if err != nil {
		c.logKOReaderAuthFailure(r.Context(), creds, payload.Document, err)
		writeKOReaderAuthError(w, r, err)
		return
	}
	writeKOReaderJSON(w, r, http.StatusOK, map[string]interface{}{
		"state":     "OK",
		"document":  result.Record.Document,
		"timestamp": result.Record.Timestamp,
	})
}

func (c *Controller) koreaderGetProgress(w http.ResponseWriter, r *http.Request) {
	if !c.currentConfig().KOReader.Enabled {
		slog.Warn("KOReader progress pull rejected: service disabled",
			"username", strings.TrimSpace(r.Header.Get("x-auth-user")),
			"document", chi.URLParam(r, "document"),
			"client_ip", requestClientIP(r),
			"user_agent", r.UserAgent(),
		)
		jsonError(w, http.StatusServiceUnavailable, "KOReader sync is disabled")
		return
	}
	creds := readKOReaderCredentials(r)
	document := chi.URLParam(r, "document")
	slog.Info("KOReader progress pull request received",
		"username", creds.Username,
		"client_key_prefix", authKeyPreview(creds.Key),
		"document", document,
		"client_ip", requestClientIP(r),
		"user_agent", r.UserAgent(),
	)
	record, err := c.koreader.GetProgress(r.Context(), creds, document)
	if err != nil {
		switch {
		case errors.Is(err, ksvc.ErrProgressNotFound):
			writeKOReaderJSON(w, r, http.StatusNotFound, map[string]string{"message": "Not found"})
		case errors.Is(err, ksvc.ErrForbidden), errors.Is(err, ksvc.ErrUnauthorized):
			c.logKOReaderAuthFailure(r.Context(), creds, document, err)
			writeKOReaderAuthError(w, r, err)
		default:
			writeKOReaderJSON(w, r, http.StatusInternalServerError, map[string]string{"message": "Unknown server error"})
		}
		return
	}
	writeKOReaderJSON(w, r, http.StatusOK, map[string]interface{}{
		"state":      "OK",
		"username":   record.Username,
		"document":   record.Document,
		"progress":   record.Progress,
		"percentage": record.Percentage,
		"device":     record.Device,
		"device_id":  record.DeviceID,
		"timestamp":  record.Timestamp,
	})
}

func readKOReaderCredentials(r *http.Request) ksvc.Credentials {
	return ksvc.Credentials{
		Username: strings.TrimSpace(r.Header.Get("x-auth-user")),
		Key:      strings.TrimSpace(r.Header.Get("x-auth-key")),
	}
}

func writeKOReaderJSON(w http.ResponseWriter, r *http.Request, status int, data interface{}) {
	contentType := "application/json"
	if r != nil && strings.Contains(strings.ToLower(r.Header.Get("Accept")), "application/vnd.koreader.v1+json") {
		contentType = "application/vnd.koreader.v1+json"
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeKOReaderAuthError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ksvc.ErrUnauthorized):
		writeKOReaderJSON(w, r, http.StatusUnauthorized, map[string]string{"message": "Unauthorized"})
	case errors.Is(err, ksvc.ErrForbidden):
		writeKOReaderJSON(w, r, http.StatusForbidden, map[string]string{"message": "Forbidden"})
	default:
		writeKOReaderJSON(w, r, http.StatusInternalServerError, map[string]string{"message": "Unknown server error"})
	}
}

func (c *Controller) logKOReaderAuthFailure(ctx context.Context, creds ksvc.Credentials, document string, err error) {
	status := "auth_failed"
	message := "Unauthorized"
	switch {
	case errors.Is(err, ksvc.ErrForbidden):
		status = "auth_failed_forbidden"
		message = "Forbidden"
	case errors.Is(err, ksvc.ErrUnauthorized):
		status = "auth_failed_invalid_key"
		message = "Unauthorized"
	default:
		return
	}
	_ = c.store.CreateKOReaderSyncEvent(ctx, database.CreateKOReaderSyncEventParams{
		Direction: "auth",
		Username:  strings.TrimSpace(creds.Username),
		Document:  strings.TrimSpace(document),
		Status:    status,
		Message:   message,
	})
	slog.Warn("KOReader auth-related request failed",
		"username", strings.TrimSpace(creds.Username),
		"document", strings.TrimSpace(document),
		"status", status,
		"message", message,
		"client_key_prefix", authKeyPreview(creds.Key),
	)
}

func requestClientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	for _, header := range []string{"X-Forwarded-For", "X-Real-IP"} {
		value := strings.TrimSpace(r.Header.Get(header))
		if value == "" {
			continue
		}
		if header == "X-Forwarded-For" && strings.Contains(value, ",") {
			parts := strings.Split(value, ",")
			return strings.TrimSpace(parts[0])
		}
		return value
	}
	return r.RemoteAddr
}

func authKeyPreview(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "<empty>"
	}
	if len(value) <= 8 {
		return value
	}
	return value[:8]
}
