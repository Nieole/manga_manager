package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"manga-manager/internal/database"
)

func TestCollectionViewsIncludeStaticAndSmartCollections(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	lib, seriesA, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)
	_, seriesB, _ := seedBookFixture(t, store, rootDir, "Library B", "Series Beta", "Beta 01.cbz", 10)

	db := controller.store.(*database.SqlStore).DB()
	res, err := db.ExecContext(context.Background(), `INSERT INTO collections (name, description) VALUES (?, ?)`, "Manual Picks", "static")
	if err != nil {
		t.Fatalf("insert collection failed: %v", err)
	}
	collectionID, _ := res.LastInsertId()
	if _, err := db.ExecContext(context.Background(), `INSERT INTO collection_series (collection_id, series_id) VALUES (?, ?)`, collectionID, seriesA.ID); err != nil {
		t.Fatalf("insert collection_series failed: %v", err)
	}

	tag, err := store.UpsertTag(context.Background(), "Action")
	if err != nil {
		t.Fatalf("UpsertTag failed: %v", err)
	}
	if err := store.LinkSeriesTag(context.Background(), database.LinkSeriesTagParams{SeriesID: seriesA.ID, TagID: tag.ID}); err != nil {
		t.Fatalf("LinkSeriesTag A failed: %v", err)
	}
	if err := store.LinkSeriesTag(context.Background(), database.LinkSeriesTagParams{SeriesID: seriesB.ID, TagID: tag.ID}); err != nil {
		t.Fatalf("LinkSeriesTag B failed: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO smart_filters (library_id, name, active_tag, sort_by_field, sort_dir, page_size)
		VALUES (?, ?, ?, ?, ?, ?)
	`, lib.ID, "Action in A", "Action", "name", "asc", 30); err != nil {
		t.Fatalf("insert smart filter failed: %v", err)
	}

	rec := httptest.NewRecorder()
	controller.listCollectionViews(rec, httptest.NewRequest(http.MethodGet, "/api/collection-views", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected list collection views 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var views []CollectionView
	if err := json.NewDecoder(rec.Body).Decode(&views); err != nil {
		t.Fatalf("decode collection views failed: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("expected 2 collection views, got %+v", views)
	}
	byKind := map[string]CollectionView{}
	for _, view := range views {
		byKind[view.Kind] = view
	}
	if byKind["collection"].Name != "Manual Picks" || byKind["collection"].SeriesCount != 1 || byKind["collection"].SourceType != "manual" {
		t.Fatalf("unexpected static collection view: %+v", byKind["collection"])
	}
	if byKind["smart"].Name != "Action in A" || byKind["smart"].SeriesCount != 1 || byKind["smart"].SourceType != "smart_filter" || byKind["smart"].LibraryID == nil || *byKind["smart"].LibraryID != lib.ID {
		t.Fatalf("unexpected smart collection view: %+v", byKind["smart"])
	}
}

func TestSmartCollectionSeriesUsesAdvancedRules(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	lib, unreadMatch, unreadBook := seedBookFixture(t, store, rootDir, "Library A", "Unread Match", "Unread 01.cbz", 10)
	_, readingSeries, readingBook := seedBookFixture(t, store, rootDir, "Library A2", "Reading Match", "Reading 01.cbz", 100)
	_ = readingSeries
	_, completedSeries, completedBook := seedBookFixture(t, store, rootDir, "Library A3", "Completed Match", "Completed 01.cbz", 100)
	_ = completedSeries

	db := controller.store.(*database.SqlStore).DB()
	if _, err := db.ExecContext(context.Background(), `UPDATE series SET library_id = ?, rating = 8.2, created_at = datetime('now', '-5 days') WHERE id = ?`, lib.ID, unreadMatch.ID); err != nil {
		t.Fatalf("update unread series failed: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), `UPDATE series SET library_id = ?, rating = 8.5, created_at = datetime('now', '-5 days') WHERE id = ?`, lib.ID, readingSeries.ID); err != nil {
		t.Fatalf("update reading series failed: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), `UPDATE books SET library_id = ? WHERE series_id IN (?, ?)`, lib.ID, readingSeries.ID, completedSeries.ID); err != nil {
		t.Fatalf("update book libraries failed: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), `UPDATE series SET library_id = ?, rating = 9.1, created_at = datetime('now', '-60 days') WHERE id = ?`, lib.ID, completedSeries.ID); err != nil {
		t.Fatalf("update completed series failed: %v", err)
	}
	if err := store.UpdateBookProgress(context.Background(), database.UpdateBookProgressParams{
		ID:           readingBook.ID,
		LastReadPage: sql.NullInt64{Int64: 40, Valid: true},
	}); err != nil {
		t.Fatalf("UpdateBookProgress reading failed: %v", err)
	}
	if err := store.UpdateBookProgress(context.Background(), database.UpdateBookProgressParams{
		ID:           completedBook.ID,
		LastReadPage: sql.NullInt64{Int64: 100, Valid: true},
	}); err != nil {
		t.Fatalf("UpdateBookProgress completed failed: %v", err)
	}
	_ = unreadBook

	res, err := db.ExecContext(context.Background(), `
		INSERT INTO smart_filters (
			library_id, name, read_state, min_rating, max_rating, min_progress, max_progress, added_within_days, sort_by_field, sort_dir, page_size
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, lib.ID, "Fresh unread high rating", "unread", 8.0, 9.0, 0, 10, 30, "rating", "desc", 30)
	if err != nil {
		t.Fatalf("insert advanced smart filter failed: %v", err)
	}
	filterID, _ := res.LastInsertId()

	rec := httptest.NewRecorder()
	req := requestWithRouteParam(http.MethodGet, "/api/collection-views/smart/1/series", nil, "filterId", strconv.FormatInt(filterID, 10))
	controller.getSmartCollectionSeries(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected smart collection series 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var payload SmartCollectionSeriesResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode advanced smart collection failed: %v", err)
	}
	if payload.Total != 1 || len(payload.Items) != 1 || payload.Items[0].ID != unreadMatch.ID {
		t.Fatalf("expected only unread fresh high-rating series, got %+v", payload)
	}
	if payload.Filter.ReadState == nil || *payload.Filter.ReadState != "unread" || payload.Filter.MinRating == nil || *payload.Filter.MinRating != 8.0 {
		t.Fatalf("expected advanced filter metadata, got %+v", payload.Filter)
	}
}

func TestSmartCollectionSeriesUsesSavedFilter(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	lib, seriesA, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)
	_, seriesB, _ := seedBookFixture(t, store, rootDir, "Library B", "Series Beta", "Beta 01.cbz", 10)

	tag, err := store.UpsertTag(context.Background(), "Action")
	if err != nil {
		t.Fatalf("UpsertTag failed: %v", err)
	}
	if err := store.LinkSeriesTag(context.Background(), database.LinkSeriesTagParams{SeriesID: seriesA.ID, TagID: tag.ID}); err != nil {
		t.Fatalf("LinkSeriesTag A failed: %v", err)
	}
	if err := store.LinkSeriesTag(context.Background(), database.LinkSeriesTagParams{SeriesID: seriesB.ID, TagID: tag.ID}); err != nil {
		t.Fatalf("LinkSeriesTag B failed: %v", err)
	}

	db := controller.store.(*database.SqlStore).DB()
	res, err := db.ExecContext(context.Background(), `
		INSERT INTO smart_filters (library_id, name, active_tag, sort_by_field, sort_dir, page_size)
		VALUES (?, ?, ?, ?, ?, ?)
	`, lib.ID, "Action in A", "Action", "name", "asc", 30)
	if err != nil {
		t.Fatalf("insert smart filter failed: %v", err)
	}
	filterID, _ := res.LastInsertId()

	rec := httptest.NewRecorder()
	req := requestWithRouteParam(http.MethodGet, "/api/collection-views/smart/1/series", nil, "filterId", strconv.FormatInt(filterID, 10))
	controller.getSmartCollectionSeries(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected smart collection series 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var payload SmartCollectionSeriesResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode smart collection series failed: %v", err)
	}
	if payload.Total != 1 || len(payload.Items) != 1 || payload.Items[0].ID != seriesA.ID {
		t.Fatalf("expected only library A action series, got %+v", payload)
	}
	if payload.Filter.ID != filterID || payload.Kind != "smart" || payload.ViewID != "smart:"+strconv.FormatInt(filterID, 10) {
		t.Fatalf("unexpected smart collection payload metadata: %+v", payload)
	}
}

