package seriesmeta

import (
	"context"

	"manga-manager/internal/database"
	"manga-manager/internal/parser"
)

// ScanAssociationQueries is the scanner's transaction-bound append capability.
type ScanAssociationQueries interface {
	UpsertTag(context.Context, string) (database.Tag, error)
	LinkSeriesTag(context.Context, database.LinkSeriesTagParams) error
	UpsertAuthor(context.Context, database.UpsertAuthorParams) (database.Author, error)
	LinkSeriesAuthor(context.Context, database.LinkSeriesAuthorParams) error
}

// AppendScanAssociations appends ComicInfo tags and authors, preserving existing
// associations. Collection locks and individual write failures do not stop scanning.
func AppendScanAssociations(ctx context.Context, q ScanAssociationQueries, seriesID int64, info *parser.ComicInfo) {
	if info == nil {
		return
	}
	for _, tag := range info.GetTags() {
		if inserted, err := q.UpsertTag(ctx, tag); err == nil {
			_ = q.LinkSeriesTag(ctx, database.LinkSeriesTagParams{SeriesID: seriesID, TagID: inserted.ID})
		}
	}
	for _, author := range info.GetAuthors() {
		if inserted, err := q.UpsertAuthor(ctx, database.UpsertAuthorParams{Name: author.Name, Role: author.Role}); err == nil {
			_ = q.LinkSeriesAuthor(ctx, database.LinkSeriesAuthorParams{SeriesID: seriesID, AuthorID: inserted.ID})
		}
	}
}
