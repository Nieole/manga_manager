package metadata

import (
	"context"
	"strings"

	"manga-manager/internal/config"
)

// Fetcher consumes single-series metadata enrichment.
type Fetcher interface {
	Name() string
	FetchSeriesMetadata(context.Context, string) (*SeriesMetadata, error)
}

// Searcher consumes paginated candidates without enrichment authority.
type Searcher interface {
	Name() string
	SearchMetadata(context.Context, string, int, int) ([]*SeriesMetadata, int, error)
}

// Recommender generates reasoned recommendations.
type Recommender interface {
	GenerateRecommendations(context.Context, []string, []CandidateSeries, int) ([]AIRecommendation, error)
}

// Grouper generates collection suggestions without applying them.
type Grouper interface {
	Name() string
	GenerateGrouping(context.Context, []CandidateSeries) ([]AIGroupCollection, error)
}

// ConnectionTester checks the configured model connection.
type ConnectionTester interface {
	TestLLM(context.Context, string) (string, error)
}

// Provider combines the capabilities implemented by catalogue source adapters.
type Provider interface {
	Fetcher
	Searcher
}

// AIProvider is the concrete factory result; consumers choose only their required capability.
type AIProvider interface {
	Provider
	Recommender
	Grouper
	ConnectionTester
}

// NewAIProvider 根据运行时配置选择 LLM 实现。
// apiMode 用来兼容 Responses API 和旧 Chat Completions API；timeout 为请求超时秒数，0 或负值使用默认 120 秒。
// 默认回退 Ollama 是为了让本地部署在未配置云端密钥时仍能使用基础 AI 能力。
func NewAIProvider(provider, apiMode, baseURL, requestPath, model, apiKey string, timeout int) AIProvider {
	if timeout <= 0 {
		timeout = 120
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "openai-legacy" {
		provider = "openai"
		if apiMode == "" {
			apiMode = "chat_completions"
		}
	}

	switch provider {
	case "openai":
		endpoint := config.LLMEndpoint(config.LLMConnection{Provider: "openai", APIMode: apiMode, BaseURL: baseURL, RequestPath: requestPath})
		if strings.EqualFold(apiMode, "chat_completions") {
			return NewOpenAILegacyProvider(endpoint, model, apiKey, timeout)
		}
		return NewOpenAIProvider(endpoint, model, apiKey, timeout)
	default:
		// 默认回退到 ollama
		return NewOllamaProvider(baseURL, model, timeout)
	}
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

// SeriesAuthor 表示外部元数据中的作者条目（姓名 + 角色）
type SeriesAuthor struct {
	Name string
	Role string
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
	Authors       []SeriesAuthor // 作者/绘师等参与人员
	SourceID      int            // 外部数据源条目 ID（如 Bangumi subject ID）
	SourceURL     string
	Provider      string
	Confidence    float64
	ReleaseDate   string // 发行日期
	VolumeCount   int    // 册数/卷数
}