func TestSnapshotSmartCollectionCreatesStaticCollection(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	lib, seriesA, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)
	_, seriesB, _ := seedBookFixture(t, store, rootDir, "Library B", "Series Beta", "Beta 01.cbz", 10)

	tag, err := store.UpsertTag(context.Background(), "Action")
	if err != nil {
		t.Fatalf("UpsertTag failed: %v", err)
	}
	if err := store.LinkSeriesTag(context.Background(), database.LinkSeriesTagParams{SeriesID: seriesA.ID, TagID: tag.ID}); err != nil {
		t.Fatalf("LinkSeriesTag A failed: %v", err)
	}
	if err := store.LinkSeriesTag(context.Background(), database.LinkSeriesTagParams{SeriesID: seriesB.ID, TagID: tag.ID}); err != nil {
		t.Fatalf("LinkSeriesTag B failed: %v", err)
	}

	db := controller.store.(*database.SqlStore).DB()
	res, err := db.ExecContext(context.Background(), `
		INSERT INTO smart_filters (library_id, name, active_tag, sort_by_field, sort_dir, page_size)
		VALUES (?, ?, ?, ?, ?, ?)
	`, lib.ID, "Action in A", "Action", "name", "asc", 30)
	if err != nil {
		t.Fatalf("insert smart filter failed: %v", err)
	}
	filterID, _ := res.LastInsertId()

	body := []byte(`{"name":"Frozen Action","description":"snapshot"}`)
	rec := httptest.NewRecorder()
	req := requestWithRouteParam(http.MethodPost, "/api/collection-views/smart/1/snapshot", body, "filterId", strconv.FormatInt(filterID, 10))
	controller.snapshotSmartCollection(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected snapshot 201, got %d body=%s", rec.Code, rec.Body.String())
	}

	var created map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode snapshot response failed: %v", err)
	}
	collectionID := int64(created["id"].(float64))

	var name, sourceType string
	var memberCount int
	row := db.QueryRowContext(context.Background(), `
		SELECT c.name, c.source_type, COUNT(cs.series_id)
		FROM collections c
		LEFT JOIN collection_series cs ON cs.collection_id = c.id
		WHERE c.id = ?
		GROUP BY c.id
	`, collectionID)
	if err := row.Scan(&name, &sourceType, &memberCount); err != nil {
		t.Fatalf("query snapshot collection failed: %v", err)
	}
	if name != "Frozen Action" || sourceType != "smart_snapshot" || memberCount != 1 {
		t.Fatalf("unexpected snapshot collection: name=%q source=%q members=%d", name, sourceType, memberCount)
	}
}

