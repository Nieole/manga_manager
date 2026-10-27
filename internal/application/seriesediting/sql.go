package seriesediting

import (
	"context"

	"manga-manager/internal/database"
)

// SQLTransactions is used only by the SQLite adapter; use cases consume scoped stores.
type SQLTransactions interface {
	ExecTx(context.Context, func(*database.Queries) error) error
	RefreshSeriesStats(context.Context, int64) error
}

type sqlStore struct{ transactions SQLTransactions }

// SQLStore exposes adapter capabilities; each editor consumes its own storage interface.
type SQLStore interface {
	Store
	SelectionStore
}

// NewSQLStore adapts existing SQLite transactions without exporting raw queries to the use case.
func NewSQLStore(transactions SQLTransactions) SQLStore { return sqlStore{transactions: transactions} }

func (s sqlStore) Edit(ctx context.Context, fn func(Transaction) error) error {
	return s.transactions.ExecTx(ctx, func(q *database.Queries) error { return fn(q) })
}

func (s sqlStore) EditSelection(ctx context.Context, fn func(SelectionTransaction) error) error {
	return s.transactions.ExecTx(ctx, func(q *database.Queries) error { return fn(q) })
}

func (s sqlStore) RefreshSeriesStats(ctx context.Context, id int64) error {
	return s.transactions.RefreshSeriesStats(ctx, id)
}
