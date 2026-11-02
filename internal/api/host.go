package api

import (
	"context"

	"manga-manager/internal/application"
	"manga-manager/internal/application/configuration"
	"manga-manager/internal/application/diskjobs"
	"manga-manager/internal/application/intelligence"
	"manga-manager/internal/application/libraries"
	"manga-manager/internal/application/maintenance"
	"manga-manager/internal/application/relocating"
	appruntime "manga-manager/internal/application/runtime"
	"manga-manager/internal/application/scanning"
	"manga-manager/internal/bookcontent"
	"manga-manager/internal/config"
	"manga-manager/internal/dashboard"
	"manga-manager/internal/database"
	"manga-manager/internal/diskwork"
	"manga-manager/internal/external"
	"manga-manager/internal/koreader"
	"manga-manager/internal/organize"
	"manga-manager/internal/proposal"
	"manga-manager/internal/recommendation"
	"manga-manager/internal/relocation"
	"manga-manager/internal/scanner"
	"manga-manager/internal/task"

	"github.com/go-chi/chi/v5"
)

// Lifecycle supplies transport background ownership without exposing the application graph.
type Lifecycle interface {
	Done() <-chan struct{}
	Go(func())
	Close()
	Shutdown(context.Context, func(context.Context) error) error
}

// ReadingDependencies supplies content and reader query projections.
type ReadingDependencies struct {
	Capabilities    *ReaderCapabilities
	Content         *bookcontent.Service
	ImageCache      *bookcontent.MemoryCache
	Statistics      *dashboard.Service
	Recommendations *recommendation.Service
	KOReader        *koreader.Service
}

// RunDependencies joins the transport projection to the same admitted queue.
type RunDependencies struct {
	Engine   *task.Engine
	Runs     *application.Runs
	Store    task.Store
	Registry *appruntime.Registry
}

// LibraryDependencies supplies library and disk-use cases to their transport family.
type LibraryDependencies struct {
	Management   *libraries.Service
	Scans        *scanning.Service
	Maintenance  *maintenance.Service
	Moves        *relocating.Service
	Disk         *diskjobs.Service
	Intelligence *intelligence.Service
	External     *external.Manager
	Relocation   *relocation.Service
	Scanner      *scanner.Scanner
	Watcher      *scanner.FileWatcher
	WatcherError error
	DiskWork     *diskwork.Runner
	Runs         RunDependencies
}

// AdministrationDependencies supplies configuration publication, not a Runtime.
type AdministrationDependencies struct{ Configuration *configuration.Service }

// HostDependencies groups capability inputs by the families migrating off Controller.
// Legacy handlers consume these bindings until their family migration removes them.
type HostDependencies struct {
	Reading        ReadingDependencies
	Organization   OrganizationDependencies
	Libraries      LibraryDependencies
	Administration AdministrationDependencies
	Lifecycle      Lifecycle
}

// Transport owns HTTP-only authentication, push projection and legacy route state.
// Construction does not create, recover or start an application.
type Transport struct{ legacy *Controller }

// NewTransport creates transport leaf state. Store is restricted to the retained
// Controller adapter until the HTTP family migrations remove its compatibility surface.
func NewTransport(store database.Store, cfg *config.Manager, configPath string) *Transport {
	c := &Controller{store: store, config: cfg, configPath: configPath, sse: newSSEBroker(), auth: newAuthState(), openPath: openPathInDefaultFileManager}
	c.taskEngine = &taskEngine{slots: c.taskSlots, backoff: c.taskBackoffPolicy, sampleInterval: c.runSampleInterval, resumeEnabled: c.taskResumeEnabled, identities: make(map[int64]TaskIdentity)}
	c.taskEngine.configSnapshot = c.currentConfig
	c.taskEngine.libraryIDs = c.taskLibraries
	return &Transport{legacy: c}
}

