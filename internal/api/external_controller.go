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
	// 每个系列 id 都会被展开成该系列的全部书，几十个 id 就能变成整库几万本。
	// 全站唯一的闸门是 1 MiB 请求体上限，而它是明确按十万级 int64 id 设计的，
	// 挡不住这条路径。批量标记读状态那边早有同款上限（maxBulkReadStateSeries），
	// 这个端点是同构的形状，只是漏了。
	if len(req.SeriesIDs) > maxExternalTransferSeries {
		jsonError(w, http.StatusBadRequest, "Too many series in one request")
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

	// 把**已经算好的** plan 交给后台任务，不能让后台任务再用同一份入参重新规划一遍——
	// 否则一次传输请求会把 PrepareTransfer（含全部 DB 往返）完整跑两遍。
	taskKey, started := c.launchExternalLibraryTransferTask(libraryID, sessionID, plan)
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
	if !c.taskEngine.startPausableCancelableTaskMsg(taskKey, "scan_external_library", "task.msg.scan_external_library.start", nil, 0) {
		return taskKey, false
	}

	lib, err := c.store.GetLibrary(context.Background(), libraryID)
	if err == nil {
		c.taskEngine.setTaskMetadata(taskKey, map[string]string{"session_id": sessionID}, lib.Name)
	}
	taskCtx, cleanupCancel := c.taskEngine.newTaskContext(taskKey)

	c.runBackgroundTask(taskKey, func() {
		defer cleanupCancel()
		var lastUpdate time.Time
		snapshot, err := c.external.ScanSession(taskCtx, sessionID, func(current, total int, message string) {
			now := time.Now()
			if now.Sub(lastUpdate) >= 500*time.Millisecond {
				c.taskEngine.updateTaskDetails(taskKey, current, total, message, "discovering", "", map[string]int64{
					"scanned_files": int64(current),
				}, nil)
				lastUpdate = now
			}
		})
		if errors.Is(err, context.Canceled) {
			c.taskEngine.completeTaskMsg(taskKey, "cancelled", "task.msg.scan_external_library.cancelled", nil)
			return
		}
		if err != nil {
			c.taskEngine.failTaskErrMsg(taskKey, "task.msg.scan_external_library.failed", nil, err.Error())
			return
		}
		if snapshot.ScannedFiles > 0 {
			c.taskEngine.updateTaskMsg(taskKey, snapshot.ScannedFiles, snapshot.ScannedFiles, "task.msg.scan_external_library.progress", map[string]string{"count": strconv.Itoa(snapshot.ScannedFiles)})
			c.taskEngine.finishTaskMsg(taskKey, "task.msg.scan_external_library.complete", nil)
		} else {
			c.taskEngine.completeTaskMsg(taskKey, "completed", "task.msg.scan_external_library.complete_empty", nil)
		}
		c.PublishEvent("refresh")
	})
	return taskKey, true
}

