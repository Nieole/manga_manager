package api

import (
	"context"

	"manga-manager/internal/database"
	"manga-manager/internal/proposal"
)

// proposalDB 是 proposal.Database 的生产实现。模块的任何签名都不提及 database.Store，
// 两者在这里接上：全量存储与 *database.Queries 都天然满足 proposal.Queries，
// 事务外的读直接由内嵌的 Store 提供，只有事务回调需要换个形状。
type proposalDB struct {
	database.Store
}

// ExecTx 遮蔽内嵌 Store 的同名方法：模块只认 proposal.Queries 这个收窄后的形状。
func (d proposalDB) ExecTx(ctx context.Context, fn func(proposal.Queries) error) error {
	return d.Store.ExecTx(ctx, func(q *database.Queries) error { return fn(q) })
}
