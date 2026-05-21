package api

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/parser"
)

const bookPageSourceCacheTTL = 30 * time.Second

type bookPageSource struct {
	ID             int64
	Path           string
	FileModifiedAt time.Time
	Size           int64
	PageCount      int64
}

type cachedBookPageSource struct {
	source   bookPageSource
	cachedAt time.Time
}

func (c *Controller) getBookPageSource(ctx context.Context, bookID int64) (bookPageSource, error) {
	now := time.Now()
	if c.bookPageSourceCache != nil {
		if cached, ok := c.bookPageSourceCache.Get(bookID); ok && now.Sub(cached.cachedAt) < bookPageSourceCacheTTL {
			return cached.source, nil
		}
	}

	book, err := c.store.GetBook(ctx, bookID)
	if err != nil {
		return bookPageSource{}, err
	}
	source := bookPageSourceFromBook(book)
	if c.bookPageSourceCache != nil {
		c.bookPageSourceCache.Add(bookID, cachedBookPageSource{source: source, cachedAt: now})
	}
	return source, nil
}

func bookPageSourceFromBook(book database.Book) bookPageSource {
	return bookPageSource{
		ID:             book.ID,
		Path:           book.Path,
		FileModifiedAt: book.FileModifiedAt,
		Size:           book.Size,
		PageCount:      book.PageCount,
	}
}

func (c *Controller) listBookArchivePages(ctx context.Context, book database.Book) ([]parser.PageMetadata, error) {
	return c.listBookArchiveSourcePages(ctx, bookPageSourceFromBook(book))
}

func (c *Controller) listBookArchiveSourcePages(ctx context.Context, source bookPageSource) ([]parser.PageMetadata, error) {
	cacheKey := bookArchivePageCacheKey(source)
	if c.pageCache != nil {
		if cached, ok := c.pageCache.Get(cacheKey); ok {
			return clonePageMetadata(cached), nil
		}
	}

	arc, err := parser.GetArchiveFromPool(source.Path)
	if err != nil {
		return nil, err
	}

	pages, err := arc.GetPages()
	if err != nil {
		return nil, err
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("archive has no pages")
	}
	if c.pageCache != nil {
		c.pageCache.Add(cacheKey, clonePageMetadata(pages))
	}
	return pages, nil
}

func (c *Controller) getBookArchivePage(ctx context.Context, book database.Book, pageNumber int64) (parser.PageMetadata, error) {
	return c.getBookArchiveSourcePage(ctx, bookPageSourceFromBook(book), pageNumber)
}

func (c *Controller) getBookArchiveSourcePage(ctx context.Context, source bookPageSource, pageNumber int64) (parser.PageMetadata, error) {
	if pageNumber < 1 {
		return parser.PageMetadata{}, sql.ErrNoRows
	}

	pages, err := c.listBookArchiveSourcePages(ctx, source)
	if err != nil {
		return parser.PageMetadata{}, err
	}
	if int(pageNumber) > len(pages) {
		return parser.PageMetadata{}, sql.ErrNoRows
	}
	return pages[pageNumber-1], nil
}

func bookArchivePageCacheKey(source bookPageSource) string {
	return strconv.FormatInt(source.ID, 10) + "|" + source.Path + "|" + strconv.FormatInt(source.FileModifiedAt.UnixNano(), 10) + "|" + strconv.FormatInt(source.Size, 10)
}

func (c *Controller) purgeReadingPathCaches() {
	if c.bookPageSourceCache != nil {
		c.bookPageSourceCache.Purge()
	}
	if c.pageCache != nil {
		c.pageCache.Purge()
	}
}

func clonePageMetadata(pages []parser.PageMetadata) []parser.PageMetadata {
	if pages == nil {
		return nil
	}
	cloned := make([]parser.PageMetadata, len(pages))
	copy(cloned, pages)
	return cloned
}
