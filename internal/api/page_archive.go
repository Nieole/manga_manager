package api

import (
	"context"
	"database/sql"
	"fmt"

	"manga-manager/internal/database"
	"manga-manager/internal/parser"
)

func (c *Controller) listBookArchivePages(ctx context.Context, book database.Book) ([]parser.PageMetadata, error) {
	arc, err := parser.GetArchiveFromPool(book.Path)
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
	return pages, nil
}

func (c *Controller) getBookArchivePage(ctx context.Context, book database.Book, pageNumber int64) (parser.PageMetadata, error) {
	if pageNumber < 1 {
		return parser.PageMetadata{}, sql.ErrNoRows
	}

	pages, err := c.listBookArchivePages(ctx, book)
	if err != nil {
		return parser.PageMetadata{}, err
	}
	if int(pageNumber) > len(pages) {
		return parser.PageMetadata{}, sql.ErrNoRows
	}
	return pages[pageNumber-1], nil
}
