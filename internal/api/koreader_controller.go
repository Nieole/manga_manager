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
	// ID 是 "<来源表>:<主键>" 形式的复合标识，只用于前端 key，不是任何接口的入参。
	//
	// 不能是裸整数：这个列表是 koreader_progress 与 koreader_sync_events 的 UNION ALL，
	// 两张表各有独立的 AUTOINCREMENT、都从 1 开始，同一个 id 同时是两边的合法主键。
	// 裸整数一旦被前端当进度主键传给「重置进度」，删掉的就是另一台设备的阅读进度。
	ID          string `json:"id"`
	SourceTable string `json:"source_table"`
	// ProgressID 只在这一行确实对应一条进度记录时才有值；「重置进度」只能用它。
	ProgressID    *int64  `json:"progress_id,omitempty"`
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
			ID:            fmt.Sprintf("%s:%d", conflict.SourceTable, conflict.SourceID),
			SourceTable:   conflict.SourceTable,
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
		if conflict.ProgressID.Valid {
			progressID := conflict.ProgressID.Int64
			item.ProgressID = &progressID
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
		writeTaskLaunchError(w, err, "A KOReader index rebuild is already running", "Failed to start KOReader index rebuild")
		return
	}
	jsonResponse(w, http.StatusAccepted, map[string]string{"message": apiText(requestLocale(r), "koreader.task.index_rebuild_started")})
}

func (c *Controller) applyKOReaderMatching(w http.ResponseWriter, r *http.Request) {
	if err := c.launchRefreshKOReaderMatchingTask(); err != nil {
		writeTaskLaunchError(w, err, "A KOReader matching refresh is already running", "Failed to start KOReader matching refresh")
		return
	}
	jsonResponse(w, http.StatusAccepted, map[string]string{"message": apiText(requestLocale(r), "koreader.task.match_apply_started")})
}

func (c *Controller) reconcileKOReaderProgress(w http.ResponseWriter, r *http.Request) {
	if err := c.launchReconcileKOReaderProgressTask(); err != nil {
		writeTaskLaunchError(w, err, "A KOReader progress reconciliation is already running", "Failed to start KOReader progress reconciliation")
		return
	}
	jsonResponse(w, http.StatusAccepted, map[string]string{"message": apiText(requestLocale(r), "koreader.task.reconcile_started")})
}

// koreaderTaskBatchSize 是三个 KOReader 任务每批取多少条待处理记录：一次查询取回多少行、
// 因而一次在内存里握住多少行。三处取同一个值，同一个库上的批次节奏才一致。
const koreaderTaskBatchSize = 500

// koreaderMatchMetadata 是三个 KOReader 任务共同的**任务参数**：任务中心按 match_mode 与
// path_ignore_extension 渲染「路径索引 / 二进制哈希索引」那块标签（前端 formatKOReaderIndexLabel）。
// 三处必须写同一份，缺一个键界面就回落到默认标签，用户看到的索引类型是错的。
func koreaderMatchMetadata(cfg config.Config) map[string]string {
	return map[string]string{
		"match_mode":            cfg.KOReader.MatchMode,
		"path_ignore_extension": strconv.FormatBool(cfg.KOReader.PathIgnoreExtension),
	}
}

// koreaderFingerprintFrame 把一次**指纹**重建进度翻成**一帧**：**计数推进**、**阶段**、文案与
// 指标同属一次事件，拆开报会被投递水位撕断（撕开的样子见 TaskProgress.Report）。
// 书籍指纹重建与匹配刷新的第一阶段共用它，帧的内容因此只有一份。
//
// 档位与卷键不进帧的**标签**，只走**任务参数**那条通道（`taskIOMetricsParams` 会把空值滤掉）：
// **路径**匹配模式下这个任务一次盘都不读，两项恒为空，而标签是有一个显示一个——写进去等于
// 把「没有这回事」显示成「实况为空」。IO 那几项指标同样恒为零，但面板只显示大于零的指标。
func koreaderFingerprintFrame(current, total int, metrics taskIOMetrics) TaskFrame {
	frameMetrics := metrics.frameMetrics()
	frameMetrics["processed_books"] = int64(current)
	return TaskFrame{
		Current: &current,
		Total:   &total,
		Phase:   "hashing",
		Code:    "task.msg.koreader_rebuild_hashes.progress",
		Params:  map[string]string{"updated": strconv.Itoa(current), "total": strconv.Itoa(total)},
		Metrics: frameMetrics,
	}
}

