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
	// 系列与提案的读取
	GetSeries(ctx context.Context, id int64) (database.Series, error)
	GetTagsForSeries(ctx context.Context, seriesID int64) ([]database.Tag, error)
	GetAuthorsForSeries(ctx context.Context, seriesID int64) ([]database.Author, error)
	GetMetadataReview(ctx context.Context, id int64) (database.MetadataReview, error)
	ListPendingMetadataReviewsBySeries(ctx context.Context, seriesID int64) ([]database.MetadataReview, error)
	ListRecentRejectedMetadataReviewsBySeries(ctx context.Context, arg database.ListRecentRejectedMetadataReviewsBySeriesParams) ([]database.MetadataReview, error)
	ListMetadataReviewFieldsByReviews(ctx context.Context, reviewIDs []int64) ([]database.MetadataReviewField, error)

	// 提案的建立、消解与终态推进
	CreateMetadataReview(ctx context.Context, arg database.CreateMetadataReviewParams) (database.MetadataReview, error)
	CreateMetadataReviewField(ctx context.Context, arg database.CreateMetadataReviewFieldParams) (database.MetadataReviewField, error)
	DeleteMetadataReviewField(ctx context.Context, arg database.DeleteMetadataReviewFieldParams) error
	ResolvePendingMetadataReview(ctx context.Context, arg database.ResolvePendingMetadataReviewParams) (int64, error)

	// 应用一条提案时对系列的写入
	UpdateSeriesMetadata(ctx context.Context, arg database.UpdateSeriesMetadataParams) (database.Series, error)
	UpsertSeriesMetadataProvenance(ctx context.Context, arg database.UpsertSeriesMetadataProvenanceParams) (database.SeriesMetadataProvenance, error)
	ClearSeriesTags(ctx context.Context, seriesID int64) error
	UpsertTag(ctx context.Context, name string) (database.Tag, error)
	LinkSeriesTag(ctx context.Context, arg database.LinkSeriesTagParams) error
	ClearSeriesAuthors(ctx context.Context, seriesID int64) error
	UpsertAuthor(ctx context.Context, arg database.UpsertAuthorParams) (database.Author, error)
	LinkSeriesAuthor(ctx context.Context, arg database.LinkSeriesAuthorParams) error
	GetLinksForSeries(ctx context.Context, seriesID int64) ([]database.SeriesLink, error)
	LinkSeriesLink(ctx context.Context, arg database.LinkSeriesLinkParams) (database.SeriesLink, error)
	RefreshSeriesStats(ctx context.Context, id int64) error
}

// Database 是本包可用的数据库能力：事务外只列读路径要用的那些、裁决前要加载的那几样，
// 加上拒绝那一条自成原子的 CAS；其余写入一律经 ExecTx——fn 内的写入要么整体生效，
// 要么整体回滚。
//
// 事务外的这几样刻意逐个列出而不是内嵌整个 Queries：那样会把系列元数据的写入也开放到
// 事务之外，「写元数据」与「推终态」就又能被拆到两次事务里去，而它们必须原子。
//
// 字段行同样只列批量版，与 Queries 一个口径：读路径与裁决路径都不该有第二种取法。
//
// 裁决前的加载则刻意留在事务之外，与生产上的真实形态一致：读到待裁决与真正写入之间
// 存在窗口，别的请求可以在此期间把同一条提案推进到终态。事务内那道 CAS 守的就是这个窗口，
// 把加载一并塞进事务只会让它变成一段永远撞不上、也测不出来的死代码。
type Database interface {
	GetSeries(ctx context.Context, id int64) (database.Series, error)
	// 集合字段的当前值只有这两条能回答：裁决前要判它们此刻空不空，读通路要展示它们。
	GetTagsForSeries(ctx context.Context, seriesID int64) ([]database.Tag, error)
	GetAuthorsForSeries(ctx context.Context, seriesID int64) ([]database.Author, error)
	GetMetadataReview(ctx context.Context, id int64) (database.MetadataReview, error)
	ListMetadataReviewFieldsByReviews(ctx context.Context, reviewIDs []int64) ([]database.MetadataReviewField, error)
	ResolvePendingMetadataReview(ctx context.Context, arg database.ResolvePendingMetadataReviewParams) (int64, error)

	// 读路径
	ListTagsBySeriesIDs(ctx context.Context, seriesIDs []int64) ([]database.ListTagsBySeriesIDsRow, error)
	ListAuthorsBySeriesIDs(ctx context.Context, seriesIDs []int64) ([]database.ListAuthorsBySeriesIDsRow, error)
	ListPendingMetadataReviewsBySeries(ctx context.Context, seriesID int64) ([]database.MetadataReview, error)
	GetSeriesMetadataProvenance(ctx context.Context, seriesID int64) ([]database.SeriesMetadataProvenance, error)
	ListPendingMetadataReviewInbox(ctx context.Context, arg database.ListPendingMetadataReviewInboxParams) ([]database.ListPendingMetadataReviewInboxRow, error)
	CountPendingMetadataReviewInbox(ctx context.Context, arg database.CountPendingMetadataReviewInboxParams) (int64, error)

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
