package database

import "context"

// AIGroupingWrite supplies only the transaction capabilities for this use case.
type AIGroupingWrite interface {
	GetLibrary(ctx context.Context, id int64) (Library, error)
	CollectionCreateWrite
	GetAIGroupingReview(ctx context.Context, id int64) (AiGroupingReview, error)
	GetAIGroupingReviewCollection(ctx context.Context, id int64) (AiGroupingReviewCollection, error)
	ListAIGroupingReviewCollections(ctx context.Context, reviewID int64) ([]AiGroupingReviewCollection, error)
	CountPendingAIGroupingReviewCollections(ctx context.Context, reviewID int64) (int64, error)
	CountAppliedAIGroupingReviewCollections(ctx context.Context, reviewID int64) (int64, error)
	MarkAIGroupingReviewCollectionApplied(ctx context.Context, arg MarkAIGroupingReviewCollectionAppliedParams) (int64, error)
	MarkAIGroupingReviewCollectionRejected(ctx context.Context, id int64) error
	MarkAIGroupingReviewCollectionsRejected(ctx context.Context, reviewID int64) error
	UpdateAIGroupingReviewCollection(ctx context.Context, arg UpdateAIGroupingReviewCollectionParams) (AiGroupingReviewCollection, error)
	UpdateAIGroupingReviewStatus(ctx context.Context, arg UpdateAIGroupingReviewStatusParams) (AiGroupingReview, error)
}
type aiGroupingWrite struct{ collectionCreateWrite }

func (s *SqlStore) WithAIGroupingWrite(ctx context.Context, apply func(AIGroupingWrite) error) error {
	return s.ExecTx(ctx, func(q *Queries) error { return apply(aiGroupingWrite{collectionCreateWrite{q: q}}) })
}
func (tx aiGroupingWrite) GetAIGroupingReview(ctx context.Context, id int64) (AiGroupingReview, error) {
	return tx.q.GetAIGroupingReview(ctx, id)
}
func (tx aiGroupingWrite) GetAIGroupingReviewCollection(ctx context.Context, id int64) (AiGroupingReviewCollection, error) {
	return tx.q.GetAIGroupingReviewCollection(ctx, id)
}
func (tx aiGroupingWrite) ListAIGroupingReviewCollections(ctx context.Context, reviewID int64) ([]AiGroupingReviewCollection, error) {
	return tx.q.ListAIGroupingReviewCollections(ctx, reviewID)
}
func (tx aiGroupingWrite) CountPendingAIGroupingReviewCollections(ctx context.Context, reviewID int64) (int64, error) {
	return tx.q.CountPendingAIGroupingReviewCollections(ctx, reviewID)
}
func (tx aiGroupingWrite) CountAppliedAIGroupingReviewCollections(ctx context.Context, reviewID int64) (int64, error) {
	return tx.q.CountAppliedAIGroupingReviewCollections(ctx, reviewID)
}
func (tx aiGroupingWrite) MarkAIGroupingReviewCollectionApplied(ctx context.Context, arg MarkAIGroupingReviewCollectionAppliedParams) (int64, error) {
	return tx.q.MarkAIGroupingReviewCollectionApplied(ctx, arg)
}
func (tx aiGroupingWrite) MarkAIGroupingReviewCollectionRejected(ctx context.Context, id int64) error {
	return tx.q.MarkAIGroupingReviewCollectionRejected(ctx, id)
}
func (tx aiGroupingWrite) MarkAIGroupingReviewCollectionsRejected(ctx context.Context, reviewID int64) error {
	return tx.q.MarkAIGroupingReviewCollectionsRejected(ctx, reviewID)
}
func (tx aiGroupingWrite) UpdateAIGroupingReviewCollection(ctx context.Context, arg UpdateAIGroupingReviewCollectionParams) (AiGroupingReviewCollection, error) {
	return tx.q.UpdateAIGroupingReviewCollection(ctx, arg)
}
func (tx aiGroupingWrite) UpdateAIGroupingReviewStatus(ctx context.Context, arg UpdateAIGroupingReviewStatusParams) (AiGroupingReview, error) {
	return tx.q.UpdateAIGroupingReviewStatus(ctx, arg)
}

// AIGroupingGenerationWrite supplies only the transaction capabilities for this use case.
type AIGroupingGenerationWrite interface {
	CreateAIGroupingReview(ctx context.Context, arg CreateAIGroupingReviewParams) (AiGroupingReview, error)
	CreateAIGroupingReviewCollection(ctx context.Context, arg CreateAIGroupingReviewCollectionParams) (AiGroupingReviewCollection, error)
}
type aiGroupingGenerationWrite struct{ q *Queries }

func (s *SqlStore) WithAIGroupingGenerationWrite(ctx context.Context, apply func(AIGroupingGenerationWrite) error) error {
	return s.ExecTx(ctx, func(q *Queries) error { return apply(aiGroupingGenerationWrite{q: q}) })
}
func (tx aiGroupingGenerationWrite) CreateAIGroupingReview(ctx context.Context, arg CreateAIGroupingReviewParams) (AiGroupingReview, error) {
	return tx.q.CreateAIGroupingReview(ctx, arg)
}
func (tx aiGroupingGenerationWrite) CreateAIGroupingReviewCollection(ctx context.Context, arg CreateAIGroupingReviewCollectionParams) (AiGroupingReviewCollection, error) {
	return tx.q.CreateAIGroupingReviewCollection(ctx, arg)
}

func (tx aiGroupingWrite) GetLibrary(ctx context.Context, id int64) (Library, error) {
	return tx.q.GetLibrary(ctx, id)
}
