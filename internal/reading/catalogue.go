package reading

import (
	"context"

	"manga-manager/internal/database"
)

// SeriesCatalogueStore keeps subject aggregates inside SQL so filtering, ordering,
// counts and pagination all observe the same state before selecting a page.
type SeriesCatalogueStore interface {
	SearchSeriesPaged(context.Context, int64, database.SeriesListFilters, int32, int32, string) ([]database.SearchSeriesPagedRow, int, error)
	SearchSeriesCursor(context.Context, int64, database.SeriesListFilters, int32, string, string) ([]database.SearchSeriesPagedRow, string, bool, error)
	GetUserRecentReadSeries(context.Context, int64, int64, int64) ([]database.GetRecentReadSeriesRow, error)
	GetRecentReadSeries(context.Context, database.GetRecentReadSeriesParams) ([]database.GetRecentReadSeriesRow, error)
}

// CatalogueQueries selects the reader for aggregate catalogue views.
type CatalogueQueries struct{ store SeriesCatalogueStore }

func NewCatalogueQueries(store SeriesCatalogueStore) *CatalogueQueries {
	return &CatalogueQueries{store: store}
}

func (q *CatalogueQueries) SearchPaged(ctx context.Context, uid, libraryID int64, filters database.SeriesListFilters, limit, offset int32, sortBy string) ([]database.SearchSeriesPagedRow, int, error) {
	filters.UserID = uid
	return q.store.SearchSeriesPaged(ctx, libraryID, filters, limit, offset, sortBy)
}

func (q *CatalogueQueries) SearchCursor(ctx context.Context, uid, libraryID int64, filters database.SeriesListFilters, limit int32, sortBy, cursor string) ([]database.SearchSeriesPagedRow, string, bool, error) {
	filters.UserID = uid
	return q.store.SearchSeriesCursor(ctx, libraryID, filters, limit, sortBy, cursor)
}

func (q *CatalogueQueries) RecentSeries(ctx context.Context, uid, libraryID, limit int64) ([]database.GetRecentReadSeriesRow, error) {
	if uid > 0 {
		return q.store.GetUserRecentReadSeries(ctx, uid, libraryID, limit)
	}
	return q.store.GetRecentReadSeries(ctx, database.GetRecentReadSeriesParams{LibraryID: libraryID, LibraryID_2: libraryID, Limit: limit})
}
