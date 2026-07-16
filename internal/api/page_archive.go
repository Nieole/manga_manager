// 业务说明：本文件是业务实现，属于后端 HTTP API 层，负责把前端请求转换为数据库、扫描器、图片处理和元数据服务调用。
// 它承载资料库浏览、阅读器取页、系列维护、任务进度、系统设置和静态资源缓存等对外业务契约。
// 维护时应重点关注请求参数校验、错误语义、缓存头、并发任务状态和前后端字段兼容性。

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
	LibraryID      int64
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
		LibraryID:      book.LibraryID,
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
	pages, _, err := c.listBookArchiveSourcePagesWithStats(ctx, source)
	return pages, err
}

func (c *Controller) listBookArchiveSourcePagesWithStats(ctx context.Context, source bookPageSource) ([]parser.PageMetadata, bool, error) {
	cacheKey := bookArchivePageCacheKey(source)
	if c.pageCache != nil {
		if cached, ok := c.pageCache.Get(cacheKey); ok {
			return clonePageMetadata(cached), true, nil
		}
	}

	arc, err := parser.GetArchiveFromPool(source.Path)
	if err != nil {
		return nil, false, err
	}

	pages, err := arc.GetPages()
	if err != nil {
		return nil, false, err
	}
	if len(pages) == 0 {
		return nil, false, fmt.Errorf("archive has no pages")
	}
	if c.pageCache != nil {
		c.pageCache.Add(cacheKey, clonePageMetadata(pages))
	}
	return pages, false, nil
}

func (c *Controller) getBookArchiveSourcePageWithStats(ctx context.Context, source bookPageSource, pageNumber int64) (parser.PageMetadata, bool, error) {
	if pageNumber < 1 {
		return parser.PageMetadata{}, false, sql.ErrNoRows
	}

	pages, manifestCacheHit, err := c.listBookArchiveSourcePagesWithStats(ctx, source)
	if err != nil {
		return parser.PageMetadata{}, manifestCacheHit, err
	}
	if int(pageNumber) > len(pages) {
		return parser.PageMetadata{}, manifestCacheHit, sql.ErrNoRows
	}
	return pages[pageNumber-1], manifestCacheHit, nil
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
