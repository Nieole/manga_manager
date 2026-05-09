package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"manga-manager/internal/database"
	"manga-manager/internal/metadata"
)

func seedAIGroupingReviewFixture(t *testing.T) (*Controller, database.Store, database.Library, database.Series, database.Series) {
	t.Helper()

	controller, store, _, rootDir := newTestController(t)
	lib, seriesA, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)

	seriesPath := filepath.Join(rootDir, "Library A", "Series Beta")
	seriesB, err := store.CreateSeries(context.Background(), database.CreateSeriesParams{
		LibraryID:   lib.ID,
		Name:        "Series Beta",
		Path:        seriesPath,
		Title:       sql.NullString{String: "Beta Title", Valid: true},
		NameInitial: database.SeriesInitial("Beta Title", "Series Beta"),
	})
	if err != nil {
		t.Fatalf("CreateSeries beta failed: %v", err)
	}

	return controller, store, lib, seriesA, seriesB
}

func TestCreateAIGroupingReviewDoesNotCreateCollectionsUntilApply(t *testing.T) {
	controller, store, lib, seriesA, seriesB := seedAIGroupingReviewFixture(t)

	review, created, err := controller.createAIGroupingReview(context.Background(), lib.ID, "ollama", []metadata.CandidateSeries{
		{ID: seriesA.ID, Title: seriesA.Name},
		{ID: seriesB.ID, Title: "Beta Title"},
	}, []metadata.AIGroupCollection{
		{Name: "  Shared Universe  ", Description: "same world", SeriesIDs: []int64{seriesB.ID, seriesA.ID, seriesA.ID, 9999}},
		{Name: "invalid", SeriesIDs: []int64{9999}},
		{Name: "   ", SeriesIDs: []int64{seriesA.ID}},
	})
	if err != nil {
		t.Fatalf("createAIGroupingReview failed: %v", err)
	}
	if created != 1 || review.CollectionCount != 1 {
		t.Fatalf("expected one valid proposal, got created=%d review=%+v", created, review)
	}

	var collectionCount int
	row := controller.store.(*database.SqlStore).DB().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM collections`)
	if err := row.Scan(&collectionCount); err != nil {
		t.Fatalf("count collections failed: %v", err)
	}
	if collectionCount != 0 {
		t.Fatalf("expected no collections before apply, got %d", collectionCount)
	}

	listRec := httptest.NewRecorder()
	controller.listAIGroupingReviews(listRec, httptest.NewRequest(http.MethodGet, "/api/ai-grouping/reviews?status=pending", nil))
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected list reviews 200, got %d body=%s", listRec.Code, listRec.Body.String())
	}
	var list aiGroupingReviewsResponse
	if err := json.NewDecoder(listRec.Body).Decode(&list); err != nil {
		t.Fatalf("decode list response failed: %v", err)
	}
	if list.Total != 1 || len(list.Items) != 1 || len(list.Items[0].Collections) != 1 {
		t.Fatalf("unexpected list response: %+v", list)
	}
	gotSeriesIDs := list.Items[0].Collections[0].SeriesIDs
	if len(gotSeriesIDs) != 2 || gotSeriesIDs[0] != seriesA.ID || gotSeriesIDs[1] != seriesB.ID {
		t.Fatalf("expected sanitized sorted series ids, got %+v", gotSeriesIDs)
	}
	if len(list.Items[0].Collections[0].Series) != 2 {
		t.Fatalf("expected series names resolved, got %+v", list.Items[0].Collections[0].Series)
	}

	applyRec := httptest.NewRecorder()
	controller.applyAIGroupingReview(applyRec, requestWithRouteParam(http.MethodPost, "/api/ai-grouping/reviews/1/apply", nil, "reviewId", strconv.FormatInt(review.ID, 10)))
	if applyRec.Code != http.StatusOK {
		t.Fatalf("expected apply review 200, got %d body=%s", applyRec.Code, applyRec.Body.String())
	}

	row = controller.store.(*database.SqlStore).DB().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM collections`)
	if err := row.Scan(&collectionCount); err != nil {
		t.Fatalf("count collections after apply failed: %v", err)
	}
	if collectionCount != 1 {
		t.Fatalf("expected one collection after apply, got %d", collectionCount)
	}

	var linked int
	row = controller.store.(*database.SqlStore).DB().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM collection_series`)
	if err := row.Scan(&linked); err != nil {
		t.Fatalf("count collection_series failed: %v", err)
	}
	if linked != 2 {
		t.Fatalf("expected two linked series after apply, got %d", linked)
	}

	updated, err := store.GetAIGroupingReview(context.Background(), review.ID)
	if err != nil {
		t.Fatalf("GetAIGroupingReview failed: %v", err)
	}
	if updated.Status != "applied" || !updated.AppliedAt.Valid {
		t.Fatalf("expected review applied, got %+v", updated)
	}
}

func TestRejectAIGroupingReviewDoesNotCreateCollections(t *testing.T) {
	controller, store, lib, seriesA, _ := seedAIGroupingReviewFixture(t)

	review, _, err := controller.createAIGroupingReview(context.Background(), lib.ID, "ollama", []metadata.CandidateSeries{
		{ID: seriesA.ID, Title: seriesA.Name},
	}, []metadata.AIGroupCollection{
		{Name: "Solo", SeriesIDs: []int64{seriesA.ID}},
	})
	if err != nil {
		t.Fatalf("createAIGroupingReview failed: %v", err)
	}

	rejectRec := httptest.NewRecorder()
	controller.rejectAIGroupingReview(rejectRec, requestWithRouteParam(http.MethodPost, "/api/ai-grouping/reviews/1/reject", nil, "reviewId", strconv.FormatInt(review.ID, 10)))
	if rejectRec.Code != http.StatusOK {
		t.Fatalf("expected reject review 200, got %d body=%s", rejectRec.Code, rejectRec.Body.String())
	}

	var collectionCount int
	row := controller.store.(*database.SqlStore).DB().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM collections`)
	if err := row.Scan(&collectionCount); err != nil {
		t.Fatalf("count collections failed: %v", err)
	}
	if collectionCount != 0 {
		t.Fatalf("expected no collections after reject, got %d", collectionCount)
	}

	updated, err := store.GetAIGroupingReview(context.Background(), review.ID)
	if err != nil {
		t.Fatalf("GetAIGroupingReview failed: %v", err)
	}
	if updated.Status != "rejected" || !updated.RejectedAt.Valid {
		t.Fatalf("expected review rejected, got %+v", updated)
	}
}
