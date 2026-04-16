package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"manga-manager/internal/external"

	"github.com/go-chi/chi/v5"
)

type externalLibrarySessionRequest struct {
	ExternalPath string `json:"external_path"`
}

type externalLibraryTransferRequest struct {
	SeriesIDs []int64 `json:"series_ids"`
}

func (c *Controller) createExternalLibrarySession(w http.ResponseWriter, r *http.Request) {
	libraryID, err := parseID(r, "libraryId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid library ID")
		return
	}

	var req externalLibrarySessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	req.ExternalPath = strings.TrimSpace(req.ExternalPath)
	if req.ExternalPath == "" {
		jsonError(w, http.StatusBadRequest, "External path is required")
		return
	}

	session, err := c.external.CreateSession(r.Context(), libraryID, req.ExternalPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			jsonError(w, http.StatusBadRequest, "External path does not exist")
			return
		}
		jsonError(w, http.StatusBadRequest, fmt.Sprintf("Failed to create external session: %v", err))
		return
	}

	if !c.launchExternalLibraryScanTask(libraryID, session.SessionID) {
		c.external.ClearSession(libraryID, session.SessionID)
		jsonResponse(w, http.StatusConflict, map[string]string{"error": "An external library scan is already running"})
		return
	}

	jsonResponse(w, http.StatusAccepted, session)
}

func (c *Controller) getExternalLibrarySession(w http.ResponseWriter, r *http.Request) {
	libraryID, err := parseID(r, "libraryId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid library ID")
		return
	}

	sessionID := chi.URLParam(r, "sessionId")
	if strings.TrimSpace(sessionID) == "" {
		jsonError(w, http.StatusBadRequest, "Missing session ID")
		return
	}

	session, err := c.external.GetSession(libraryID, sessionID)
	if err != nil {
		if errors.Is(err, external.ErrSessionNotFound) {
			jsonError(w, http.StatusNotFound, "External session not found")
			return
		}
		jsonError(w, http.StatusInternalServerError, "Failed to fetch external session")
		return
	}
	jsonResponse(w, http.StatusOK, session)
}

func (c *Controller) getExternalLibrarySeries(w http.ResponseWriter, r *http.Request) {
	libraryID, err := parseID(r, "libraryId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid library ID")
		return
	}

	sessionID := chi.URLParam(r, "sessionId")
	if strings.TrimSpace(sessionID) == "" {
		jsonError(w, http.StatusBadRequest, "Missing session ID")
		return
	}

	ids := make([]int64, 0)
	if raw := strings.TrimSpace(r.URL.Query().Get("ids")); raw != "" {
		for _, part := range strings.Split(raw, ",") {
			if strings.TrimSpace(part) == "" {
				continue
			}
			seriesID, parseErr := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
			if parseErr != nil {
				jsonError(w, http.StatusBadRequest, "Invalid series IDs")
				return
			}
			ids = append(ids, seriesID)
		}
	}

	items, err := c.external.GetSeriesCoverage(libraryID, sessionID, ids)
	if err != nil {
		if errors.Is(err, external.ErrSessionNotFound) {
			jsonError(w, http.StatusNotFound, "External session not found")
			return
		}
		jsonError(w, http.StatusInternalServerError, "Failed to fetch external library coverage")
		return
	}
	jsonResponse(w, http.StatusOK, items)
}

func (c *Controller) transferToExternalLibrary(w http.ResponseWriter, r *http.Request) {
	libraryID, err := parseID(r, "libraryId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid library ID")
		return
	}
	sessionID := chi.URLParam(r, "sessionId")
	if strings.TrimSpace(sessionID) == "" {
		jsonError(w, http.StatusBadRequest, "Missing session ID")
		return
	}

	var req externalLibraryTransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	if len(req.SeriesIDs) == 0 {
		jsonError(w, http.StatusBadRequest, "series_ids is required")
		return
	}

	plan, err := c.external.PrepareTransfer(r.Context(), libraryID, sessionID, req.SeriesIDs)
	if err != nil {
		switch {
		case errors.Is(err, external.ErrSessionNotFound):
			jsonError(w, http.StatusNotFound, "External session not found")
		case errors.Is(err, external.ErrSessionNotReady):
			jsonError(w, http.StatusConflict, "External session is not ready")
		default:
			jsonError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to prepare transfer: %v", err))
		}
		return
	}

	if plan.MissingBooks == 0 {
		jsonResponse(w, http.StatusOK, map[string]any{
			"message":        "Selected series already exist in the external library",
			"series_count":   plan.SeriesCount,
			"missing_books":  0,
			"existing_books": plan.ExistingBooks,
		})
		return
	}

	if !c.launchExternalLibraryTransferTask(libraryID, sessionID, req.SeriesIDs) {
		jsonResponse(w, http.StatusConflict, map[string]string{"error": "An external library transfer is already running"})
		return
	}

	jsonResponse(w, http.StatusAccepted, map[string]any{
		"message":        "External library transfer queued",
		"series_count":   plan.SeriesCount,
		"missing_books":  plan.MissingBooks,
		"existing_books": plan.ExistingBooks,
	})
}

