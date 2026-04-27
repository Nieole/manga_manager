package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"manga-manager/internal/database"
)

func TestReadingListLifecycle(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	_, seriesA, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)
	_, seriesB, _ := seedBookFixture(t, store, rootDir, "Library B", "Series Beta", "Beta 01.cbz", 10)

	createRec := httptest.NewRecorder()
	controller.createReadingList(createRec, httptest.NewRequest(http.MethodPost, "/api/reading-lists/", jsonBody(`{"name":"Cosmic Order","description":"main + side stories"}`)))
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected create 201, got %d body=%s", createRec.Code, createRec.Body.String())
	}
	var created database.ReadingList
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("decode created reading list failed: %v", err)
	}
	if created.Name != "Cosmic Order" {
		t.Fatalf("unexpected created reading list: %+v", created)
	}

	addARec := httptest.NewRecorder()
	controller.addReadingListItem(addARec, requestWithRouteParam(http.MethodPost, "/api/reading-lists/1/items", []byte(`{"series_id":`+strconv.FormatInt(seriesA.ID, 10)+`,"note":"start here"}`), "listId", strconv.FormatInt(created.ID, 10)))
	if addARec.Code != http.StatusOK {
		t.Fatalf("expected add A 200, got %d body=%s", addARec.Code, addARec.Body.String())
	}
	var itemA database.ReadingListItem
	if err := json.NewDecoder(addARec.Body).Decode(&itemA); err != nil {
		t.Fatalf("decode item A failed: %v", err)
	}

	addBRec := httptest.NewRecorder()
	controller.addReadingListItem(addBRec, requestWithRouteParam(http.MethodPost, "/api/reading-lists/1/items", []byte(`{"series_id":`+strconv.FormatInt(seriesB.ID, 10)+`}`), "listId", strconv.FormatInt(created.ID, 10)))
	if addBRec.Code != http.StatusOK {
		t.Fatalf("expected add B 200, got %d body=%s", addBRec.Code, addBRec.Body.String())
	}
	var itemB database.ReadingListItem
	if err := json.NewDecoder(addBRec.Body).Decode(&itemB); err != nil {
		t.Fatalf("decode item B failed: %v", err)
	}

	reorderRec := httptest.NewRecorder()
	reorderBody := []byte(`{"item_ids":[` + strconv.FormatInt(itemB.ID, 10) + `,` + strconv.FormatInt(itemA.ID, 10) + `]}`)
	controller.reorderReadingListItems(reorderRec, requestWithRouteParam(http.MethodPost, "/api/reading-lists/1/items/reorder", reorderBody, "listId", strconv.FormatInt(created.ID, 10)))
	if reorderRec.Code != http.StatusOK {
		t.Fatalf("expected reorder 200, got %d body=%s", reorderRec.Code, reorderRec.Body.String())
	}

	listItemsRec := httptest.NewRecorder()
	controller.listReadingListItems(listItemsRec, requestWithRouteParam(http.MethodGet, "/api/reading-lists/1/items", nil, "listId", strconv.FormatInt(created.ID, 10)))
	if listItemsRec.Code != http.StatusOK {
		t.Fatalf("expected list items 200, got %d body=%s", listItemsRec.Code, listItemsRec.Body.String())
	}
	var items []database.ListReadingListItemsRow
	if err := json.NewDecoder(listItemsRec.Body).Decode(&items); err != nil {
		t.Fatalf("decode list items failed: %v", err)
	}
	if len(items) != 2 || items[0].SeriesID != seriesB.ID || items[1].SeriesID != seriesA.ID {
		t.Fatalf("unexpected item order: %+v", items)
	}
	if items[1].NextBookID <= 0 || items[1].Note != "start here" {
		t.Fatalf("unexpected item details: %+v", items[1])
	}

	removeRec := httptest.NewRecorder()
	controller.removeReadingListItem(removeRec, requestWithRouteParams(http.MethodDelete, "/api/reading-lists/1/items/1", nil, map[string]string{
		"listId": strconv.FormatInt(created.ID, 10),
		"itemId": strconv.FormatInt(itemB.ID, 10),
	}))
	if removeRec.Code != http.StatusOK {
		t.Fatalf("expected remove 200, got %d body=%s", removeRec.Code, removeRec.Body.String())
	}

	listRec := httptest.NewRecorder()
	controller.listReadingLists(listRec, httptest.NewRequest(http.MethodGet, "/api/reading-lists/", nil))
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected list 200, got %d body=%s", listRec.Code, listRec.Body.String())
	}
	var lists []database.ListReadingListsRow
	if err := json.NewDecoder(listRec.Body).Decode(&lists); err != nil {
		t.Fatalf("decode reading lists failed: %v", err)
	}
	if len(lists) != 1 || lists[0].ItemCount != 1 {
		t.Fatalf("expected one list with one item, got %+v", lists)
	}

	deleteRec := httptest.NewRecorder()
	controller.deleteReadingList(deleteRec, requestWithRouteParam(http.MethodDelete, "/api/reading-lists/1", nil, "listId", strconv.FormatInt(created.ID, 10)))
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("expected delete 200, got %d body=%s", deleteRec.Code, deleteRec.Body.String())
	}
	remaining, err := store.ListReadingLists(context.Background())
	if err != nil {
		t.Fatalf("ListReadingLists failed: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("expected no reading lists after delete, got %+v", remaining)
	}
}

