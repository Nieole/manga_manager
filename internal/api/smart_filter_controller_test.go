package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestSmartFilterLifecycleHandlers(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	lib, _, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)

	createBody := []byte(`{"name":"Unread long series","activeTag":"Action","activeStatus":"ongoing","sortByField":"updated","sortDir":"desc","pageSize":50}`)
	createReq := requestWithRouteParam(http.MethodPost, "/api/libraries/1/smart-filters", createBody, "libraryId", strconv.FormatInt(lib.ID, 10))
	createRec := httptest.NewRecorder()
	controller.upsertSmartFilter(createRec, createReq)

	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected create smart filter 201, got %d body=%s", createRec.Code, createRec.Body.String())
	}

	var created SmartFilter
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("decode created smart filter failed: %v", err)
	}
	if created.ID == 0 || created.LibraryID != lib.ID || created.Name != "Unread long series" {
		t.Fatalf("unexpected created smart filter: %+v", created)
	}
	if created.ActiveTag == nil || *created.ActiveTag != "Action" || created.SortByField != "updated" || created.SortDir != "desc" || created.PageSize != 50 {
		t.Fatalf("unexpected created smart filter fields: %+v", created)
	}

	updateBody := []byte(`{"name":"Unread long series","activeAuthor":"Author A","sortByField":"name","sortDir":"asc","pageSize":30}`)
	updateRec := httptest.NewRecorder()
	controller.upsertSmartFilter(updateRec, requestWithRouteParam(http.MethodPost, "/api/libraries/1/smart-filters", updateBody, "libraryId", strconv.FormatInt(lib.ID, 10)))
	if updateRec.Code != http.StatusCreated {
		t.Fatalf("expected update smart filter 201, got %d body=%s", updateRec.Code, updateRec.Body.String())
	}

	var updated SmartFilter
	if err := json.NewDecoder(updateRec.Body).Decode(&updated); err != nil {
		t.Fatalf("decode updated smart filter failed: %v", err)
	}
	if updated.ID != created.ID {
		t.Fatalf("expected same id after upsert, got created=%d updated=%d", created.ID, updated.ID)
	}
	if updated.ActiveTag != nil || updated.ActiveAuthor == nil || *updated.ActiveAuthor != "Author A" {
		t.Fatalf("expected upsert to replace filter fields, got %+v", updated)
	}

	listRec := httptest.NewRecorder()
	controller.listSmartFilters(listRec, requestWithRouteParam(http.MethodGet, "/api/libraries/1/smart-filters", nil, "libraryId", strconv.FormatInt(lib.ID, 10)))
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected list smart filters 200, got %d body=%s", listRec.Code, listRec.Body.String())
	}

	var items []SmartFilter
	if err := json.NewDecoder(listRec.Body).Decode(&items); err != nil {
		t.Fatalf("decode smart filters failed: %v", err)
	}
	if len(items) != 1 || items[0].ID != created.ID {
		t.Fatalf("expected one smart filter, got %+v", items)
	}

	deleteRec := httptest.NewRecorder()
	controller.deleteSmartFilter(deleteRec, requestWithRouteParam(http.MethodDelete, "/api/smart-filters/1", nil, "filterId", strconv.FormatInt(created.ID, 10)))
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("expected delete smart filter 200, got %d body=%s", deleteRec.Code, deleteRec.Body.String())
	}

	listAfterDeleteRec := httptest.NewRecorder()
	controller.listSmartFilters(listAfterDeleteRec, requestWithRouteParam(http.MethodGet, "/api/libraries/1/smart-filters", nil, "libraryId", strconv.FormatInt(lib.ID, 10)))
	var afterDelete []SmartFilter
	if err := json.NewDecoder(listAfterDeleteRec.Body).Decode(&afterDelete); err != nil {
		t.Fatalf("decode smart filters after delete failed: %v", err)
	}
	if len(afterDelete) != 0 {
		t.Fatalf("expected no smart filters after delete, got %+v", afterDelete)
	}
}

func TestSmartFilterValidationHandlers(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	lib, _, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)

	tests := []struct {
		name string
		body string
	}{
		{name: "missing name", body: `{"sortByField":"name","sortDir":"asc","pageSize":30}`},
		{name: "invalid sort", body: `{"name":"bad","sortByField":"path","sortDir":"asc","pageSize":30}`},
		{name: "invalid dir", body: `{"name":"bad","sortByField":"name","sortDir":"sideways","pageSize":30}`},
		{name: "invalid page size", body: `{"name":"bad","sortByField":"name","sortDir":"asc","pageSize":999}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			controller.upsertSmartFilter(rec, requestWithRouteParam(http.MethodPost, "/api/libraries/1/smart-filters", []byte(tt.body), "libraryId", strconv.FormatInt(lib.ID, 10)))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
			}
		})
	}

	t.Run("invalid library id", func(t *testing.T) {
		rec := httptest.NewRecorder()
		controller.listSmartFilters(rec, requestWithRouteParam(http.MethodGet, "/api/libraries/bad/smart-filters", nil, "libraryId", "bad"))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("missing library", func(t *testing.T) {
		rec := httptest.NewRecorder()
		controller.listSmartFilters(rec, requestWithRouteParam(http.MethodGet, "/api/libraries/999/smart-filters", nil, "libraryId", "999"))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", rec.Code)
		}
	})

	t.Run("delete missing filter", func(t *testing.T) {
		rec := httptest.NewRecorder()
		controller.deleteSmartFilter(rec, requestWithRouteParam(http.MethodDelete, "/api/smart-filters/999", nil, "filterId", "999"))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", rec.Code)
		}
	})
}
