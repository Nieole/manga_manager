// 本文件是本包与数据库之间的接缝，以及子域入口 Service 的构造。

package proposal

import (
	"context"

	"manga-manager/internal/database"
)

// Queries 是提案裁决在一次事务内要用到的查询集合。生产上 *database.Queries 天然满足它，
// 装配处负责把全量存储包成这个形状。
//
// 字段行查询只列批量版：逐条版留在接口之外，「在一批提案上逐条取字段行」这类 N+1
// 因此在本包内写不出来。参数与返回值仍是 database 包的类型，收窄换来的只有这条编译期约束。
type Queries interface {
	GetTagsForSeries(ctx context.Context, seriesID int64) ([]database.Tag, error)
	GetAuthorsForSeries(ctx context.Context, seriesID int64) ([]database.Author, error)
	ListPendingMetadataReviewsBySeries(ctx context.Context, seriesID int64) ([]database.MetadataReview, error)
	ListRecentRejectedMetadataReviewsBySeries(ctx context.Context, arg database.ListRecentRejectedMetadataReviewsBySeriesParams) ([]database.MetadataReview, error)
	ListMetadataReviewFieldsByReviews(ctx context.Context, reviewIDs []int64) ([]database.MetadataReviewField, error)
	CreateMetadataReview(ctx context.Context, arg database.CreateMetadataReviewParams) (database.MetadataReview, error)
	CreateMetadataReviewField(ctx context.Context, arg database.CreateMetadataReviewFieldParams) (database.MetadataReviewField, error)
}

// Database 是「开一次事务」的能力：fn 内的写入要么整体生效，要么整体回滚。
type Database interface {
	ExecTx(ctx context.Context, fn func(Queries) error) error
}

// Service 是提案子域的入口。它不持有生命周期、不开 goroutine，也不吃配置——
// 置信度默认值是本包的常量表。
type Service struct {
	db Database
}

func NewService(db Database) *Service {
	return &Service{db: db}
}
