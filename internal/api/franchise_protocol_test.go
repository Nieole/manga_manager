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

func TestRelationWritesKeepFranchiseProtocolAndGraphIdentity(t *testing.T) {
	c, store, _, _ := newTestController(t)
	libraryID, ids := seedFranchiseSeries(t, store, 3)
	create := httptest.NewRecorder()
	c.createSeriesRelation(create, requestWithRouteParam(http.MethodPost, "/api/series/1/relations", []byte(`{"target_series_id":`+strconv.FormatInt(ids[1], 10)+`}`), "seriesId", strconv.FormatInt(ids[0], 10)))
	if create.Code != http.StatusCreated {
		t.Fatalf("create %d: %s", create.Code, create.Body.String())
	}
	c.runtime.WaitBackground()
	snapshots := readFranchises(t, store)
	if len(snapshots) != 1 {
		t.Fatalf("snapshots=%+v", snapshots)
	}
	collectionID := snapshots[0].ID
	list, err := c.relations.List(context.Background(), ids[0])
	if err != nil {
		t.Fatal(err)
	}
	update := httptest.NewRecorder()
	c.updateSeriesRelation(update, requestWithRouteParam(http.MethodPut, "/api/series/relations/1", []byte(`{"relation_type":"spinoff"}`), "relationId", strconv.FormatInt(list[0].ID, 10)))
	if update.Code != http.StatusOK {
		t.Fatalf("update %d: %s", update.Code, update.Body.String())
	}
	c.runtime.WaitBackground()
	after := readFranchises(t, store)
	if len(after) != 1 || after[0].ID != collectionID {
		t.Fatalf("identity=%+v", after)
	}
	opds := httptest.NewRecorder()
	c.opdsStaticCollectionSeries(opds, requestWithRouteParam(http.MethodGet, "/opds/v1.2/collections/1", nil, "collectionId", strconv.FormatInt(collectionID, 10)))
	var feed OPDSFeed
	if err := xml.Unmarshal(opds.Body.Bytes(), &feed); err != nil {
		t.Fatal(err)
	}
	if opds.Code != http.StatusOK || len(feed.Entries) != 2 {
		t.Fatalf("OPDS %d: %+v", opds.Code, feed)
	}
	mihon := httptest.NewRecorder()
	c.mihonCollectionSeries(mihon, requestWithRouteParam(http.MethodGet, "/api/mihon/v1/collections/1/series", nil, "collectionId", strconv.FormatInt(collectionID, 10)))
	var page MihonSeriesPageResponse
	if err := json.NewDecoder(mihon.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if mihon.Code != http.StatusOK || page.Total != 2 {
		t.Fatalf("Mihon %d: %+v", mihon.Code, page)
	}
	graph := httptest.NewRecorder()
	c.getLibraryFranchiseGraph(graph, requestWithRouteParam(http.MethodGet, "/api/libraries/1/franchise-graph", nil, "libraryId", strconv.FormatInt(libraryID, 10)))
	var edges []struct {
		ID   int64  `json:"id"`
		Type string `json:"relation_type"`
	}
	if err := json.NewDecoder(graph.Body).Decode(&edges); err != nil {
		t.Fatal(err)
	}
	if graph.Code != http.StatusOK || len(edges) != 1 || edges[0].Type != "spinoff" {
		t.Fatalf("graph %d: %+v", graph.Code, edges)
	}
}
