package api

import (
	"context"
	"manga-manager/internal/bookcontent"
	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/metadata"
	"manga-manager/internal/proposal"
	"manga-manager/internal/scanner"
	"manga-manager/internal/task"
	"net/http"
)

// ContentQueries locates content and diagnoses unavailable library storage.
type ContentQueries interface {
	GetLibrary(context.Context, int64) (database.Library, error)
	GetBookCoverPath(context.Context, int64) (string, error)
}
type contentHTTP struct {
	store         ContentQueries
	content       *bookcontent.Service
	imageCache    *bookcontent.MemoryCache
	currentConfig func() config.Config
	engine        *task.Engine
}

// CatalogueMaintenanceStore is the catalogue's retained maintenance boundary.
type CatalogueMaintenanceStore interface {
	GetSeries(context.Context, int64) (database.Series, error)
	UpdateSeriesFavorite(context.Context, database.UpdateSeriesFavoriteParams) error
	ReplaceSeriesCustomFields(context.Context, int64, []database.SeriesCustomField) error
	RenameTag(context.Context, int64, string) error
	MergeTags(context.Context, int64, int64) error
	DeleteTag(context.Context, int64) error
}
type catalogMaintenanceHTTP struct {
	store                         CatalogueMaintenanceStore
	engine                        *task.Engine
	openPath                      func(string) error
	invalidateDashboardStatsCache func(string)
}

func (c *catalogMaintenanceHTTP) guardLibraryRead(w http.ResponseWriter, id int64) (func(), bool) {
	return guardLibraryRead(w, id, c.engine)
}

type coverHTTP struct {
	store interface {
		GetBook(context.Context, int64) (database.Book, error)
	}
	scanner                       *scanner.Scanner
	invalidateDashboardStatsCache func(string)
}

// ProposalQueue is independent of the organization review capability.
type ProposalQueue interface {
	Queue(context.Context, database.Series, *metadata.SeriesMetadata, string, string, proposal.QueueOptions) (proposal.QueueResult, error)
}
type providerHTTP struct {
	*providerClients
	store     OrganizationSeriesQuery
	proposals ProposalQueue
}

// providerClients owns only configured external client factories.
type providerClients struct {
	currentConfig   func() config.Config
	fetcherFactory  func(string) metadata.Fetcher
	searcherFactory func(string) metadata.Searcher
}
