package api

import (
	"context"
	"manga-manager/internal/application/runtime"
	"manga-manager/internal/application/scanning"
	"manga-manager/internal/application/seriesediting"
	"manga-manager/internal/bookcontent"
	"manga-manager/internal/config"
	"manga-manager/internal/dashboard"
	"manga-manager/internal/database"
	"manga-manager/internal/logger"
	"manga-manager/internal/organize"
	"manga-manager/internal/recommendation"
	"manga-manager/internal/scanner"
	"manga-manager/internal/task"
	"time"
)

func newAPIFixture(store database.Store, scan *scanner.Scanner, cfg *config.Manager, cfgPath string, sizes controllerCacheSizes) *apiFixture {
	transport := NewTransport(cfg)
	cache := newImageMemoryCache(sizes.imageBytes)
	content := bookcontent.New(func(ctx context.Context, id int64) (bookcontent.Source, error) {
		b, err := store.GetBook(ctx, id)
		return bookPageSourceFromBook(b), err
	}, cfg.Snapshot, cache, sizes.bookPageSource, sizes.page)
	stats := dashboard.New()
	effects := transport.RuntimeEffects()
	app, err := runtime.New(store, cfg, runtime.Options{ConfigPath: cfgPath, Scanner: scan, Tasks: transport.TaskOptions(task.Config{}), Effects: effects})
	if err != nil {
		panic(err)
	}
	host := transport.Bind(HostDependencies{
		Catalogue: store, CoverQueries: store,
		Reading: ReadingDependencies{Capabilities: NewReaderCapabilities(store, app.Relations, app.Proposals, app.Engine, func() { stats.InvalidateVolatile("reading") }), Content: content, ImageCache: cache, Statistics: stats, StatisticsQueries: store, ContentQueries: store, Recommendations: recommendation.New(24 * time.Hour)},
		Organization: OrganizationDependencies{Relations: app.Relations, Proposals: app.Proposals, ProposalQueue: app.Proposals,
			Collections: organize.NewCollections(store), Shelves: organize.NewSmartCollections(store), ReadingLists: organize.NewReadingLists(store), Grouping: organize.NewAIGrouping(store, func() { effects.Publish("refresh") }), GroupingQueries: store, Series: store, StaticCollections: store,
			Metadata: seriesediting.New(seriesediting.NewSQLStore(store)), Selection: seriesediting.NewSelection(seriesediting.NewSQLStore(store), func() { effects.InvalidateStats("bulk_edit") }), UnmatchedCount: store.CountUnmatchedKOReaderProgress, KOReaderEnabled: func() bool { return cfg.Snapshot().KOReader.Enabled }, Publish: effects.Publish},
		Libraries:      LibraryDependencies{Removal: store, Initials: store, Queries: store, ComicInfo: store, Management: app.LibraryManagement, Scans: app.Scans, Maintenance: app.Maintenance, Moves: app.Moves, Disk: app.Disk, Intelligence: app.Intelligence, External: app.External, Relocation: app.Relocation, Scanner: app.Scanner, Runs: RunDependencies{Engine: app.Engine, Runs: app.Runs, Store: app.Tasks, Registry: app.Registry}},
		Administration: AdministrationDependencies{Configuration: app.Configuration, Store: store, Manager: cfg, KOReader: app.KOReader}, Lifecycle: app,
	})
	if err := app.Recover(context.Background()); err != nil {
		app.Close()
		panic(err)
	}
	return &apiFixture{Host: host, admin: host.administration,
		media: host.content,
		store: store, config: cfg, configPath: cfgPath, scanner: app.Scanner, stats: stats, imageCache: cache, content: content,
		recommendations: transport.effects.recommendations, taskEngine: transport.projection, sse: transport.events, auth: transport.auth,
		runtime: app, process: app, configuration: app.Configuration, scans: app.Scans, maintenance: app.Maintenance, diskJobs: app.Disk, intelligence: app.Intelligence,
		moves: app.Moves, external: app.External, koreader: app.KOReader, proposals: app.Proposals, relations: app.Relations, relocation: app.Relocation, libraryManagement: app.LibraryManagement, diskWork: app.DiskWork, watcher: app.Watcher, watcherInitErr: app.WatcherError}
}

type controllerCacheSizes struct {
	imageBytes           int64
	page, bookPageSource int
}

func defaultControllerCacheSizes() controllerCacheSizes {
	return controllerCacheSizes{defaultImageCacheBytes, 128, 512}
}
func (c *apiFixture) Close() {
	if c.process != nil {
		c.process.Close()
	}
}
func (c *apiFixture) currentConfig() config.Config {
	if c.config == nil {
		return config.Config{}
	}
	return c.config.Snapshot()
}
func (c *apiFixture) runBackground(fn func())        { c.process.Go(fn) }
func (c *apiFixture) lifecycleDone() <-chan struct{} { return c.process.Done() }
func (c *apiFixture) PublishEvent(event string)      { c.sse.publish(event) }
func (c *apiFixture) ShutdownNotify()                { c.sse.closeClients() }
func (c *apiFixture) dispatchScheduledScans(ctx context.Context, now time.Time, last map[int64]time.Time) {
	c.runtime.DispatchScheduledScans(ctx, now, last)
}
func (c *apiFixture) handleScannerBatchEvent(action string) { c.scans.BatchEvent(action) }
func (c *apiFixture) RebuildFranchiseCollections(ctx context.Context) error {
	return c.relations.Rebuild(ctx)
}
func (c *apiFixture) runSampleInterval() time.Duration { return sampleIntervalOf(c.currentConfig()) }
func (c *apiFixture) runLimitsForPath(path string) RunLimits {
	return *runLimitsFromDomain(scanning.LimitsForPath(c.currentConfig(), path))
}
func (c *apiFixture) acquireLibraryExclusive(ctx context.Context, id int64) (func(), error) {
	return c.taskEngine.engine.AcquireLibraryExclusiveForRun(ctx, id, logger.RunIDFrom(ctx))
}
func (c *apiFixture) libraryAccessConflicts(ctx context.Context, id int64) ([]task.LibraryConflict, error) {
	return c.taskEngine.engine.LibraryConflicts(ctx, id, logger.RunIDFrom(ctx))
}
