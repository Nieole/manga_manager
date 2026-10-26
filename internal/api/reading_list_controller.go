package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"manga-manager/internal/database"
	"manga-manager/internal/organize"
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

type readingListItemResponse = organize.ReadingListItem

func (c *Controller) readingLists() *organize.ReadingLists { return organize.NewReadingLists(c.store) }

func readingListError(w http.ResponseWriter, err error, fallback string) {
	switch {
	case errors.Is(err, organize.ErrReadingListMissing), errors.Is(err, organize.ErrReadingListSeriesMissing), errors.Is(err, organize.ErrReadingListItemMissing):
		jsonError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, organize.ErrReadingListNameRequired), errors.Is(err, organize.ErrReadingListItemsRequired), errors.Is(err, organize.ErrReadingListTooManyItems):
		jsonError(w, http.StatusBadRequest, err.Error())
	default:
		jsonError(w, http.StatusInternalServerError, fallback)
	}
}

func (c *Controller) listReadingLists(w http.ResponseWriter, r *http.Request) {
	items, err := c.readingLists().List(r.Context())
	if err != nil {
		readingListError(w, err, "Failed to list reading lists")
		return
	}
	if items == nil {
		items = []database.ListReadingListsRow{}
	}
	jsonResponse(w, http.StatusOK, items)
}

func (c *Controller) createReadingList(w http.ResponseWriter, r *http.Request) {
	var req CreateReadingListRequest
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	item, err := c.readingLists().Create(r.Context(), req.Name, req.Description)
	if err != nil {
		readingListError(w, err, "Failed to create reading list")
		return
	}
	jsonResponse(w, http.StatusCreated, item)
}

func (c *Controller) updateReadingList(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "listId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid reading list ID")
		return
	}
	var req CreateReadingListRequest
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	item, err := c.readingLists().Update(r.Context(), id, req.Name, req.Description)
	if err != nil {
		readingListError(w, err, "Failed to update reading list")
		return
	}
	jsonResponse(w, http.StatusOK, item)
}

func (c *Controller) deleteReadingList(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "listId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid reading list ID")
		return
	}
	if err := c.readingLists().Delete(r.Context(), id); err != nil {
		readingListError(w, err, "Failed to delete reading list")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (c *Controller) listReadingListItems(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "listId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid reading list ID")
		return
	}
	items, err := c.readingLists().Items(r.Context(), c.currentUserID(r), id)
	if err != nil {
		readingListError(w, err, "Failed to list reading list items")
		return
	}
	jsonResponse(w, http.StatusOK, items)
}

func (c *Controller) addReadingListItem(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "listId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid reading list ID")
		return
	}
	var req AddReadingListItemRequest
	if json.NewDecoder(r.Body).Decode(&req) != nil || req.SeriesID <= 0 {
		jsonError(w, http.StatusBadRequest, "series_id is required")
		return
	}
	item, err := c.readingLists().Add(r.Context(), id, req.SeriesID, req.Note)
	if err != nil {
		readingListError(w, err, "Failed to add series to reading list")
		return
	}
	jsonResponse(w, http.StatusOK, item)
}

func (c *Controller) removeReadingListItem(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "listId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid reading list ID")
		return
	}
	itemID, err := parseID(r, "itemId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid reading list item ID")
		return
	}
	if err := c.readingLists().Remove(r.Context(), id, itemID); err != nil {
		readingListError(w, err, "Failed to remove reading list item")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "removed"})
}

func (c *Controller) reorderReadingListItems(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "listId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid reading list ID")
		return
	}
	var req ReorderReadingListItemsRequest
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		jsonError(w, http.StatusBadRequest, "item_ids is required")
		return
	}
	if err := c.readingLists().Reorder(r.Context(), id, req.ItemIDs); err != nil {
		readingListError(w, err, "Failed to reorder reading list items")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "reordered"})
}