// launchExternalLibraryTransferTask 用**调用方已经算好的** plan 起后台传输任务。
// 传 plan 而不是 seriesIDs，是为了不让 PrepareTransfer 在一次请求里跑两遍。
func (c *Controller) launchExternalLibraryTransferTask(libraryID int64, sessionID string, plan external.TransferPlan) (string, bool) {
	taskKey := externalLibraryTransferTaskKey(libraryID, sessionID)
	if !c.taskEngine.startPausableCancelableTaskMsg(taskKey, "transfer_external_library", "task.msg.transfer_external_library.start", nil, 0) {
		return taskKey, false
	}

	lib, err := c.store.GetLibrary(context.Background(), libraryID)
	if err == nil {
		c.taskEngine.setTaskMetadata(taskKey, map[string]string{
			"session_id":   sessionID,
			"series_count": strconv.Itoa(plan.SeriesCount),
		}, lib.Name)
	}
	taskCtx, cleanupCancel := c.taskEngine.newTaskContext(taskKey)

	c.runBackgroundTask(taskKey, func() {
		defer cleanupCancel()
		if err := taskCtx.Err(); err != nil {
			c.taskEngine.completeTaskMsg(taskKey, "cancelled", "task.msg.transfer_external_library.cancelled", nil)
			return
		}
		if len(plan.Operations) == 0 {
			c.taskEngine.finishTaskMsg(taskKey, "task.msg.transfer_external_library.all_exist", nil)
			c.PublishEvent("refresh")
			return
		}

		failures := make([]string, 0)
		skipped := 0
		createdDirs := make(map[string]struct{})
		var lastUpdate time.Time
		for index, op := range plan.Operations {
			if err := taskcontrol.Wait(taskCtx); errors.Is(err, context.Canceled) {
				c.taskEngine.completeTaskMsg(taskKey, "cancelled", "task.msg.transfer_external_library.cancelled", nil)
				return
			}
			now := time.Now()
			if now.Sub(lastUpdate) >= 500*time.Millisecond {
				c.taskEngine.updateTaskDetailsMsg(taskKey, index, len(plan.Operations), "task.msg.transfer_external_library.transferring", map[string]string{"path": op.RelativePath}, "transferring_files", op.RelativePath, map[string]int64{
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
				c.taskEngine.updateTaskDetailsMsg(taskKey, index+1, len(plan.Operations), "task.msg.transfer_external_library.progress", map[string]string{"done": strconv.Itoa(index + 1), "total": strconv.Itoa(len(plan.Operations))}, "transferring_files", op.RelativePath, map[string]int64{
					"transferred_files": int64(index + 1),
				}, nil)
				lastUpdate = now
			}
		}

		if len(failures) > 0 {
			c.taskEngine.failTaskErrMsg(taskKey,
				"task.msg.transfer_external_library.complete_with_failures",
				map[string]string{"success": strconv.Itoa(len(plan.Operations) - len(failures)), "failed": strconv.Itoa(len(failures))},
				strings.Join(failures, "\n"))
			c.PublishEvent("refresh")
			return
		}

		c.taskEngine.finishTaskMsg(taskKey, "task.msg.transfer_external_library.complete", map[string]string{"added": strconv.Itoa(len(plan.Operations)), "existing": strconv.Itoa(plan.ExistingBooks + skipped)})
		c.PublishEvent("refresh")
	})
	return taskKey, true
}

// copyFileToExternalLibrary 把一本书拷到外部库，返回「目标是否已存在（跳过）」。
//
// 必须先写同目录临时文件再 rename，不能直接往最终路径写。外部库传输的目的地通常是
// USB/SD 外接盘，单本几百 MB、整个系列要跑几十分钟；直接写最终路径时，进程被强杀
// 或外接盘中途掉线就会留下一个截断的 .cbz。而后续两处「已存在」的判定——本函数开头的
// os.Stat，以及 external/manager.go 里按扩展名列举归档——都**只看路径不看内容**，
// 于是这本坏书在阅读器上永远打不开，重传多少次都会被当成「已经有了」而跳过。
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

	// 临时文件名与源文件名无关，是刻意的：漫画文件名最容易逼近 NAME_MAX（255 字节，
	// 中文名 UTF-8 每字 3 字节只够约 85 字），把原名嵌进 pattern 再加随机后缀会让一批
	// 今天能正常传输的书直接报 "file name too long"。与 comicinfo 写回同款定长 pattern。
	targetFile, err := os.CreateTemp(parentDir, ".mm-transfer-*.tmp")
	if err != nil {
		return false, err
	}
	tmpName := targetFile.Name()
	committed := false
	defer func() {
		if committed {
			return
		}
		_ = targetFile.Close()
		_ = os.Remove(tmpName)
	}()

	bufPtr := transferCopyBufferPool.Get().(*[]byte)
	defer transferCopyBufferPool.Put(bufPtr)

	if _, err := io.CopyBuffer(targetFile, sourceFile, *bufPtr); err != nil {
		return false, err
	}
	// Sync 之后再 rename：外接盘掉电时，只有落盘的数据才谈得上「原子替换」。
	if err := targetFile.Sync(); err != nil {
		return false, err
	}
	if err := targetFile.Close(); err != nil {
		return false, err
	}
	// os.CreateTemp 建的是 0600，尽力放宽到 0644。用 _ = 忽略错误是有意的：
	// 目的地常是 exFAT/FAT32/网络挂载，chmod 在那里要么是 no-op 要么直接 ENOTSUP，
	// 为一个在该文件系统上根本没有意义的权限位让整本书传输失败不划算。
	_ = os.Chmod(tmpName, 0o644)
	// mtime 在 rename 之前设：os.Rename 不改 mtime，而 rename 之后目标已对外可见，
	// 此时再改属性等于让别人看到一个属性还没定型的文件。
	if err := os.Chtimes(tmpName, info.ModTime(), info.ModTime()); err != nil {
		return false, err
	}
	// rename 前再确认一次目标不存在。开头那次 Stat 到这里隔了整个拷贝过程（可能几分钟），
	// 期间别的传输任务完全可能已经把同一本书放好了；rename 会无声覆盖，那才是真的丢数据。
	if _, err := os.Stat(dst); err == nil {
		return true, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return false, err
	}
	committed = true
	return false, nil
}
