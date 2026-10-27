package organize_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"manga-manager/internal/application/seriesediting"
	"manga-manager/internal/database"
	"manga-manager/internal/organize"
	"manga-manager/internal/seriesmeta"
)

func newFranchiseFacts(t *testing.T) (database.Store, *organize.Relations, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "facts.db")
	if err := database.Migrate(path); err != nil {
		t.Fatal(err)
	}
	store, err := database.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err := store.DB().Exec(`INSERT INTO libraries(id,name,path) VALUES(1,'First','first'),(2,'Second','second'),(3,'Other','other'); INSERT INTO series(id,library_id,name,path) VALUES(1,1,'Alpha','a'),(2,1,'Beta','b'),(3,2,'Gamma','c'),(4,3,'Delta','d'),(5,3,'Epsilon','e'),(6,1,'Isolated','f')`); err != nil {
		t.Fatal(err)
	}
	service := organize.NewRelations(store, func(func()) {})
	for _, pair := range [][2]int64{{1, 2}, {2, 3}, {4, 5}} {
		if _, err := service.Create(context.Background(), pair[0], pair[1], "sequel"); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.Rebuild(context.Background()); err != nil {
		t.Fatal(err)
	}
	return store, service, path
}

func TestFranchiseSeriesDeletionPersistsRepairResponsibility(t *testing.T) {
	store, service, _ := newFranchiseFacts(t)
	before, err := service.FranchiseState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteSeries(context.Background(), 3); err != nil {
		t.Fatal(err)
	}
	after, err := service.FranchiseState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !after.Stale || after.DesiredVersion <= before.DesiredVersion {
		t.Fatalf("series cascade lost repair responsibility: before=%+v after=%+v", before, after)
	}
}

func TestManualTitleEditPersistsFranchiseFactChange(t *testing.T) {
	store, service, _ := newFranchiseFacts(t)
	before, err := service.FranchiseState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	edited, err := seriesediting.New(seriesediting.NewSQLStore(store)).Save(context.Background(), 1, seriesmeta.ManualInput{Title: "Edited title"})
	if err != nil || edited.Series.Title.String != "Edited title" {
		t.Fatalf("manual edit=%+v %v", edited, err)
	}
	after, err := service.FranchiseState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !after.Stale || after.DesiredVersion <= before.DesiredVersion {
		t.Fatalf("manual edit lost repair responsibility: before=%+v after=%+v", before, after)
	}
}

