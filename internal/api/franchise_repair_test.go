package api

import (
	"context"
	"encoding/json"
	"manga-manager/internal/seriescontext"
	"net/http"
	"net/http/httptest"
	"testing"

	"manga-manager/internal/organize"
)

func TestFranchiseStateHTTPSeparatesUnavailableStaleFailedAndRepaired(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	controller.relations = organize.NewRelations(store, func(func()) {})
	controller.organizationHTTP.relations = controller.relations
	controller.reader.context = seriescontext.New(store, controller.relations, controller.proposals, controller.taskEngine.engine)
	read := func() organize.FranchiseState {
		t.Helper()
		rec := httptest.NewRecorder()
		controller.organizationHTTP.getFranchiseState(rec, httptest.NewRequest(http.MethodGet, "/api/franchise-state", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("state HTTP%d: %s", rec.Code, rec.Body.String())
		}
		var state organize.FranchiseState
		if err := json.Unmarshal(rec.Body.Bytes(), &state); err != nil {
			t.Fatal(err)
		}
		return state
	}
	if initial := read(); initial.Available || !initial.Stale {
		t.Fatalf("first result=%+v", initial)
	}
	_, ids := seedFranchiseSeries(t, store, 3)
	if _, err := controller.relations.Create(context.Background(), ids[0], ids[1], "sequel"); err != nil {
		t.Fatal(err)
	}
	if err := controller.relations.Rebuild(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := read()
	if !before.Available || before.Stale {
		t.Fatalf("rebuilt=%+v", before)
	}
	if _, err := controller.relations.Create(context.Background(), ids[1], ids[2], "sequel"); err != nil {
		t.Fatal(err)
	}
	if pending := read(); !pending.Available || !pending.Stale || pending.Status != "pending" {
		t.Fatalf("pending=%+v", pending)
	}
	if _, err := store.DB().Exec(`CREATE TRIGGER reject_member BEFORE INSERT ON collection_series BEGIN SELECT RAISE(ABORT,'member failed'); END`); err != nil {
		t.Fatal(err)
	}
	if err := controller.relations.Rebuild(context.Background()); err == nil {
		t.Fatal("expected failure")
	}
	if failed := read(); failed.Status != "failed" || !failed.Stale || !failed.Available {
		t.Fatalf("failed=%+v", failed)
	}
	if _, err := store.DB().Exec(`DROP TRIGGER reject_member`); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	controller.organizationHTTP.rebuildFranchiseCollectionsHandler(rec, httptest.NewRequest(http.MethodPost, "/api/system/rebuild-franchises", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("retry HTTP%d: %s", rec.Code, rec.Body.String())
	}
	if repaired := read(); repaired.Stale || repaired.Status != "current" || !repaired.Available {
		t.Fatalf("retry=%+v", repaired)
	}
}
