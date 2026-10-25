package organize_test

import (
	"context"
	"database/sql"
	"errors"
	"manga-manager/internal/database"
	"manga-manager/internal/organize"
	"testing"
)

func TestSmartShelfSavedConditionsDriveMembersAndCount(t *testing.T) {
	store, _ := newCollections(t)
	ctx := context.Background()
	db := store.(*database.SqlStore).DB()
	if _, err := db.Exec(`INSERT INTO libraries(id,name,path) VALUES(1,'Library','lib'); INSERT INTO series(id,library_id,name,path,rating) VALUES(1,1,'Alpha','a',9),(2,1,'Beta','b',3)`); err != nil {
		t.Fatal(err)
	}
	service := organize.NewSmartCollections(store)
	rating := 8.0
	filter, err := service.Save(ctx, 1, organize.SaveSmartFilter{Name: " Picks ", MinRating: &rating})
	if err != nil {
		t.Fatal(err)
	}
	page, err := service.Members(ctx, filter.ID, 0, 30, 0)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != 1 || page.Filter.Name != "Picks" {
		t.Fatalf("page=%+v", page)
	}
	views, err := service.Views(ctx, 0)
	if err != nil || len(views) != 1 || views[0].SeriesCount != 1 {
		t.Fatalf("views=%+v err=%v", views, err)
	}
}
func TestSmartSnapshotIsOneTimeAndManuallyOwned(t *testing.T) {
	store, manual := newCollections(t)
	ctx := context.Background()
	db := store.(*database.SqlStore).DB()
	if _, err := db.Exec(`INSERT INTO libraries(id,name,path) VALUES(1,'Library','lib'); INSERT INTO series(id,library_id,name,path,rating) VALUES(1,1,'Alpha','a',9),(2,1,'Beta','b',8)`); err != nil {
		t.Fatal(err)
	}
	service := organize.NewSmartCollections(store)
	filter, err := service.Save(ctx, 1, organize.SaveSmartFilter{Name: "Shelf"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Snapshot(ctx, filter.ID, 0, organize.SmartSnapshotRequest{Name: "Picks", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 2 || result.SeriesCount != 1 || !result.Truncated {
		t.Fatalf("result=%+v", result)
	}
	if _, err := service.Update(ctx, filter.ID, organize.SaveSmartFilter{Name: "Shelf", ActiveTag: ptr("Missing")}); err != nil {
		t.Fatal(err)
	}
	members, err := manual.Members(ctx, result.ID)
	if err != nil || len(members) != 1 || members[0].SeriesID != 1 {
		t.Fatalf("snapshot=%+v err=%v", members, err)
	}
	if _, err := manual.AddMembers(ctx, result.ID, []int64{2}); err != nil {
		t.Fatal(err)
	}
	if err := manual.Update(ctx, result.ID, "Edited", ""); err != nil {
		t.Fatal(err)
	}
}
func ptr[T any](value T) *T { return &value }
func TestSmartShelfAdvancedConditionsAreUserScopedAndPageable(t *testing.T) {
	store, _ := newCollections(t)
	ctx := context.Background()
	db := store.(*database.SqlStore).DB()
	if _, err := db.Exec(`
 INSERT INTO libraries(id,name,path) VALUES(1,'Library','lib');
 INSERT INTO users(id,username,password_hash,role) VALUES(1,'alice','hash','admin'),(2,'bob','hash','regular');
 INSERT INTO series(id,library_id,name,path,rating,status,name_initial,book_count,total_pages) VALUES(1,1,'Alpha','a',9,'ongoing','A',2,100),(2,1,'Another','b',8,'ongoing','A',2,100),(3,1,'Avoid','c',3,'ongoing','A',2,100);
 INSERT INTO tags(id,name) VALUES(1,'Action');INSERT INTO authors(id,name,role) VALUES(1,'Writer','writer');
 INSERT INTO series_tags(series_id,tag_id) VALUES(1,1),(2,1),(3,1);
 INSERT INTO series_authors(series_id,author_id) VALUES(1,1),(2,1),(3,1);
 INSERT INTO user_series_progress(user_id,series_id,read_pages,read_book_count,completed_book_count) VALUES(1,1,40,1,0),(1,2,60,1,0);
 `); err != nil {
		t.Fatal(err)
	}
	service := organize.NewSmartCollections(store)
	filter, err := service.Save(ctx, 1, organize.SaveSmartFilter{Name: "Advanced", ActiveTag: ptr(" Action "), ActiveAuthor: ptr("Writer"), ActiveStatus: ptr("ongoing"), ActiveLetter: ptr("A"), ReadState: ptr("reading"), MinRating: ptr(7.0), MaxRating: ptr(10.0), MinProgress: ptr(30.0), MaxProgress: ptr(70.0), AddedWithinDays: ptr(30), SortByField: "rating", SortDir: "desc"})
	if err != nil {
		t.Fatal(err)
	}
	for offset, want := range []int64{1, 2} {
		page, err := service.Members(ctx, filter.ID, 1, 1, offset)
		if err != nil || page.Total != 2 || len(page.Items) != 1 || page.Items[0].ID != want {
			t.Fatalf("offset=%d page=%+v err=%v", offset, page, err)
		}
	}
	views, err := service.Views(ctx, 1)
	if err != nil || views[0].SeriesCount != 2 {
		t.Fatalf("alice views=%+v err=%v", views, err)
	}
	page, err := service.Members(ctx, filter.ID, 2, 1, 0)
	if err != nil || page.Total != 0 || len(page.Items) != 0 {
		t.Fatalf("bob page=%+v err=%v", page, err)
	}
	views, err = service.Views(ctx, 2)
	if err != nil || views[0].SeriesCount != 0 {
		t.Fatalf("bob views=%+v err=%v", views, err)
	}
	result, err := service.Snapshot(ctx, filter.ID, 1, organize.SmartSnapshotRequest{Name: "Alice Picks"})
	if err != nil || result.SeriesCount != 2 {
		t.Fatalf("alice snapshot=%+v err=%v", result, err)
	}
	if _, err := service.Snapshot(ctx, filter.ID, 2, organize.SmartSnapshotRequest{Name: "Bob Picks"}); !errors.Is(err, organize.ErrSmartSnapshotEmpty) {
		t.Fatalf("bob snapshot=%v", err)
	}
}
func TestSmartSnapshotPreviewConflictAndFailureRollback(t *testing.T) {
	store, manual := newCollections(t)
	ctx := context.Background()
	db := store.(*database.SqlStore).DB()
	if _, err := db.Exec(`INSERT INTO libraries(id,name,path) VALUES(1,'Library','lib'); INSERT INTO series(id,library_id,name,path) VALUES(1,1,'Alpha','a'),(2,1,'Beta','b')`); err != nil {
		t.Fatal(err)
	}
	service := organize.NewSmartCollections(store)
	filter, err := service.Save(ctx, 1, organize.SaveSmartFilter{Name: "Shelf"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manual.Create(ctx, "Taken", ""); err != nil {
		t.Fatal(err)
	}
	preview, err := service.PreviewSnapshot(ctx, filter.ID, 0, organize.SmartSnapshotRequest{Name: " taken ", Limit: 1}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.NameConflict || !preview.Truncated || preview.SnapshotCount != 1 || preview.Total != 2 || len(preview.Items) != 1 {
		t.Fatalf("preview=%+v", preview)
	}
	if _, err := service.Snapshot(ctx, filter.ID, 0, organize.SmartSnapshotRequest{Name: "taken"}); !errors.Is(err, organize.ErrCollectionNameTaken) {
		t.Fatalf("conflict=%v", err)
	}
	if _, err := db.Exec(`CREATE TRIGGER fail_member BEFORE INSERT ON collection_series WHEN NEW.series_id=2 BEGIN SELECT RAISE(ABORT,'second member failed'); END`); err != nil {
		t.Fatal(err)
	}
	result, err := service.Snapshot(ctx, filter.ID, 0, organize.SmartSnapshotRequest{Name: "Rollback"})
	if err == nil || result.ID != 0 {
		t.Fatalf("failed snapshot=%+v err=%v", result, err)
	}
	list, err := manual.List(ctx)
	if err != nil || len(list) != 1 || list[0].Name != "Taken" {
		t.Fatalf("rollback list=%+v err=%v", list, err)
	}
	if _, err := db.Exec(`DROP TRIGGER fail_member`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Snapshot(ctx, filter.ID, 0, organize.SmartSnapshotRequest{Name: "Rollback"}); err != nil {
		t.Fatal(err)
	}
}
func TestSmartShelfValidationAndDelete(t *testing.T) {
	store, _ := newCollections(t)
	ctx := context.Background()
	db := store.(*database.SqlStore).DB()
	if _, err := db.Exec(`INSERT INTO libraries(id,name,path) VALUES(1,'Library','lib')`); err != nil {
		t.Fatal(err)
	}
	service := organize.NewSmartCollections(store)
	for _, req := range []organize.SaveSmartFilter{{Name: " "}, {Name: "Shelf", ReadState: ptr("bad")}, {Name: "Shelf", MinRating: ptr(11.0)}, {Name: "Shelf", MinProgress: ptr(80.0), MaxProgress: ptr(20.0)}, {Name: "Shelf", PageSize: 2}, {Name: "Shelf", AddedWithinDays: ptr(0)}} {
		var invalid *organize.InvalidSmartFilter
		if _, err := service.Save(ctx, 1, req); !errors.As(err, &invalid) {
			t.Fatalf("request=%+v err=%v", req, err)
		}
	}
	filter, err := service.Save(ctx, 1, organize.SaveSmartFilter{Name: "Shelf"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Save(ctx, 1, organize.SaveSmartFilter{Name: "Shelf", SortByField: "rating"}); err != nil {
		t.Fatal(err)
	}
	list, err := service.List(ctx, 1)
	if err != nil || len(list) != 1 || list[0].ID != filter.ID || list[0].SortByField != "rating" {
		t.Fatalf("upsert=%+v err=%v", list, err)
	}
	if _, err := service.Snapshot(ctx, filter.ID, 0, organize.SmartSnapshotRequest{}); !errors.Is(err, organize.ErrSmartSnapshotEmpty) {
		t.Fatalf("empty=%v", err)
	}
	if err := service.Delete(ctx, filter.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Get(ctx, filter.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("deleted=%v", err)
	}
}
func TestSmartShelfRenameReportsNameConflict(t *testing.T) {
	store, _ := newCollections(t)
	ctx := context.Background()
	db := store.(*database.SqlStore).DB()
	if _, err := db.Exec(`INSERT INTO libraries(id,name,path) VALUES(1,'Library','lib')`); err != nil {
		t.Fatal(err)
	}
	service := organize.NewSmartCollections(store)
	first, err := service.Save(ctx, 1, organize.SaveSmartFilter{Name: "First"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Save(ctx, 1, organize.SaveSmartFilter{Name: "Taken"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Update(ctx, first.ID, organize.SaveSmartFilter{Name: "Taken"}); !errors.Is(err, organize.ErrSmartFilterNameTaken) {
		t.Fatalf("rename conflict=%v", err)
	}
}
