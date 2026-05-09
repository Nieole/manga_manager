package api

import (
	"context"
	"database/sql"
	"fmt"

	"manga-manager/internal/database"
	"manga-manager/internal/parser"
)

func (c *Controller) ensurePageManifest(ctx context.Context, book database.Book) ([]database.PageManifestEntry, error) {
	pages, err := c.store.ListPageManifest(ctx, book.ID)
	if err != nil {
		return nil, err
	}
	if len(pages) > 0 {
		return pages, nil
	}

	arc, err := parser.GetArchiveFromPool(book.Path)
	if err != nil {
		return nil, err
	}

	archivePages, err := arc.GetPages()
	if err != nil {
		return nil, err
	}
	if len(archivePages) == 0 {
		return nil, fmt.Errorf("archive has no pages")
	}

	pages = make([]database.PageManifestEntry, 0, len(archivePages))
	for idx, page := range archivePages {
		pages = append(pages, database.PageManifestEntry{
			BookID:     book.ID,
			PageNumber: int64(idx + 1),
			EntryName:  page.Name,
			Size:       page.Size,
			MediaType:  page.MediaType,
		})
	}
	if err := c.store.ReplacePageManifest(ctx, book.ID, pages); err != nil {
		return nil, err
	}
	return pages, nil
}

func (c *Controller) getPageManifestEntry(ctx context.Context, book database.Book, pageNumber int64) (database.PageManifestEntry, error) {
	if pageNumber < 1 {
		return database.PageManifestEntry{}, sql.ErrNoRows
	}

	page, err := c.store.GetPageManifestEntry(ctx, book.ID, pageNumber)
	if err == nil {
		return page, nil
	}
	if err != sql.ErrNoRows {
		return database.PageManifestEntry{}, err
	}

	pages, err := c.ensurePageManifest(ctx, book)
	if err != nil {
		return database.PageManifestEntry{}, err
	}
	if pageNumber < 1 || int(pageNumber) > len(pages) {
		return database.PageManifestEntry{}, sql.ErrNoRows
	}
	return pages[pageNumber-1], nil
}