// koreaderReconcileFrame 把一次进度对账翻成一帧，整帧报出的理由同 koreaderFingerprintFrame。
// 进度对账与匹配刷新的第二阶段共用它。
func koreaderReconcileFrame(current, total int) TaskFrame {
	return TaskFrame{
		Current: &current,
		Total:   &total,
		Phase:   "reconciling_progress",
		Code:    "task.msg.reconcile_koreader_progress.progress",
		Params:  map[string]string{"processed": strconv.Itoa(current), "total": strconv.Itoa(total)},
		Metrics: map[string]int64{"processed_progress": int64(current)},
	}
}

// holdingStepCount 摘掉一帧的计数推进，把进度条留在原处，其余照报。
//
// 只有匹配刷新用它：那个任务的进度条数的是它自己的两个阶段（0/2 → 1/2），而两个阶段内部复用的
// 正是上面两个帧——原样报进去，用户会看到「40 / 共 2」这种读数。逐条目计数在那里只进占位参数
// 与指标，两处都还在帧里。
func holdingStepCount(frame TaskFrame) TaskFrame {
	frame.Current, frame.Total = nil, nil
	return frame
}

// launchRebuildBookHashesTask 是书籍**指纹**重建任务的启动点，走引擎的启动入口。
func (c *Controller) launchRebuildBookHashesTask() error {
	spec := TaskSpec{
		Key:          "rebuild_book_hashes",
		Type:         "rebuild_book_hashes",
		StartCode:    "task.msg.koreader_rebuild_hashes.start",
		CanCancel:    true,
		CanPause:     true,
		Metadata:     koreaderMatchMetadata(c.currentConfig()),
		Limits:       c.taskLimitsForPath("", true),
		CompleteCode: "task.msg.koreader_rebuild_hashes.complete",
		CancelCode:   "task.msg.koreader_rebuild_hashes.cancelled",
		FailCode:     "task.msg.koreader_rebuild_hashes.failed",
	}

	return c.taskEngine.Run(spec, func(ctx context.Context, tp *TaskProgress) (TaskResult, error) {
		metrics := taskIOMetrics{}
		opts := ksvc.RebuildOptions{BatchSize: koreaderTaskBatchSize, AbsorbDiskWork: metrics.absorbHashedFile}
		updated, total, err := c.koreader.RebuildBookIdentities(ctx, opts, func(current, total int) {
			tp.Report(koreaderFingerprintFrame(current, total, metrics))
			// IO 参数走的是另一条通道（存储 IO 面板按参数名读），只能单独报一次。
			tp.MergeParams(taskIOMetricsParams(metrics))
		})
		if err != nil {
			return TaskResult{}, err
		}
		return TaskResult{Params: map[string]string{"updated": strconv.Itoa(updated), "total": strconv.Itoa(total)}}, nil
	})
}

// launchReconcileKOReaderProgressTask 是 KOReader 进度对账任务的启动点，走引擎的启动入口。
//
// 三个任务里只有它不报并发上限：另外两个要逐本读书文件算指纹，taskLimitsForPath 给出的正是
// 那条路径上的上限，而对账只重算已落库记录的归属。零值的 Limits 表示「没有上限可报」，
// 不是「上限为 0」——后者会在任务面板上多出一组假数据（见 TaskSpec.Limits）。
func (c *Controller) launchReconcileKOReaderProgressTask() error {
	spec := TaskSpec{
		Key:          "reconcile_koreader_progress",
		Type:         "reconcile_koreader_progress",
		StartCode:    "task.msg.reconcile_koreader_progress.start",
		CanCancel:    true,
		CanPause:     true,
		Metadata:     koreaderMatchMetadata(c.currentConfig()),
		CompleteCode: "task.msg.reconcile_koreader_progress.complete",
		CancelCode:   "task.msg.reconcile_koreader_progress.cancelled",
		FailCode:     "task.msg.reconcile_koreader_progress.failed",
	}

	return c.taskEngine.Run(spec, func(ctx context.Context, tp *TaskProgress) (TaskResult, error) {
		updated, total, err := c.koreader.ReconcileProgress(ctx, koreaderTaskBatchSize, func(current, total int) {
			tp.Report(koreaderReconcileFrame(current, total))
		})
		if err != nil {
			return TaskResult{}, err
		}
		return TaskResult{Params: map[string]string{"updated": strconv.Itoa(updated), "total": strconv.Itoa(total)}}, nil
	})
}