func TestFranchiseCascadeSourcesRecoverAfterReopenWithStableIdentity(t *testing.T) {
	for _, source := range []string{"series", "library", "missing-series"} {
		t.Run(source, func(t *testing.T) {
			store, service, path := newFranchiseFacts(t)
			ctx := context.Background()
			before, err := organize.NewSmartCollections(store).Views(ctx, 0)
			if err != nil {
				t.Fatal(err)
			}
			ids := map[string]int64{}
			for _, view := range before {
				ids[view.Name] = view.NumericID
			}
			var deleteErr error
			switch source {
			case "series":
				deleteErr = store.DeleteSeries(ctx, 3)
			case "library":
				deleteErr = store.DeleteLibrary(ctx, 2)
			case "missing-series":
				deleteErr = store.DeleteMissingSeries(ctx, 3)
			}
			if deleteErr != nil {
				t.Fatal(deleteErr)
			}
			pending, err := service.FranchiseState(ctx)
			if err != nil || !pending.Stale {
				t.Fatalf("cascade state=%+v %v", pending, err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			if err := database.Migrate(path); err != nil {
				t.Fatal(err)
			}
			reopened, err := database.NewStore(path)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			recovered := organize.NewRelations(reopened, func(func()) {})
			if ran, err := recovered.RepairDue(ctx, time.Now()); !ran || err != nil {
				t.Fatalf("reopen repair=%v %v", ran, err)
			}
			after, err := organize.NewSmartCollections(reopened).Views(ctx, 0)
			if err != nil || len(after) != 2 {
				t.Fatalf("views=%+v %v", after, err)
			}
			for _, view := range after {
				if ids[view.Name] != view.NumericID || view.SeriesCount != 2 {
					t.Fatalf("stable projection=%+v", view)
				}
			}
			if _, err := reopened.GetSeries(ctx, 3); !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("deleted series returned: %v", err)
			}
		})
	}
}

func TestFranchiseFactAndCascadeRollbackWhenDirtyWriteFails(t *testing.T) {
	for _, source := range []string{"manual", "series", "library"} {
		t.Run(source, func(t *testing.T) {
			store, service, _ := newFranchiseFacts(t)
			ctx := context.Background()
			before, err := service.FranchiseState(ctx)
			if err != nil {
				t.Fatal(err)
			}
			original, err := store.GetSeries(ctx, 1)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.DB().Exec(`CREATE TRIGGER reject_franchise_mark BEFORE UPDATE ON franchise_rebuild_state BEGIN SELECT RAISE(ABORT,'dirty write failed'); END`); err != nil {
				t.Fatal(err)
			}
			var writeErr error
			switch source {
			case "manual":
				_, writeErr = seriesediting.New(seriesediting.NewSQLStore(store)).Save(ctx, 1, seriesmeta.ManualInput{Title: "Never committed"})
			case "series":
				writeErr = store.DeleteSeries(ctx, 1)
			case "library":
				writeErr = store.DeleteLibrary(ctx, 1)
			}
			if writeErr == nil {
				t.Fatal("fact committed without dirty responsibility")
			}
			after, err := service.FranchiseState(ctx)
			if err != nil || after != before {
				t.Fatalf("failed dirty state=%+v %v", after, err)
			}
			saved, err := store.GetSeries(ctx, 1)
			if err != nil || saved.Title != original.Title {
				t.Fatalf("fact rollback=%+v %v", saved, err)
			}
			edges, err := service.List(ctx, 1)
			if err != nil || len(edges) != 1 {
				t.Fatalf("cascade rollback=%+v %v", edges, err)
			}
		})
	}
}

func TestUnrelatedOrUnchangedSeriesFactsDoNotInvalidateFranchises(t *testing.T) {
	store, service, _ := newFranchiseFacts(t)
	ctx := context.Background()
	before, err := service.FranchiseState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	editor := seriesediting.New(seriesediting.NewSQLStore(store))
	if _, err := editor.Save(ctx, 6, seriesmeta.ManualInput{Title: "Unrelated title"}); err != nil {
		t.Fatal(err)
	}
	if _, err := editor.Save(ctx, 1, seriesmeta.ManualInput{Summary: "Updated summary", Status: "ongoing", Publisher: "Changed publisher"}); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteSeries(ctx, 6); err != nil {
		t.Fatal(err)
	}
	after, err := service.FranchiseState(ctx)
	if err != nil || after != before {
		t.Fatalf("unrelated edits dirtied projection: before=%+v after=%+v %v", before, after, err)
	}
}

func TestOldFranchiseFailureCannotOverrideNewCascadeRepair(t *testing.T) {
	store, base, _ := newFranchiseFacts(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := seriesediting.New(seriesediting.NewSQLStore(store)).Save(ctx, 1, seriesmeta.ManualInput{Title: "New title"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`CREATE TRIGGER reject_projection BEFORE UPDATE ON collections BEGIN SELECT RAISE(ABORT,'projection failed'); END`); err != nil {
		t.Fatal(err)
	}
	barrier := delayedFranchiseFailure{Store: store, reached: make(chan struct{}), proceed: make(chan struct{})}
	waiting := organize.NewRelations(barrier, func(func()) {})
	done := make(chan error, 1)
	go func() { done <- waiting.Rebuild(ctx) }()
	select {
	case <-barrier.reached:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := store.DeleteLibrary(ctx, 2); err != nil {
		close(barrier.proceed)
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`DROP TRIGGER reject_projection`); err != nil {
		close(barrier.proceed)
		t.Fatal(err)
	}
	if err := base.Rebuild(ctx); err != nil {
		close(barrier.proceed)
		t.Fatal(err)
	}
	close(barrier.proceed)
	if err := <-done; err == nil {
		t.Fatal("expected the earlier projection failure")
	}
	state, err := base.FranchiseState(ctx)
	if err != nil || state.Stale || state.LastError != "" {
		t.Fatalf("late failure replaced cascade repair: %+v %v", state, err)
	}
}

func TestNameChangeAfterOldRebuildCommitRemainsPending(t *testing.T) {
	store, base, _ := newFranchiseFacts(t)
	before, err := organize.NewSmartCollections(store).Views(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	var alphaID int64
	for _, view := range before {
		if view.Name == "Alpha Franchise" {
			alphaID = view.NumericID
		}
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
	// The same SQLite series-name fact is written by ingestion upserts.
	if _, err := store.DB().ExecContext(ctx, `UPDATE series SET name = 'Renamed Alpha' WHERE id = 1`); err != nil {
		close(barrier.proceed)
		t.Fatal(err)
	}
	close(barrier.proceed)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	state, err := base.FranchiseState(ctx)
	if err != nil || !state.Stale {
		t.Fatalf("new name disappeared behind old completion: %+v %v", state, err)
	}
	if _, err := base.RepairDue(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	renamed, err := organize.NewCollections(store).Get(ctx, alphaID)
	if err != nil || renamed.Name != "Renamed Alpha Franchise" {
		t.Fatalf("name repair lost identity: %+v %v", renamed, err)
	}
}

func TestInstallingFranchiseFactCoverageRepairsPreviouslyUnmarkedCascade(t *testing.T) {
	store, service, path := newFranchiseFacts(t)
	ctx := context.Background()
	if _, err := store.DB().Exec(`DROP TRIGGER trg_series_bd_franchise_pending; DROP TRIGGER trg_series_au_franchise_pending`); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteLibrary(ctx, 2); err != nil {
		t.Fatal(err)
	}
	before, err := service.FranchiseState(ctx)
	if err != nil || before.Stale {
		t.Fatalf("pre-coverage fixture=%+v %v", before, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(path); err != nil {
		t.Fatal(err)
	}
	reopened, err := database.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	recovered := organize.NewRelations(reopened, func(func()) {})
	pending, err := recovered.FranchiseState(ctx)
	if err != nil || !pending.Stale || pending.DesiredVersion <= before.DesiredVersion {
		t.Fatalf("first coverage installation trusted old completion: before=%+v after=%+v %v", before, pending, err)
	}
	if err := database.Migrate(path); err != nil {
		t.Fatal(err)
	}
	repeated, err := recovered.FranchiseState(ctx)
	if err != nil || repeated != pending {
		t.Fatalf("repeated migration dirtied again: %+v %v", repeated, err)
	}
	if ran, err := recovered.RepairDue(ctx, time.Now()); !ran || err != nil {
		t.Fatalf("upgrade repair=%v %v", ran, err)
	}
	views, err := organize.NewSmartCollections(reopened).Views(ctx, 0)
	if err != nil || len(views) != 2 {
		t.Fatalf("repaired upgrade=%+v %v", views, err)
	}
	for _, view := range views {
		if view.SeriesCount != 2 {
			t.Fatalf("old cascade not reconciled: %+v", view)
		}
	}
}
