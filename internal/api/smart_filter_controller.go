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
	ID              int64     `json:"id"`
	LibraryID       int64     `json:"library_id"`
	Name            string    `json:"name"`
	ActiveTag       *string   `json:"activeTag"`
	ActiveAuthor    *string   `json:"activeAuthor"`
	ActiveStatus    *string   `json:"activeStatus"`
	ActiveLetter    *string   `json:"activeLetter"`
	ReadState       *string   `json:"readState"`
	MinRating       *float64  `json:"minRating"`
	MaxRating       *float64  `json:"maxRating"`
	MinProgress     *float64  `json:"minProgress"`
	MaxProgress     *float64  `json:"maxProgress"`
	AddedWithinDays *int      `json:"addedWithinDays"`
	SortByField     string    `json:"sortByField"`
	SortDir         string    `json:"sortDir"`
	PageSize        int       `json:"pageSize"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type UpsertSmartFilterRequest struct {
	Name            string   `json:"name"`
	ActiveTag       *string  `json:"activeTag"`
	ActiveAuthor    *string  `json:"activeAuthor"`
	ActiveStatus    *string  `json:"activeStatus"`
	ActiveLetter    *string  `json:"activeLetter"`
	ReadState       *string  `json:"readState"`
	MinRating       *float64 `json:"minRating"`
	MaxRating       *float64 `json:"maxRating"`
	MinProgress     *float64 `json:"minProgress"`
	MaxProgress     *float64 `json:"maxProgress"`
	AddedWithinDays *int     `json:"addedWithinDays"`
	SortByField     string   `json:"sortByField"`
	SortDir         string   `json:"sortDir"`
	PageSize        int      `json:"pageSize"`
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
		       read_state, min_rating, max_rating, min_progress, max_progress, added_within_days,
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
			read_state, min_rating, max_rating, min_progress, max_progress, added_within_days,
			sort_by_field, sort_dir, page_size, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT(library_id, name) DO UPDATE SET
			active_tag = excluded.active_tag,
			active_author = excluded.active_author,
			active_status = excluded.active_status,
			active_letter = excluded.active_letter,
			read_state = excluded.read_state,
			min_rating = excluded.min_rating,
			max_rating = excluded.max_rating,
			min_progress = excluded.min_progress,
			max_progress = excluded.max_progress,
			added_within_days = excluded.added_within_days,
			sort_by_field = excluded.sort_by_field,
			sort_dir = excluded.sort_dir,
			page_size = excluded.page_size,
			updated_at = CURRENT_TIMESTAMP
		RETURNING id, library_id, name, active_tag, active_author, active_status, active_letter,
		          read_state, min_rating, max_rating, min_progress, max_progress, added_within_days,
		          sort_by_field, sort_dir, page_size, created_at, updated_at
	`, libraryID, normalized.Name, normalized.ActiveTag, normalized.ActiveAuthor, normalized.ActiveStatus, normalized.ActiveLetter,
		normalized.ReadState, normalized.MinRating, normalized.MaxRating, normalized.MinProgress, normalized.MaxProgress, normalized.AddedWithinDays,
		normalized.SortByField, normalized.SortDir, normalized.PageSize)

	item, err := scanSmartFilter(row)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to save smart filter")
		return
	}
	jsonResponse(w, http.StatusCreated, item)
}

func (c *Controller) updateSmartFilter(w http.ResponseWriter, r *http.Request) {
	filterID, err := parseID(r, "filterId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid smart filter ID")
		return
	}
	current, err := c.getSmartFilterByID(r, filterID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, http.StatusNotFound, "Smart filter not found")
			return
		}
		jsonError(w, http.StatusInternalServerError, "Failed to load smart filter")
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
		UPDATE smart_filters
		SET name = ?,
		    active_tag = ?,
		    active_author = ?,
		    active_status = ?,
		    active_letter = ?,
		    read_state = ?,
		    min_rating = ?,
		    max_rating = ?,
		    min_progress = ?,
		    max_progress = ?,
		    added_within_days = ?,
		    sort_by_field = ?,
		    sort_dir = ?,
		    page_size = ?,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
		RETURNING id, library_id, name, active_tag, active_author, active_status, active_letter,
		          read_state, min_rating, max_rating, min_progress, max_progress, added_within_days,
		          sort_by_field, sort_dir, page_size, created_at, updated_at
	`, normalized.Name, normalized.ActiveTag, normalized.ActiveAuthor, normalized.ActiveStatus, normalized.ActiveLetter,
		normalized.ReadState, normalized.MinRating, normalized.MaxRating, normalized.MinProgress, normalized.MaxProgress, normalized.AddedWithinDays,
		normalized.SortByField, normalized.SortDir, normalized.PageSize, current.ID)

	item, err := scanSmartFilter(row)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to update smart filter")
		return
	}
	jsonResponse(w, http.StatusOK, item)
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
	var readState sql.NullString
	var minRating, maxRating, minProgress, maxProgress sql.NullFloat64
	var addedWithinDays sql.NullInt64
	err := row.Scan(
		&item.ID,
		&item.LibraryID,
		&item.Name,
		&tag,
		&author,
		&status,
		&letter,
		&readState,
		&minRating,
		&maxRating,
		&minProgress,
		&maxProgress,
		&addedWithinDays,
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
	item.ReadState = nullStringPointer(readState)
	item.MinRating = nullFloatPointer(minRating)
	item.MaxRating = nullFloatPointer(maxRating)
	item.MinProgress = nullFloatPointer(minProgress)
	item.MaxProgress = nullFloatPointer(maxProgress)
	item.AddedWithinDays = nullIntPointer(addedWithinDays)
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
	req.ReadState = cleanOptionalString(req.ReadState)
	if req.ReadState != nil {
		switch *req.ReadState {
		case "unread", "reading", "completed":
		default:
			return req, errors.New("Invalid read state")
		}
	}
	if err := validateOptionalFloatRange(req.MinRating, req.MaxRating, 0, 10, "rating"); err != nil {
		return req, err
	}
	if err := validateOptionalFloatRange(req.MinProgress, req.MaxProgress, 0, 100, "progress"); err != nil {
		return req, err
	}
	if req.AddedWithinDays != nil {
		if *req.AddedWithinDays <= 0 || *req.AddedWithinDays > 3650 {
			return req, errors.New("Invalid added window")
		}
	}

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

func validateOptionalFloatRange(minValue, maxValue *float64, minAllowed, maxAllowed float64, label string) error {
	if minValue != nil && (*minValue < minAllowed || *minValue > maxAllowed) {
		return errors.New("Invalid " + label + " range")
	}
	if maxValue != nil && (*maxValue < minAllowed || *maxValue > maxAllowed) {
		return errors.New("Invalid " + label + " range")
	}
	if minValue != nil && maxValue != nil && *minValue > *maxValue {
		return errors.New("Invalid " + label + " range")
	}
	return nil
}

func nullStringPointer(value sql.NullString) *string {
	if !value.Valid || value.String == "" {
		return nil
	}
	return &value.String
}

func nullFloatPointer(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	return &value.Float64
}

func nullIntPointer(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	result := int(value.Int64)
	return &result
}
