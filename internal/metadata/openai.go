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

// OpenAIProvider 基于标准 OpenAI API 格式的元数据提供者（支持 OpenAI, DeepSeek, LM Studio 等）
type OpenAIProvider struct {
	Endpoint string
	Model    string
	APIKey   string
	client   *http.Client
}

func NewOpenAIProvider(endpoint, model, apiKey string) *OpenAIProvider {
	if endpoint == "" {
		endpoint = "https://api.openai.com/v1"
	}
	if model == "" {
		model = "gpt-3.5-turbo"
	}
	return &OpenAIProvider{
		Endpoint: endpoint,
		Model:    model,
		APIKey:   apiKey,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (o *OpenAIProvider) Name() string {
	return "OpenAI/Compatible LLM"
}

// openAIRequest OpenAI Chat Completions 请求体
type openAIRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
	// 为了确保返回 JSON（仅适用于支持此特性的提供商，比如 OpenAI, DeepSeek 等）。
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type responseFormat struct {
	Type string `json:"type"`
}

// openAIResponse OpenAI Chat Completions 响应体
type openAIResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// sendRequest 统一发送封装
func (o *OpenAIProvider) sendRequest(ctx context.Context, prompt string, requireJSON bool) (string, error) {
	reqBody := openAIRequest{
		Model: o.Model,
		Messages: []chatMessage{
			{Role: "user", Content: prompt},
		},
		Stream: false,
	}

	if requireJSON {
		reqBody.ResponseFormat = &responseFormat{Type: "json_object"}
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("openai: failed to marshal request: %w", err)
	}

	url := strings.TrimRight(o.Endpoint, "/")
	if !strings.HasSuffix(url, "/chat/completions") {
		url += "/chat/completions"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("openai: failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if o.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.APIKey)
	}

	resp, err := o.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("openai: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("openai: API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var aiResp openAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&aiResp); err != nil {
		return "", fmt.Errorf("openai: failed to decode response: %w", err)
	}

	if len(aiResp.Choices) == 0 {
		return "", fmt.Errorf("openai: no choices returned in response")
	}

	return aiResp.Choices[0].Message.Content, nil
}

// 抽取 Markdown 中的 JSON 块（有些模型仍然会输出 ```json 前缀）
func extractJSONString(input string) string {
	input = strings.TrimSpace(input)
	if strings.HasPrefix(input, "```json") {
		input = strings.TrimPrefix(input, "```json")
		input = strings.TrimSuffix(input, "```")
	} else if strings.HasPrefix(input, "```") {
		input = strings.TrimPrefix(input, "```")
		input = strings.TrimSuffix(input, "```")
	}
	return strings.TrimSpace(input)
}

func (o *OpenAIProvider) FetchSeriesMetadata(ctx context.Context, title string) (*SeriesMetadata, error) {
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

	content, err := o.sendRequest(ctx, prompt, true)
	if err != nil {
		return nil, err
	}
	content = extractJSONString(content)

	var result llmMetadataResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, fmt.Errorf("openai: response is not valid JSON: %w\nRaw: %s", err, content)
	}

	metadata := &SeriesMetadata{
		Title:     result.Title,
		Summary:   result.Summary,
		Publisher: result.Publisher,
		Status:    result.Status,
		Tags:      result.Tags,
		Rating:    result.Rating,
	}

	return metadata, nil
}

func (o *OpenAIProvider) SearchMetadata(ctx context.Context, title string, limit, offset int) ([]*SeriesMetadata, int, error) {
	result, err := o.FetchSeriesMetadata(ctx, title)
	if err != nil {
		return nil, 0, err
	}
	if result.Title == "" && result.Summary == "" {
		return []*SeriesMetadata{}, 0, nil
	}
	return []*SeriesMetadata{result}, 1, nil
}

// GenerateRecommendations 请求LLM从候选列表中挑选合适的漫画并生成推荐理由
func (o *OpenAIProvider) GenerateRecommendations(ctx context.Context, userTags []string, candidates []CandidateSeries, limit int) ([]AIRecommendation, error) {
	if len(candidates) == 0 {
		return nil, nil
	}

	tagsStr := strings.Join(userTags, ", ")
	if tagsStr == "" {
		tagsStr = "无特定偏好(随机挑好书)"
	}

	var candidatesText strings.Builder
	for _, c := range candidates {
		title := c.Title
		if title == "" {
			title = fmt.Sprintf("未知标题 (ID: %d)", c.ID)
		}
		summary := c.Summary
		if len(summary) > 100 {
			summary = summary[:100] + "..."
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

	content, err := o.sendRequest(ctx, prompt, true)
	if err != nil {
		return nil, err
	}
	content = extractJSONString(content)

	var result aiRecommendationResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, fmt.Errorf("openai: recommendation response is not valid JSON: %w\nRaw: %s", err, content)
	}

	return result.Recommendations, nil
}

// GenerateGrouping 请求LLM对系列分类归档
func (o *OpenAIProvider) GenerateGrouping(ctx context.Context, seriesList []CandidateSeries) ([]AIGroupCollection, error) {
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

	content, err := o.sendRequest(ctx, prompt, true)
	if err != nil {
		return nil, err
	}
	content = extractJSONString(content)

	var result AIGroupingResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, fmt.Errorf("openai: grouping response is not valid JSON: %w\nRaw: %s", err, content)
	}

	return result.Collections, nil
}
