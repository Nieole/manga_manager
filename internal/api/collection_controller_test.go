package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"manga-manager/internal/database"

	"github.com/go-chi/chi/v5"
)

func TestCollectionLifecycleHandlers(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	_, series, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)

	createReq := httptest.NewRequest(http.MethodPost, "/api/collections", bytes.NewBufferString(`{"name":"Favorites","description":"picked"}`))
	createRec := httptest.NewRecorder()
	controller.createCollection(createRec, createReq)

	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", createRec.Code)
	}

	var created map[string]any
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("decode create collection failed: %v", err)
	}
	collectionID := int64(created["id"].(float64))

	addReq := requestWithRouteParam(http.MethodPost, "/api/collections/1/series", []byte(`{"series_ids":[1]}`), "collectionId", strconv.FormatInt(collectionID, 10))
	addRec := httptest.NewRecorder()
	controller.addSeriesToCollection(addRec, addReq)

	if addRec.Code != http.StatusOK {
		t.Fatalf("expected add series 200, got %d", addRec.Code)
	}

	listRec := httptest.NewRecorder()
	controller.listCollections(listRec, httptest.NewRequest(http.MethodGet, "/api/collections", nil))

	if listRec.Code != http.StatusOK {
		t.Fatalf("expected list collections 200, got %d", listRec.Code)
	}

	var collections []Collection
	if err := json.NewDecoder(listRec.Body).Decode(&collections); err != nil {
		t.Fatalf("decode collections failed: %v", err)
	}
	if len(collections) != 1 {
		t.Fatalf("expected 1 collection, got %d", len(collections))
	}
	if collections[0].SeriesCount != 1 {
		t.Fatalf("expected series_count 1, got %d", collections[0].SeriesCount)
	}

	seriesRec := httptest.NewRecorder()
	controller.getCollectionSeries(seriesRec, requestWithRouteParam(http.MethodGet, "/api/collections/1/series", nil, "collectionId", strconv.FormatInt(collectionID, 10)))

	if seriesRec.Code != http.StatusOK {
		t.Fatalf("expected get collection series 200, got %d", seriesRec.Code)
	}

	var items []CollectionSeriesItem
	if err := json.NewDecoder(seriesRec.Body).Decode(&items); err != nil {
		t.Fatalf("decode collection series failed: %v", err)
	}
	if len(items) != 1 || items[0].SeriesID != series.ID {
		t.Fatalf("expected collection to include series %d, got %+v", series.ID, items)
	}

	updateReq := requestWithRouteParam(http.MethodPut, "/api/collections/1", []byte(`{"name":"Updated","description":"refined"}`), "collectionId", strconv.FormatInt(collectionID, 10))
	updateRec := httptest.NewRecorder()
	controller.updateCollection(updateRec, updateReq)

	if updateRec.Code != http.StatusOK {
		t.Fatalf("expected update collection 200, got %d", updateRec.Code)
	}

	var name string
	var description sql.NullString
	row := controller.store.(*database.SqlStore).DB().QueryRowContext(context.Background(), "SELECT name, description FROM collections WHERE id = ?", collectionID)
	if err := row.Scan(&name, &description); err != nil {
		t.Fatalf("query updated collection failed: %v", err)
	}
	if name != "Updated" || !description.Valid || description.String != "refined" {
		t.Fatalf("unexpected collection row: name=%q description=%+v", name, description)
	}

	removeRec := httptest.NewRecorder()
	controller.removeSeriesFromCollection(removeRec, requestWithRouteParam(http.MethodDelete, "/api/collections/1/series/1", nil, "collectionId", strconv.FormatInt(collectionID, 10)))
	if removeRec.Code != http.StatusBadRequest {
		t.Fatalf("expected missing series route param to fail with 400, got %d", removeRec.Code)
	}
}

func TestCollectionRemoveSeriesAndDeleteHandlers(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	_, series, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)

	db := controller.store.(*database.SqlStore).DB()
	res, err := db.ExecContext(context.Background(), `INSERT INTO collections (name, description) VALUES (?, ?)`, "Favorites", "picked")
	if err != nil {
		t.Fatalf("insert collection failed: %v", err)
	}
	collectionID, _ := res.LastInsertId()
	if _, err := db.ExecContext(context.Background(), `INSERT INTO collection_series (collection_id, series_id) VALUES (?, ?)`, collectionID, series.ID); err != nil {
		t.Fatalf("insert collection_series failed: %v", err)
	}

	removeReq := requestWithRouteParam(http.MethodDelete, "/api/collections/1/series/1", nil, "collectionId", strconv.FormatInt(collectionID, 10))
	removeReq = withRouteParam(removeReq, "seriesId", strconv.FormatInt(series.ID, 10))
	removeRec := httptest.NewRecorder()
	controller.removeSeriesFromCollection(removeRec, removeReq)

	if removeRec.Code != http.StatusOK {
		t.Fatalf("expected remove series 200, got %d", removeRec.Code)
	}

	var count int
	row := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM collection_series WHERE collection_id = ? AND series_id = ?`, collectionID, series.ID)
	if err := row.Scan(&count); err != nil {
		t.Fatalf("count collection_series failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected relation removed, got %d rows", count)
	}

	deleteRec := httptest.NewRecorder()
	controller.deleteCollection(deleteRec, requestWithRouteParam(http.MethodDelete, "/api/collections/1", nil, "collectionId", strconv.FormatInt(collectionID, 10)))

	if deleteRec.Code != http.StatusOK {
		t.Fatalf("expected delete collection 200, got %d", deleteRec.Code)
	}

	row = db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM collections WHERE id = ?`, collectionID)
	if err := row.Scan(&count); err != nil {
		t.Fatalf("count collections failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected collection deleted, got %d rows", count)
	}
}

