package api

import (
	"context"

	"manga-manager/internal/dashboard"
	"manga-manager/internal/database"
	"manga-manager/internal/reading"
	"manga-manager/internal/recommendation"
)

// ReaderQueryStore supplies read-only catalogue and statistics projections.
type ReaderQueryStore interface {
	reading.BookQueryStore
	reading.RecentStore
	dashboard.Store
	recommendation.Store
	CountRecentAddedSeries(ctx context.Context, libraryID int64) (int64, error)
	CountSeriesByLibrary(ctx context.Context, libraryID int64) (int64, error)
	GetActivityHeatmap(ctx context.Context, sinceDate string) ([]database.GetActivityHeatmapRow, error)
	GetAllAuthors(ctx context.Context) ([]database.Author, error)
	GetAllTags(ctx context.Context) ([]database.Tag, error)
	GetAuthorsForSeries(ctx context.Context, seriesID int64) ([]database.Author, error)
	GetContinueReadingSequels(ctx context.Context) ([]database.GetContinueReadingSequelsRow, error)
	GetLinksForSeries(ctx context.Context, seriesID int64) ([]database.SeriesLink, error)
	GetMihonSeries(ctx context.Context, id int64) (database.GetMihonSeriesRow, error)
	GetSeries(ctx context.Context, id int64) (database.Series, error)
	GetTagsForSeries(ctx context.Context, seriesID int64) ([]database.Tag, error)
	GetUserActivityHeatmap(ctx context.Context, userID int64, offsetClause string) ([]database.ActivityDay, error)
	GetUserBookReadingTimeTop(ctx context.Context, userID int64, limit int) ([]database.BookReadingTimeRow, error)
	GetUserPeriodStats(ctx context.Context, userID int64, year, month int) (database.UserPeriodStats, error)
	GetUserReadingStreak(ctx context.Context, userID int64) (current, longest int, err error)
	GetUserTotalReadingTime(ctx context.Context, userID int64) (int64, error)
	ListSeriesByLibrary(ctx context.Context, libraryID int64) ([]database.ListSeriesByLibraryRow, error)
	ListLibraries(ctx context.Context) ([]database.Library, error)
	ListOPDSLibrarySeriesPaged(ctx context.Context, arg database.ListOPDSLibrarySeriesPagedParams) ([]database.ListOPDSLibrarySeriesPagedRow, error)
	ListRecentAddedSeries(ctx context.Context, arg database.ListRecentAddedSeriesParams) ([]database.ListRecentAddedSeriesRow, error)
	ListSeriesCustomFields(ctx context.Context, seriesID int64) ([]database.SeriesCustomField, error)
	SearchAuthors(ctx context.Context, query string, limit int) ([]database.Author, error)
	SearchProtocolSeries(ctx context.Context, keyword string, limit, offset int32) ([]database.ProtocolSeriesRow, int, error)
	SearchTags(ctx context.Context, query string, limit int) ([]database.Tag, error)
}
