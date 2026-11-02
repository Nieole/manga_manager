package api

import (
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
)

// apiFixture retains explicit SQLite/application resources for assertions and independently bound adapters.
// Resource replacement is not handler injection: tests must rebind the consumed port.
type apiFixture struct {
	*Host
	admin          *administrationHTTP
	media          *contentHTTP
	store          database.Store
	imageCache     *imageMemoryCache
	content        *bookcontent.Service
	stats          *dashboard.Service
	scanner        *scanner.Scanner
	config         *config.Manager
	diskWork       *diskwork.Runner
	koreader       *koreader.Service
	external       *external.Manager
	diskJobs       *diskjobs.Service
	intelligence   *intelligence.Service
	proposals      *proposal.Service
	relocation     *relocation.Service
	configPath     string
	scans          *scanning.Service
	maintenance    *maintenance.Service
	watcher        *scanner.FileWatcher
	watcherInitErr error
	moves          *relocating.Service

	sse *sseBroker

	recommendations *recommendation.Service

	taskEngine *taskEngine

	auth *authState

	runtime       *appruntime.Runtime
	process       Lifecycle
	configuration *configuration.Service

	relations         *organize.Relations
	libraryManagement *libraries.Service
}
