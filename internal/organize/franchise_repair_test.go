package organize_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/organize"
)

func TestFranchiseRepairResponsibilitySurvivesWithoutBackgroundNotification(t *testing.T) {
	store, _ := newCollections(t)
	if _, err := store.(*database.SqlStore).DB().Exec(`INSERT INTO libraries(id,name,path) VALUES(1,'Library','lib'),(2,'Other library','other'); INSERT INTO series(id,library_id,name,path) VALUES(1,1,'Alpha','a'),(2,2,'Beta','b')`); err != nil {
		t.Fatal(err)
	}
	service := organize.NewRelations(store, func(func()) {})
	ctx := context.Background()
	initial, err := service.FranchiseState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if initial.Available || !initial.Stale {
		t.Fatalf("new database claims a successful projection: %+v", initial)
	}
	if _, err := service.Create(ctx, 1, 2, "sequel"); err != nil {
		t.Fatal(err)
	}
	// A fresh service has no memory of the discarded notification.
	recovered := organize.NewRelations(store, func(fn func()) { fn() })
	before, err := recovered.FranchiseState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if before.DesiredVersion <= initial.DesiredVersion || !before.Stale {
		t.Fatalf("missing durable dirty version: %+v", before)
	}
	if err := recovered.Rebuild(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := recovered.FranchiseState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !after.Available || after.Stale || after.AppliedVersion != before.DesiredVersion {
		t.Fatalf("not repaired: %+v", after)
	}
	views, err := organize.NewSmartCollections(store).Views(ctx, readerSubject(t, 0))
	if err != nil || len(views) != 1 || views[0].SeriesCount != 2 {
		t.Fatalf("cross-library projection=%+v, %v", views, err)
	}
}

func TestFranchiseFailureKeepsOldProjectionAndHonorsBackoffAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "repair.db")
	open := func() database.Store {
		t.Helper()
		if err := database.Migrate(path); err != nil {
			t.Fatal(err)
		}
		store, err := database.NewStore(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		return store
	}
	store := open()
	db := store.(*database.SqlStore).DB()
	if _, err := db.Exec(`INSERT INTO libraries(id,name,path) VALUES(1,'Library','lib'); INSERT INTO series(id,library_id,name,path) VALUES(1,1,'Alpha','a'),(2,1,'Beta','b'),(3,1,'Gamma','c')`); err != nil {
		t.Fatal(err)
	}
	service := organize.NewRelations(store, func(func()) {})
	ctx := context.Background()
	if _, err := service.Create(ctx, 1, 2, "sequel"); err != nil {
		t.Fatal(err)
	}
	if err := service.Rebuild(ctx); err != nil {
		t.Fatal(err)
	}
	views, err := organize.NewSmartCollections(store).Views(ctx, readerSubject(t, 0))
	if err != nil || len(views) != 1 {
		t.Fatalf("views=%+v, %v", views, err)
	}
	id := views[0].NumericID
	if _, err := service.Create(ctx, 2, 3, "sequel"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER reject_member BEFORE INSERT ON collection_series WHEN NEW.series_id=3 BEGIN SELECT RAISE(ABORT,'member failed'); END`); err != nil {
		t.Fatal(err)
	}
	if err := service.Rebuild(ctx); err == nil {
		t.Fatal("expected rebuild failure")
	}
	failed, err := service.FranchiseState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !failed.Available || !failed.Stale || failed.Status != "failed" || failed.NextRetryAtMS <= time.Now().UnixMilli() {
		t.Fatalf("failure state=%+v", failed)
	}
	if ran, err := service.RepairDue(ctx, time.Now()); ran || err != nil {
		t.Fatalf("backoff ignored: %v %v", ran, err)
	}
	old, err := organize.NewCollections(store).Members(ctx, id)
	if err != nil || len(old) != 2 {
		t.Fatalf("previous projection changed: %+v %v", old, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = open()
	recovered := organize.NewRelations(store, func(func()) {})
	persisted, err := recovered.FranchiseState(ctx)
	if err != nil || persisted != failed {
		t.Fatalf("reopen lost state=%+v, %v", persisted, err)
	}
	if _, err := store.(*database.SqlStore).DB().Exec(`DROP TRIGGER reject_member`); err != nil {
		t.Fatal(err)
	}
	if ran, err := recovered.RepairDue(ctx, time.UnixMilli(failed.NextRetryAtMS)); !ran || err != nil {
		t.Fatalf("due repair=%v %v", ran, err)
	}
	repaired, err := recovered.FranchiseState(ctx)
	if err != nil || repaired.Stale || !repaired.Available || repaired.LastError != "" {
		t.Fatalf("repair=%+v %v", repaired, err)
	}
	members, err := organize.NewCollections(store).Members(ctx, id)
	if err != nil || len(members) != 3 {
		t.Fatalf("stable projection=%+v %v", members, err)
	}
}

func TestRelationWriteRollsBackWhenDurableMarkFails(t *testing.T) {
	store, _ := newCollections(t)
	db := store.(*database.SqlStore).DB()
	if _, err := db.Exec(`INSERT INTO libraries(id,name,path) VALUES(1,'Library','lib'); INSERT INTO series(id,library_id,name,path) VALUES(1,1,'Alpha','a'),(2,1,'Beta','b'); CREATE TRIGGER reject_dirty BEFORE UPDATE ON franchise_rebuild_state BEGIN SELECT RAISE(ABORT,'dirty failed'); END`); err != nil {
		t.Fatal(err)
	}
	service := organize.NewRelations(store, func(func()) {})
	if _, err := service.Create(context.Background(), 1, 2, "sequel"); err == nil {
		t.Fatal("relation committed without repair responsibility")
	}
	edges, err := service.List(context.Background(), 1)
	if err != nil || len(edges) != 0 {
		t.Fatalf("relation rollback=%+v %v", edges, err)
	}
}

type delayedFranchiseFailure struct {
	database.Store
	reached, proceed chan struct{}
}

func (s delayedFranchiseFailure) RecordFranchiseRebuildFailure(ctx context.Context, arg database.RecordFranchiseRebuildFailureParams) (int64, error) {
	close(s.reached)
	select {
	case <-s.proceed:
	case <-ctx.Done():
		return 0, ctx.Err()
	}
	return s.Store.RecordFranchiseRebuildFailure(ctx, arg)
}

func TestOldFranchiseFailureCannotReplaceNewerSuccess(t *testing.T) {
	store, _ := newCollections(t)
	db := store.(*database.SqlStore).DB()
	if _, err := db.Exec(`INSERT INTO libraries(id,name,path) VALUES(1,'Library','lib'); INSERT INTO series(id,library_id,name,path) VALUES(1,1,'Alpha','a'),(2,1,'Beta','b'); CREATE TRIGGER reject_member BEFORE INSERT ON collection_series BEGIN SELECT RAISE(ABORT,'member failed'); END`); err != nil {
		t.Fatal(err)
	}
	base := organize.NewRelations(store, func(func()) {})
	if _, err := base.Create(context.Background(), 1, 2, "sequel"); err != nil {
		t.Fatal(err)
	}
	barrier := delayedFranchiseFailure{Store: store, reached: make(chan struct{}), proceed: make(chan struct{})}
	waiting := organize.NewRelations(barrier, func(func()) {})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- waiting.Rebuild(ctx) }()
	select {
	case <-barrier.reached:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, err := db.Exec(`DROP TRIGGER reject_member`); err != nil {
		close(barrier.proceed)
		t.Fatal(err)
	}
	if err := base.Rebuild(ctx); err != nil {
		close(barrier.proceed)
		t.Fatal(err)
	}
	close(barrier.proceed)
	if err := <-done; err == nil {
		t.Fatal("old failure disappeared")
	}
	state, err := base.FranchiseState(ctx)
	if err != nil || state.Stale || state.LastError != "" || state.Status != "current" {
		t.Fatalf("old failure replaced success: %+v %v", state, err)
	}
}

type afterFranchiseCommit struct {
	database.Store
	reached, proceed chan struct{}
}

func (s afterFranchiseCommit) ExecTx(ctx context.Context, fn func(*database.Queries) error) error {
	err := s.Store.ExecTx(ctx, fn)
	close(s.reached)
	select {
	case <-s.proceed:
	case <-ctx.Done():
		return ctx.Err()
	}
	return err
}

func TestNewRelationChangeCannotBeClearedByOldRebuildCompletion(t *testing.T) {
	store, _ := newCollections(t)
	if _, err := store.(*database.SqlStore).DB().Exec(`INSERT INTO libraries(id,name,path) VALUES(1,'Library','lib'); INSERT INTO series(id,library_id,name,path) VALUES(1,1,'Alpha','a'),(2,1,'Beta','b'),(3,1,'Gamma','c')`); err != nil {
		t.Fatal(err)
	}
	base := organize.NewRelations(store, func(func()) {})
	if _, err := base.Create(context.Background(), 1, 2, "sequel"); err != nil {
		t.Fatal(err)
	}
	barrier := afterFranchiseCommit{Store: store, reached: make(chan struct{}), proceed: make(chan struct{})}
	waiting := organize.NewRelations(barrier, func(func()) {})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- waiting.Rebuild(ctx) }()
	select {
	case <-barrier.reached:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, err := base.Create(ctx, 2, 3, "sequel"); err != nil {
		close(barrier.proceed)
		t.Fatal(err)
	}
	close(barrier.proceed)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	state, err := base.FranchiseState(ctx)
	if err != nil || !state.Stale || state.DesiredVersion <= state.AppliedVersion {
		t.Fatalf("new change lost: %+v %v", state, err)
	}
	if ran, err := base.RepairDue(ctx, time.Now()); !ran || err != nil {
		t.Fatalf("repair=%v %v", ran, err)
	}
}

func TestFranchiseUpgradePreservesExistingResultAsStale(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.db")
	if err := database.Migrate(path); err != nil {
		t.Fatal(err)
	}
	store, err := database.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`INSERT INTO collections(id,name,source_type,source_key) VALUES(42,'Old result','system_franchise','1'); DROP TABLE franchise_rebuild_state`); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(path); err != nil {
		t.Fatal(err)
	}
	store, err = database.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := organize.NewRelations(store, func(func()) {})
	state, err := service.FranchiseState(context.Background())
	if err != nil || !state.Available || !state.Stale {
		t.Fatalf("upgrade=%+v %v", state, err)
	}
	collection, err := organize.NewCollections(store).Get(context.Background(), 42)
	if err != nil || collection.Name != "Old result" {
		t.Fatalf("upgrade destroyed result: %+v %v", collection, err)
	}
}

func TestFranchiseCompletionMustCommitWithProjection(t *testing.T) {
	store, _ := newCollections(t)
	if _, err := store.DB().Exec(`INSERT INTO libraries(id,name,path) VALUES(1,'Library','lib'); INSERT INTO series(id,library_id,name,path) VALUES(1,1,'Alpha','a'),(2,1,'Beta','b'); CREATE TRIGGER ignore_completion BEFORE UPDATE ON franchise_rebuild_state WHEN NEW.applied_version > OLD.applied_version BEGIN SELECT RAISE(IGNORE); END`); err != nil {
		t.Fatal(err)
	}
	service := organize.NewRelations(store, func(func()) {})
	if _, err := service.Create(context.Background(), 1, 2, "sequel"); err != nil {
		t.Fatal(err)
	}
	if err := service.Rebuild(context.Background()); err == nil {
		t.Fatal("projection reported success without recording completion")
	}
	views, err := organize.NewSmartCollections(store).Views(context.Background(), readerSubject(t, 0))
	if err != nil || len(views) != 0 {
		t.Fatalf("projection committed without completion: %+v %v", views, err)
	}
}