func TestSeriesRelationHandlers(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	_, seriesA, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)
	_, seriesB, _ := seedBookFixture(t, store, rootDir, "Library B", "Series Beta", "Beta 01.cbz", 10)

	body := []byte(`{"target_series_id":` + strconv.FormatInt(seriesB.ID, 10) + `,"relation_type":"spinoff"}`)
	createReq := requestWithRouteParam(http.MethodPost, "/api/series/1/relations", body, "seriesId", strconv.FormatInt(seriesA.ID, 10))
	createRec := httptest.NewRecorder()
	controller.createSeriesRelation(createRec, createReq)

	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected create relation 201, got %d", createRec.Code)
	}

	listRec := httptest.NewRecorder()
	controller.getSeriesRelations(listRec, requestWithRouteParam(http.MethodGet, "/api/series/1/relations", nil, "seriesId", strconv.FormatInt(seriesA.ID, 10)))

	if listRec.Code != http.StatusOK {
		t.Fatalf("expected list relations 200, got %d", listRec.Code)
	}

	var relations []SeriesRelation
	if err := json.NewDecoder(listRec.Body).Decode(&relations); err != nil {
		t.Fatalf("decode relations failed: %v", err)
	}
	if len(relations) != 1 {
		t.Fatalf("expected 1 relation, got %d", len(relations))
	}
	if relations[0].TargetSeriesID != seriesB.ID || relations[0].RelationType != "spinoff" || relations[0].TargetSeriesName != seriesB.Name {
		t.Fatalf("unexpected relation payload: %+v", relations[0])
	}

	reverseBody := []byte(`{"target_series_id":` + strconv.FormatInt(seriesA.ID, 10) + `,"relation_type":"sequel"}`)
	reverseReq := requestWithRouteParam(http.MethodPost, "/api/series/2/relations", reverseBody, "seriesId", strconv.FormatInt(seriesB.ID, 10))
	reverseRec := httptest.NewRecorder()
	controller.createSeriesRelation(reverseRec, reverseReq)
	if reverseRec.Code != http.StatusOK {
		t.Fatalf("expected reverse duplicate relation 200, got %d body=%s", reverseRec.Code, reverseRec.Body.String())
	}

	var duplicateCount int
	duplicateRow := controller.store.(*database.SqlStore).DB().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM series_relations WHERE (source_series_id = ? AND target_series_id = ?) OR (source_series_id = ? AND target_series_id = ?)`, seriesA.ID, seriesB.ID, seriesB.ID, seriesA.ID)
	if err := duplicateRow.Scan(&duplicateCount); err != nil {
		t.Fatalf("count duplicate relations failed: %v", err)
	}
	if duplicateCount != 1 {
		t.Fatalf("expected reverse relation to reuse existing row, got %d rows", duplicateCount)
	}

	deleteRec := httptest.NewRecorder()
	controller.deleteSeriesRelation(deleteRec, requestWithRouteParam(http.MethodDelete, "/api/series/relations/1", nil, "relationId", strconv.FormatInt(relations[0].ID, 10)))

	if deleteRec.Code != http.StatusOK {
		t.Fatalf("expected delete relation 200, got %d", deleteRec.Code)
	}

	var count int
	row := controller.store.(*database.SqlStore).DB().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM series_relations WHERE source_series_id = ? AND target_series_id = ?`, seriesA.ID, seriesB.ID)
	if err := row.Scan(&count); err != nil {
		t.Fatalf("count relations failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected relation deleted, got %d rows", count)
	}
}

