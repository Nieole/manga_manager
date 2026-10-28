package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"manga-manager/internal/organize"
)

func TestCollectionAddMissingSeriesReturnsNotFoundAndKeepsBatchAtomic(t *testing.T) {
	c, store, _, root := newTestController(t)
	_, _, book := seedBookFixture(t, store, root, "Lib", "Series", "01.cbz", 20)
	collections := organize.NewCollections(store)
	id, err := collections.Create(context.Background(), "Picks", "")
	if err != nil {
		t.Fatal(err)
	}
	req := requestWithRouteParam(http.MethodPost, "/", []byte(`{"series_ids":[`+strconv.FormatInt(book.SeriesID, 10)+`,999999]}`), "collectionId", strconv.FormatInt(id, 10))
	rec := httptest.NewRecorder()
	c.addSeriesToCollection(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing series status=%d body=%s", rec.Code, rec.Body.String())
	}
	members, err := collections.Members(context.Background(), id)
	if err != nil || len(members) != 0 {
		t.Fatalf("partial batch=%+v err=%v", members, err)
	}
}

func TestSnapshotMissingMemberReturnsNotFoundAndRollsBackCollection(t *testing.T) {
	c, store, _, root := newTestController(t)
	_, _, book := seedBookFixture(t, store, root, "Lib", "Series", "01.cbz", 20)
	filter, err := organize.NewSmartCollections(store).Save(context.Background(), book.LibraryID, organize.SaveSmartFilter{Name: "Shelf"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`CREATE TRIGGER remove_snapshot_member AFTER INSERT ON collections BEGIN DELETE FROM series;END`); err != nil {
		t.Fatal(err)
	}
	req := requestWithRouteParam(http.MethodPost, "/", []byte(`{"name":"Snapshot"}`), "filterId", strconv.FormatInt(filter.ID, 10))
	rec := httptest.NewRecorder()
	c.snapshotSmartCollection(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing snapshot member status=%d body=%s", rec.Code, rec.Body.String())
	}
	list, err := organize.NewCollections(store).List(context.Background())
	if err != nil || len(list) != 0 {
		t.Fatalf("partial snapshot=%+v err=%v", list, err)
	}
}
