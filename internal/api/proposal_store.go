package api

import (
	"context"

	"manga-manager/internal/database"
	"manga-manager/internal/proposal"
)

// proposalDB 是 proposal.Database 的生产实现。模块的任何签名都不提及 database.Store，
// 两者在这里接上：*database.Queries 天然满足 proposal.Queries，只需把事务回调换个形状。
type proposalDB struct {
	store database.Store
}

func (d proposalDB) ExecTx(ctx context.Context, fn func(proposal.Queries) error) error {
	return d.store.ExecTx(ctx, func(q *database.Queries) error { return fn(q) })
}
