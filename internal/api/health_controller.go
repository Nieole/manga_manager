package api

import (
	"net/http"
	"strconv"
	"strings"

	"manga-manager/internal/database"
)

func (c *Controller) getHealthReport(w http.ResponseWriter, r *http.Request) {
	libraryID, err := parseOptionalInt64(r.URL.Query().Get("library_id"))
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid library ID")
		return
	}
	limit, err := parseOptionalInt(r.URL.Query().Get("limit"), 50)
	if err != nil || limit <= 0 {
		jsonError(w, http.StatusBadRequest, "Invalid limit")
		return
	}
	report, err := c.store.GetHealthReport(r.Context(), database.HealthIssueFilters{
		LibraryID: libraryID,
		Type:      strings.TrimSpace(r.URL.Query().Get("type")),
		Limit:     limit,
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to build health report")
		return
	}
	jsonResponse(w, http.StatusOK, report)
}

func parseOptionalInt64(value string) (int64, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	return strconv.ParseInt(value, 10, 64)
}

func parseOptionalInt(value string, fallback int) (int, error) {
	if strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	return strconv.Atoi(value)
}
