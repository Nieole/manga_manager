package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

	"manga-manager/internal/database"
)

type CreateReadingListRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type AddReadingListItemRequest struct {
	SeriesID int64  `json:"series_id"`
	Note     string `json:"note"`
}

type ReorderReadingListItemsRequest struct {
	ItemIDs []int64 `json:"item_ids"`
}

func (c *Controller) listReadingLists(w http.ResponseWriter, r *http.Request) {
	items, err := c.store.ListReadingLists(r.Context())
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to list reading lists")
		return
	}
	if items == nil {
		items = []database.ListReadingListsRow{}
	}
	jsonResponse(w, http.StatusOK, items)
}

func (c *Controller) createReadingList(w http.ResponseWriter, r *http.Request) {
	var req CreateReadingListRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		jsonError(w, http.StatusBadRequest, "Name is required")
		return
	}
	item, err := c.store.CreateReadingList(r.Context(), database.CreateReadingListParams{
		Name:        name,
		Description: strings.TrimSpace(req.Description),
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to create reading list")
		return
	}
	jsonResponse(w, http.StatusCreated, item)
}

func (c *Controller) updateReadingList(w http.ResponseWriter, r *http.Request) {
	listID, err := parseID(r, "listId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid reading list ID")
		return
	}
	var req CreateReadingListRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		jsonError(w, http.StatusBadRequest, "Name is required")
		return
	}
	item, err := c.store.UpdateReadingList(r.Context(), database.UpdateReadingListParams{
		ID:          listID,
		Name:        name,
		Description: strings.TrimSpace(req.Description),
	})
	if err != nil {
		if err == sql.ErrNoRows {
			jsonError(w, http.StatusNotFound, "Reading list not found")
			return
		}
		jsonError(w, http.StatusInternalServerError, "Failed to update reading list")
		return
	}
	jsonResponse(w, http.StatusOK, item)
}

func (c *Controller) deleteReadingList(w http.ResponseWriter, r *http.Request) {
	listID, err := parseID(r, "listId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid reading list ID")
		return
	}
	if err := c.store.DeleteReadingList(r.Context(), listID); err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to delete reading list")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (c *Controller) listReadingListItems(w http.ResponseWriter, r *http.Request) {
	listID, err := parseID(r, "listId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid reading list ID")
		return
	}
	if _, err := c.store.GetReadingList(r.Context(), listID); err != nil {
		if err == sql.ErrNoRows {
			jsonError(w, http.StatusNotFound, "Reading list not found")
			return
		}
		jsonError(w, http.StatusInternalServerError, "Failed to load reading list")
		return
	}
	items, err := c.store.ListReadingListItems(r.Context(), listID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to list reading list items")
		return
	}
	if items == nil {
		items = []database.ListReadingListItemsRow{}
	}
	jsonResponse(w, http.StatusOK, items)
}

func (c *Controller) addReadingListItem(w http.ResponseWriter, r *http.Request) {
	listID, err := parseID(r, "listId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid reading list ID")
		return
	}
	var req AddReadingListItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SeriesID <= 0 {
		jsonError(w, http.StatusBadRequest, "series_id is required")
		return
	}
	if _, err := c.store.GetSeries(r.Context(), req.SeriesID); err != nil {
		if err == sql.ErrNoRows {
			jsonError(w, http.StatusNotFound, "Series not found")
			return
		}
		jsonError(w, http.StatusInternalServerError, "Failed to load series")
		return
	}
	item, err := c.store.AddReadingListItem(r.Context(), database.AddReadingListItemParams{
		ReadingListID: listID,
		SeriesID:      req.SeriesID,
		Note:          strings.TrimSpace(req.Note),
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to add series to reading list")
		return
	}
	jsonResponse(w, http.StatusOK, item)
}

func (c *Controller) removeReadingListItem(w http.ResponseWriter, r *http.Request) {
	listID, err := parseID(r, "listId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid reading list ID")
		return
	}
	itemID, err := parseID(r, "itemId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid reading list item ID")
		return
	}
	if err := c.store.RemoveReadingListItem(r.Context(), database.RemoveReadingListItemParams{
		ReadingListID: listID,
		ID:            itemID,
	}); err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to remove reading list item")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "removed"})
}

func (c *Controller) reorderReadingListItems(w http.ResponseWriter, r *http.Request) {
	listID, err := parseID(r, "listId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid reading list ID")
		return
	}
	var req ReorderReadingListItemsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.ItemIDs) == 0 {
		jsonError(w, http.StatusBadRequest, "item_ids is required")
		return
	}
	if err := c.store.ExecTx(r.Context(), func(q *database.Queries) error {
		for index, itemID := range req.ItemIDs {
			if err := q.UpdateReadingListItemSortOrder(r.Context(), database.UpdateReadingListItemSortOrderParams{
				ReadingListID: listID,
				ID:            itemID,
				SortOrder:     int64((index + 1) * 10),
			}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to reorder reading list items")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "reordered"})
}