func TestSmartCollectionSnapshotPreviewReportsSafetySignals(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	lib, seriesA, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)
	_, seriesB, _ := seedBookFixture(t, store, rootDir, "Library B", "Series Beta", "Beta 01.cbz", 10)
	_, seriesC, _ := seedBookFixture(t, store, rootDir, "Library C", "Series Gamma", "Gamma 01.cbz", 8)

	tag, err := store.UpsertTag(context.Background(), "Action")
	if err != nil {
		t.Fatalf("UpsertTag failed: %v", err)
	}
	for _, series := range []database.Series{seriesA, seriesB, seriesC} {
		if _, err := controller.store.(*database.SqlStore).DB().ExecContext(context.Background(), `UPDATE series SET library_id = ? WHERE id = ?`, lib.ID, series.ID); err != nil {
			t.Fatalf("update library failed: %v", err)
		}
		if err := store.LinkSeriesTag(context.Background(), database.LinkSeriesTagParams{SeriesID: series.ID, TagID: tag.ID}); err != nil {
			t.Fatalf("LinkSeriesTag failed: %v", err)
		}
	}

	db := controller.store.(*database.SqlStore).DB()
	if _, err := db.ExecContext(context.Background(), `INSERT INTO collections (name, description) VALUES (?, ?)`, "Frozen Action", "existing"); err != nil {
		t.Fatalf("insert existing collection failed: %v", err)
	}
	res, err := db.ExecContext(context.Background(), `
		INSERT INTO smart_filters (library_id, name, active_tag, sort_by_field, sort_dir, page_size)
		VALUES (?, ?, ?, ?, ?, ?)
	`, lib.ID, "Action in A", "Action", "name", "asc", 30)
	if err != nil {
		t.Fatalf("insert smart filter failed: %v", err)
	}
	filterID, _ := res.LastInsertId()

	rec := httptest.NewRecorder()
	req := requestWithRouteParam(http.MethodGet, "/api/collection-views/smart/1/snapshot-preview?name=Frozen%20Action&preview_limit=2&limit=2", nil, "filterId", strconv.FormatInt(filterID, 10))
	controller.previewSmartCollectionSnapshot(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected snapshot preview 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var payload SmartCollectionSnapshotPreviewResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode snapshot preview failed: %v", err)
	}
	if payload.FilterID != filterID || payload.Name != "Frozen Action" || !payload.NameConflict {
		t.Fatalf("unexpected preview identity/conflict: %+v", payload)
	}
	if payload.Total != 3 || payload.PreviewLimit != 2 || payload.SnapshotLimit != 2 || payload.SnapshotCount != 2 || !payload.Truncated {
		t.Fatalf("unexpected preview counts: %+v", payload)
	}
	if len(payload.Items) != 2 || payload.Filter.ID != filterID {
		t.Fatalf("unexpected preview items/filter: %+v", payload)
	}
}

func TestSmartCollectionSeriesValidation(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	rec := httptest.NewRecorder()
	controller.getSmartCollectionSeries(rec, requestWithRouteParam(http.MethodGet, "/api/collection-views/smart/bad/series", nil, "filterId", "bad"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid smart collection id 400, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	controller.getSmartCollectionSeries(rec, requestWithRouteParam(http.MethodGet, "/api/collection-views/smart/999/series", nil, "filterId", "999"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected missing smart collection 404, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	controller.previewSmartCollectionSnapshot(rec, requestWithRouteParam(http.MethodGet, "/api/collection-views/smart/bad/snapshot-preview", nil, "filterId", "bad"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid smart collection preview id 400, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	controller.previewSmartCollectionSnapshot(rec, requestWithRouteParam(http.MethodGet, "/api/collection-views/smart/999/snapshot-preview", nil, "filterId", "999"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected missing smart collection preview 404, got %d", rec.Code)
	}
}
