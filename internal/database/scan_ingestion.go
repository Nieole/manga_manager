package database

import "context"

// ScanIngestionWrite is the batch's series, book, evidence and append-only association capability.
// It cannot read or mutate unrelated features or expose a database handle.
type ScanIngestionWrite interface {
	UpsertSeriesByPath(context.Context, UpsertSeriesByPathParams) (Series, error)
	RehomeBookPath(context.Context, RehomeBookPathParams) (int64, error)
	UpsertBookByPath(context.Context, UpsertBookByPathParams) (Book, error)
	UpsertBookByPathKeepingPagesAndCover(context.Context, UpsertBookByPathKeepingPagesAndCoverParams) (Book, error)
	UpdateBookIdentity(context.Context, UpdateBookIdentityParams) error
	UpsertTag(context.Context, string) (Tag, error)
	LinkSeriesTag(context.Context, LinkSeriesTagParams) error
	UpsertAuthor(context.Context, UpsertAuthorParams) (Author, error)
	LinkSeriesAuthor(context.Context, LinkSeriesAuthorParams) error
}

// WithScanIngestion keeps the callback's capability scoped to one SQLite write transaction.
func (s *SqlStore) WithScanIngestion(ctx context.Context, apply func(ScanIngestionWrite) error) error {
	return s.ExecTx(ctx, func(q *Queries) error { return apply(scanIngestionWrite{q: q}) })
}

type scanIngestionWrite struct{ q *Queries }

func (tx scanIngestionWrite) UpsertSeriesByPath(ctx context.Context, p UpsertSeriesByPathParams) (Series, error) {
	return tx.q.UpsertSeriesByPath(ctx, p)
}
func (tx scanIngestionWrite) RehomeBookPath(ctx context.Context, p RehomeBookPathParams) (int64, error) {
	return tx.q.RehomeBookPath(ctx, p)
}
func (tx scanIngestionWrite) UpsertBookByPath(ctx context.Context, p UpsertBookByPathParams) (Book, error) {
	return tx.q.UpsertBookByPath(ctx, p)
}
func (tx scanIngestionWrite) UpsertBookByPathKeepingPagesAndCover(ctx context.Context, p UpsertBookByPathKeepingPagesAndCoverParams) (Book, error) {
	return tx.q.UpsertBookByPathKeepingPagesAndCover(ctx, p)
}
func (tx scanIngestionWrite) UpdateBookIdentity(ctx context.Context, p UpdateBookIdentityParams) error {
	return tx.q.UpdateBookIdentity(ctx, p)
}
func (tx scanIngestionWrite) UpsertTag(ctx context.Context, p string) (Tag, error) {
	return tx.q.UpsertTag(ctx, p)
}
func (tx scanIngestionWrite) LinkSeriesTag(ctx context.Context, p LinkSeriesTagParams) error {
	return tx.q.LinkSeriesTag(ctx, p)
}
func (tx scanIngestionWrite) UpsertAuthor(ctx context.Context, p UpsertAuthorParams) (Author, error) {
	return tx.q.UpsertAuthor(ctx, p)
}
func (tx scanIngestionWrite) LinkSeriesAuthor(ctx context.Context, p LinkSeriesAuthorParams) error {
	return tx.q.LinkSeriesAuthor(ctx, p)
}
