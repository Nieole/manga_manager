package reading

import (
	"context"
	"errors"

	"manga-manager/internal/database"
)

// CatalogueFilters describes list selection independently of the selected reader.
type CatalogueFilters struct {
	Keyword         string
	Letter          string
	Status          string
	Tags            []string
	Authors         []string
	ReadState       string
	MinRating       *float64
	MaxRating       *float64
	MinProgress     *float64
	MaxProgress     *float64
	AddedWithinDays int
}

// CatalogueSearch selects a page of series; unusable cursors fall back to Offset.
type CatalogueSearch struct {
	LibraryID int64
	Filters   CatalogueFilters
	Limit     int32
	Offset    int32
	SortBy    string
	Cursor    string
}

// CataloguePage is a reader-bound list projection. Items retain the efficient SQL
// aggregate shape; details and ordered reader books use BookSeriesView instead.
type CataloguePage struct {
	Subject    Subject
	Items      []database.SearchSeriesPagedRow
	Total      int
	NextCursor string
	HasMore    bool
}

func (q *CatalogueQueries) Search(ctx context.Context, subject Subject, input CatalogueSearch) (CataloguePage, error) {
	if err := subject.validate(); err != nil {
		return CataloguePage{}, err
	}
	filters := input.Filters
	storedFilters := database.SeriesListFilters{
		Keyword: filters.Keyword, Letter: filters.Letter, Status: filters.Status, Tags: filters.Tags, Authors: filters.Authors,
		ReadState: filters.ReadState, MinRating: filters.MinRating, MaxRating: filters.MaxRating,
		MinProgress: filters.MinProgress, MaxProgress: filters.MaxProgress, AddedWithinDays: filters.AddedWithinDays,
		UserID: subject.userID,
	}
	if input.Limit <= 0 {
		input.Limit = 50
	}
	if input.Offset < 0 {
		input.Offset = 0
	}
	if input.Cursor != "" {
		items, next, more, err := q.store.SearchSeriesCursor(ctx, input.LibraryID, storedFilters, input.Limit, input.SortBy, input.Cursor)
		if err == nil {
			return CataloguePage{Subject: subject, Items: items, NextCursor: next, HasMore: more}, nil
		}
		if !errors.Is(err, database.ErrSeriesCursorUnusable) {
			return CataloguePage{}, err
		}
	}
	items, total, err := q.store.SearchSeriesPaged(ctx, input.LibraryID, storedFilters, input.Limit, input.Offset, input.SortBy)
	if err != nil {
		return CataloguePage{}, err
	}
	page := CataloguePage{Subject: subject, Items: items, Total: total, HasMore: int64(input.Offset)+int64(input.Limit) < int64(total)}
	if page.HasMore && len(items) > 0 {
		page.NextCursor = database.NextSeriesSearchCursor(input.SortBy, items[len(items)-1])
	}
	return page, nil
}

// RecentSeriesView binds the aggregate recent-series list to its reader.
type RecentSeriesView struct {
	Subject Subject
	Items   []database.GetRecentReadSeriesRow
}

func (q *CatalogueQueries) Recent(ctx context.Context, subject Subject, libraryID, limit int64) (RecentSeriesView, error) {
	if err := subject.validate(); err != nil {
		return RecentSeriesView{}, err
	}
	var items []database.GetRecentReadSeriesRow
	var err error
	if subject.anonymous {
		items, err = q.store.GetRecentReadSeries(ctx, database.GetRecentReadSeriesParams{LibraryID: libraryID, LibraryID_2: libraryID, Limit: limit})
	} else {
		items, err = q.store.GetUserRecentReadSeries(ctx, subject.userID, libraryID, limit)
	}
	if err != nil {
		return RecentSeriesView{}, err
	}
	return RecentSeriesView{Subject: subject, Items: items}, nil
}
