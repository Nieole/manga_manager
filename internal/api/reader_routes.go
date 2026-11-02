package api

import "github.com/go-chi/chi/v5"

// SetupRoutes mounts the session-authenticated reader API inside /api.
func (c *ReaderHTTP) SetupRoutes(r chi.Router) {
	c.setupRootRoutes(r)
	r.Route("/series", c.setupSeriesRoutes)
	r.Route("/books", c.setupBooksRoutes)
	r.Route("/tags", c.setupTagsRoutes)
	r.Route("/authors", c.setupAuthorsRoutes)
}

func (c *ReaderHTTP) setupRootRoutes(r chi.Router) {
	r.Get("/search", c.searchBooks)
	r.Get("/recommendations", c.getRecommendations)
	r.Get("/stats/dashboard", c.getDashboardStats)
	r.Get("/stats/activity-heatmap", c.getActivityHeatmap)
	r.Get("/stats/recent-read", c.getRecentReadAll)
	r.Get("/stats/recommendations", c.getRecommendations)
	r.Get("/stats/streak", c.getReadingStreak)
	r.Get("/stats/reading-time", c.getReadingTimeStats)
	r.Get("/stats/period", c.getPeriodStats)
	r.Get("/book-info/{bookId}", c.getBookInfo)
	r.Get("/book-next/{bookId}", c.getNextBook)
	r.Get("/book-prev/{bookId}", c.getPrevBook)
}

func (c *ReaderHTTP) setupSeriesRoutes(r chi.Router) {
	r.Get("/{libraryId}", c.getSeriesByLibrary)
	r.Post("/bulk-progress", c.bulkUpdateSeriesProgress)
	r.Get("/search", c.searchSeriesPaged)
	r.Get("/recent-read", c.getRecentReadSeries)
	r.Get("/info/{seriesId}", c.getSeriesInfo)
	r.Get("/{seriesId}/tags", c.getSeriesTags)
	r.Get("/{seriesId}/custom-fields", c.getSeriesCustomFields)
	r.Get("/{seriesId}/review", c.getSeriesReview)
	r.Put("/{seriesId}/review", c.putSeriesReview)
	r.Delete("/{seriesId}/review", c.deleteSeriesReview)
	r.Get("/{seriesId}/authors", c.getSeriesAuthors)
	r.Get("/{seriesId}/links", c.getSeriesLinks)
	r.Get("/{seriesId}/context", c.getSeriesContext)
	r.Get("/{seriesId}/continue", c.getSeriesContinueEndpoint)
}

func (c *ReaderHTTP) setupBooksRoutes(r chi.Router) {
	r.Post("/bulk-progress", c.bulkUpdateBookProgress)
	r.Post("/bulk-progress/sync", c.bulkSyncBookProgress)
	r.Post("/{bookId}/progress", c.updateBookProgress)
	r.Post("/{bookId}/reading-time", c.addBookReadingTime)
	r.Get("/{bookId}/bookmarks", c.listReadingBookmarks)
	r.Post("/{bookId}/bookmarks", c.upsertReadingBookmark)
	r.Delete("/{bookId}/bookmarks/{bookmarkId}", c.deleteReadingBookmark)
	r.Get("/{seriesId}", c.getBooksBySeries)
}

func (c *ReaderHTTP) setupTagsRoutes(r chi.Router) {
	r.Get("/all", c.getAllTags)
	r.Get("/search", c.searchTags)
}

func (c *ReaderHTTP) setupAuthorsRoutes(r chi.Router) {
	r.Get("/all", c.getAllAuthors)
	r.Get("/search", c.searchAuthors)
}
