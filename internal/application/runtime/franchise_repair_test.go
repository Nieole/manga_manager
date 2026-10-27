package runtime_test

import (
	"context"
	"testing"
	"time"

	"manga-manager/internal/application/runtime"
	"manga-manager/internal/organize"
	"manga-manager/internal/reading"
)

func TestRuntimeResumesDurableFranchiseRepairAndRetriesFailure(t *testing.T) {
	store, cfg := runtimeFixture(t)
	if _, err := store.DB().Exec(`INSERT INTO libraries(id,name,path) VALUES(1,'Library','lib'); INSERT INTO series(id,library_id,name,path) VALUES(1,1,'Alpha','a'),(2,1,'Beta','b'),(3,1,'Gamma','c')`); err != nil {
		t.Fatal(err)
	}
	disconnected := organize.NewRelations(store, func(func()) {})
	if _, err := disconnected.Create(context.Background(), 1, 2, "sequel"); err != nil {
		t.Fatal(err)
	}
	app, err := runtime.New(store, cfg, runtime.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	if err := app.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	await := func(status string) {
		t.Helper()
		deadline := time.NewTimer(10 * time.Second)
		defer deadline.Stop()
		poll := time.NewTicker(10 * time.Millisecond)
		defer poll.Stop()
		for {
			state, err := app.Relations.FranchiseState(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if state.Status == status {
				return
			}
			select {
			case <-deadline.C:
				t.Fatalf("wanted %s; state=%+v", status, state)
			case <-poll.C:
			}
		}
	}
	await("current")
	if _, err := store.DB().Exec(`CREATE TRIGGER reject_member BEFORE INSERT ON collection_series WHEN NEW.series_id=3 BEGIN SELECT RAISE(ABORT,'member failed'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Relations.Create(context.Background(), 2, 3, "sequel"); err != nil {
		t.Fatal(err)
	}
	await("failed")
	if _, err := store.DB().Exec(`DROP TRIGGER reject_member`); err != nil {
		t.Fatal(err)
	}
	await("current")
	views, err := organize.NewSmartCollections(store).Views(context.Background(), reading.AnonymousSubject())
	if err != nil || len(views) != 1 || views[0].SeriesCount != 3 {
		t.Fatalf("runtime projection=%+v %v", views, err)
	}
}
