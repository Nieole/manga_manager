package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"manga-manager/internal/database"
)

type SmartFilter struct {
	ID           int64     `json:"id"`
	LibraryID    int64     `json:"library_id"`
	Name         string    `json:"name"`
	ActiveTag    *string   `json:"activeTag"`
	ActiveAuthor *string   `json:"activeAuthor"`
	ActiveStatus *string   `json:"activeStatus"`
	ActiveLetter *string   `json:"activeLetter"`
	SortByField  string    `json:"sortByField"`
	SortDir      string    `json:"sortDir"`
	PageSize     int       `json:"pageSize"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type UpsertSmartFilterRequest struct {
	Name         string  `json:"name"`
	ActiveTag    *string `json:"activeTag"`
	ActiveAuthor *string `json:"activeAuthor"`
	ActiveStatus *string `json:"activeStatus"`
	ActiveLetter *string `json:"activeLetter"`
	SortByField  string  `json:"sortByField"`
	SortDir      string  `json:"sortDir"`
	PageSize     int     `json:"pageSize"`
}

func (c *Controller) listSmartFilters(w http.ResponseWriter, r *http.Request) {
	libraryID, err := parseID(r, "libraryId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid library ID")
		return
	}
	if _, err := c.store.GetLibrary(r.Context(), libraryID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, http.StatusNotFound, "Library not found")
			return
		}
		jsonError(w, http.StatusInternalServerError, "Failed to load library")
		return
	}

	db := c.store.(*database.SqlStore).DB()
	rows, err := db.QueryContext(r.Context(), `
		SELECT id, library_id, name, active_tag, active_author, active_status, active_letter,
		       sort_by_field, sort_dir, page_size, created_at, updated_at
		FROM smart_filters
		WHERE library_id = ?
		ORDER BY updated_at DESC, id DESC
	`, libraryID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to list smart filters")
		return
	}
	defer rows.Close()

	items := make([]SmartFilter, 0)
	for rows.Next() {
		item, err := scanSmartFilter(rows)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "Failed to scan smart filters")
			return
		}
		items = append(items, item)
	}
	jsonResponse(w, http.StatusOK, items)
}

func (c *Controller) upsertSmartFilter(w http.ResponseWriter, r *http.Request) {
	libraryID, err := parseID(r, "libraryId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid library ID")
		return
	}
	if _, err := c.store.GetLibrary(r.Context(), libraryID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, http.StatusNotFound, "Library not found")
			return
		}
		jsonError(w, http.StatusInternalServerError, "Failed to load library")
		return
	}

	var req UpsertSmartFilterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	normalized, err := normalizeSmartFilterRequest(req)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	db := c.store.(*database.SqlStore).DB()
	row := db.QueryRowContext(r.Context(), `
		INSERT INTO smart_filters (
			library_id, name, active_tag, active_author, active_status, active_letter,
			sort_by_field, sort_dir, page_size, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT(library_id, name) DO UPDATE SET
			active_tag = excluded.active_tag,
			active_author = excluded.active_author,
			active_status = excluded.active_status,
			active_letter = excluded.active_letter,
			sort_by_field = excluded.sort_by_field,
			sort_dir = excluded.sort_dir,
			page_size = excluded.page_size,
			updated_at = CURRENT_TIMESTAMP
		RETURNING id, library_id, name, active_tag, active_author, active_status, active_letter,
		          sort_by_field, sort_dir, page_size, created_at, updated_at
	`, libraryID, normalized.Name, normalized.ActiveTag, normalized.ActiveAuthor, normalized.ActiveStatus, normalized.ActiveLetter, normalized.SortByField, normalized.SortDir, normalized.PageSize)

	item, err := scanSmartFilter(row)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to save smart filter")
		return
	}
	jsonResponse(w, http.StatusCreated, item)
}

func (c *Controller) deleteSmartFilter(w http.ResponseWriter, r *http.Request) {
	filterID, err := parseID(r, "filterId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid smart filter ID")
		return
	}
	db := c.store.(*database.SqlStore).DB()
	res, err := db.ExecContext(r.Context(), `DELETE FROM smart_filters WHERE id = ?`, filterID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to delete smart filter")
		return
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		jsonError(w, http.StatusNotFound, "Smart filter not found")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "deleted"})
}

type smartFilterScanner interface {
	Scan(dest ...any) error
}

func scanSmartFilter(row smartFilterScanner) (SmartFilter, error) {
	var item SmartFilter
	var tag, author, status, letter sql.NullString
	err := row.Scan(
		&item.ID,
		&item.LibraryID,
		&item.Name,
		&tag,
		&author,
		&status,
		&letter,
		&item.SortByField,
		&item.SortDir,
		&item.PageSize,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		return item, err
	}
	item.ActiveTag = nullStringPointer(tag)
	item.ActiveAuthor = nullStringPointer(author)
	item.ActiveStatus = nullStringPointer(status)
	item.ActiveLetter = nullStringPointer(letter)
	return item, nil
}

func normalizeSmartFilterRequest(req UpsertSmartFilterRequest) (UpsertSmartFilterRequest, error) {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return req, errors.New("Name is required")
	}
	if len([]rune(req.Name)) > 80 {
		return req, errors.New("Name is too long")
	}

	req.ActiveTag = cleanOptionalString(req.ActiveTag)
	req.ActiveAuthor = cleanOptionalString(req.ActiveAuthor)
	req.ActiveStatus = cleanOptionalString(req.ActiveStatus)
	req.ActiveLetter = cleanOptionalString(req.ActiveLetter)

	if req.SortByField == "" {
		req.SortByField = "name"
	}
	switch req.SortByField {
	case "name", "created", "updated", "rating", "volumes", "books", "pages", "read", "favorite":
	default:
		return req, errors.New("Invalid sort field")
	}

	if req.SortDir == "" {
		req.SortDir = "asc"
	}
	if req.SortDir != "asc" && req.SortDir != "desc" {
		return req, errors.New("Invalid sort direction")
	}

	switch req.PageSize {
	case 30, 50, 100:
		return req, nil
	case 0:
		req.PageSize = 30
		return req, nil
	default:
		return req, errors.New("Invalid page size")
	}
}

func cleanOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func nullStringPointer(value sql.NullString) *string {
	if !value.Valid || value.String == "" {
		return nil
	}
	return &value.String
}
