package metadata

import "context"

// Provider 定义了一个外部元数据服务需要实现的标准获取接口
type Provider interface {
	Name() string
	FetchSeriesMetadata(ctx context.Context, title string) (*SeriesMetadata, error)
	SearchMetadata(ctx context.Context, title string, limit, offset int) ([]*SeriesMetadata, int, error)
}

// SeriesMetadata 供多数据源统一返回的内部使用的数据承载对象
type SeriesMetadata struct {
	Title         string
	OriginalTitle string // 原名/别名
	Summary       string
	Publisher     string
	Status        string
	CoverURL      string
	Rating        float64
	Tags          []string
	SourceID      int    // 外部数据源条目 ID（如 Bangumi subject ID）
	ReleaseDate   string // 发行日期
	VolumeCount   int    // 册数/卷数
}
