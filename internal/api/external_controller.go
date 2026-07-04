// 业务说明：本文件是业务实现，属于后端 HTTP API 层，负责把前端请求转换为数据库、扫描器、图片处理和元数据服务调用。
// 它承载资料库浏览、阅读器取页、系列维护、任务进度、系统设置和静态资源缓存等对外业务契约。
// 维护时应重点关注请求参数校验、错误语义、缓存头、并发任务状态和前后端字段兼容性。

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
	"sync"
	"time"

	"manga-manager/internal/external"
	"manga-manager/internal/taskcontrol"

	"github.com/go-chi/chi/v5"
)

type externalLibrarySessionRequest struct {
	ExternalPath    string `json:"external_path"`
	IgnoreExtension bool   `json:"ignore_extension"`
}

type externalLibraryTransferRequest struct {
	SeriesIDs []int64 `json:"series_ids"`
}

const transferCopyBufferSize = 1024 * 1024

var transferCopyBufferPool = sync.Pool{
	New: func() any {
		buf := make([]byte, transferCopyBufferSize)
		return &buf
	},
}

func externalLibraryScanTaskKey(libraryID int64, sessionID string) string {
	return fmt.Sprintf("scan_external_library_%s_%d", sessionID, libraryID)
}

func externalLibraryTransferTaskKey(libraryID int64, sessionID string) string {
	return fmt.Sprintf("transfer_external_library_%s_%d", sessionID, libraryID)
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

	session, err := c.external.CreateSession(r.Context(), libraryID, req.ExternalPath, req.IgnoreExtension)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			jsonError(w, http.StatusBadRequest, "External path does not exist")
			return
		}
		jsonError(w, http.StatusBadRequest, fmt.Sprintf("Failed to create external session: %v", err))
		return
	}

	taskKey, started := c.launchExternalLibraryScanTask(libraryID, session.SessionID)
	if !started {
		c.external.ClearSession(libraryID, session.SessionID)
		jsonResponse(w, http.StatusConflict, map[string]string{"error": "An external library scan is already running"})
		return
	}

	jsonResponse(w, http.StatusAccepted, map[string]any{
		"session":  session,
		"task_key": taskKey,
	})
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
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
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
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
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

	taskKey, started := c.launchExternalLibraryTransferTask(libraryID, sessionID, req.SeriesIDs)
	if !started {
		jsonResponse(w, http.StatusConflict, map[string]string{"error": "An external library transfer is already running"})
		return
	}

	jsonResponse(w, http.StatusAccepted, map[string]any{
		"message":        "External library transfer queued",
		"series_count":   plan.SeriesCount,
		"missing_books":  plan.MissingBooks,
		"existing_books": plan.ExistingBooks,
		"task_key":       taskKey,
	})
}

func (c *Controller) launchExternalLibraryScanTask(libraryID int64, sessionID string) (string, bool) {
	taskKey := externalLibraryScanTaskKey(libraryID, sessionID)
	if !c.startPausableCancelableTask(taskKey, "scan_external_library", "正在扫描外部资源库", 0) {
		return taskKey, false
	}

	lib, err := c.store.GetLibrary(context.Background(), libraryID)
	if err == nil {
		c.setTaskMetadata(taskKey, map[string]string{"session_id": sessionID}, lib.Name)
	}
	taskCtx, cleanupCancel := c.newTaskContext(taskKey)

	c.runBackground(func() {
		defer cleanupCancel()
		var lastUpdate time.Time
		snapshot, err := c.external.ScanSession(taskCtx, sessionID, func(current, total int, message string) {
			now := time.Now()
			if now.Sub(lastUpdate) >= 500*time.Millisecond {
				c.updateTaskDetails(taskKey, current, total, message, "discovering", "", map[string]int64{
					"scanned_files": int64(current),
				}, nil)
				lastUpdate = now
			}
		})
		if errors.Is(err, context.Canceled) {
			c.completeTaskMsg(taskKey, "cancelled", "task.msg.scan_external_library.cancelled", nil)
			return
		}
		if err != nil {
			c.failTaskErrMsg(taskKey, "task.msg.scan_external_library.failed", nil, err.Error())
			return
		}
		if snapshot.ScannedFiles > 0 {
			c.updateTaskMsg(taskKey, snapshot.ScannedFiles, snapshot.ScannedFiles, "task.msg.scan_external_library.progress", map[string]string{"count": strconv.Itoa(snapshot.ScannedFiles)})
			c.finishTaskMsg(taskKey, "task.msg.scan_external_library.complete", nil)
		} else {
			c.completeTaskMsg(taskKey, "completed", "task.msg.scan_external_library.complete_empty", nil)
		}
		c.PublishEvent("refresh")
	})
	return taskKey, true
}

