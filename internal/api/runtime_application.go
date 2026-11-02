package api

import (
	"context"
	"manga-manager/internal/application/intelligence"
	"manga-manager/internal/application/runtime"
	"manga-manager/internal/application/seriesediting"
	"manga-manager/internal/organize"
	"manga-manager/internal/scanner"
	"manga-manager/internal/task"
)

// assembleRuntime binds HTTP-owned leaf effects to the application process graph.
func (c *Controller) assembleRuntime(scan *scanner.Scanner) error {
	e := c.taskEngine
	c.taskEngine = e
	app, err := runtime.New(c.store, c.config, runtime.Options{ConfigPath: c.configPath, Scanner: scan, Tasks: task.Config{Publish: e.publisher(c.sse.publishAdmin), PublishLive: e.livePublisher(c.sse.publishAdmin), RunBackground: func(fn func()) { e.runBackground(fn) }, Now: e.clock, Slots: e.slotLimit, Backoff: e.backoffPolicy, SampleInterval: e.samplingInterval}, Effects: runtime.Effects{Publish: c.PublishEvent, PurgeReadingPaths: c.purgeReadingPathCaches, InvalidateStats: c.invalidateDashboardStatsCache, WarmStats: c.warmDashboardStatsCacheAsync, InvalidateRecommendations: c.purgeRecommendationCache, InvalidateReading: func() { c.invalidateVolatileStatsCache("koreader_progress") }, RunEvents: c.sse.run, StopEvents: c.sse.closeClients, TrimPageCache: c.enforcePageCacheBudget, ObserveIdentity: e.observeIdentity, Sources: intelligence.Sources{Metadata: c.getMetadataFetcher}}})
	if err != nil {
		return err
	}
	c.runtime = app
	c.process = app
	c.configuration = app.Configuration
	e.engine = app.Engine
	e.runs = app.Runs
	e.runStore = app.Tasks
	e.runBackground = app.Go
	e.registry = app.Registry
	e.runtime = app
	e.libraryIDs = c.taskLibraries
	c.scanner = app.Scanner
	c.diskWork = app.DiskWork
	c.scans = app.Scans
	c.maintenance = app.Maintenance
	c.moves = app.Moves
	c.diskJobs = app.Disk
	c.intelligence = app.Intelligence
	c.koreader = app.KOReader
	c.external = app.External
	c.proposals = app.Proposals
	c.relocation = app.Relocation
	c.relations = app.Relations
	c.libraryManagement = app.LibraryManagement
	c.organizationHTTP = newOrganizationHTTP(OrganizationDependencies{
		Relations: app.Relations, Proposals: app.Proposals,
		Collections: organize.NewCollections(c.store), Shelves: organize.NewSmartCollections(c.store), ReadingLists: organize.NewReadingLists(c.store),
		Grouping: organize.NewAIGrouping(c.store, func() { c.sse.publish("refresh") }), GroupingQueries: c.store, Series: c.store, StaticCollections: c.store,
		Metadata: seriesediting.New(seriesediting.NewSQLStore(c.store)), Selection: seriesediting.NewSelection(seriesediting.NewSQLStore(c.store), func() { c.invalidateDashboardStatsCache("bulk_edit") }),
		UnmatchedCount: c.store.CountUnmatchedKOReaderProgress, KOReaderEnabled: func() bool { return c.config.Snapshot().KOReader.Enabled }, Publish: c.sse.publish,
	})
	c.watcher = app.Watcher
	c.watcherInitErr = app.WatcherError
	if err = app.Recover(context.Background()); err != nil {
		app.Close()
		return err
	}
	return nil
}
