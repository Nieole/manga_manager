package organize_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/organize"
	"manga-manager/internal/reading"
)

func TestOrderedReadingListsOwnMembershipAndAtomicReorder(t *testing.T) {
	store, _ := newCollections(t)
	ctx := context.Background()
	db := store.(*database.SqlStore).DB()
	if _, err := db.Exec(`INSERT INTO libraries(id,name,path) VALUES(1,'Library','library'); INSERT INTO series(id,library_id,name,path) VALUES(1,1,'Alpha','a'),(2,1,'Beta','b')`); err != nil {
		t.Fatal(err)
	}
	lists := organize.NewReadingLists(store)
	list, err := lists.Create(ctx, "  Order  ", " description ")
	if err != nil || list.Name != "Order" || list.Description != "description" {
		t.Fatalf("create=%+v err=%v", list, err)
	}
	a, err := lists.Add(ctx, list.ID, 1, " first ")
	if err != nil {
		t.Fatal(err)
	}
	b, err := lists.Add(ctx, list.ID, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	again, err := lists.Add(ctx, list.ID, 1, "edited")
	if err != nil || again.ID != a.ID || again.SortOrder != a.SortOrder {
		t.Fatalf("duplicate=%+v %v", again, err)
	}
	if err := lists.Reorder(ctx, list.ID, []int64{b.ID, 999}); !errors.Is(err, organize.ErrReadingListItemMissing) {
		t.Fatalf("bad reorder=%v", err)
	}
	items, err := lists.Items(ctx, 0, list.ID)
	if err != nil || len(items) != 2 || items[0].ID != a.ID {
		t.Fatalf("rollback=%+v %v", items, err)
	}
	if err := lists.Reorder(ctx, list.ID, []int64{b.ID, a.ID}); err != nil {
		t.Fatal(err)
	}
	items, err = lists.Items(ctx, 0, list.ID)
	if err != nil || items[0].ID != b.ID || items[1].Note != "edited" {
		t.Fatalf("order=%+v %v", items, err)
	}
	if err := lists.Remove(ctx, list.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	page, err := lists.Page(ctx, 0, list.ID, 1, 0)
	if err != nil || page.Total != 1 || page.Rows[0].ItemID != a.ID {
		t.Fatalf("page=%+v %v", page, err)
	}
	if _, err := lists.Update(ctx, list.ID, "Renamed", "changed"); err != nil {
		t.Fatal(err)
	}
	got, err := lists.Get(ctx, list.ID)
	if err != nil || got.Name != "Renamed" {
		t.Fatalf("get=%+v %v", got, err)
	}
	if err := lists.Delete(ctx, list.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := lists.Get(ctx, list.ID); !errors.Is(err, organize.ErrReadingListMissing) {
		t.Fatalf("delete=%v", err)
	}
}

func TestReadingListProgressFollowsReaderFactsAndNeverCreatesActivity(t *testing.T) {
	store, _ := newCollections(t)
	ctx := context.Background()
	db := store.(*database.SqlStore).DB()
	if _, err := db.Exec(`INSERT INTO libraries(id,name,path) VALUES(1,'Library','library'); INSERT INTO series(id,library_id,name,path) VALUES(1,1,'Series','series'); INSERT INTO books(id,library_id,series_id,name,path,page_count,size,file_modified_at) VALUES(1,1,1,'01','one',10,1,CURRENT_TIMESTAMP),(2,1,1,'02','two',0,1,CURRENT_TIMESTAMP); INSERT INTO users(id,username,password_hash,role) VALUES(1,'one','x','admin'),(2,'two','x','user')`); err != nil {
		t.Fatal(err)
	}
	lists := organize.NewReadingLists(store)
	list, err := lists.Create(ctx, "Order", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = lists.Add(ctx, list.ID, 1, ""); err != nil {
		t.Fatal(err)
	}
	writes := reading.New(store, nil)
	at := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	if _, err = writes.MarkSeries(ctx, reading.MarkSeriesInput{SeriesIDs: []int64{1}, IsRead: true, OccurredAt: &at}); err != nil {
		t.Fatal(err)
	}
	assertProgress := func(uid, started, completed, next int64) {
		t.Helper()
		items, err := lists.Items(ctx, uid, list.ID)
		if err != nil || len(items) != 1 || items[0].ReadBooks != started || items[0].CompletedBooks != completed || items[0].TotalBooks != 2 || items[0].NextBookID != next {
			t.Fatalf("user=%d items=%+v err=%v", uid, items, err)
		}
		page, err := lists.Page(ctx, uid, list.ID, 1, 0)
		if err != nil || page.Total != 1 || len(page.Rows) != 1 || page.Rows[0].NextBookID != next {
			t.Fatalf("user=%d page=%+v err=%v", uid, page, err)
		}
	}
	assertProgress(0, 2, 2, 1)
	assertProgress(1, 0, 0, 1)
	if _, err = writes.MarkSeries(ctx, reading.MarkSeriesInput{UserID: 1, SeriesIDs: []int64{1}, IsRead: true, OccurredAt: &at}); err != nil {
		t.Fatal(err)
	}
	assertProgress(1, 2, 2, 1)
	assertProgress(2, 0, 0, 1)
	unknown, err := reading.NewQueries(store).Book(ctx, 1, 2)
	if err != nil || !unknown.IsRead || unknown.LastReadPage.Valid {
		t.Fatalf("unknown=%+v err=%v", unknown, err)
	}
	later := at.Add(time.Hour)
	if _, err = writes.RecordPosition(ctx, reading.Position{UserID: 1, BookID: 1, Page: 3, OccurredAt: &later}); err != nil {
		t.Fatal(err)
	}
	assertProgress(1, 2, 1, 1)
	later = later.Add(time.Hour)
	if _, err = writes.MarkBooks(ctx, reading.MarkBooksInput{UserID: 1, BookIDs: []int64{1}, IsRead: false, OccurredAt: &later}); err != nil {
		t.Fatal(err)
	}
	before, err := store.GetUserActivityHeatmap(ctx, 1, "+0 hours")
	if err != nil {
		t.Fatal(err)
	}
	assertProgress(1, 1, 1, 1)
	after, err := store.GetUserActivityHeatmap(ctx, 1, "+0 hours")
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("read activity before=%+v after=%+v err=%v", before, after, err)
	}
	// Deleting a catalogued member leaves a readable empty list with stable list identity.
	if err := store.DeleteSeries(ctx, 1); err != nil {
		t.Fatal(err)
	}
	page, err := lists.Page(ctx, 1, list.ID, 10, 0)
	if err != nil || page.List.ID != list.ID || page.Total != 0 || len(page.Rows) != 0 {
		t.Fatalf("deleted member=%+v err=%v", page, err)
	}
}

func TestReadingListTargetsKeepCatalogueOrderAndCompletedFallback(t *testing.T) {
	store, _ := newCollections(t)
	ctx := context.Background()
	if _, err := store.(*database.SqlStore).DB().Exec(`
INSERT INTO libraries(id,name,path) VALUES(1,'Library','library');
INSERT INTO series(id,library_id,name,path) VALUES(1,1,'Series','series'),(2,1,'Empty','empty');
INSERT INTO books(id,library_id,series_id,name,path,volume,sort_number,page_count,size,file_modified_at) VALUES
(1,1,1,'02','one','1',2,10,1,CURRENT_TIMESTAMP),
(2,1,1,'10','two','2',1,10,1,CURRENT_TIMESTAMP),
(3,1,1,'03','three','3',1,10,1,CURRENT_TIMESTAMP),
(4,1,1,'04','four','4',NULL,10,1,CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	lists := organize.NewReadingLists(store)
	list, err := lists.Create(ctx, "Order", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, sid := range []int64{1, 2} {
		if _, err := lists.Add(ctx, list.ID, sid, ""); err != nil {
			t.Fatal(err)
		}
	}
	assertTarget := func(want int64) {
		t.Helper()
		items, err := lists.Items(ctx, 0, list.ID)
		if err != nil || len(items) != 2 || items[0].NextBookID != want || items[1].NextBookID != 0 {
			t.Fatalf("items=%+v err=%v want target=%d and empty=0", items, err, want)
		}
		page, err := lists.Page(ctx, 0, list.ID, 10, 0)
		if err != nil || len(page.Rows) != 2 || page.Rows[0].NextBookID != want || page.Rows[1].NextBookID != 0 {
			t.Fatalf("page=%+v err=%v want target=%d and empty=0", page, err, want)
		}
	}
	assertTarget(4) // NULL sort number precedes numbered books, independently of volume.
	writes := reading.New(store, nil)
	for _, step := range []struct {
		ids  []int64
		want int64
	}{{[]int64{4}, 3}, {[]int64{3}, 2}, {[]int64{2}, 1}, {[]int64{1}, 4}} {
		if _, err := writes.MarkBooks(ctx, reading.MarkBooksInput{BookIDs: step.ids, IsRead: true}); err != nil {
			t.Fatal(err)
		}
		assertTarget(step.want)
	}
}
