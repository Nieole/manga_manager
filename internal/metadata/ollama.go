package metadata

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OllamaProvider 基于 Ollama 本地 LLM 推理的元数据刮削来源
type OllamaProvider struct {
	Endpoint string
	Model    string
	client   *http.Client
}

func NewOllamaProvider(endpoint, model string) *OllamaProvider {
	if endpoint == "" {
		endpoint = "http://localhost:11434"
	}
	if model == "" {
		model = "qwen2.5"
	}
	return &OllamaProvider{
		Endpoint: endpoint,
		Model:    model,
		client: &http.Client{
			Timeout: 120 * time.Second, // LLM 推理需较长的超时
		},
	}
}

func (o *OllamaProvider) Name() string {
	return "Ollama LLM"
}

// ollamaRequest Ollama /api/generate 请求体
type ollamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
	Format string `json:"format"`
}

// ollamaResponse Ollama /api/generate 响应体
type ollamaResponse struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

// llmMetadataResult LLM 返回的 JSON 结构化元数据
type llmMetadataResult struct {
	Title     string   `json:"title"`
	Summary   string   `json:"summary"`
	Publisher string   `json:"publisher"`
	Status    string   `json:"status"`
	Tags      []string `json:"tags"`
	Rating    float64  `json:"rating"`
}

func (o *OllamaProvider) FetchSeriesMetadata(ctx context.Context, title string) (*SeriesMetadata, error) {
	prompt := fmt.Sprintf(`你是一个漫画和书籍数据专家。请根据以下漫画/书籍系列的名称，提供该系列的详细元数据信息。
如果你不确定或不了解这个作品，请在所有字段中返回空值。

系列名称: %s

请严格以 JSON 格式回复，不要包含任何其他文字。JSON 结构如下:
{
  "title": "作品正式中文名（如有），否则原名",
  "summary": "作品简介（100-300字）",
  "publisher": "出版社名称",
  "status": "连载状态（连载中/已完结/未知）",
  "tags": ["标签1", "标签2", "标签3"],
  "rating": 0.0
}`, title)

	reqBody := ollamaRequest{
		Model:  o.Model,
		Prompt: prompt,
		Stream: false,
		Format: "json",
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("ollama: failed to marshal request: %w", err)
	}

	url := strings.TrimRight(o.Endpoint, "/") + "/api/generate"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("ollama: failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama: request failed (is Ollama running at %s?): %w", o.Endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama: API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var ollamaResp ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return nil, fmt.Errorf("ollama: failed to decode response: %w", err)
	}

	// 解析 LLM 返回的 JSON
	var result llmMetadataResult
	if err := json.Unmarshal([]byte(ollamaResp.Response), &result); err != nil {
		return nil, fmt.Errorf("ollama: LLM response is not valid JSON: %w\nRaw: %s", err, ollamaResp.Response)
	}

	// 如果 LLM 没提供有意义的数据，视为未找到
	if result.Title == "" && result.Summary == "" {
		return nil, nil
	}

	return &SeriesMetadata{
		Title:     result.Title,
		Summary:   result.Summary,
		Publisher: result.Publisher,
		Status:    result.Status,
		Tags:      result.Tags,
		Rating:    result.Rating,
	}, nil
}

func (o *OllamaProvider) SearchMetadata(ctx context.Context, title string, limit, offset int) ([]*SeriesMetadata, int, error) {
	result, err := o.FetchSeriesMetadata(ctx, title)
	if err != nil {
		return nil, 0, err
	}
	if result == nil {
		return nil, 0, nil
	}
	return []*SeriesMetadata{result}, 1, nil
}

// AIRecommendation 推荐条目结构
type AIRecommendation struct {
	SeriesID int64  `json:"series_id"`
	Reason   string `json:"reason"`
}

// aiRecommendationResult LLM返回的推荐列表格式
type aiRecommendationResult struct {
	Recommendations []AIRecommendation `json:"recommendations"`
}

