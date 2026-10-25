package organize_test

import (
	"context"
	"errors"
	"manga-manager/internal/database"
	"manga-manager/internal/organize"
	"testing"
)

func TestRelationChangesRebuildDerivedCollections(t *testing.T) {
	store, manual := newCollections(t)
	db := store.(*database.SqlStore).DB()
	ctx := context.Background()
	if _, err := db.Exec(`INSERT INTO libraries(id,name,path) VALUES(1,'Library','lib');INSERT INTO series(id,library_id,name,path) VALUES(1,1,'Alpha','a'),(2,1,'Beta','b'),(3,1,'Gamma','c')`); err != nil {
		t.Fatal(err)
	}
	relations := organize.NewRelations(store, func(work func()) { work() })
	created, err := relations.Create(ctx, 1, 2, " ")
	if err != nil || created.Existing {
		t.Fatalf("created=%+v err=%v", created, err)
	}
	views, err := organize.NewSmartCollections(store).Views(ctx, 0)
	if err != nil || len(views) != 1 {
		t.Fatalf("views=%+v err=%v", views, err)
	}
	id := views[0].NumericID
	members, err := manual.Members(ctx, id)
	if err != nil || len(members) != 2 {
		t.Fatalf("members=%+v err=%v", members, err)
	}
	list, err := relations.List(ctx, 1)
	if err != nil || len(list) != 1 || list[0].RelationType != "sequel" {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	if err := relations.Update(ctx, list[0].ID, " spinoff "); err != nil {
		t.Fatal(err)
	}
	if err := relations.Delete(ctx, list[0].ID); err != nil {
		t.Fatal(err)
	}
	views, err = organize.NewSmartCollections(store).Views(ctx, 0)
	if err != nil || len(views) != 0 {
		t.Fatalf("deleted views=%+v err=%v", views, err)
	}
}
func TestFranchiseMergeSplitKeepsUnaffectedIdentitiesAndTimes(t *testing.T) {
	store, manual := newCollections(t)
	db := store.(*database.SqlStore).DB()
	ctx := context.Background()
	if _, err := db.Exec(`INSERT INTO libraries(id,name,path) VALUES(1,'Library','lib');INSERT INTO series(id,library_id,name,path) VALUES(1,1,'Alpha','a'),(2,1,'Beta','b'),(3,1,'Gamma','c'),(4,1,'Delta','d'),(5,1,'Epsilon','e')`); err != nil {
		t.Fatal(err)
	}
	service := organize.NewRelations(store, func(work func()) { work() })
	for _, pair := range [][2]int64{{1, 2}, {4, 5}} {
		if _, err := service.Create(ctx, pair[0], pair[1], "sequel"); err != nil {
			t.Fatal(err)
		}
	}
	views, err := organize.NewSmartCollections(store).Views(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]int64{}
	for _, view := range views {
		ids[view.Name] = view.NumericID
	}
	alpha, delta := ids["Alpha Franchise"], ids["Delta Franchise"]
	if alpha == 0 || delta == 0 {
		t.Fatalf("views=%+v", views)
	}
	if _, err := db.Exec(`UPDATE collections SET updated_at='2001-01-01 00:00:00';UPDATE collection_series SET added_at='2001-01-01 00:00:00'`); err != nil {
		t.Fatal(err)
	}
	before, err := manual.Get(ctx, alpha)
	if err != nil {
		t.Fatal(err)
	}
	unrelated, err := manual.Get(ctx, delta)
	if err != nil {
		t.Fatal(err)
	}
	members, err := manual.Members(ctx, alpha)
	if err != nil {
		t.Fatal(err)
	}
	initialTime := members[0].AddedAt
	list, err := service.List(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Update(ctx, list[0].ID, "spinoff"); err != nil {
		t.Fatal(err)
	}
	same, err := manual.Get(ctx, alpha)
	if err != nil || !same.UpdatedAt.Time.Equal(before.UpdatedAt.Time) {
		t.Fatalf("unchanged=%+v err=%v", same, err)
	}
	if _, err := service.Create(ctx, 2, 3, "sequel"); err != nil {
		t.Fatal(err)
	}
	changed, err := manual.Get(ctx, alpha)
	if err != nil || changed.SeriesCount != 3 || !changed.UpdatedAt.Time.After(before.UpdatedAt.Time) {
		t.Fatalf("expanded=%+v err=%v", changed, err)
	}
	members, err = manual.Members(ctx, alpha)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range members {
		if m.SeriesID <= 2 && !m.AddedAt.Time.Equal(initialTime.Time) {
			t.Fatalf("existing member time changed: %+v", m)
		}
	}
	unchanged, err := manual.Get(ctx, delta)
	if err != nil || !unchanged.UpdatedAt.Time.Equal(unrelated.UpdatedAt.Time) {
		t.Fatalf("unrelated=%+v err=%v", unchanged, err)
	}
	if _, err := service.Create(ctx, 3, 4, "sequel"); err != nil {
		t.Fatal(err)
	}
	merged, err := manual.Get(ctx, alpha)
	if err != nil || merged.SeriesCount != 5 {
		t.Fatalf("merged=%+v err=%v", merged, err)
	}
	graph, err := service.Graph(ctx, 3)
	if err != nil {
		t.Fatal(err)
	}
	var bridge int64
	for _, edge := range graph {
		if edge.SourceSeriesID == 3 && edge.TargetSeriesID == 4 {
			bridge = edge.ID
		}
	}
	if bridge == 0 {
		t.Fatalf("bridge absent: %+v", graph)
	}
	if err := service.Delete(ctx, bridge); err != nil {
		t.Fatal(err)
	}
	split, err := manual.Get(ctx, alpha)
	if err != nil || split.SeriesCount != 3 {
		t.Fatalf("split=%+v err=%v", split, err)
	}
	views, err = organize.NewSmartCollections(store).Views(ctx, 0)
	if err != nil || len(views) != 2 {
		t.Fatalf("split views=%+v err=%v", views, err)
	}
	if err := manual.Update(ctx, alpha, "Edited", ""); !errors.Is(err, organize.ErrCollectionReadOnly) {
		t.Fatalf("manual mutation=%v", err)
	}
}
func TestFranchiseRebuildFailureKeepsPreviousProjectionAndQueuedLifecycle(t *testing.T) {
	store, manual := newCollections(t)
	db := store.(*database.SqlStore).DB()
	ctx := context.Background()
	if _, err := db.Exec(`INSERT INTO libraries(id,name,path) VALUES(1,'Library','lib');INSERT INTO series(id,library_id,name,path) VALUES(1,1,'Alpha','a'),(2,1,'Beta','b'),(3,1,'Gamma','c')`); err != nil {
		t.Fatal(err)
	}
	var work []func()
	service := organize.NewRelations(store, func(fn func()) { work = append(work, fn) })
	for _, target := range []int64{2, 3} {
		if _, err := service.Create(ctx, 1, target, "sequel"); err != nil {
			t.Fatal(err)
		}
	}
	if len(work) != 1 {
		t.Fatalf("lifecycle work=%d want one coalesced rebuild", len(work))
	}
	if _, err := db.Exec(`CREATE TRIGGER reject_member BEFORE INSERT ON collection_series WHEN NEW.series_id=3 BEGIN SELECT RAISE(ABORT,'member failed'); END`); err != nil {
		t.Fatal(err)
	}
	work[0]()
	if err := service.Rebuild(ctx); err == nil {
		t.Fatal("rebuild should fail")
	}
	views, err := organize.NewSmartCollections(store).Views(ctx, 0)
	if err != nil || len(views) != 0 {
		t.Fatalf("failed rebuild leaked partial collection: %+v err=%v", views, err)
	}
	if _, err := db.Exec(`DROP TRIGGER reject_member`); err != nil {
		t.Fatal(err)
	}
	if err := service.Rebuild(ctx); err != nil {
		t.Fatal(err)
	}
	views, err = organize.NewSmartCollections(store).Views(ctx, 0)
	if err != nil || len(views) != 1 {
		t.Fatalf("recovered=%+v err=%v", views, err)
	}
	members, err := manual.Members(ctx, views[0].NumericID)
	if err != nil || len(members) != 3 {
		t.Fatalf("recovered members=%+v err=%v", members, err)
	}
	if _, err := db.Exec(`DELETE FROM series WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if err := service.Rebuild(ctx); err != nil {
		t.Fatal(err)
	}
	views, err = organize.NewSmartCollections(store).Views(ctx, 0)
	if err != nil || len(views) != 0 {
		t.Fatalf("deleted series projection=%+v err=%v", views, err)
	}
}
