package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"manga-manager/internal/organize"
	"net/http"
)

type SmartFilter = organize.SmartFilter
type UpsertSmartFilterRequest = organize.SaveSmartFilter

func smartFilterError(w http.ResponseWriter, err error, missing, fallback string) {
	var invalid *organize.InvalidSmartFilter
	switch {
	case errors.As(err, &invalid):
		jsonError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, sql.ErrNoRows):
		jsonError(w, http.StatusNotFound, missing)
	case errors.Is(err, organize.ErrSmartFilterNameTaken):
		jsonError(w, http.StatusConflict, "A smart filter with this name already exists in this library")
	default:
		jsonError(w, http.StatusInternalServerError, fallback)
	}
}
func (c *organizationHTTP) listSmartFilters(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "libraryId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid library ID")
		return
	}
	rows, err := c.shelves.List(r.Context(), id)
	if err != nil {
		smartFilterError(w, err, "Library not found", "Failed to list smart filters")
		return
	}
	jsonResponse(w, http.StatusOK, rows)
}
func (c *organizationHTTP) upsertSmartFilter(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "libraryId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid library ID")
		return
	}
	var req UpsertSmartFilterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	row, err := c.shelves.Save(r.Context(), id, req)
	if err != nil {
		smartFilterError(w, err, "Library not found", "Failed to save smart filter")
		return
	}
	jsonResponse(w, http.StatusCreated, row)
}
func (c *organizationHTTP) updateSmartFilter(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "filterId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid smart filter ID")
		return
	}
	var req UpsertSmartFilterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	row, err := c.shelves.Update(r.Context(), id, req)
	if err != nil {
		smartFilterError(w, err, "Smart filter not found", "Failed to update smart filter")
		return
	}
	jsonResponse(w, http.StatusOK, row)
}
func (c *organizationHTTP) deleteSmartFilter(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "filterId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid smart filter ID")
		return
	}
	if err := c.shelves.Delete(r.Context(), id); err != nil {
		smartFilterError(w, err, "Smart filter not found", "Failed to delete smart filter")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "deleted"})
}
