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

	lru "github.com/hashicorp/golang-lru/v2"
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

// pageArchiveCache 把「阅读路径上的两级只读缓存」收成一个组件。
//
// 二者的生命周期是绑定的：都以「某本书在磁盘上的那个归档文件」为事实来源，
// 因此库结构一变（扫描、删库、重建缩略图）就必须一起失效——purgeReadingPathCaches
// 早就是这么用的，只是此前 Controller 上挂着两个裸字段，让「必须同时清」这条约束
// 只存在于那个函数体里，新增调用点很容易只清一个。
//
//	source:   bookID → 归档路径/mtime/size（带 TTL，避免每次取页都查一次 books）
//	manifest: 归档路径+签名 → 页清单（不带 TTL，靠 mtime+size 进 key 天然失效）
type pageArchiveCache struct {
	source   *lru.Cache[int64, cachedBookPageSource]
	manifest *lru.Cache[string, []parser.PageMetadata]
}

func newPageArchiveCache(sourceSize, manifestSize int) *pageArchiveCache {
	source, _ := lru.New[int64, cachedBookPageSource](sourceSize)
	manifest, _ := lru.New[string, []parser.PageMetadata](manifestSize)
	return &pageArchiveCache{source: source, manifest: manifest}
}

// lookupSource 返回未过期的书籍归档来源。
func (p *pageArchiveCache) lookupSource(bookID int64, now time.Time) (bookPageSource, bool) {
	if p == nil || p.source == nil {
		return bookPageSource{}, false
	}
	cached, ok := p.source.Get(bookID)
	if !ok || now.Sub(cached.cachedAt) >= bookPageSourceCacheTTL {
		return bookPageSource{}, false
	}
	return cached.source, true
}

func (p *pageArchiveCache) storeSource(bookID int64, source bookPageSource, now time.Time) {
	if p == nil || p.source == nil {
		return
	}
	p.source.Add(bookID, cachedBookPageSource{source: source, cachedAt: now})
}

// lookupManifest 返回归档页清单的副本（调用方可能排序/切片，不能交出缓存里的底层数组）。
func (p *pageArchiveCache) lookupManifest(key string) ([]parser.PageMetadata, bool) {
	if p == nil || p.manifest == nil {
		return nil, false
	}
	cached, ok := p.manifest.Get(key)
	if !ok {
		return nil, false
	}
	return clonePageMetadata(cached), true
}

func (p *pageArchiveCache) storeManifest(key string, pages []parser.PageMetadata) {
	if p == nil || p.manifest == nil {
		return
	}
	p.manifest.Add(key, clonePageMetadata(pages))
}

// purge 同时清空两级缓存。库结构变化后必须调用，缺一不可。
func (p *pageArchiveCache) purge() {
	if p == nil {
		return
	}
	if p.source != nil {
		p.source.Purge()
	}
	if p.manifest != nil {
		p.manifest.Purge()
	}
}

func (c *Controller) getBookPageSource(ctx context.Context, bookID int64) (bookPageSource, error) {
	now := time.Now()
	if source, ok := c.pageArchive.lookupSource(bookID, now); ok {
		return source, nil
	}

	book, err := c.store.GetBook(ctx, bookID)
	if err != nil {
		return bookPageSource{}, err
	}
	source := bookPageSourceFromBook(book)
	c.pageArchive.storeSource(bookID, source, now)
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
	if cached, ok := c.pageArchive.lookupManifest(cacheKey); ok {
		return cached, true, nil
	}

	arc, releaseArchive, err := parser.GetArchiveFromPool(source.Path)
	if err != nil {
		return nil, false, err
	}
	defer releaseArchive()

	pages, err := arc.GetPages()
	if err != nil {
		return nil, false, err
	}
	if len(pages) == 0 {
		return nil, false, fmt.Errorf("archive has no pages")
	}
	c.pageArchive.storeManifest(cacheKey, pages)
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
	c.pageArchive.purge()
}

func clonePageMetadata(pages []parser.PageMetadata) []parser.PageMetadata {
	if pages == nil {
		return nil
	}
	cloned := make([]parser.PageMetadata, len(pages))
	copy(cloned, pages)
	return cloned
}