// CandidateSeries 候选漫画信息
type CandidateSeries struct {
	ID      int64  `json:"id"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
}

// GenerateRecommendations 请求LLM从候选列表中挑选合适的漫画并生成推荐理由
func (o *OllamaProvider) GenerateRecommendations(ctx context.Context, userTags []string, candidates []CandidateSeries, limit int) ([]AIRecommendation, error) {
	if len(candidates) == 0 {
		return nil, nil
	}

	tagsStr := strings.Join(userTags, ", ")
	if tagsStr == "" {
		tagsStr = "无特定偏好(随机挑好书)"
	}

	// 构造候选列表说明
	var candidatesText strings.Builder
	for _, c := range candidates {
		title := c.Title
		if title == "" {
			title = fmt.Sprintf("未知标题 (ID: %d)", c.ID)
		}
		summary := c.Summary
		if len(summary) > 100 {
			summary = summary[:100] + "..." // 截断防止 token 爆炸
		}
		candidatesText.WriteString(fmt.Sprintf("- ID: %d, 标题: %s, 简介: %s\n", c.ID, title, summary))
	}

	prompt := fmt.Sprintf(`你是一个专业的漫画和书籍推荐助手。
请根据读者最近喜欢的阅读标签：[%s]，从下列候选作品中挑选出最符合他们口味的 %d 部作品。
每一部选出的作品，请仔细阅读它的简介，给出一句充满吸引力、富有情感的推荐语（50字左右，像向朋友安利一样）。

候选作品列表：
%s

请严格以 JSON 格式回复，不要包含任何其他文字。JSON 结构如下:
{
  "recommendations": [
    {
      "series_id": 123,
      "reason": "这部漫画巧妙地将科幻设定融入日常，绝对会让你大呼过瘾！"
    }
  ]
}`, tagsStr, limit, candidatesText.String())

	reqBody := ollamaRequest{
		Model:  o.Model,
		Prompt: prompt,
		Stream: false,
		Format: "json", // 强制要求 JSON 结构输出
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("ollama: failed to marshal recommendation request: %w", err)
	}

	url := strings.TrimRight(o.Endpoint, "/") + "/api/generate"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("ollama: failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// 推荐任务推理时间长，给独立的较长 context 或者直接用 client 的 120s
	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama: recommendation request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama: API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var ollamaResp ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return nil, fmt.Errorf("ollama: failed to decode recommendation response: %w", err)
	}

	var result aiRecommendationResult
	if err := json.Unmarshal([]byte(ollamaResp.Response), &result); err != nil {
		return nil, fmt.Errorf("ollama: recommendation response is not valid JSON: %w\nRaw: %s", err, ollamaResp.Response)
	}

	return result.Recommendations, nil
}

// AIGroupingResult LLM返回的分组列表格式
type AIGroupingResult struct {
	Collections []AIGroupCollection `json:"collections"`
}

// AIGroupCollection 单个分类
type AIGroupCollection struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	SeriesIDs   []int64 `json:"series_ids"`
}

// GenerateGrouping 请求LLM对系列分类归档
func (o *OllamaProvider) GenerateGrouping(ctx context.Context, seriesList []CandidateSeries) ([]AIGroupCollection, error) {
	if len(seriesList) == 0 {
		return nil, nil
	}

	var textBuilder strings.Builder
	for _, s := range seriesList {
		title := s.Title
		if title == "" {
			title = fmt.Sprintf("未知标题 (ID: %d)", s.ID)
		}
		summary := s.Summary
		if len(summary) > 100 {
			summary = summary[:100] + "..."
		}
		textBuilder.WriteString(fmt.Sprintf("- ID: %d, 标题: %s, 简介: %s\n", s.ID, title, summary))
	}

	prompt := fmt.Sprintf(`你是一个专业的图书管理员和漫画策展人。
请仔细阅读以下给定的所有漫画作品清单，分析它们的类型、背景设定或关联性，将它们逻辑分组成 3 到 5 个不同的主题合集(Collections)。

漫画作品清单：
%s

请严格以 JSON 格式回复，不要包含任何其他文字。必须输出以下格式:
{
  "collections": [
    {
      "name": "赛博朋克与科幻",
      "description": "探讨未来科技与人性的硬核科幻作品集",
      "series_ids": [12, 45, 89]
    }
  ]
}`, textBuilder.String())

	reqBody := ollamaRequest{
		Model:  o.Model,
		Prompt: prompt,
		Stream: false,
		Format: "json",
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("ollama: failed to marshal grouping request: %w", err)
	}

	url := strings.TrimRight(o.Endpoint, "/") + "/api/generate"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("ollama: failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// 分组推理时间也很长
	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama: grouping request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama: API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var ollamaResp ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return nil, fmt.Errorf("ollama: failed to decode grouping response: %w", err)
	}

	var result AIGroupingResult
	if err := json.Unmarshal([]byte(ollamaResp.Response), &result); err != nil {
		return nil, fmt.Errorf("ollama: grouping response is not valid JSON: %w\nRaw: %s", err, ollamaResp.Response)
	}

	return result.Collections, nil
}
