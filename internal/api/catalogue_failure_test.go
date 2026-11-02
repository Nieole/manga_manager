package api

import (
	"manga-manager/internal/catalogsearch"
	"manga-manager/internal/seriescontext"

	"context"
	"errors"
	"manga-manager/internal/database"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

type failedSeriesSearch struct{ database.Store }

func (failedSeriesSearch) SearchGlobalSeries(context.Context, string, int32) ([]database.SeriesSearchHit, error) {
	return nil, errors.New("unavailable")
}
func TestCatalogueSearchFailureIsNotEmptySuccess(t *testing.T) {
	c, store, _, _ := newTestController(t)
	c.reader.search = catalogsearch.New(failedSeriesSearch{store})
	rec := httptest.NewRecorder()
	c.reader.searchBooks(rec, httptest.NewRequest(http.MethodGet, "/?q=anything&target=series", nil))
	if rec.Code != 500 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

type failedContextTags struct{ database.Store }

func (failedContextTags) GetTagsForSeries(context.Context, int64) ([]database.Tag, error) {
	return nil, errors.New("tags unavailable")
}
func TestSeriesContextAncillaryFailureIsNotEmptySuccess(t *testing.T) {
	c, store, _, root := newTestController(t)
	_, series, _ := seedBookFixture(t, store, root, "Library", "Series", "01.cbz", 10)
	c.reader.context = seriescontext.New(failedContextTags{store}, c.relations, c.proposals, c.taskEngine.engine)
	rec := httptest.NewRecorder()
	c.reader.getSeriesContext(rec, requestWithRouteParam(http.MethodGet, "/", nil, "seriesId", strconv.FormatInt(series.ID, 10)))
	if rec.Code != 500 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

type failedReaderFacts struct{ database.Store }

func (failedReaderFacts) GetUserReadBooksCount(context.Context, int64) (int64, error) {
	return 0, errors.New("reader facts unavailable")
}
func (failedReaderFacts) GetUserTopReadingTags(context.Context, int64, int64) ([]database.GetTopReadingTagsRow, error) {
	return nil, errors.New("preferences unavailable")
}
func TestDashboardFailureDoesNotReturnGlobalReaderFacts(t *testing.T) {
	c, store, _, _ := newTestController(t)
	c.reader.store = failedReaderFacts{store}
	rec := httptest.NewRecorder()
	c.reader.getDashboardStats(rec, withUserContext(httptest.NewRequest(http.MethodGet, "/", nil), database.User{ID: 1}))
	if rec.Code != 500 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
func TestRecommendationPreferenceFailureIsNotEmptySuccess(t *testing.T) {
	c, store, _, _ := newTestController(t)
	c.reader.store = failedReaderFacts{store}
	rec := httptest.NewRecorder()
	c.reader.getRecommendations(rec, withUserContext(httptest.NewRequest(http.MethodGet, "/?refresh=true", nil), database.User{ID: 1}))
	if rec.Code != 500 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
