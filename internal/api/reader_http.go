package api

import (
	"context"
	"net/http"

	"manga-manager/internal/bookcontent"
	"manga-manager/internal/catalogsearch"
	"manga-manager/internal/config"
	"manga-manager/internal/dashboard"
	"manga-manager/internal/database"
	"manga-manager/internal/organize"
	"manga-manager/internal/reading"
	"manga-manager/internal/recommendation"
	"manga-manager/internal/seriescontext"
	"manga-manager/internal/seriesreview"
	"manga-manager/internal/task"
)

// ReaderStore is the assembly input for reader use cases, not a handler dependency.
// It cannot access accounts, configuration, library writes or the database handle.
type ReaderStore interface {
	ReaderQueryStore
	reading.Store
	reading.BookmarkStore
	reading.SeriesCatalogueStore
	seriesreview.Store
	catalogsearch.Store
}

// ReaderCapabilities joins established reader commands and query owners.
type ReaderCapabilities struct {
	queries   ReaderQueryStore
	books     *reading.Queries
	catalogue *reading.CatalogueQueries
	bookmarks *reading.Bookmarks
	positions *reading.Service
	reviews   *seriesreview.Service
	search    *catalogsearch.Search
	context   *seriescontext.Service
}

func NewReaderCapabilities(store ReaderStore, relations seriescontext.Relations, proposals seriescontext.Proposals, runs seriescontext.Runs, invalidate func()) *ReaderCapabilities {
	return &ReaderCapabilities{queries: store, books: reading.NewQueries(store), catalogue: reading.NewCatalogueQueries(store), bookmarks: reading.NewBookmarks(store), positions: reading.New(store, invalidate), reviews: seriesreview.New(store), search: catalogsearch.New(store), context: seriescontext.New(store, relations, proposals, runs)}
}

// ReaderHTTP owns reader and catalogue protocol parsing. Leaf transport effects
// share the host's authentication, content access and organization query adapters.
type ReaderHTTP struct {
	*ReaderCapabilities
	store                                                               ReaderQueryStore
	stats                                                               *dashboard.Service
	recommendations                                                     *recommendation.Service
	content                                                             *bookcontent.Service
	currentConfig                                                       func() config.Config
	failedRuns                                                          func(context.Context, []task.Snapshot) ([]RunSnapshot, error)
	requireProtocolEnabled                                              func(string) func(http.Handler) http.Handler
	requireBasicAuth                                                    func(http.Handler) http.Handler
	serveBookFile, serveCoverImage, serveThumbnailImage, servePageImage func(http.ResponseWriter, *http.Request)
	servePageImageByNumber                                              func(http.ResponseWriter, *http.Request, int64, int64)
	guardBookRead                                                       func(http.ResponseWriter, *http.Request, int64) (func(), bool)
	readingLists                                                        func() *organize.ReadingLists
	loadCollectionViews                                                 func(context.Context, reading.Subject) ([]CollectionView, error)
	loadStaticCollectionSeries                                          func(context.Context, int64, int, int) (CollectionView, []collectionSeriesListItem, int, error)
	loadSmartCollectionSeries                                           func(context.Context, SmartFilter, int, int, reading.Subject) ([]database.SearchSeriesPagedRow, int, error)
	getSmartFilterByID                                                  func(*http.Request, int64) (SmartFilter, error)
}

func (c *ReaderHTTP) currentReadingSubject(r *http.Request) (reading.Subject, error) {
	if u, ok := userFromContext(r.Context()); ok {
		return reading.UserSubject(u.ID)
	}
	return reading.AnonymousSubject(), nil
}
func (c *ReaderHTTP) currentUserID(r *http.Request) int64 {
	if u, ok := userFromContext(r.Context()); ok {
		return u.ID
	}
	return 0
}
