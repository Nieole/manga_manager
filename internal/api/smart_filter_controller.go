// 业务说明：本文件是业务实现，属于后端 HTTP API 层，负责把前端请求转换为数据库、扫描器、图片处理和元数据服务调用。
// 它承载资料库浏览、阅读器取页、系列维护、任务进度、系统设置和静态资源缓存等对外业务契约。
// 维护时应重点关注请求参数校验、错误语义、缓存头、并发任务状态和前后端字段兼容性。

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

	rows, err := c.store.ListSmartFiltersByLibrary(r.Context(), libraryID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to list smart filters")
		return
	}
	items := make([]SmartFilter, 0, len(rows))
	for _, row := range rows {
		items = append(items, smartFilterFromDB(row))
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

	row, err := c.store.UpsertSmartFilter(r.Context(), database.UpsertSmartFilterParams{
		LibraryID:       libraryID,
		Name:            normalized.Name,
		ActiveTag:       nullStringFromPointer(normalized.ActiveTag),
		ActiveAuthor:    nullStringFromPointer(normalized.ActiveAuthor),
		ActiveStatus:    nullStringFromPointer(normalized.ActiveStatus),
		ActiveLetter:    nullStringFromPointer(normalized.ActiveLetter),
		ReadState:       nullStringFromPointer(normalized.ReadState),
		MinRating:       nullFloatFromPointer(normalized.MinRating),
		MaxRating:       nullFloatFromPointer(normalized.MaxRating),
		MinProgress:     nullFloatFromPointer(normalized.MinProgress),
		MaxProgress:     nullFloatFromPointer(normalized.MaxProgress),
		AddedWithinDays: nullIntFromPointer(normalized.AddedWithinDays),
		SortByField:     normalized.SortByField,
		SortDir:         normalized.SortDir,
		PageSize:        int64(normalized.PageSize),
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to save smart filter")
		return
	}
	jsonResponse(w, http.StatusCreated, smartFilterFromDB(row))
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

	row, err := c.store.UpdateSmartFilter(r.Context(), database.UpdateSmartFilterParams{
		ID:              current.ID,
		Name:            normalized.Name,
		ActiveTag:       nullStringFromPointer(normalized.ActiveTag),
		ActiveAuthor:    nullStringFromPointer(normalized.ActiveAuthor),
		ActiveStatus:    nullStringFromPointer(normalized.ActiveStatus),
		ActiveLetter:    nullStringFromPointer(normalized.ActiveLetter),
		ReadState:       nullStringFromPointer(normalized.ReadState),
		MinRating:       nullFloatFromPointer(normalized.MinRating),
		MaxRating:       nullFloatFromPointer(normalized.MaxRating),
		MinProgress:     nullFloatFromPointer(normalized.MinProgress),
		MaxProgress:     nullFloatFromPointer(normalized.MaxProgress),
		AddedWithinDays: nullIntFromPointer(normalized.AddedWithinDays),
		SortByField:     normalized.SortByField,
		SortDir:         normalized.SortDir,
		PageSize:        int64(normalized.PageSize),
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to update smart filter")
		return
	}
	jsonResponse(w, http.StatusOK, smartFilterFromDB(row))
}

func (c *Controller) deleteSmartFilter(w http.ResponseWriter, r *http.Request) {
	filterID, err := parseID(r, "filterId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid smart filter ID")
		return
	}
	affected, err := c.store.DeleteSmartFilter(r.Context(), filterID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to delete smart filter")
		return
	}
	if affected == 0 {
		jsonError(w, http.StatusNotFound, "Smart filter not found")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func smartFilterFromDB(row database.SmartFilter) SmartFilter {
	return SmartFilter{
		ID:              row.ID,
		LibraryID:       row.LibraryID,
		Name:            row.Name,
		ActiveTag:       nullStringPointer(row.ActiveTag),
		ActiveAuthor:    nullStringPointer(row.ActiveAuthor),
		ActiveStatus:    nullStringPointer(row.ActiveStatus),
		ActiveLetter:    nullStringPointer(row.ActiveLetter),
		ReadState:       nullStringPointer(row.ReadState),
		MinRating:       nullFloatPointer(row.MinRating),
		MaxRating:       nullFloatPointer(row.MaxRating),
		MinProgress:     nullFloatPointer(row.MinProgress),
		MaxProgress:     nullFloatPointer(row.MaxProgress),
		AddedWithinDays: nullIntPointer(row.AddedWithinDays),
		SortByField:     row.SortByField,
		SortDir:         row.SortDir,
		PageSize:        int(row.PageSize),
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}
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

func nullStringFromPointer(value *string) sql.NullString {
	if value == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *value, Valid: true}
}

func nullFloatFromPointer(value *float64) sql.NullFloat64 {
	if value == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *value, Valid: true}
}

func nullIntFromPointer(value *int) sql.NullInt64 {
	if value == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*value), Valid: true}
}
