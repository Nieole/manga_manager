package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func koreaderIndexLabel(cfg config.Config) string {
	if cfg.KOReader.MatchMode == config.KOReaderMatchModeFilePath {
		if cfg.KOReader.PathIgnoreExtension {
			return "路径索引（忽略扩展名）"
		}
		return "路径索引"
	}
	return "二进制哈希索引"
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
	result := make([]KOReaderUnmatchedItem, 0, len(items))
	for _, item := range items {
		suggestion := "建议先重建 KOReader 匹配索引，再重关联未匹配记录。"
		if cfg.KOReader.MatchMode == config.KOReaderMatchModeFilePath {
			suggestion = fmt.Sprintf("请确认 KOReader 上报路径在文件名及向上 %d 层路径范围内可对应本地书籍。", config.KOReaderPathMatchDepth)
			if cfg.KOReader.PathIgnoreExtension {
				suggestion += " 当前已忽略扩展名。"
			}
		} else {
			suggestion = "请确认 KOReader 当前使用的是二进制哈希匹配，并先重建匹配索引。"
		}
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
					{Field: "koreader.accounts.username", Message: "用户名不能为空。", Severity: "error"},
				},
			},
		})
		return
	}
	account, err := c.koreader.CreateAccount(r.Context(), req.Username)
	if err != nil {
		switch {
		case errors.Is(err, ksvc.ErrAlreadyConfigured):
			jsonResponse(w, http.StatusConflict, map[string]string{"error": "KOReader 用户名已存在"})
		case errors.Is(err, ksvc.ErrUnauthorized):
			jsonResponse(w, http.StatusUnprocessableEntity, map[string]interface{}{
				"error": "KOReader account validation failed",
				"validation": config.ValidationResult{
					Valid: false,
					Issues: []config.ValidationIssue{
						{Field: "koreader.accounts.username", Message: "用户名不能为空。", Severity: "error"},
					},
				},
			})
		default:
			jsonError(w, http.StatusInternalServerError, "Failed to create KOReader account")
		}
		return
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
	jsonResponse(w, http.StatusOK, map[string]string{"message": "KOReader 账号已删除"})
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
		issues = append(issues, config.ValidationIssue{Field: "koreader.base_path", Message: "同步路径必须以 / 开头。", Severity: "error"})
	}
	req.MatchMode = strings.TrimSpace(strings.ToLower(req.MatchMode))
	if req.MatchMode == "" {
		req.MatchMode = config.KOReaderMatchModeBinaryHash
	}
	switch req.MatchMode {
	case config.KOReaderMatchModeBinaryHash, config.KOReaderMatchModeFilePath:
	default:
		issues = append(issues, config.ValidationIssue{Field: "koreader.match_mode", Message: "匹配模式必须是 binary_hash 或 file_path。", Severity: "error"})
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
	jsonResponse(w, http.StatusAccepted, map[string]string{"message": "KOReader 索引重建已启动"})
}

func (c *Controller) applyKOReaderMatching(w http.ResponseWriter, r *http.Request) {
	if err := c.launchRefreshKOReaderMatchingTask(); err != nil {
		jsonError(w, http.StatusConflict, err.Error())
		return
	}
	jsonResponse(w, http.StatusAccepted, map[string]string{"message": "KOReader 匹配规则应用任务已启动"})
}

func (c *Controller) reconcileKOReaderProgress(w http.ResponseWriter, r *http.Request) {
	if err := c.launchReconcileKOReaderProgressTask(); err != nil {
		jsonError(w, http.StatusConflict, err.Error())
		return
	}
	jsonResponse(w, http.StatusAccepted, map[string]string{"message": "未匹配同步记录重关联已启动"})
}

func (c *Controller) launchRebuildBookHashesTask() error {
	key := "rebuild_book_hashes"
	cfg := c.currentConfig()
	indexLabel := koreaderIndexLabel(cfg)
	if !c.startTask(key, "rebuild_book_hashes", fmt.Sprintf("开始重建 KOReader %s", indexLabel), 0) {
		return fmt.Errorf("task already running")
	}
	c.setTaskMetadata(key, map[string]string{
		"match_mode":            cfg.KOReader.MatchMode,
		"path_ignore_extension": strconv.FormatBool(cfg.KOReader.PathIgnoreExtension),
	}, "系统")

	go func() {
		updated, total, err := c.koreader.RebuildBookIdentities(context.Background(), 500, func(current, total int, message string) {
			c.updateTask(key, current, total, message)
		})
		if err != nil {
			c.failTaskWithError(key, fmt.Sprintf("KOReader %s重建失败: %v", indexLabel, err), err.Error())
			return
		}
		c.finishTask(key, fmt.Sprintf("KOReader %s重建完成，已更新 %d / %d 本书籍", indexLabel, updated, total))
	}()
	return nil
}

