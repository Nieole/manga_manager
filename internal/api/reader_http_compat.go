package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (c *Controller) SetupOPDSRoutes(r chi.Router)  { c.readerHTTP().SetupOPDSRoutes(r) }
func (c *Controller) setupMihonRoutes(r chi.Router) { c.readerHTTP().setupMihonRoutes(r) }
func (c *Controller) getBooksBySeries(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().getBooksBySeries(w, r)
}
func (c *Controller) getBookInfo(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().getBookInfo(w, r)
}
func (c *Controller) searchSeriesPaged(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().searchSeriesPaged(w, r)
}
func (c *Controller) getRecentReadSeries(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().getRecentReadSeries(w, r)
}
func (c *Controller) getSeriesInfo(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().getSeriesInfo(w, r)
}
func (c *Controller) getSeriesTags(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().getSeriesTags(w, r)
}
func (c *Controller) getSeriesAuthors(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().getSeriesAuthors(w, r)
}
func (c *Controller) getSeriesLinks(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().getSeriesLinks(w, r)
}
func (c *Controller) getSeriesContext(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().getSeriesContext(w, r)
}
func (c *Controller) getSeriesContinueEndpoint(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().getSeriesContinueEndpoint(w, r)
}
func (c *Controller) getAllTags(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().getAllTags(w, r)
}
func (c *Controller) searchTags(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().searchTags(w, r)
}
func (c *Controller) getAllAuthors(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().getAllAuthors(w, r)
}
func (c *Controller) searchAuthors(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().searchAuthors(w, r)
}
func (c *Controller) getDashboardStats(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().getDashboardStats(w, r)
}
func (c *Controller) getActivityHeatmap(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().getActivityHeatmap(w, r)
}
func (c *Controller) getReadingStreak(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().getReadingStreak(w, r)
}
func (c *Controller) getReadingTimeStats(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().getReadingTimeStats(w, r)
}
func (c *Controller) getPeriodStats(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().getPeriodStats(w, r)
}
func (c *Controller) getRecentReadAll(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().getRecentReadAll(w, r)
}
func (c *Controller) getRecommendations(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().getRecommendations(w, r)
}
func (c *Controller) getNextBook(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().getNextBook(w, r)
}
func (c *Controller) getPrevBook(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().getPrevBook(w, r)
}
func (c *Controller) updateBookProgress(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().updateBookProgress(w, r)
}
func (c *Controller) addBookReadingTime(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().addBookReadingTime(w, r)
}
func (c *Controller) bulkSyncBookProgress(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().bulkSyncBookProgress(w, r)
}
func (c *Controller) listReadingBookmarks(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().listReadingBookmarks(w, r)
}
func (c *Controller) upsertReadingBookmark(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().upsertReadingBookmark(w, r)
}
func (c *Controller) deleteReadingBookmark(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().deleteReadingBookmark(w, r)
}
func (c *Controller) searchBooks(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().searchBooks(w, r)
}
func (c *Controller) getSeriesReview(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().getSeriesReview(w, r)
}
func (c *Controller) putSeriesReview(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().putSeriesReview(w, r)
}
func (c *Controller) deleteSeriesReview(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().deleteSeriesReview(w, r)
}
func (c *Controller) bulkUpdateBookProgress(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().bulkUpdateBookProgress(w, r)
}
func (c *Controller) bulkUpdateSeriesProgress(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().bulkUpdateSeriesProgress(w, r)
}
func (c *Controller) opdsRoot(w http.ResponseWriter, r *http.Request) { c.readerHTTP().opdsRoot(w, r) }
func (c *Controller) opdsOpenSearch(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().opdsOpenSearch(w, r)
}
func (c *Controller) opdsRecentAdded(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().opdsRecentAdded(w, r)
}
func (c *Controller) opdsCollections(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().opdsCollections(w, r)
}
func (c *Controller) opdsReadingLists(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().opdsReadingLists(w, r)
}
func (c *Controller) opdsReadingListSeries(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().opdsReadingListSeries(w, r)
}
func (c *Controller) opdsStaticCollectionSeries(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().opdsStaticCollectionSeries(w, r)
}
func (c *Controller) opdsSmartCollectionSeries(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().opdsSmartCollectionSeries(w, r)
}
func (c *Controller) opdsSearch(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().opdsSearch(w, r)
}
func (c *Controller) opdsLibraries(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().opdsLibraries(w, r)
}
func (c *Controller) opdsLibrarySeries(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().opdsLibrarySeries(w, r)
}
func (c *Controller) opdsSeriesBooks(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().opdsSeriesBooks(w, r)
}
func (c *Controller) opdsStreamPageImage(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().opdsStreamPageImage(w, r)
}
func (c *Controller) opdsContinueReading(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().opdsContinueReading(w, r)
}
func (c *Controller) mihonLibraries(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().mihonLibraries(w, r)
}
func (c *Controller) mihonRecentlyAdded(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().mihonRecentlyAdded(w, r)
}
func (c *Controller) mihonContinueReading(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().mihonContinueReading(w, r)
}
func (c *Controller) mihonCollections(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().mihonCollections(w, r)
}
func (c *Controller) mihonReadingLists(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().mihonReadingLists(w, r)
}
func (c *Controller) mihonReadingListSeries(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().mihonReadingListSeries(w, r)
}
func (c *Controller) mihonCollectionSeries(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().mihonCollectionSeries(w, r)
}
func (c *Controller) mihonSmartCollectionSeries(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().mihonSmartCollectionSeries(w, r)
}
func (c *Controller) mihonSeries(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().mihonSeries(w, r)
}
func (c *Controller) mihonSeriesDetail(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().mihonSeriesDetail(w, r)
}
func (c *Controller) mihonSeriesBooks(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().mihonSeriesBooks(w, r)
}
func (c *Controller) mihonBookPages(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().mihonBookPages(w, r)
}

func (c *Controller) getSeriesByLibrary(w http.ResponseWriter, r *http.Request) {
	c.readerHTTP().getSeriesByLibrary(w, r)
}

// Compatibility construction remains for embedded callers until ticket 47.
func (c *Controller) newReaderHTTP() *ReaderHTTP {
	caps := NewReaderCapabilities(c.store, c.relations, c.proposals, c.taskEngine.engine, func() { c.stats.InvalidateVolatile("reading") })
	return c.bindReaderHTTP(caps)
}
func (c *Controller) bindReaderHTTP(caps *ReaderCapabilities) *ReaderHTTP {
	return &ReaderHTTP{ReaderCapabilities: caps, store: caps.queries, stats: c.stats, recommendations: c.recommendations, content: c.content, currentConfig: c.currentConfig, failedRuns: c.taskEngine.statusesFrom, requireProtocolEnabled: c.requireProtocolEnabled, requireBasicAuth: c.requireBasicAuth, serveBookFile: c.serveBookFile, serveCoverImage: c.serveCoverImage, serveThumbnailImage: c.serveThumbnailImage, servePageImage: c.servePageImage, servePageImageByNumber: c.servePageImageByNumber, guardBookRead: c.guardBookRead, readingLists: c.readingLists, loadCollectionViews: c.loadCollectionViews, loadStaticCollectionSeries: c.loadStaticCollectionSeries, loadSmartCollectionSeries: c.loadSmartCollectionSeries, getSmartFilterByID: c.getSmartFilterByID}
}
func (c *Controller) readerHTTP() *ReaderHTTP {
	if c.reader != nil {
		return c.reader
	}
	return c.newReaderHTTP()
}
