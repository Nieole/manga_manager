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
