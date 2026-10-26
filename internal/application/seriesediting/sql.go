package seriesediting

import (
	"context"

	"manga-manager/internal/database"
)

// SQLTransactions is used only by the SQLite adapter; use cases receive Store.
type SQLTransactions interface {
	ExecTx(context.Context, func(*database.Queries) error) error
}

type sqlStore struct{ transactions SQLTransactions }

// NewSQLStore adapts existing SQLite transactions without exporting raw queries to the use case.
func NewSQLStore(transactions SQLTransactions) Store { return sqlStore{transactions: transactions} }

func (s sqlStore) Edit(ctx context.Context, fn func(Transaction) error) error {
	return s.transactions.ExecTx(ctx, func(q *database.Queries) error { return fn(q) })
}
