// 业务说明：本文件是业务实现，属于元数据聚合链路，负责从本地规则、外部站点和 AI Provider 获取漫画标题、简介、人物、标签与关系信息。
// 它支撑系列详情、智能补全、关系图谱和搜索索引的内容质量。
// 维护时应关注 Provider 契约、失败回退、限流、提示词稳定性和人工审核数据不被覆盖。

package metadata

import (
	"context"
	"strings"

	"manga-manager/internal/config"
)

// Provider 定义外部元数据服务的最小契约。
// 业务上它既要支持“按标题直接补全当前系列”，也要支持“搜索候选后交给用户审核”，因此返回值不能直接覆盖数据库。
type Provider interface {
	Name() string
	FetchSeriesMetadata(ctx context.Context, title string) (*SeriesMetadata, error)
	SearchMetadata(ctx context.Context, title string, limit, offset int) ([]*SeriesMetadata, int, error)
}

// AIProvider 继承 Provider 接口并扩展 LLM 能力。
// 推荐和智能分组面向的是资料库级辅助决策，输出必须保留理由和候选集合，方便前端做审核而不是静默改库。
type AIProvider interface {
	Provider
	GenerateRecommendations(ctx context.Context, userTags []string, candidates []CandidateSeries, limit int) ([]AIRecommendation, error)
	GenerateGrouping(ctx context.Context, seriesList []CandidateSeries) ([]AIGroupCollection, error)
	TestLLM(ctx context.Context, prompt string) (string, error)
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
		cfg := &config.Config{}
		cfg.LLM.Provider = "openai"
		cfg.LLM.APIMode = apiMode
		cfg.LLM.BaseURL = baseURL
		cfg.LLM.RequestPath = requestPath
		endpoint := config.BuildLLMEndpoint(cfg)
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
