package metadata

import "context"

// Provider 定义了一个外部元数据服务需要实现的标准获取接口
type Provider interface {
	Name() string
	FetchSeriesMetadata(ctx context.Context, title string) (*SeriesMetadata, error)
	SearchMetadata(ctx context.Context, title string, limit, offset int) ([]*SeriesMetadata, int, error)
}

// AIProvider 继承 Provider 接口并扩展针对 LLM 的推荐与智能分组功能
type AIProvider interface {
	Provider
	GenerateRecommendations(ctx context.Context, userTags []string, candidates []CandidateSeries, limit int) ([]AIRecommendation, error)
	GenerateGrouping(ctx context.Context, seriesList []CandidateSeries) ([]AIGroupCollection, error)
	TestLLM(ctx context.Context, prompt string) (string, error)
}

// NewAIProvider 工厂方法，根据配置切换 LLM 实例
// timeout 为请求超时秒数，0 或负值使用默认 120 秒
func NewAIProvider(provider, endpoint, model, apiKey string, timeout int) AIProvider {
	if timeout <= 0 {
		timeout = 120
	}
	if provider == "openai" {
		return NewOpenAIProvider(endpoint, model, apiKey, timeout)
	}
	// 默认回退到 ollama
	return NewOllamaProvider(endpoint, model, timeout)
}

// AIRecommendation 推荐条目结构
type AIRecommendation struct {
	SeriesID int64  `json:"series_id"`
	Reason   string `json:"reason"`
}

// AIGroupCollection 单个分类
type AIGroupCollection struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	SeriesIDs   []int64 `json:"series_ids"`
}

// CandidateSeries 候选漫画信息
type CandidateSeries struct {
	ID      int64  `json:"id"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
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