func TestReadingListValidation(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	_, series, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)
	list, err := store.CreateReadingList(context.Background(), database.CreateReadingListParams{Name: "Order", Description: ""})
	if err != nil {
		t.Fatalf("CreateReadingList failed: %v", err)
	}

	t.Run("create requires name", func(t *testing.T) {
		rec := httptest.NewRecorder()
		controller.createReadingList(rec, httptest.NewRequest(http.MethodPost, "/api/reading-lists/", jsonBody(`{"name":" "}`)))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected create validation 400, got %d", rec.Code)
		}
	})

	t.Run("route ids are validated", func(t *testing.T) {
		rec := httptest.NewRecorder()
		controller.listReadingListItems(rec, requestWithRouteParam(http.MethodGet, "/api/reading-lists/bad/items", nil, "listId", "bad"))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected bad list id 400, got %d", rec.Code)
		}
	})

	t.Run("add requires existing series", func(t *testing.T) {
		rec := httptest.NewRecorder()
		controller.addReadingListItem(rec, requestWithRouteParam(http.MethodPost, "/api/reading-lists/1/items", []byte(`{"series_id":999}`), "listId", strconv.FormatInt(list.ID, 10)))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected missing series 404, got %d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("readding updates note instead of duplicating", func(t *testing.T) {
		firstRec := httptest.NewRecorder()
		controller.addReadingListItem(firstRec, requestWithRouteParam(http.MethodPost, "/api/reading-lists/1/items", []byte(`{"series_id":`+strconv.FormatInt(series.ID, 10)+`,"note":"old"}`), "listId", strconv.FormatInt(list.ID, 10)))
		if firstRec.Code != http.StatusOK {
			t.Fatalf("expected first add 200, got %d", firstRec.Code)
		}
		secondRec := httptest.NewRecorder()
		controller.addReadingListItem(secondRec, requestWithRouteParam(http.MethodPost, "/api/reading-lists/1/items", []byte(`{"series_id":`+strconv.FormatInt(series.ID, 10)+`,"note":"new"}`), "listId", strconv.FormatInt(list.ID, 10)))
		if secondRec.Code != http.StatusOK {
			t.Fatalf("expected second add 200, got %d", secondRec.Code)
		}
		items, err := store.ListReadingListItems(context.Background(), list.ID)
		if err != nil {
			t.Fatalf("ListReadingListItems failed: %v", err)
		}
		if len(items) != 1 || items[0].Note != "new" {
			t.Fatalf("expected updated single item, got %+v", items)
		}
	})
}

func jsonBody(raw string) *strings.Reader {
	return strings.NewReader(raw)
}