// launchRefreshKOReaderMatchingTask 是匹配刷新任务的启动点，走引擎的启动入口。
//
// 它是**两阶段**任务：先重建**指纹**，再对账进度。进度条数的因此是阶段而不是条目——总数在
// 任务声明里就是 2，两个阶段的逐条目回调一次都不动它，逐条目计数只进占位参数与指标。
//
// 两步各有专属失败文案码，取消则无论停在哪一步都落同一条取消码：taskFailure 把取消挡在专属码
// 之外，否则用户按下取消看到的会是「索引重建失败」。
//
// 任务声明里那条默认失败码今天走不到（两步的失败各被覆盖、取消走取消码）。留着是因为
// FailCode 留空会让将来任何一条未覆盖的失败路径把**起始**文案原样渲染成失败态的文案。
func (c *Controller) launchRefreshKOReaderMatchingTask() error {
	spec := TaskSpec{
		Key:          "refresh_koreader_matching",
		Type:         "refresh_koreader_matching",
		StartCode:    "task.msg.refresh_koreader_matching.start",
		Total:        2,
		CanCancel:    true,
		CanPause:     true,
		Metadata:     koreaderMatchMetadata(c.currentConfig()),
		Limits:       c.taskLimitsForPath("", true),
		CompleteCode: "task.msg.refresh_koreader_matching.complete",
		CancelCode:   "task.msg.refresh_koreader_matching.cancelled",
		FailCode:     "task.msg.refresh_koreader_matching.failed",
	}

	return c.taskEngine.Run(spec, func(ctx context.Context, tp *TaskProgress) (TaskResult, error) {
		tp.Phase("hashing", "task.msg.refresh_koreader_matching.rebuild_start", nil)
		metrics := taskIOMetrics{}
		opts := ksvc.RebuildOptions{BatchSize: koreaderTaskBatchSize, AbsorbDiskWork: metrics.absorbHashedFile}
		updatedBooks, totalBooks, err := c.koreader.RebuildBookIdentities(ctx, opts, func(current, total int) {
			tp.Report(holdingStepCount(koreaderFingerprintFrame(current, total, metrics)))
			tp.MergeParams(taskIOMetricsParams(metrics))
		})
		if err != nil {
			return taskFailure("task.msg.refresh_koreader_matching.rebuild_failed", err), err
		}

		// 阶段跃迁与阶段计数是同一件事，必须一帧报出：分成两次的话，先出去的那条载荷带着
		// 新计数与旧阶段名，而补齐的那条会被水位吞掉（阶段与文案码此时已经一字未变）。
		reconcileStep := 1
		tp.Report(TaskFrame{
			Current: &reconcileStep,
			Phase:   "reconciling_progress",
			Code:    "task.msg.refresh_koreader_matching.reconcile_start",
			Params:  map[string]string{"updated": strconv.Itoa(updatedBooks), "total": strconv.Itoa(totalBooks)},
		})
		updatedProgress, totalProgress, err := c.koreader.ReconcileProgress(ctx, koreaderTaskBatchSize, func(current, total int) {
			tp.Report(holdingStepCount(koreaderReconcileFrame(current, total)))
		})
		if err != nil {
			return taskFailure("task.msg.refresh_koreader_matching.reconcile_failed", err), err
		}

		return TaskResult{Params: map[string]string{
			"updatedBooks":    strconv.Itoa(updatedBooks),
			"totalBooks":      strconv.Itoa(totalBooks),
			"updatedProgress": strconv.Itoa(updatedProgress),
			"totalProgress":   strconv.Itoa(totalProgress),
		}}, nil
	})
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
		"client_ip", c.clientIP(r),
	)
	writeKOReaderJSON(w, r, http.StatusCreated, map[string]string{"username": account.Username})
}

func (c *Controller) koreaderAuth(w http.ResponseWriter, r *http.Request) {
	if !c.currentConfig().KOReader.Enabled {
		slog.Warn("KOReader auth request rejected: service disabled",
			"username", strings.TrimSpace(r.Header.Get("x-auth-user")),
			"client_ip", c.clientIP(r),
			"user_agent", r.UserAgent(),
		)
		jsonError(w, http.StatusServiceUnavailable, "KOReader sync is disabled")
		return
	}
	creds := readKOReaderCredentials(r)
	slog.Info("KOReader auth request received",
		"username", creds.Username,
		"client_key_prefix", authKeyPreview(creds.Key),
		"client_ip", c.clientIP(r),
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
			"client_ip", c.clientIP(r),
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
		"client_ip", c.clientIP(r),
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
			"client_ip", c.clientIP(r),
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
		"client_ip", c.clientIP(r),
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
	var status, message string
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