func (c *Controller) launchExternalLibraryScanTask(libraryID int64, sessionID string) bool {
	taskKey := fmt.Sprintf("scan_external_library_%s_%d", sessionID, libraryID)
	if !c.startTask(taskKey, "scan_external_library", "正在扫描外部资源库", 0) {
		return false
	}

	lib, err := c.store.GetLibrary(context.Background(), libraryID)
	if err == nil {
		c.setTaskMetadata(taskKey, map[string]string{"session_id": sessionID}, lib.Name)
	}

	go func() {
		_, err := c.external.ScanSession(context.Background(), sessionID, func(current, total int, message string) {
			c.updateTask(taskKey, current, total, message)
		})
		if err != nil {
			c.failTaskWithError(taskKey, "外部资源库扫描失败", err.Error())
			return
		}
		c.finishTask(taskKey, "外部资源库扫描完成")
		c.PublishEvent("refresh")
	}()
	return true
}

func (c *Controller) launchExternalLibraryTransferTask(libraryID int64, sessionID string, seriesIDs []int64) bool {
	taskKey := fmt.Sprintf("transfer_external_library_%s_%d", sessionID, libraryID)
	if !c.startTask(taskKey, "transfer_external_library", "正在传输到外部资源库", 0) {
		return false
	}

	lib, err := c.store.GetLibrary(context.Background(), libraryID)
	if err == nil {
		c.setTaskMetadata(taskKey, map[string]string{
			"session_id":   sessionID,
			"series_count": strconv.Itoa(len(seriesIDs)),
		}, lib.Name)
	}

	go func() {
		plan, err := c.external.PrepareTransfer(context.Background(), libraryID, sessionID, seriesIDs)
		if err != nil {
			c.failTaskWithError(taskKey, "外部资源库传输失败", err.Error())
			return
		}
		if len(plan.Operations) == 0 {
			c.finishTask(taskKey, "所选系列已全部存在于外部资源库")
			c.PublishEvent("refresh")
			return
		}

		failures := make([]string, 0)
		skipped := 0
		for index, op := range plan.Operations {
			c.updateTask(taskKey, index, len(plan.Operations), fmt.Sprintf("正在传输 %s", op.RelativePath))
			skippedCopy, err := copyFileToExternalLibrary(op.SourcePath, op.Destination)
			if err != nil {
				failures = append(failures, fmt.Sprintf("%s: %v", op.RelativePath, err))
				continue
			}
			if err := c.external.MarkTransferred(libraryID, sessionID, op); err != nil {
				failures = append(failures, fmt.Sprintf("%s: %v", op.RelativePath, err))
				continue
			}
			if skippedCopy {
				skipped++
			}
			c.updateTask(taskKey, index+1, len(plan.Operations), fmt.Sprintf("已传输 %d / %d 本资源", index+1, len(plan.Operations)))
		}

		if len(failures) > 0 {
			c.failTaskWithError(taskKey,
				fmt.Sprintf("外部资源库传输完成，成功 %d，失败 %d", len(plan.Operations)-len(failures), len(failures)),
				strings.Join(failures, "\n"))
			c.PublishEvent("refresh")
			return
		}

		c.finishTask(taskKey, fmt.Sprintf("外部资源库传输完成，新增 %d，本已存在 %d", len(plan.Operations), plan.ExistingBooks+skipped))
		c.PublishEvent("refresh")
	}()
	return true
}

func copyFileToExternalLibrary(src, dst string) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return false, err
	}
	if _, err := os.Stat(dst); err == nil {
		return true, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}

	sourceFile, err := os.Open(src)
	if err != nil {
		return false, err
	}
	defer sourceFile.Close()

	info, err := sourceFile.Stat()
	if err != nil {
		return false, err
	}

	targetFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return true, nil
		}
		return false, err
	}

	if _, err := io.Copy(targetFile, sourceFile); err != nil {
		targetFile.Close()
		_ = os.Remove(dst)
		return false, err
	}
	if err := targetFile.Close(); err != nil {
		return false, err
	}

	return false, os.Chtimes(dst, info.ModTime(), info.ModTime())
}
