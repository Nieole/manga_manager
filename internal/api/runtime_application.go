package api

import (
	"context"
	"manga-manager/internal/application/intelligence"
	"manga-manager/internal/application/runtime"
	"manga-manager/internal/scanner"
	"manga-manager/internal/task"
)

// assembleRuntime binds HTTP-owned leaf effects to the application process graph.
func (c *Controller) assembleRuntime(scan *scanner.Scanner) error {
	e := &taskEngine{now: nil, slots: c.taskSlots, backoff: c.taskBackoffPolicy, sampleInterval: c.runSampleInterval, resumeEnabled: c.taskResumeEnabled, identities: make(map[int64]TaskIdentity)}
	c.taskEngine = e
	app, err := runtime.New(c.store, c.config, runtime.Options{ConfigPath: c.configPath, Scanner: scan, Tasks: task.Config{Publish: e.publisher(c.sse.publishAdmin), PublishLive: e.livePublisher(c.sse.publishAdmin), RunBackground: func(fn func()) { e.runBackground(fn) }, Now: e.clock, Slots: e.slotLimit, Backoff: e.backoffPolicy, SampleInterval: e.samplingInterval}, Effects: runtime.Effects{Publish: c.PublishEvent, PurgeReadingPaths: c.purgeReadingPathCaches, InvalidateStats: c.invalidateDashboardStatsCache, WarmStats: c.warmDashboardStatsCacheAsync, InvalidateReading: func() { c.invalidateVolatileStatsCache("koreader_progress") }, RunEvents: c.sse.run, StopEvents: c.sse.closeClients, TrimPageCache: c.enforcePageCacheBudget, ObserveIdentity: e.observeIdentity, Sources: intelligence.Sources{Metadata: c.getProvider}}})
	if err != nil {
		return err
	}
	c.runtime = app
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
	c.watcher = app.Watcher
	c.watcherInitErr = app.WatcherError
	if err = app.Recover(context.Background()); err != nil {
		app.Close()
		return err
	}
	return nil
}
