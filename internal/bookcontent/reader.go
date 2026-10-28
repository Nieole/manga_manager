// Package bookcontent owns book sources, archive borrowing, image variants and cache eligibility.
// It returns materialized pages or original-content handles retaining library access until Close.
// Callers own protocol mapping and task admission.
package bookcontent

import (
	"context"
	"errors"
	"fmt"

	"manga-manager/internal/parser"
)

// ErrPageNotFound identifies an out-of-range page, independently of a transport protocol.
var ErrPageNotFound = errors.New("page not found")

// ArchiveBorrower lends an archive until its idempotent release is called.
type ArchiveBorrower func(string) (parser.Archive, func(), error)

// ArchiveReader materializes pages without retaining archive or file handles in the result.
type ArchiveReader struct{ borrow ArchiveBorrower }

// NewArchiveReader uses the shared parser pool unless a source-specific opener is supplied.
func NewArchiveReader(borrow ArchiveBorrower) *ArchiveReader {
	if borrow == nil {
		borrow = parser.GetArchiveFromPool
	}
	return &ArchiveReader{borrow: func(path string) (parser.Archive, func(), error) {
		arc, release, err := borrow(path)
		if errors.Is(err, parser.ErrArchivePublishing) {
			err = fmt.Errorf("%w: %w", ErrBusy, err)
		}
		return arc, release, err
	}}
}

// PageBytes is an independent page buffer and its archive entry facts.
type PageBytes struct {
	Data          []byte
	Page          parser.PageMetadata
	ArchiveOpened bool
}

// List returns the archive's naturally ordered readable pages and closes its borrow.
func (r *ArchiveReader) List(ctx context.Context, path string) ([]parser.PageMetadata, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	arc, release, err := r.borrow(path)
	if err != nil {
		return nil, err
	}
	defer release()
	pages, err := arc.GetPages()
	if err != nil {
		return nil, err
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("archive has no pages")
	}
	return pages, nil
}

// Page selects a one-based page and releases the archive before returning its bytes.
func (r *ArchiveReader) Page(ctx context.Context, path string, number int64) (PageBytes, error) {
	if number < 1 {
		return PageBytes{}, ErrPageNotFound
	}
	if err := ctx.Err(); err != nil {
		return PageBytes{}, err
	}
	arc, release, err := r.borrow(path)
	if err != nil {
		return PageBytes{}, err
	}
	defer release()
	pages, err := arc.GetPages()
	if err != nil {
		return PageBytes{ArchiveOpened: true}, err
	}
	if number > int64(len(pages)) {
		return PageBytes{ArchiveOpened: true}, ErrPageNotFound
	}
	page := pages[number-1]
	data, err := arc.ReadPage(page.Name)
	return PageBytes{Data: data, Page: page, ArchiveOpened: true}, err
}

// NamedPage reads an entry already selected by a manifest or ingestion operation.
func (r *ArchiveReader) NamedPage(ctx context.Context, path string, page parser.PageMetadata) (PageBytes, error) {
	if err := ctx.Err(); err != nil {
		return PageBytes{}, err
	}
	arc, release, err := r.borrow(path)
	if err != nil {
		return PageBytes{}, err
	}
	defer release()
	data, err := arc.ReadPage(page.Name)
	return PageBytes{Data: data, Page: page, ArchiveOpened: true}, err
}