// RuntimeEffects exposes transport-owned leaf operations for the composition root.
// Bind must finish before recovery or background inputs can invoke these leaves.
func (t *Transport) RuntimeEffects() appruntime.Effects {
	c := t.legacy
	return appruntime.Effects{Publish: c.PublishEvent, PurgeReadingPaths: c.purgeReadingPathCaches, InvalidateStats: c.invalidateDashboardStatsCache, WarmStats: c.warmDashboardStatsCacheAsync, InvalidateRecommendations: c.purgeRecommendationCache, InvalidateReading: func() { c.invalidateVolatileStatsCache("koreader_progress") }, RunEvents: c.sse.run, StopEvents: c.sse.closeClients, TrimPageCache: c.enforcePageCacheBudget, ObserveIdentity: c.taskEngine.observeIdentity, Sources: intelligence.Sources{Metadata: c.getMetadataFetcher}}
}

// TaskOptions attaches transport projections while retaining caller-controlled clocks and scheduling.
func (t *Transport) TaskOptions(options task.Config) task.Config {
	e, c := t.legacy.taskEngine, t.legacy
	if options.Publish == nil {
		options.Publish = e.publisher(c.sse.publishAdmin)
	}
	if options.PublishLive == nil {
		options.PublishLive = e.livePublisher(c.sse.publishAdmin)
	}
	if options.RunBackground == nil {
		options.RunBackground = func(fn func()) { e.runBackground(fn) }
	}
	if options.Now == nil {
		options.Now = e.clock
	}
	if options.Slots == nil {
		options.Slots = e.slotLimit
	}
	if options.Backoff == nil {
		options.Backoff = e.backoffPolicy
	}
	if options.SampleInterval == nil {
		options.SampleInterval = e.samplingInterval
	}
	return options
}

// Host mounts feature adapters. Its compatibility controller is removed by the
// reader, organization, library and administration migration tickets.
type Host struct{ compatibility *Controller }

// Bind installs capabilities without constructing or starting another application.
func (t *Transport) Bind(d HostDependencies) *Host {
	c := t.legacy
	c.process = d.Lifecycle
	c.configuration = d.Administration.Configuration
	c.content, c.imageCache = d.Reading.Content, d.Reading.ImageCache
	c.stats, c.recommendations = d.Reading.Statistics, d.Reading.Recommendations
	c.koreader = d.Reading.KOReader
	c.organizationHTTP = newOrganizationHTTP(d.Organization)
	// Compatibility bindings for catalog/scrape only; removed by 47.
	c.relations, _ = d.Organization.Relations.(*organize.Relations)
	c.proposals, _ = d.Organization.Proposals.(*proposal.Service)
	c.libraryManagement, c.scans = d.Libraries.Management, d.Libraries.Scans
	c.maintenance, c.moves, c.diskJobs = d.Libraries.Maintenance, d.Libraries.Moves, d.Libraries.Disk
	c.intelligence, c.external, c.relocation = d.Libraries.Intelligence, d.Libraries.External, d.Libraries.Relocation
	c.scanner, c.watcher, c.watcherInitErr = d.Libraries.Scanner, d.Libraries.Watcher, d.Libraries.WatcherError
	c.diskWork = d.Libraries.DiskWork
	e := c.taskEngine
	e.engine, e.runs, e.runStore, e.registry = d.Libraries.Runs.Engine, d.Libraries.Runs.Runs, d.Libraries.Runs.Store, d.Libraries.Runs.Registry
	e.runBackground = d.Lifecycle.Go
	if d.Reading.Capabilities != nil {
		c.reader = c.bindReaderHTTP(d.Reading.Capabilities)
	}
	return &Host{compatibility: c}
}

func (h *Host) SetupRoutes(r chi.Router)         { h.compatibility.SetupRoutes(r) }
func (h *Host) SetupOPDSRoutes(r chi.Router)     { h.compatibility.SetupOPDSRoutes(r) }
func (h *Host) SetupKOReaderRoutes(r chi.Router) { h.compatibility.SetupKOReaderRoutes(r) }
