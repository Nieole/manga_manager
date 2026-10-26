package organize_test

import (
	"context"
	"errors"
	"manga-manager/internal/database"
	"manga-manager/internal/organize"
	"path/filepath"
	"testing"
)

func TestManualCollectionsNameAndIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "collections.db")
	if err := database.Migrate(path); err != nil {
		t.Fatal(err)
	}
	store, err := database.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := organize.NewCollections(store)
	ctx := context.Background()
	id, err := service.Create(ctx, "  Stories  ", " picked ")
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.Get(ctx, id)
	if err != nil || got.Name != "Stories" || got.Description.String != "picked" {
		t.Fatalf("get=%+v err=%v", got, err)
	}
	if _, err := service.Create(ctx, "stories", ""); !errors.Is(err, organize.ErrCollectionNameTaken) {
		t.Fatalf("duplicate=%v", err)
	}
	if err := service.Update(ctx, id, "STORIES", "changed"); err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Get(ctx, id); !errors.Is(err, organize.ErrCollectionNotFound) {
		t.Fatalf("deleted=%v", err)
	}
}
func TestRenameKeepsItsOwnIdentityBeyondFirstCollection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "collections.db")
	if err := database.Migrate(path); err != nil {
		t.Fatal(err)
	}
	store, err := database.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := organize.NewCollections(store)
	ctx := context.Background()
	if _, err := service.Create(ctx, "First", ""); err != nil {
		t.Fatal(err)
	}
	id, err := service.Create(ctx, "Second", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Update(ctx, id, "SECOND", "edited"); err != nil {
		t.Fatal(err)
	}
}
func TestManualMembershipRollsBackWhenTimestampWriteFails(t *testing.T) {
	store, service := newCollections(t)
	ctx := context.Background()
	db := store.(*database.SqlStore).DB()
	if _, err := db.Exec(`INSERT INTO libraries(id,name,path) VALUES(1,'Library','library'); INSERT INTO series(id,library_id,name,path) VALUES(1,1,'Series','series')`); err != nil {
		t.Fatal(err)
	}
	id, err := service.Create(ctx, "Picks", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER reject_touch BEFORE UPDATE ON collections BEGIN SELECT RAISE(ABORT,'touch failed'); END`); err != nil {
		t.Fatal(err)
	}
	n, err := service.AddMembers(ctx, id, []int64{1})
	if err == nil || n != 0 {
		t.Fatalf("failed transaction added=%d err=%v", n, err)
	}
	members, err := service.Members(ctx, id)
	if err != nil || len(members) != 0 {
		t.Fatalf("members=%v err=%v", members, err)
	}
	if _, err := db.Exec(`DROP TRIGGER reject_touch`); err != nil {
		t.Fatal(err)
	}
	n, err = service.AddMembers(ctx, id, []int64{1, 1})
	if err != nil || n != 1 {
		t.Fatalf("retry added=%d err=%v", n, err)
	}
	if err := service.RemoveMember(ctx, id, 1); err != nil {
		t.Fatal(err)
	}
	if err := service.RemoveMember(ctx, id, 1); !errors.Is(err, organize.ErrCollectionMemberNotFound) {
		t.Fatalf("missing member=%v", err)
	}
}
func TestOnlyContinuouslyDerivedCollectionsRejectManualChanges(t *testing.T) {
	for _, source := range []string{"manual", "smart_snapshot", "ai_grouping", "system_franchise"} {
		t.Run(source, func(t *testing.T) {
			store, service := newCollections(t)
			ctx := context.Background()
			db := store.(*database.SqlStore).DB()
			if _, err := db.Exec(`INSERT INTO libraries(id,name,path) VALUES(1,'Library','library'); INSERT INTO series(id,library_id,name,path) VALUES(1,1,'Series','series')`); err != nil {
				t.Fatal(err)
			}
			result, err := db.Exec(`INSERT INTO collections(name,source_type) VALUES('Picks',?)`, source)
			if err != nil {
				t.Fatal(err)
			}
			id, err := result.LastInsertId()
			if err != nil {
				t.Fatal(err)
			}
			updateErr := service.Update(ctx, id, "Edited", "")
			_, addErr := service.AddMembers(ctx, id, []int64{1})
			removeErr := service.RemoveMember(ctx, id, 1)
			deleteErr := service.Delete(ctx, id)
			for _, err := range []error{updateErr, addErr, removeErr, deleteErr} {
				if source == "system_franchise" {
					if !errors.Is(err, organize.ErrCollectionReadOnly) {
						t.Fatalf("derived mutation=%v", err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
func TestMemberBatchIsAtomicForMissingSeries(t *testing.T) {
	store, service := newCollections(t)
	ctx := context.Background()
	db := store.(*database.SqlStore).DB()
	if _, err := db.Exec(`INSERT INTO libraries(id,name,path) VALUES(1,'Library','library'); INSERT INTO series(id,library_id,name,path) VALUES(1,1,'Series','series')`); err != nil {
		t.Fatal(err)
	}
	id, err := service.Create(ctx, "Picks", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.AddMembers(ctx, id, []int64{1, 99999}); err == nil {
		t.Fatal("missing series accepted")
	}
	members, err := service.Members(ctx, id)
	if err != nil || len(members) != 0 {
		t.Fatalf("partial members=%v err=%v", members, err)
	}
}
func newCollections(t *testing.T) (database.Store, *organize.Collections) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "collections.db")
	if err := database.Migrate(path); err != nil {
		t.Fatal(err)
	}
	store, err := database.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, organize.NewCollections(store)
}
func TestRenameUsesSQLiteNameComparison(t *testing.T) {
	_, service := newCollections(t)
	ctx := context.Background()
	id, err := service.Create(ctx, "K", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Create(ctx, "k", ""); err != nil {
		t.Fatal(err)
	}
	if err := service.Update(ctx, id, "k", ""); !errors.Is(err, organize.ErrCollectionNameTaken) {
		t.Fatalf("conflicting rename=%v", err)
	}
}
