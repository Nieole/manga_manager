package database

import "context"

// ReadingListWrite supplies only ordered list membership and identity writes.
type ReadingListWrite interface {
	GetReadingList(ctx context.Context, id int64) (ReadingList, error)
	CreateReadingList(ctx context.Context, arg CreateReadingListParams) (ReadingList, error)
	UpdateReadingList(ctx context.Context, arg UpdateReadingListParams) (ReadingList, error)
	DeleteReadingList(ctx context.Context, id int64) (int64, error)
	GetSeries(ctx context.Context, id int64) (Series, error)
	AddReadingListItem(ctx context.Context, arg AddReadingListItemParams) (ReadingListItem, error)
	RemoveReadingListItem(ctx context.Context, arg RemoveReadingListItemParams) (int64, error)
	UpdateReadingListItemSortOrder(ctx context.Context, arg UpdateReadingListItemSortOrderParams) (int64, error)
}

type readingListWrite struct{ q *Queries }

// WithReadingListWrite keeps list mutation callbacks isolated from unrelated SQL.
func (s *SqlStore) WithReadingListWrite(ctx context.Context, apply func(ReadingListWrite) error) error {
	return s.ExecTx(ctx, func(q *Queries) error { return apply(readingListWrite{q: q}) })
}
func (tx readingListWrite) GetReadingList(ctx context.Context, id int64) (ReadingList, error) {
	return tx.q.GetReadingList(ctx, id)
}
func (tx readingListWrite) CreateReadingList(ctx context.Context, arg CreateReadingListParams) (ReadingList, error) {
	return tx.q.CreateReadingList(ctx, arg)
}
func (tx readingListWrite) UpdateReadingList(ctx context.Context, arg UpdateReadingListParams) (ReadingList, error) {
	return tx.q.UpdateReadingList(ctx, arg)
}
func (tx readingListWrite) DeleteReadingList(ctx context.Context, id int64) (int64, error) {
	return tx.q.DeleteReadingList(ctx, id)
}
func (tx readingListWrite) GetSeries(ctx context.Context, id int64) (Series, error) {
	return tx.q.GetSeries(ctx, id)
}
func (tx readingListWrite) AddReadingListItem(ctx context.Context, arg AddReadingListItemParams) (ReadingListItem, error) {
	return tx.q.AddReadingListItem(ctx, arg)
}
func (tx readingListWrite) RemoveReadingListItem(ctx context.Context, arg RemoveReadingListItemParams) (int64, error) {
	return tx.q.RemoveReadingListItem(ctx, arg)
}
func (tx readingListWrite) UpdateReadingListItemSortOrder(ctx context.Context, arg UpdateReadingListItemSortOrderParams) (int64, error) {
	return tx.q.UpdateReadingListItemSortOrder(ctx, arg)
}
