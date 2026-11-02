// Package bootstrap is the production composition root for application resources and HTTP adapters.
// It registers capabilities and restores consistency before exposing a ready process.
package bootstrap

import (
	"context"
	"time"

	"manga-manager/internal/api"
	"manga-manager/internal/application/runtime"
	"manga-manager/internal/application/seriesediting"
	"manga-manager/internal/bookcontent"
	"manga-manager/internal/config"
	"manga-manager/internal/dashboard"
	"manga-manager/internal/database"
	"manga-manager/internal/organize"
	"manga-manager/internal/recommendation"
	"manga-manager/internal/runtimecfg"
	"manga-manager/internal/scanner"
	"manga-manager/internal/task"
)

// Options controls process inputs and scheduling at the production assembly seam.
type Options struct {
	ConfigPath  string
	Scanner     *scanner.Scanner
	Tasks       task.Config
	WatchConfig func(string, *config.Manager) (runtime.ConfigWatch, error)
}

// Process owns the assembled application and its HTTP host. Application is the
// composition result for embedded callers, never an HTTP handler dependency.
type Process struct {
	Application *runtime.Runtime
	HTTP        *api.Host
}

// Open assembles one graph, restores consistency and interrupted runs, then opens inputs.
// On failure every constructed process resource is closed and no HTTP host is returned.
// The caller retains ownership of the supplied database connection and process log file.
func Open(ctx context.Context, store database.Store, cfg *config.Manager, options Options) (*Process, error) {
	resources := runtimecfg.NewResources()
	transport := api.NewTransport(store, cfg, options.ConfigPath)
	memory := bookcontent.NewMemoryCache(bookcontent.DefaultImageCacheBytes)
	content := bookcontent.New(func(ctx context.Context, id int64) (bookcontent.Source, error) {
		book, err := store.GetBook(ctx, id)
		return bookcontent.Source{ID: book.ID, LibraryID: book.LibraryID, Path: book.Path, FileModifiedAt: book.FileModifiedAt, Size: book.Size, PageCount: book.PageCount}, err
	}, cfg.Snapshot, memory, 512, 128)
	effects := transport.RuntimeEffects()
	app, err := runtime.New(store, cfg, runtime.Options{ConfigPath: options.ConfigPath, Scanner: options.Scanner, Resources: resources, Tasks: transport.TaskOptions(options.Tasks), Effects: effects, WatchConfig: options.WatchConfig})
	if err != nil {
		resources.Close()
		return nil, err
	}
	stats := dashboard.New()
	reader := api.NewReaderCapabilities(store, app.Relations, app.Proposals, app.Engine, func() { stats.InvalidateVolatile("reading") })
	host := transport.Bind(api.HostDependencies{
		Reading: api.ReadingDependencies{Capabilities: reader, Content: content, ImageCache: memory, Statistics: stats, Recommendations: recommendation.New(24 * time.Hour), KOReader: app.KOReader},
		Organization: api.OrganizationDependencies{
			Relations: app.Relations, Proposals: app.Proposals,
			Collections: organize.NewCollections(store), Shelves: organize.NewSmartCollections(store), ReadingLists: organize.NewReadingLists(store),
			Grouping: organize.NewAIGrouping(store, func() { effects.Publish("refresh") }), GroupingQueries: store, Series: store, StaticCollections: store,
			Metadata: seriesediting.New(seriesediting.NewSQLStore(store)), Selection: seriesediting.NewSelection(seriesediting.NewSQLStore(store), func() { effects.InvalidateStats("bulk_edit") }),
			UnmatchedCount: store.CountUnmatchedKOReaderProgress, KOReaderEnabled: func() bool { return cfg.Snapshot().KOReader.Enabled }, Publish: effects.Publish,
		},
		Libraries:      api.LibraryDependencies{Management: app.LibraryManagement, Scans: app.Scans, Maintenance: app.Maintenance, Moves: app.Moves, Disk: app.Disk, Intelligence: app.Intelligence, External: app.External, Relocation: app.Relocation, Scanner: app.Scanner, Watcher: app.Watcher, WatcherError: app.WatcherError, DiskWork: app.DiskWork, Runs: api.RunDependencies{Engine: app.Engine, Runs: app.Runs, Store: app.Tasks, Registry: app.Registry}},
		Administration: api.AdministrationDependencies{Configuration: app.Configuration},
		Lifecycle:      app,
	})
	if err := app.Start(ctx); err != nil {
		app.Close()
		return nil, err
	}
	return &Process{Application: app, HTTP: host}, nil
}

func (p *Process) Close() { p.Application.Close() }
func (p *Process) Shutdown(ctx context.Context, drain func(context.Context) error) error {
	return p.Application.Shutdown(ctx, drain)
}
