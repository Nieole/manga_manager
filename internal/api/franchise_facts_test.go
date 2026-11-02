package api

import (
	"context"
	"manga-manager/internal/seriescontext"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/metadata"
	"manga-manager/internal/organize"
	"manga-manager/internal/proposal"
)

func TestFranchiseFactCoverageUsesMetadataHTTPAndProposalUseCases(t *testing.T) {
	c, store, _, _ := newTestController(t)
	_, ids := seedFranchiseSeries(t, store, 2)
	c.relations = organize.NewRelations(store, func(func()) {})
	c.organizationHTTP.relations = c.relations
	c.reader.context = seriescontext.New(store, c.relations, c.proposals, c.taskEngine.engine)
	ctx := context.Background()
	if _, err := c.relations.Create(ctx, ids[0], ids[1], "sequel"); err != nil {
		t.Fatal(err)
	}
	if err := c.relations.Rebuild(ctx); err != nil {
		t.Fatal(err)
	}
	before, err := c.relations.FranchiseState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if code, body := putSeriesInfo(t, c, ids[0], `{"title":"HTTP edited title","locked_fields":""}`); code != http.StatusOK {
		t.Fatalf("manual HTTP%d %+v", code, body)
	}
	manual, err := c.relations.FranchiseState(ctx)
	if err != nil || !manual.Stale || manual.DesiredVersion <= before.DesiredVersion {
		t.Fatalf("manual responsibility=%+v %v", manual, err)
	}
	if err := c.relations.Rebuild(ctx); err != nil {
		t.Fatal(err)
	}
	current, err := store.GetSeries(ctx, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	queued, err := c.proposals.Queue(ctx, current, &metadata.SeriesMetadata{Provider: "bangumi", Title: "Proposal edited title"}, "bangumi", current.Name, proposal.QueueOptions{})
	if err != nil || queued.Status != proposal.QueueQueued {
		t.Fatalf("proposal=%+v %v", queued, err)
	}
	rec := httptest.NewRecorder()
	c.organizationHTTP.applyMetadataReview(rec, requestWithRouteParam(http.MethodPost, "/api/metadata/reviews/x/apply", nil, "reviewId", strconv.FormatInt(queued.Proposal.ID, 10)))
	if rec.Code != http.StatusOK {
		t.Fatalf("proposal HTTP%d %s", rec.Code, rec.Body.String())
	}
	applied, err := c.relations.FranchiseState(ctx)
	if err != nil || !applied.Stale || applied.DesiredVersion <= manual.DesiredVersion {
		t.Fatalf("proposal responsibility=%+v %v", applied, err)
	}
}

func TestLibraryDeletionHTTPCascadesToDurableFranchiseRepair(t *testing.T) {
	c, store, _, _ := newTestController(t)
	_, ids := seedFranchiseSeries(t, store, 3)
	ctx := context.Background()
	other, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Other library", Path: t.TempDir(), ScanMode: "none", ScanFormats: "cbz", ScanInterval: 60})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE series SET library_id = ? WHERE id = ?`, other.ID, ids[2]); err != nil {
		t.Fatal(err)
	}
	c.relations = organize.NewRelations(store, func(func()) {})
	c.organizationHTTP.relations = c.relations
	c.reader.context = seriescontext.New(store, c.relations, c.proposals, c.taskEngine.engine)
	for _, pair := range [][2]int64{{ids[0], ids[1]}, {ids[1], ids[2]}} {
		if _, err := c.relations.Create(ctx, pair[0], pair[1], "sequel"); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.relations.Rebuild(ctx); err != nil {
		t.Fatal(err)
	}
	before := readFranchises(t, store)
	if len(before) != 1 {
		t.Fatalf("before=%+v", before)
	}
	rec := httptest.NewRecorder()
	c.families.library.deleteLibrary(rec, requestWithRouteParam(http.MethodDelete, "/api/libraries/x", nil, "libraryId", strconv.FormatInt(other.ID, 10)))
	if rec.Code != http.StatusOK {
		t.Fatalf("delete HTTP%d %s", rec.Code, rec.Body.String())
	}
	state, err := c.relations.FranchiseState(ctx)
	if err != nil || !state.Stale {
		t.Fatalf("delete responsibility=%+v %v", state, err)
	}
	if ran, err := c.relations.RepairDue(ctx, time.Now()); !ran || err != nil {
		t.Fatalf("repair=%v %v", ran, err)
	}
	after := readFranchises(t, store)
	if len(after) != 1 || after[0].ID != before[0].ID || len(after[0].Members) != 2 {
		t.Fatalf("cross-library identity=%+v", after)
	}
}