func TestCollectionValidationHandlers(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	_, series, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)

	t.Run("create collection validates request", func(t *testing.T) {
		rec := httptest.NewRecorder()
		controller.createCollection(rec, httptest.NewRequest(http.MethodPost, "/api/collections", bytes.NewBufferString(`{}`)))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected create collection 400, got %d", rec.Code)
		}
	})

	t.Run("update collection validates route and body", func(t *testing.T) {
		rec := httptest.NewRecorder()
		controller.updateCollection(rec, requestWithRouteParam(http.MethodPut, "/api/collections/bad", []byte(`{"name":"x"}`), "collectionId", "bad"))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected invalid collection id 400, got %d", rec.Code)
		}

		db := controller.store.(*database.SqlStore).DB()
		res, err := db.ExecContext(context.Background(), `INSERT INTO collections (name, description) VALUES (?, ?)`, "Favorites", "picked")
		if err != nil {
			t.Fatalf("insert collection failed: %v", err)
		}
		collectionID, _ := res.LastInsertId()

		rec = httptest.NewRecorder()
		controller.updateCollection(rec, requestWithRouteParam(http.MethodPut, "/api/collections/1", []byte(`{`), "collectionId", strconv.FormatInt(collectionID, 10)))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected invalid update body 400, got %d", rec.Code)
		}
	})

	t.Run("collection series handlers validate route and payload", func(t *testing.T) {
		db := controller.store.(*database.SqlStore).DB()
		res, err := db.ExecContext(context.Background(), `INSERT INTO collections (name, description) VALUES (?, ?)`, "Queue", "picked")
		if err != nil {
			t.Fatalf("insert collection failed: %v", err)
		}
		collectionID, _ := res.LastInsertId()

		rec := httptest.NewRecorder()
		controller.getCollectionSeries(rec, requestWithRouteParam(http.MethodGet, "/api/collections/bad/series", nil, "collectionId", "bad"))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected invalid get collection series 400, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		controller.addSeriesToCollection(rec, requestWithRouteParam(http.MethodPost, "/api/collections/1/series", []byte(`{}`), "collectionId", strconv.FormatInt(collectionID, 10)))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected missing series_ids 400, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		controller.addSeriesToCollection(rec, requestWithRouteParam(http.MethodPost, "/api/collections/1/series", []byte(`{"series_ids":[`+strconv.FormatInt(series.ID, 10)+`,`+strconv.FormatInt(series.ID, 10)+`]}`), "collectionId", strconv.FormatInt(collectionID, 10)))
		if rec.Code != http.StatusOK {
			t.Fatalf("expected add series 200, got %d", rec.Code)
		}
		var added map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&added); err != nil {
			t.Fatalf("decode add series response failed: %v", err)
		}
		if int(added["added"].(float64)) != 2 {
			t.Fatalf("expected added count to reflect attempted inserts, got %+v", added)
		}

		rec = httptest.NewRecorder()
		controller.removeSeriesFromCollection(rec, requestWithRouteParam(http.MethodDelete, "/api/collections/bad/series/1", nil, "collectionId", "bad"))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected invalid collection id 400, got %d", rec.Code)
		}

		req := requestWithRouteParam(http.MethodDelete, "/api/collections/1/series/bad", nil, "collectionId", strconv.FormatInt(collectionID, 10))
		req = withRouteParam(req, "seriesId", "bad")
		rec = httptest.NewRecorder()
		controller.removeSeriesFromCollection(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected invalid series id 400, got %d", rec.Code)
		}
	})

	t.Run("relation handlers validate route and payload", func(t *testing.T) {
		rec := httptest.NewRecorder()
		controller.getSeriesRelations(rec, requestWithRouteParam(http.MethodGet, "/api/series/bad/relations", nil, "seriesId", "bad"))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected invalid series id 400, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		controller.createSeriesRelation(rec, requestWithRouteParam(http.MethodPost, "/api/series/bad/relations", []byte(`{"target_series_id":1}`), "seriesId", "bad"))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected invalid source series id 400, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		controller.createSeriesRelation(rec, requestWithRouteParam(http.MethodPost, "/api/series/1/relations", []byte(`{}`), "seriesId", strconv.FormatInt(series.ID, 10)))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected missing target_series_id 400, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		controller.createSeriesRelation(rec, requestWithRouteParam(http.MethodPost, "/api/series/1/relations", []byte(`{"target_series_id":`+strconv.FormatInt(series.ID, 10)+`}`), "seriesId", strconv.FormatInt(series.ID, 10)))
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected self relation 422, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		controller.deleteSeriesRelation(rec, requestWithRouteParam(http.MethodDelete, "/api/series/relations/bad", nil, "relationId", "bad"))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected invalid relation id 400, got %d", rec.Code)
		}
	})
}

func withRouteParam(req *http.Request, key, value string) *http.Request {
	routeCtx, _ := req.Context().Value(chi.RouteCtxKey).(*chi.Context)
	if routeCtx == nil {
		routeCtx = chi.NewRouteContext()
	}
	routeCtx.URLParams.Add(key, value)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
}