func (c *Controller) launchExternalLibraryTransferTask(libraryID int64, sessionID string, seriesIDs []int64) (string, bool) {
	taskKey := externalLibraryTransferTaskKey(libraryID, sessionID)
	if !c.startPausableCancelableTask(taskKey, "transfer_external_library", "正在传输到外部资源库", 0) {
		return taskKey, false
	}

	lib, err := c.store.GetLibrary(context.Background(), libraryID)
	if err == nil {
		c.setTaskMetadata(taskKey, map[string]string{
			"session_id":   sessionID,
			"series_count": strconv.Itoa(len(seriesIDs)),
		}, lib.Name)
	}
	taskCtx, cleanupCancel := c.newTaskContext(taskKey)

	c.runBackground(func() {
		defer cleanupCancel()
		plan, err := c.external.PrepareTransfer(taskCtx, libraryID, sessionID, seriesIDs)
		if errors.Is(err, context.Canceled) {
			c.completeTaskMsg(taskKey, "cancelled", "task.msg.transfer_external_library.cancelled", nil)
			return
		}
		if err != nil {
			c.failTaskErrMsg(taskKey, "task.msg.transfer_external_library.failed", nil, err.Error())
			return
		}
		if len(plan.Operations) == 0 {
			c.finishTaskMsg(taskKey, "task.msg.transfer_external_library.all_exist", nil)
			c.PublishEvent("refresh")
			return
		}

		failures := make([]string, 0)
		skipped := 0
		createdDirs := make(map[string]struct{})
		var lastUpdate time.Time
		for index, op := range plan.Operations {
			if err := taskcontrol.Wait(taskCtx); errors.Is(err, context.Canceled) {
				c.completeTaskMsg(taskKey, "cancelled", "task.msg.transfer_external_library.cancelled", nil)
				return
			}
			now := time.Now()
			if now.Sub(lastUpdate) >= 500*time.Millisecond {
				c.updateTaskDetailsMsg(taskKey, index, len(plan.Operations), "task.msg.transfer_external_library.transferring", map[string]string{"path": op.RelativePath}, "transferring_files", op.RelativePath, map[string]int64{
					"transferred_files": int64(index),
				}, nil)
				lastUpdate = now
			}
			skippedCopy, err := copyFileToExternalLibrary(op.SourcePath, op.Destination, createdDirs)
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
			now = time.Now()
			if now.Sub(lastUpdate) >= 500*time.Millisecond {
				c.updateTaskDetailsMsg(taskKey, index+1, len(plan.Operations), "task.msg.transfer_external_library.progress", map[string]string{"done": strconv.Itoa(index + 1), "total": strconv.Itoa(len(plan.Operations))}, "transferring_files", op.RelativePath, map[string]int64{
					"transferred_files": int64(index + 1),
				}, nil)
				lastUpdate = now
			}
		}

		if len(failures) > 0 {
			c.failTaskErrMsg(taskKey,
				"task.msg.transfer_external_library.complete_with_failures",
				map[string]string{"success": strconv.Itoa(len(plan.Operations) - len(failures)), "failed": strconv.Itoa(len(failures))},
				strings.Join(failures, "\n"))
			c.PublishEvent("refresh")
			return
		}

		c.finishTaskMsg(taskKey, "task.msg.transfer_external_library.complete", map[string]string{"added": strconv.Itoa(len(plan.Operations)), "existing": strconv.Itoa(plan.ExistingBooks + skipped)})
		c.PublishEvent("refresh")
	})
	return taskKey, true
}

func copyFileToExternalLibrary(src, dst string, createdDirs map[string]struct{}) (bool, error) {
	if createdDirs == nil {
		createdDirs = make(map[string]struct{})
	}
	parentDir := filepath.Dir(dst)
	if _, ok := createdDirs[parentDir]; !ok {
		if err := os.MkdirAll(parentDir, 0o755); err != nil {
			return false, err
		}
		createdDirs[parentDir] = struct{}{}
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
	defer func() {
		_ = targetFile.Close()
	}()

	bufPtr := transferCopyBufferPool.Get().(*[]byte)
	defer transferCopyBufferPool.Put(bufPtr)

	if _, err := io.CopyBuffer(targetFile, sourceFile, *bufPtr); err != nil {
		_ = os.Remove(dst)
		return false, err
	}
	if err := targetFile.Close(); err != nil {
		return false, err
	}

	return false, os.Chtimes(dst, info.ModTime(), info.ModTime())
}
