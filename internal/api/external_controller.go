package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"manga-manager/internal/application/diskjobs"
	"manga-manager/internal/external"

	"github.com/go-chi/chi/v5"
)

type externalLibrarySessionRequest struct {
	ExternalPath    string `json:"external_path"`
	IgnoreExtension bool   `json:"ignore_extension"`
}

type externalLibraryTransferRequest struct {
	SeriesIDs []int64 `json:"series_ids"`
}

func externalLibraryScanTaskKey(libraryID int64, sessionID string) string {
	return diskjobs.ExternalScanKey(libraryID, sessionID)
}
func externalLibraryTransferTaskKey(libraryID int64, sessionID string) string {
	return diskjobs.ExternalTransferKey(libraryID, sessionID)
}

func (c *Controller) createExternalLibrarySession(w http.ResponseWriter, r *http.Request) {
	libraryID, err := parseID(r, "libraryId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid library ID")
		return
	}
	release, ok := c.guardLibraryRead(w, libraryID)
	if !ok {
		return
	}
	defer release()

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

	launch, err := c.diskJobs.CreateExternalSession(r.Context(), libraryID, req.ExternalPath, req.IgnoreExtension)
	if err != nil {
		var launchErr *diskjobs.LaunchError
		if errors.As(err, &launchErr) {
			writeTaskLaunchError(w, err, "An external library scan is already running", "Failed to start external library scan")
			return
		}
		if errors.Is(err, os.ErrNotExist) {
			jsonError(w, http.StatusBadRequest, "External path does not exist")
			return
		}
		jsonError(w, http.StatusBadRequest, fmt.Sprintf("Failed to create external session: %v", err))
		return
	}

	session := launch.Session

	jsonResponse(w, http.StatusAccepted, map[string]any{
		"session":  session,
		"task_key": externalLibraryScanTaskKey(libraryID, session.SessionID),
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
	release, ok := c.guardLibraryRead(w, libraryID)
	if !ok {
		return
	}
	defer release()

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
	release, ok := c.guardLibraryRead(w, libraryID)
	if !ok {
		return
	}
	defer release()

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
	// 传输会把系列展开为书，少量系列 id 也可能涉及大量读盘与复制。
	if len(req.SeriesIDs) > maxExternalTransferSeries {
		jsonError(w, http.StatusBadRequest, "Too many series in one request")
		return
	}

	launch, err := c.diskJobs.TransferExternal(r.Context(), libraryID, sessionID, req.SeriesIDs)
	if err != nil {
		var launchErr *diskjobs.LaunchError
		if errors.As(err, &launchErr) {
			writeTaskLaunchError(w, err, "An external library transfer is already running", "Failed to start external library transfer")
			return
		}
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

	plan := launch.Plan
	if plan.MissingBooks == 0 {
		jsonResponse(w, http.StatusOK, map[string]any{
			"message":        "Selected series already exist in the external library",
			"series_count":   plan.SeriesCount,
			"missing_books":  0,
			"existing_books": plan.ExistingBooks,
		})
		return
	}

	jsonResponse(w, http.StatusAccepted, map[string]any{
		"message":        "External library transfer queued",
		"series_count":   plan.SeriesCount,
		"missing_books":  plan.MissingBooks,
		"existing_books": plan.ExistingBooks,
		"task_key":       externalLibraryTransferTaskKey(libraryID, sessionID),
	})
}
