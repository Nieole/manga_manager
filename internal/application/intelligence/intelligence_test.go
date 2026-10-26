package intelligence_test

import (
	"context"
	"errors"
	"manga-manager/internal/application"
	"manga-manager/internal/application/intelligence"
	"manga-manager/internal/database"
	"manga-manager/internal/logger"
	"manga-manager/internal/metadata"
	"manga-manager/internal/organize"
	"manga-manager/internal/proposal"
	"manga-manager/internal/runhandle"
	"manga-manager/internal/task"
	"manga-manager/internal/taskstore"
	"path/filepath"
	"strconv"
	"testing"
)

type proposalDB struct{ database.Store }

func (d proposalDB) ExecTx(ctx context.Context, fn func(proposal.Queries) error) error {
	return d.Store.ExecTx(ctx, func(q *database.Queries) error { return fn(q) })
}

type source struct {
	locales []string
	runIDs  []int64
	keys    []string
}

func (p *source) Name() string { return "Controlled" }
func (p *source) FetchSeriesMetadata(ctx context.Context, _ string) (*metadata.SeriesMetadata, error) {
	p.locales = append(p.locales, metadata.LocaleFromContext(ctx))
	p.runIDs = append(p.runIDs, logger.RunIDFrom(ctx))
	p.keys = append(p.keys, logger.TaskKeyFrom(ctx))
	return &metadata.SeriesMetadata{Title: "Suggested", Summary: "Controlled summary", Confidence: 1}, nil
}
func (p *source) SearchMetadata(context.Context, string, int, int) ([]*metadata.SeriesMetadata, int, error) {
	return nil, 0, nil
}
func TestScrapeLibraryQueuesProposalWithOriginalSourceAndLocale(t *testing.T) {
	ctx := metadata.WithLocale(context.Background(), "en-US")
	path := filepath.Join(t.TempDir(), "library.db")
	if err := database.Migrate(path); err != nil {
		t.Fatal(err)
	}
	store, err := database.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Library", Path: t.TempDir(), ScanMode: "none", ScanFormats: "cbz", ScanInterval: 60})
	if err != nil {
		t.Fatal(err)
	}
	series, err := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib.ID, Name: "Original", Path: filepath.Join(lib.Path, "Series")})
	if err != nil {
		t.Fatal(err)
	}
	tasks := taskstore.New(store.(*database.SqlStore).DB())
	engine := task.New(task.Config{Store: tasks, RunBackground: func(fn func()) { fn() }, DecorateRunContext: application.DecorateRunContext})
	provider := &source{}
	var requestedKeys []string
	service := intelligence.New(application.NewRuns(engine, nil), store, proposal.NewService(proposalDB{store}), organize.NewAIGrouping(store, nil), intelligence.Sources{Metadata: func(key string) metadata.Provider { requestedKeys = append(requestedKeys, key); return provider }}, nil)
	launch, err := service.ScrapeLibrary(ctx, lib.ID, "controlled", task.TriggerManual)
	if err != nil {
		t.Fatal(err)
	}
	run, err := tasks.LoadRun(ctx, launch.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	sides, err := tasks.LoadRunSideData(ctx, []int64{run.ID})
	if err != nil {
		t.Fatal(err)
	}
	side := sides[run.ID]
	reviews, err := store.ListPendingMetadataReviewsBySeries(ctx, series.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != task.StatusCompleted || len(reviews) != 1 || side.Args["provider"] != "controlled" || side.Args["locale"] != "en-US" || len(provider.locales) != 1 || provider.locales[0] != "en-US" {
		t.Fatalf("run=%+v reviews=%+v locales=%v", run, reviews, provider.locales)
	}
	replay, err := service.Restart(metadata.WithLocale(context.Background(), "zh-CN"), task.Identity{Type: "scrape", Scope: task.ScopeLibrary, ScopeID: lib.ID, Variant: "one_library"}, side.Args, task.TriggerManual)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := tasks.LoadRun(ctx, replay.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if replay.Run.ID == run.ID || replayed.Status != task.StatusCompleted || len(provider.locales) != 2 || provider.locales[1] != "en-US" {
		t.Fatalf("retry=%+v locales=%v", replayed, provider.locales)
	}

	if len(requestedKeys) != 2 || requestedKeys[0] != "controlled" || requestedKeys[1] != "controlled" || provider.runIDs[0] != run.ID || provider.runIDs[1] != replayed.ID || provider.keys[0] != "scrape_library_"+strconv.FormatInt(lib.ID, 10) {
		t.Fatalf("keys=%v runIDs=%v taskkeys=%v", requestedKeys, provider.runIDs, provider.keys)
	}
	release, err := engine.AcquireLibraryExclusive(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.ScrapeAll(ctx, "controlled", task.TriggerManual); !errors.Is(err, task.ErrLibraryUnavailable) {
		t.Fatalf("system scrape bypassed exclusive: %v", err)
	}
	release()

	for _, identity := range []task.Identity{{Type: "scrape", Scope: task.ScopeSystem, Variant: "all_libraries"}, {Type: "scrape", Scope: task.ScopeLibrary, ScopeID: lib.ID, Variant: "one_library"}} {
		automatic, err := service.Restart(ctx, identity, side.Args, task.TriggerResumed)
		if !errors.Is(err, intelligence.ErrAutomaticResumeUnsupported) || automatic.Run.ID != 0 || len(provider.locales) != 2 {
			t.Fatalf("automatic=%+v err=%v locales=%v", automatic, err, provider.locales)
		}
	}

}

func (p *source) GenerateGrouping(ctx context.Context, candidates []metadata.CandidateSeries) ([]metadata.AIGroupCollection, error) {
	p.locales = append(p.locales, metadata.LocaleFromContext(ctx))
	p.runIDs = append(p.runIDs, logger.RunIDFrom(ctx))
	p.keys = append(p.keys, logger.TaskKeyFrom(ctx))
	return []metadata.AIGroupCollection{{Name: "Suggested shelf", Description: "Review this grouping", SeriesIDs: []int64{candidates[0].ID, candidates[0].ID, 99999}}}, nil
}
func TestGroupingQueuesIndependentReviewAndReplaysLocale(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "library.db")
	if err := database.Migrate(path); err != nil {
		t.Fatal(err)
	}
	store, err := database.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Library", Path: t.TempDir(), ScanMode: "none", ScanFormats: "cbz", ScanInterval: 60})
	if err != nil {
		t.Fatal(err)
	}
	series, err := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib.ID, Name: "Original", Path: filepath.Join(lib.Path, "Series")})
	if err != nil {
		t.Fatal(err)
	}
	tasks := taskstore.New(store.(*database.SqlStore).DB())
	engine := task.New(task.Config{Store: tasks, RunBackground: func(fn func()) { fn() }, DecorateRunContext: application.DecorateRunContext})
	provider := &source{}
	groups := organize.NewAIGrouping(store, nil)
	service := intelligence.New(application.NewRuns(engine, nil), store, proposal.NewService(proposalDB{store}), groups, intelligence.Sources{Grouping: func() intelligence.GroupingProvider { return provider }}, nil)
	launch, err := service.GroupLibrary(ctx, lib.ID, "en-US", task.TriggerManual)
	if err != nil {
		t.Fatal(err)
	}
	run, err := tasks.LoadRun(ctx, launch.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	reviewID, err := strconv.ParseInt(run.MessageParams["reviewId"], 10, 64)
	if err != nil {
		t.Fatalf("run=%+v err=%v", run, err)
	}
	review, err := groups.Review(ctx, reviewID)
	if err != nil {
		t.Fatal(err)
	}
	collections, err := store.ListCollectionsWithSeriesCount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	proposals, err := store.ListPendingMetadataReviewsBySeries(ctx, series.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != task.StatusCompleted || review.Review.Status != "pending" || len(review.Collections) != 1 || review.Collections[0].SeriesIds != "["+strconv.FormatInt(series.ID, 10)+"]" || len(collections) != 0 || len(proposals) != 0 {
		t.Fatalf("run=%+v review=%+v collections=%+v proposals=%+v", run, review, collections, proposals)
	}
	sides, err := tasks.LoadRunSideData(ctx, []int64{run.ID})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := service.Restart(metadata.WithLocale(ctx, "zh-CN"), task.Identity{Type: "ai_grouping", Scope: task.ScopeLibrary, ScopeID: lib.ID}, sides[run.ID].Args, task.TriggerManual)
	if err != nil {
		t.Fatal(err)
	}
	if replay.Run.ID == run.ID || len(provider.locales) != 2 || provider.locales[1] != "en-US" {
		t.Fatalf("retry=%+v locales=%v", replay, provider.locales)
	}
	automatic, err := service.Restart(ctx, task.Identity{Type: "ai_grouping", Scope: task.ScopeLibrary, ScopeID: lib.ID}, sides[run.ID].Args, task.TriggerResumed)
	if !errors.Is(err, intelligence.ErrAutomaticResumeUnsupported) || automatic.Run.ID != 0 || len(provider.locales) != 2 {
		t.Fatalf("automatic=%+v err=%v locales=%v", automatic, err, provider.locales)
	}

}

func TestQueuedGenerationCancellationReleasesCoverageWithoutCallingSource(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "library.db")
	if err := database.Migrate(path); err != nil {
		t.Fatal(err)
	}
	store, err := database.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Library", Path: t.TempDir(), ScanMode: "none", ScanFormats: "cbz", ScanInterval: 60})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib.ID, Name: "Original", Path: filepath.Join(lib.Path, "Series")})
	if err != nil {
		t.Fatal(err)
	}
	tasks := taskstore.New(store.(*database.SqlStore).DB())
	var bodies []func()
	engine := task.New(task.Config{Store: tasks, Slots: func() int { return 1 }, RunBackground: func(fn func()) { bodies = append(bodies, fn) }})
	provider := &source{}
	service := intelligence.New(application.NewRuns(engine, nil), store, proposal.NewService(proposalDB{store}), organize.NewAIGrouping(store, nil), intelligence.Sources{Metadata: func(string) metadata.Provider { return provider }, Grouping: func() intelligence.GroupingProvider { return provider }}, nil)
	_, err = engine.Start(ctx, task.RunSpec{Identity: task.Identity{Type: "block", Scope: task.ScopeSystem}, Trigger: task.TriggerManual, Key: "block"}, func(context.Context, *runhandle.Handle) (task.Result, error) { return task.Result{}, nil })
	if err != nil {
		t.Fatal(err)
	}
	scrape, err := service.ScrapeLibrary(ctx, lib.ID, "controlled", task.TriggerManual)
	if err != nil {
		t.Fatal(err)
	}
	grouping, err := service.GroupLibrary(ctx, lib.ID, "en-US", task.TriggerManual)
	if err != nil {
		t.Fatal(err)
	}
	if scrape.Run.Status != task.StatusQueued || grouping.Run.Status != task.StatusQueued {
		t.Fatalf("scrape=%+v grouping=%+v", scrape, grouping)
	}
	if release, err := engine.AcquireLibraryExclusive(ctx, lib.ID); !errors.Is(err, task.ErrLibraryUnavailable) {
		if release != nil {
			release()
		}
		t.Fatalf("queued generation not covered: %v", err)
	}
	for _, run := range []task.Run{scrape.Run, grouping.Run} {
		if err = engine.Cancel(run.ID); err != nil {
			t.Fatal(err)
		}
	}
	bodies[0]()
	for _, run := range []task.Run{scrape.Run, grouping.Run} {
		finished, err := tasks.LoadRun(ctx, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if finished.Status != task.StatusCancelled {
			t.Fatalf("finished=%+v", finished)
		}
	}
	if len(provider.locales) != 0 || len(bodies) != 1 {
		t.Fatalf("cancelled work called source: locales=%v bodies=%d", provider.locales, len(bodies))
	}
	release, err := engine.AcquireLibraryExclusive(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	release()
}
