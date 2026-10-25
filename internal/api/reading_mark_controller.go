package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"manga-manager/internal/reading"
)

type BulkUpdateBookProgressRequest struct {
	BookIDs     []int64    `json:"book_ids"`
	IsRead      bool       `json:"is_read"`
	OccurredAt  *time.Time `json:"occurred_at,omitempty"`
	OperationID string     `json:"operation_id,omitempty"`
}

type BulkUpdateSeriesProgressRequest struct {
	SeriesIDs   []int64    `json:"series_ids"`
	IsRead      bool       `json:"is_read"`
	OccurredAt  *time.Time `json:"occurred_at,omitempty"`
	OperationID string     `json:"operation_id,omitempty"`
}

func (c *Controller) bulkUpdateBookProgress(w http.ResponseWriter, r *http.Request) {
	var req BulkUpdateBookProgressRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	if len(req.BookIDs) == 0 {
		jsonResponse(w, http.StatusOK, map[string]string{"message": "No books updated"})
		return
	}
	service := reading.New(c.store, func() { c.invalidateVolatileStatsCache("bulk_book_progress") })
	result, err := service.MarkBooks(r.Context(), reading.MarkBooksInput{UserID: c.currentUserID(r), BookIDs: req.BookIDs, IsRead: req.IsRead, OccurredAt: req.OccurredAt, OperationID: req.OperationID})
	writeMarkResult(w, r, result, err, "Bulk progress update completed")
}

func (c *Controller) bulkUpdateSeriesProgress(w http.ResponseWriter, r *http.Request) {
	var req BulkUpdateSeriesProgressRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	if len(req.SeriesIDs) == 0 {
		jsonResponse(w, http.StatusOK, map[string]interface{}{"message": "No series updated", "updated": 0})
		return
	}
	service := reading.New(c.store, func() { c.invalidateVolatileStatsCache("bulk_series_progress") })
	result, err := service.MarkSeries(r.Context(), reading.MarkSeriesInput{UserID: c.currentUserID(r), SeriesIDs: req.SeriesIDs, IsRead: req.IsRead, OccurredAt: req.OccurredAt, OperationID: req.OperationID})
	writeMarkResult(w, r, result, err, "Bulk series progress update completed")
}

func writeMarkResult(w http.ResponseWriter, r *http.Request, result reading.MarkResult, err error, message string) {
	if errors.Is(err, reading.ErrMarkLimit) {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	body := map[string]interface{}{"message": message, "updated": len(result.CommittedBookIDs), "committed_book_ids": result.CommittedBookIDs, "warnings": len(result.Warnings)}
	status := http.StatusOK
	if err != nil {
		status = http.StatusInternalServerError
		body["error"] = "Failed to complete progress update"
		body["message"] = "Progress update incomplete"
		slog.ErrorContext(r.Context(), "Bulk progress update incomplete", "committed", len(result.CommittedBookIDs), "error", err)
	}
	jsonResponse(w, status, body)
}
