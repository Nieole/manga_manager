package api

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestAIGroupingAppliedIdentityRemainsReadableByProtocols(t *testing.T) {
	c, _, review, candidates := seedAIGroupingReviewWithCollections(t, "Protocol Picks")
	rec := httptest.NewRecorder()
	request := requestWithRouteParam(http.MethodPost, "/api/ai-grouping/reviews/1/collections/1/apply", nil, "reviewId", strconv.FormatInt(review.ID, 10))
	request = withRouteParam(request, "collectionId", strconv.FormatInt(candidates[0].ID, 10))
	c.applyAIGroupingReviewCollection(rec, request)
	var applied applyAIGroupingCollectionResponse
	if err := json.NewDecoder(rec.Body).Decode(&applied); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || applied.CreatedCollectionID == 0 {
		t.Fatalf("apply %d: %+v", rec.Code, applied)
	}
	id := strconv.FormatInt(applied.CreatedCollectionID, 10)
	opds := httptest.NewRecorder()
	c.opdsStaticCollectionSeries(opds, requestWithRouteParam(http.MethodGet, "/opds/v1.2/collections/"+id, nil, "collectionId", id))
	var feed OPDSFeed
	if err := xml.Unmarshal(opds.Body.Bytes(), &feed); err != nil {
		t.Fatal(err)
	}
	if opds.Code != http.StatusOK || feed.Title != "Protocol Picks" || len(feed.Entries) != 1 {
		t.Fatalf("OPDS %d: %+v", opds.Code, feed)
	}
	mihon := httptest.NewRecorder()
	c.mihonCollectionSeries(mihon, requestWithRouteParam(http.MethodGet, "/api/mihon/v1/collections/"+id+"/series", nil, "collectionId", id))
	var page MihonSeriesPageResponse
	if err := json.NewDecoder(mihon.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if mihon.Code != http.StatusOK || page.Total != 1 {
		t.Fatalf("Mihon %d: %+v", mihon.Code, page)
	}
	stored, err := c.aiGroupingActions().Review(context.Background(), review.ID)
	if err != nil || stored.Collections[0].CreatedCollectionID.Int64 != applied.CreatedCollectionID {
		t.Fatalf("review=%+v err=%v", stored, err)
	}
}