func (c *Controller) launchReconcileKOReaderProgressTask() error {
	key := "reconcile_koreader_progress"
	if !c.startTask(key, "reconcile_koreader_progress", "开始重关联 KOReader 未匹配进度", 0) {
		return fmt.Errorf("task already running")
	}
	cfg := c.currentConfig()
	c.setTaskMetadata(key, map[string]string{
		"match_mode":            cfg.KOReader.MatchMode,
		"path_ignore_extension": strconv.FormatBool(cfg.KOReader.PathIgnoreExtension),
	}, "系统")

	go func() {
		updated, total, err := c.koreader.ReconcileProgress(context.Background(), 500, func(current, total int, message string) {
			c.updateTask(key, current, total, message)
		})
		if err != nil {
			c.failTaskWithError(key, fmt.Sprintf("KOReader 进度重关联失败: %v", err), err.Error())
			return
		}
		c.finishTask(key, fmt.Sprintf("KOReader 进度重关联完成，已更新 %d / %d 条记录", updated, total))
	}()
	return nil
}

func (c *Controller) launchRefreshKOReaderMatchingTask() error {
	key := "refresh_koreader_matching"
	if !c.startTask(key, "refresh_koreader_matching", "开始应用 KOReader 匹配规则变更", 2) {
		return fmt.Errorf("task already running")
	}
	cfg := c.currentConfig()
	c.setTaskMetadata(key, map[string]string{
		"match_mode":            cfg.KOReader.MatchMode,
		"path_ignore_extension": strconv.FormatBool(cfg.KOReader.PathIgnoreExtension),
	}, "系统")

	go func() {
		indexLabel := koreaderIndexLabel(cfg)
		c.updateTask(key, 0, 2, fmt.Sprintf("开始重建 KOReader %s", indexLabel))
		updatedBooks, totalBooks, err := c.koreader.RebuildBookIdentities(context.Background(), 500, nil)
		if err != nil {
			c.failTaskWithError(key, fmt.Sprintf("KOReader %s重建失败: %v", indexLabel, err), err.Error())
			return
		}

		c.updateTask(key, 1, 2, fmt.Sprintf("%s已更新 %d / %d，本阶段开始重关联未匹配记录", indexLabel, updatedBooks, totalBooks))
		updatedProgress, totalProgress, err := c.koreader.ReconcileProgress(context.Background(), 500, nil)
		if err != nil {
			c.failTaskWithError(key, fmt.Sprintf("KOReader 进度重关联失败: %v", err), err.Error())
			return
		}

		c.finishTask(key, fmt.Sprintf("KOReader 匹配规则已应用，%s更新 %d / %d，重关联 %d / %d", indexLabel, updatedBooks, totalBooks, updatedProgress, totalProgress))
	}()
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
	if !c.currentConfig().KOReader.Enabled {
		jsonError(w, http.StatusServiceUnavailable, "KOReader sync is disabled")
		return
	}
	writeKOReaderJSON(w, r, http.StatusForbidden, map[string]string{
		"message": "Device self-registration is disabled. Create KOReader accounts from the admin UI.",
	})
}

func (c *Controller) koreaderAuth(w http.ResponseWriter, r *http.Request) {
	if !c.currentConfig().KOReader.Enabled {
		jsonError(w, http.StatusServiceUnavailable, "KOReader sync is disabled")
		return
	}
	_, err := c.koreader.Authenticate(r.Context(), readKOReaderCredentials(r))
	if err != nil {
		c.logKOReaderAuthFailure(r.Context(), readKOReaderCredentials(r), "", err)
		writeKOReaderAuthError(w, r, err)
		return
	}
	writeKOReaderJSON(w, r, http.StatusOK, map[string]string{"state": "OK", "authorized": "OK"})
}

func (c *Controller) koreaderUpdateProgress(w http.ResponseWriter, r *http.Request) {
	if !c.currentConfig().KOReader.Enabled {
		jsonError(w, http.StatusServiceUnavailable, "KOReader sync is disabled")
		return
	}
	var payload ksvc.ProgressPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request")
		return
	}

	result, err := c.koreader.SaveProgress(r.Context(), readKOReaderCredentials(r), payload)
	if err != nil {
		c.logKOReaderAuthFailure(r.Context(), readKOReaderCredentials(r), payload.Document, err)
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
		jsonError(w, http.StatusServiceUnavailable, "KOReader sync is disabled")
		return
	}
	record, err := c.koreader.GetProgress(r.Context(), readKOReaderCredentials(r), chi.URLParam(r, "document"))
	if err != nil {
		switch {
		case errors.Is(err, ksvc.ErrProgressNotFound):
			writeKOReaderJSON(w, r, http.StatusNotFound, map[string]string{"message": "Not found"})
		case errors.Is(err, ksvc.ErrForbidden), errors.Is(err, ksvc.ErrUnauthorized):
			c.logKOReaderAuthFailure(r.Context(), readKOReaderCredentials(r), chi.URLParam(r, "document"), err)
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
}
