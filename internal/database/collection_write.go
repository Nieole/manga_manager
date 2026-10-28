package database

import "context"

// CollectionCreateWrite supplies only named collection creation and member validation.
type CollectionCreateWrite interface {
	CollectionNameExists(ctx context.Context, name string) (int64, error)
	GetStaticCollectionView(ctx context.Context, id int64) (GetStaticCollectionViewRow, error)
	CreateCollection(ctx context.Context, input CreateCollectionParams) (Collection, error)
	AddSeriesToCollection(ctx context.Context, input AddSeriesToCollectionParams) (int64, error)
	TouchCollection(ctx context.Context, id int64) error
	GetSeriesNamesByIDs(ctx context.Context, ids []int64) ([]GetSeriesNamesByIDsRow, error)
}

// CollectionWrite adds the edits owned by manual collection use cases.
type CollectionWrite interface {
	CollectionCreateWrite
	UpdateCollectionDetails(ctx context.Context, input UpdateCollectionDetailsParams) error
	DeleteCollection(ctx context.Context, id int64) (int64, error)
	RemoveSeriesFromCollection(ctx context.Context, input RemoveSeriesFromCollectionParams) (int64, error)
}

// SmartSnapshotWrite reads current saved conditions without exposing manual edits.
type SmartSnapshotWrite interface {
	CollectionCreateWrite
	GetSmartFilterByID(ctx context.Context, id int64) (SmartFilter, error)
	SearchSmartCollectionSeries(ctx context.Context, filter SmartCollectionFilter, limit, offset int) ([]SearchSeriesPagedRow, int, error)
}

// WithCollectionWrite never exposes the underlying sqlc handle through its callback.
func (s *SqlStore) WithCollectionWrite(ctx context.Context, apply func(CollectionWrite) error) error {
	return s.ExecTx(ctx, func(q *Queries) error { return apply(collectionWrite{collectionCreateWrite{q: q}}) })
}

// WithSmartSnapshot reads the shelf and creates its manual snapshot under the same write lock.
func (s *SqlStore) WithSmartSnapshot(ctx context.Context, apply func(SmartSnapshotWrite) error) error {
	return s.ExecTx(ctx, func(q *Queries) error { return apply(smartSnapshotWrite{collectionCreateWrite{q: q}}) })
}

type collectionCreateWrite struct{ q *Queries }
type collectionWrite struct{ collectionCreateWrite }
type smartSnapshotWrite struct{ collectionCreateWrite }

func (tx collectionCreateWrite) CollectionNameExists(ctx context.Context, name string) (int64, error) {
	return tx.q.CollectionNameExists(ctx, name)
}
func (tx collectionCreateWrite) GetStaticCollectionView(ctx context.Context, id int64) (GetStaticCollectionViewRow, error) {
	return tx.q.GetStaticCollectionView(ctx, id)
}
func (tx collectionCreateWrite) CreateCollection(ctx context.Context, input CreateCollectionParams) (Collection, error) {
	return tx.q.CreateCollection(ctx, input)
}
func (tx collectionWrite) UpdateCollectionDetails(ctx context.Context, input UpdateCollectionDetailsParams) error {
	return tx.q.UpdateCollectionDetails(ctx, input)
}
func (tx collectionWrite) DeleteCollection(ctx context.Context, id int64) (int64, error) {
	return tx.q.DeleteCollection(ctx, id)
}
func (tx collectionCreateWrite) AddSeriesToCollection(ctx context.Context, input AddSeriesToCollectionParams) (int64, error) {
	return tx.q.AddSeriesToCollection(ctx, input)
}
func (tx collectionWrite) RemoveSeriesFromCollection(ctx context.Context, input RemoveSeriesFromCollectionParams) (int64, error) {
	return tx.q.RemoveSeriesFromCollection(ctx, input)
}
func (tx collectionCreateWrite) TouchCollection(ctx context.Context, id int64) error {
	return tx.q.TouchCollection(ctx, id)
}
func (tx collectionCreateWrite) GetSeriesNamesByIDs(ctx context.Context, ids []int64) ([]GetSeriesNamesByIDsRow, error) {
	return tx.q.GetSeriesNamesByIDs(ctx, ids)
}
func (tx smartSnapshotWrite) GetSmartFilterByID(ctx context.Context, id int64) (SmartFilter, error) {
	return tx.q.GetSmartFilterByID(ctx, id)
}
func (tx smartSnapshotWrite) SearchSmartCollectionSeries(ctx context.Context, filter SmartCollectionFilter, limit, offset int) ([]SearchSeriesPagedRow, int, error) {
	return tx.q.SearchSmartCollectionSeries(ctx, filter, limit, offset)
}
