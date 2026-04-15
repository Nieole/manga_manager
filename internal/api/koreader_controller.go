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
	Username            string                 `json:"username"`
	HasPassword         bool                   `json:"has_password"`
	Stats               database.KOReaderStats `json:"stats"`
}

type UpdateKOReaderSettingsRequest struct {
	Enabled             bool   `json:"enabled"`
	BasePath            string `json:"base_path"`
	AllowRegistration   bool   `json:"allow_registration"`
	MatchMode           string `json:"match_mode"`
	PathIgnoreExtension bool   `json:"path_ignore_extension"`
	Username            string `json:"username"`
	Password            string `json:"password"`
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
	cfg := c.currentConfig()
	jsonResponse(w, http.StatusOK, KOReaderSystemResponse{
		Enabled:             cfg.KOReader.Enabled,
		BasePath:            cfg.KOReader.BasePath,
		AllowRegistration:   cfg.KOReader.AllowRegistration,
		MatchMode:           cfg.KOReader.MatchMode,
		PathIgnoreExtension: cfg.KOReader.PathIgnoreExtension,
		PathMatchDepth:      config.KOReaderPathMatchDepth,
		Username:            stats.Username,
		HasPassword:         stats.HasPassword,
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
	req.Username = strings.TrimSpace(req.Username)
	if req.Enabled && req.Username == "" {
		issues = append(issues, config.ValidationIssue{Field: "koreader.username", Message: "启用同步后必须配置用户名。", Severity: "error"})
	}

	stats, err := c.store.GetKOReaderStats(r.Context())
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to load KOReader status")
		return
	}
	if req.Enabled && !stats.HasPassword && strings.TrimSpace(req.Password) == "" {
		issues = append(issues, config.ValidationIssue{Field: "koreader.password", Message: "首次启用同步时必须设置同步密钥。", Severity: "error"})
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

	if _, err := c.store.UpsertKOReaderSettings(r.Context(), database.UpsertKOReaderSettingsParams{
		Username:     req.Username,
		PasswordHash: passwordHashOrEmpty(req.Password),
	}); err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to persist KOReader credentials")
		return
	}

	c.getKOReaderSettings(w, r)
}

func (c *Controller) rebuildKOReaderHashes(w http.ResponseWriter, r *http.Request) {
	if err := c.launchRebuildBookHashesTask(); err != nil {
		jsonError(w, http.StatusConflict, err.Error())
		return
	}
	jsonResponse(w, http.StatusAccepted, map[string]string{"message": "KOReader 匹配索引重建已启动"})
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
	if !c.startTask(key, "rebuild_book_hashes", "开始重建 KOReader 匹配索引", 0) {
		return fmt.Errorf("task already running")
	}
	c.setTaskMetadata(key, nil, "系统")

	go func() {
		updated, total, err := c.koreader.RebuildBookIdentities(context.Background(), 10000, func(current, total int, message string) {
			c.updateTask(key, current, total, message)
		})
		if err != nil {
			c.failTaskWithError(key, fmt.Sprintf("KOReader 匹配索引重建失败: %v", err), err.Error())
			return
		}
		c.finishTask(key, fmt.Sprintf("KOReader 匹配索引重建完成，已更新 %d / %d 本书籍", updated, total))
	}()
	return nil
}

func (c *Controller) launchReconcileKOReaderProgressTask() error {
	key := "reconcile_koreader_progress"
	if !c.startTask(key, "reconcile_koreader_progress", "开始重关联 KOReader 未匹配进度", 0) {
		return fmt.Errorf("task already running")
	}
	c.setTaskMetadata(key, nil, "系统")

	go func() {
		updated, total, err := c.koreader.ReconcileProgress(context.Background(), 10000, func(current, total int, message string) {
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
		c.updateTask(key, 0, 2, "开始重建 KOReader 匹配索引")
		updatedBooks, totalBooks, err := c.koreader.RebuildBookIdentities(context.Background(), 10000, nil)
		if err != nil {
			c.failTaskWithError(key, fmt.Sprintf("KOReader 匹配索引重建失败: %v", err), err.Error())
			return
		}

		c.updateTask(key, 1, 2, fmt.Sprintf("匹配索引已更新 %d / %d，本阶段开始重关联未匹配记录", updatedBooks, totalBooks))
		updatedProgress, totalProgress, err := c.koreader.ReconcileProgress(context.Background(), 10000, nil)
		if err != nil {
			c.failTaskWithError(key, fmt.Sprintf("KOReader 进度重关联失败: %v", err), err.Error())
			return
		}

		c.finishTask(key, fmt.Sprintf("KOReader 匹配规则已应用，索引更新 %d / %d，重关联 %d / %d", updatedBooks, totalBooks, updatedProgress, totalProgress))
	}()
	return nil
}

func (c *Controller) koreaderHealthcheck(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"message": "healthy",
		"enabled": c.currentConfig().KOReader.Enabled,
	})
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

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request")
		return
	}

	settings, err := c.koreader.Register(r.Context(), req.Username, req.Password, c.currentConfig().KOReader.AllowRegistration)
	if err != nil {
		switch {
		case errors.Is(err, ksvc.ErrRegistrationClosed):
			jsonResponse(w, http.StatusForbidden, map[string]string{"message": "This server is currently not accepting new registrations."})
		case errors.Is(err, ksvc.ErrAlreadyConfigured):
			jsonResponse(w, http.StatusConflict, "Username is already registered.")
		case errors.Is(err, ksvc.ErrUnauthorized):
			jsonError(w, http.StatusBadRequest, "Invalid request")
		default:
			jsonError(w, http.StatusInternalServerError, "Unknown server error")
		}
		return
	}
	jsonResponse(w, http.StatusCreated, map[string]string{"username": settings.Username})
}

func (c *Controller) koreaderAuth(w http.ResponseWriter, r *http.Request) {
	if !c.currentConfig().KOReader.Enabled {
		jsonError(w, http.StatusServiceUnavailable, "KOReader sync is disabled")
		return
	}
	_, err := c.koreader.Authenticate(r.Context(), readKOReaderCredentials(r))
	if err != nil {
		writeKOReaderAuthError(w, err)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"authorized": "OK"})
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
		writeKOReaderAuthError(w, err)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{
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
			jsonResponse(w, http.StatusNotFound, map[string]string{"message": "Not found"})
		case errors.Is(err, ksvc.ErrForbidden), errors.Is(err, ksvc.ErrUnauthorized):
			writeKOReaderAuthError(w, err)
		default:
			jsonError(w, http.StatusInternalServerError, "Unknown server error")
		}
		return
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{
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

func writeKOReaderAuthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ksvc.ErrUnauthorized):
		jsonResponse(w, http.StatusUnauthorized, map[string]string{"message": "Unauthorized"})
	case errors.Is(err, ksvc.ErrForbidden):
		jsonResponse(w, http.StatusForbidden, map[string]string{"message": "Forbidden"})
	default:
		jsonError(w, http.StatusInternalServerError, "Unknown server error")
	}
}

func passwordHashOrEmpty(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	return ksvc.HashKey(raw)
}
