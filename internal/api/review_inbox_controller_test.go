package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"manga-manager/internal/metadata"
)

func TestReviewInboxSummaryAggregatesCounts(t *testing.T) {
	controller, _, lib, seriesA, seriesB := seedAIGroupingReviewFixture(t)

	if _, _, err := controller.createAIGroupingReview(context.Background(), lib.ID, "ollama", []metadata.CandidateSeries{
		{ID: seriesA.ID, Title: seriesA.Name},
		{ID: seriesB.ID, Title: "Beta Title"},
	}, []metadata.AIGroupCollection{
		{Name: "Shared Universe", SeriesIDs: []int64{seriesA.ID, seriesB.ID}},
	}); err != nil {
		t.Fatalf("createAIGroupingReview failed: %v", err)
	}

	rec := httptest.NewRecorder()
	controller.getReviewInboxSummary(rec, httptest.NewRequest(http.MethodGet, "/api/reviews/inbox/summary", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp reviewInboxSummaryResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode summary failed: %v", err)
	}
	if resp.Counts.AIGrouping != 1 {
		t.Fatalf("expected ai_grouping count 1, got %+v", resp.Counts)
	}
	if resp.Counts.Total != resp.Counts.Metadata+resp.Counts.AIGrouping {
		t.Fatalf("total mismatch: %+v", resp.Counts)
	}
}

func TestReviewInboxDispatchesByType(t *testing.T) {
	controller, _, lib, seriesA, seriesB := seedAIGroupingReviewFixture(t)

	if _, _, err := controller.createAIGroupingReview(context.Background(), lib.ID, "ollama", []metadata.CandidateSeries{
		{ID: seriesA.ID, Title: seriesA.Name},
		{ID: seriesB.ID, Title: "Beta Title"},
	}, []metadata.AIGroupCollection{
		{Name: "Shared Universe", SeriesIDs: []int64{seriesA.ID, seriesB.ID}},
	}); err != nil {
		t.Fatalf("createAIGroupingReview failed: %v", err)
	}

	rec := httptest.NewRecorder()
	controller.listReviewInbox(rec, httptest.NewRequest(http.MethodGet, "/api/reviews/inbox?type=ai-grouping&status=pending", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var aiList aiGroupingReviewsResponse
	if err := json.NewDecoder(rec.Body).Decode(&aiList); err != nil {
		t.Fatalf("decode ai inbox failed: %v", err)
	}
	if aiList.Total != 1 || len(aiList.Items) != 1 {
		t.Fatalf("expected 1 ai grouping review, got %+v", aiList)
	}

	rec = httptest.NewRecorder()
	controller.listReviewInbox(rec, httptest.NewRequest(http.MethodGet, "/api/reviews/inbox?type=metadata", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for metadata inbox, got %d body=%s", rec.Code, rec.Body.String())
	}
	var metaList metadataReviewInboxResponse
	if err := json.NewDecoder(rec.Body).Decode(&metaList); err != nil {
		t.Fatalf("decode metadata inbox failed: %v", err)
	}
	if metaList.Total != 0 {
		t.Fatalf("expected 0 metadata reviews, got %+v", metaList)
	}

	rec = httptest.NewRecorder()
	controller.listReviewInbox(rec, httptest.NewRequest(http.MethodGet, "/api/reviews/inbox?type=bogus", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unsupported type, got %d", rec.Code)
	}
}
