package metadata

import (
	"context"
)

// Provider 定义了一个外部元数据服务需要实现的标准获取接口
type Provider interface {
	Name() string
	FetchSeriesMetadata(ctx context.Context, title string) (*SeriesMetadata, error)
}

// 供多数据源统一返回的内部使用的数据承载对象
type SeriesMetadata struct {
	Title     string
	Summary   string
	Publisher string
	Status    string
	CoverURL  string
}

// BangumiProvider 接入基于二次元动画、书籍管理平台 HTTP API
type BangumiProvider struct {
	ClientURL string
}

func NewBangumiProvider() *BangumiProvider {
	return &BangumiProvider{
		// TODO: 配置真实的 Bangumi 的 Base URL 和 Client ID (如果有请求频率验证)
		ClientURL: "https://api.bgm.tv",
	}
}

func (b *BangumiProvider) Name() string {
	return "Bangumi"
}

func (b *BangumiProvider) FetchSeriesMetadata(ctx context.Context, title string) (*SeriesMetadata, error) {
	// 实际开发中应该使用标准库的 net/http 去拼接 `/search/subject/{title}?type=1` 查询书籍信息
	// 并取出 JSON 第一个条目的详情进行回填
	return nil, nil
}

// LLMProvider 基于外部 OpenAI 兼容大模型或本地模型推理的供应商组件
type LLMProvider struct {
	Endpoint string
	APIKey   string
}

func NewLLMProvider(endpoint, apiKey string) *LLMProvider {
	return &LLMProvider{
		Endpoint: endpoint,
		APIKey:   apiKey,
	}
}

func (l *LLMProvider) Name() string {
	return "LLM Reasoner"
}

func (l *LLMProvider) FetchSeriesMetadata(ctx context.Context, title string) (*SeriesMetadata, error) {
	// 结合 LLM SDK（比如 github.com/sashabaranov/go-openai）
	// prompt 例子: "您是一个漫画数据整理专家，请根据书名或可能的别名【{title}】推荐其作者、出版商及简介摘要..."
	return nil, nil
}
