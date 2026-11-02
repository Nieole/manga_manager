package api

import (
	"context"
	"net/http"

	"manga-manager/internal/application/diskjobs"
	"manga-manager/internal/application/intelligence"
	"manga-manager/internal/application/libraries"
	"manga-manager/internal/application/maintenance"
	"manga-manager/internal/application/relocating"
	"manga-manager/internal/application/scanning"
	"manga-manager/internal/bookcontent"
	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/external"
	"manga-manager/internal/metadata"
	"manga-manager/internal/relocation"
	"manga-manager/internal/task"
)

// LibraryQueries is the library administration projection, not a write store.
type LibraryQueries interface {
	GetLibrary(context.Context, int64) (database.Library, error)
	ListLibraries(context.Context) ([]database.Library, error)
	LibraryRelocationProtection(context.Context, int64) (*database.LibraryRelocationProtection, error)
}

// ComicInfoQueries provides only the facts used to export archive metadata.
type ComicInfoQueries interface {
	GetBook(context.Context, int64) (database.Book, error)
	GetSeries(context.Context, int64) (database.Series, error)
	ListBooksBySeries(context.Context, int64) ([]database.Book, error)
	GetTagsForSeries(context.Context, int64) ([]database.Tag, error)
	GetAuthorsForSeries(context.Context, int64) ([]database.Author, error)
}

type libraryHTTP struct {
	store             LibraryQueries
	libraryManagement *libraries.Service
	scans             *scanning.Service
}
type maintenanceHTTP struct {
	initials           interface{ BackfillSeriesInitials(context.Context) error }
	maintenance        *maintenance.Service
	intelligence       *intelligence.Service
	getMetadataFetcher func(string) metadata.Fetcher
}
type diskHTTP struct {
	store    ComicInfoQueries
	diskJobs *diskjobs.Service
	external *external.Manager
	content  *bookcontent.Service
	engine   *task.Engine
}

// BookRemovalStore is the existing deduplication maintenance boundary. Moving
// files and reporting partial outcomes is unchanged by the HTTP dependency migration.
type BookRemovalStore interface {
	FindDuplicateBooks(context.Context) ([]database.DuplicateBookRow, error)
	GetBook(context.Context, int64) (database.Book, error)
	DeleteBook(context.Context, int64) error
	RefreshSeriesDerivedData(context.Context, int64) error
}
type removalHTTP struct {
	store                         BookRemovalStore
	engine                        *task.Engine
	databasePath                  func() string
	invalidateDashboardStatsCache func(string)
}

func (c *removalHTTP) guardLibraryRead(w http.ResponseWriter, id int64) (func(), bool) {
	return guardLibraryRead(w, id, c.engine)
}

type relocationHTTP struct {
	store interface {
		LibraryRelocationProtection(context.Context, int64) (*database.LibraryRelocationProtection, error)
	}
	moves      *relocating.Service
	relocation *relocation.Service
}
type storageHTTP struct {
	store interface {
		ListLibraries(context.Context) ([]database.Library, error)
	}
	currentConfig func() config.Config
	taskEngine    interface{ latestRunByTypes(...string) *RunSnapshot }
}
type runCapabilities interface {
	listRunSnapshots(context.Context, taskFilters) ([]RunSnapshot, error)
	listTaskSummaries(context.Context, taskFilters) ([]TaskSummary, error)
	live(context.Context) (RunLive, error)
	clear(context.Context, taskFilters) (int64, error)
	snapshotForRetry(context.Context, int64) (RunSnapshot, error)
	relauncherFor(string, TaskVariant) (taskRelauncher, bool)
	pauseRun(int64) error
	resumeRun(int64) error
	cancelRun(int64) error
	pauseAll(context.Context) (int, error)
	resumeAll(context.Context) (int, error)
	setTaskDisabled(context.Context, int64, bool) error
	runEvents(context.Context, int64) (RunEventsResponse, error)
	runSamples(context.Context, int64, int) (RunSamplesResponse, error)
}

type runsHTTP struct {
	taskEngine    runCapabilities
	currentConfig func() config.Config
}

// These are independently bound adapters. None retains a Controller, Runtime,
// full database.Store or a second engine/projection/cache.
type httpFamilies struct {
	removal     *removalHTTP
	library     *libraryHTTP
	maintenance *maintenanceHTTP
	disk        *diskHTTP
	relocation  *relocationHTTP
	storage     *storageHTTP
	runs        *runsHTTP
}

func bindHTTPFamilies(d LibraryDependencies, content *bookcontent.Service, snapshot func() config.Config, projection *taskEngine, fetcher func(string) metadata.Fetcher, databasePath func() string, invalidate func(string)) httpFamilies {
	return httpFamilies{
		removal:     &removalHTTP{store: d.Removal, engine: d.Runs.Engine, databasePath: databasePath, invalidateDashboardStatsCache: invalidate},
		library:     &libraryHTTP{store: d.Queries, libraryManagement: d.Management, scans: d.Scans},
		maintenance: &maintenanceHTTP{initials: d.Initials, maintenance: d.Maintenance, intelligence: d.Intelligence, getMetadataFetcher: fetcher},
		disk:        &diskHTTP{store: d.ComicInfo, diskJobs: d.Disk, external: d.External, content: content, engine: d.Runs.Engine},
		relocation:  &relocationHTTP{store: d.Queries, moves: d.Moves, relocation: d.Relocation},
		storage:     &storageHTTP{store: d.Queries, currentConfig: snapshot, taskEngine: projection},
		runs:        &runsHTTP{taskEngine: projection, currentConfig: snapshot},
	}
}

// httpFamilies is only the legacy construction adapter. Host binds once from
// composition-root inputs and never consults these compatibility feature fields.
func (c *Controller) httpFamilies() httpFamilies {
	return bindHTTPFamilies(LibraryDependencies{Removal: c.store, Initials: c.store, Queries: c.store, ComicInfo: c.store, Management: c.libraryManagement, Scans: c.scans, Maintenance: c.maintenance, Intelligence: c.intelligence, Moves: c.moves, Relocation: c.relocation, Disk: c.diskJobs, External: c.external, Runs: RunDependencies{Engine: c.taskEngine.engine}}, c.content, c.currentConfig, c.taskEngine, c.getMetadataFetcher, c.configuration.DatabasePath, c.invalidateDashboardStatsCache)
}

func (c *diskHTTP) guardLibraryRead(w http.ResponseWriter, libraryID int64) (func(), bool) {
	return guardLibraryRead(w, libraryID, c.engine)
}

func guardLibraryRead(w http.ResponseWriter, libraryID int64, engine *task.Engine) (func(), bool) {
	release, err := engine.AcquireLibraryRead(libraryID)
	if err != nil {
		writeLibraryUnavailable(w)
		return nil, false
	}
	return release, true
}
func (c *diskHTTP) guardBookRead(w http.ResponseWriter, r *http.Request, bookID int64) (func(), bool) {
	return guardContentRead(w, r, bookID, c.content, c.engine)
}
