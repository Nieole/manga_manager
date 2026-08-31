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

func NewOpenAIProvider(endpoint, model, apiKey string, timeout int) *OpenAIProvider {
	if endpoint == "" {
		endpoint = "https://api.openai.com/v1"
	}
	if model == "" {
		model = "gpt-3.5-turbo"
	}
	if timeout <= 0 {
		timeout = 120
	}
	return &OpenAIProvider{
		Endpoint: endpoint,
		Model:    model,
		APIKey:   apiKey,
		client: &http.Client{
			Timeout: time.Duration(timeout) * time.Second,
		},
	}
}

func (o *OpenAIProvider) Name() string {
	return "OpenAI/Compatible LLM"
}

// openAIRequest OpenAI Responses API 请求体
type openAIRequest struct {
	Model  string `json:"model"`
	Input  string `json:"input"`
	Stream bool   `json:"stream"`
}

// openAIResponse OpenAI Responses API 响应体
type openAIResponse struct {
	ID     string       `json:"id"`
	Object string       `json:"object"`
	Output []outputItem `json:"output"`
}

type outputItem struct {
	Type    string        `json:"type"`
	Content []contentItem `json:"content"`
}

type contentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// sendRequest 统一发送封装
func (o *OpenAIProvider) sendRequest(ctx context.Context, prompt string, requireJSON bool) (string, error) {
	reqBody := openAIRequest{
		Model:  o.Model,
		Input:  prompt,
		Stream: false,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("openai: failed to marshal request: %w", err)
	}

	url := strings.TrimSpace(o.Endpoint)

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
		return "", fmt.Errorf("openai: request failed: %w", sanitizeTransportError(err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("openai: API returned status %d: %s", resp.StatusCode, truncateUpstreamBody(respBody))
	}

	var aiResp openAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&aiResp); err != nil {
		return "", fmt.Errorf("openai: failed to decode response: %w", err)
	}

	var finalOutput strings.Builder
	for _, outItem := range aiResp.Output {
		if outItem.Type == "message" {
			for _, content := range outItem.Content {
				if content.Type == "output_text" {
					finalOutput.WriteString(content.Text)
				}
			}
		}
	}

	result := finalOutput.String()
	if result == "" {
		return "", fmt.Errorf("openai: no output_text found in response output")
	}

	return result, nil
}

// extractJSONString 抽取 Markdown 中的 JSON 块（有些模型仍然会输出 ```json 前缀）
// extractJSONString 从 LLM 的自由文本输出里提取 JSON 载荷。
//
// 旧实现只认「整段被 ``` 包裹」这一种形态：模型只要在前面加一句
// "Here is the metadata you requested:"，整轮刮削就会失败，而失败信息还会把
// 整段原始输出拼进 error 回显给前端。实际输出形态很杂，这里按三级降级：
//  1. 去掉任意位置的 ``` 围栏（含 ```json 标注）；
//  2. 若结果本身已是合法 JSON，直接用；
//  3. 否则退到「第一个 { 到最后一个 }」的子串，再校验一次合法性。
func extractJSONString(input string) string {
	trimmed := strings.TrimSpace(input)

	// 第 1 级：剥离围栏。围栏可能出现在中间（前面有一句说明文字）。
	if idx := strings.Index(trimmed, "```"); idx >= 0 {
		rest := trimmed[idx+3:]
		rest = strings.TrimPrefix(rest, "json")
		rest = strings.TrimPrefix(rest, "JSON")
		if end := strings.Index(rest, "```"); end >= 0 {
			rest = rest[:end]
		}
		if candidate := strings.TrimSpace(rest); candidate != "" {
			trimmed = candidate
		}
	}

	// 第 2 级：已经是合法 JSON 就直接用。
	if json.Valid([]byte(trimmed)) {
		return trimmed
	}

	// 第 3 级：截取最外层的花括号区间。
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start >= 0 && end > start {
		if candidate := strings.TrimSpace(trimmed[start : end+1]); json.Valid([]byte(candidate)) {
			return candidate
		}
	}
	return trimmed
}

func (o *OpenAIProvider) FetchSeriesMetadata(ctx context.Context, title string) (*SeriesMetadata, error) {
	prompt := BuildFetchMetadataPrompt(ctx, title)

	content, err := o.sendRequest(ctx, prompt, true)
	if err != nil {
		return nil, err
	}
	content = extractJSONString(content)

	var result llmMetadataResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, fmt.Errorf("openai: response is not valid JSON: %w\nRaw: %s", err, content)
	}

	// 与 Ollama 保持同一契约：LLM 表示「不了解该作品」时返回 (nil, nil)，让调用方据此
	// 判定「没找到」。若改成返回一个各字段全空的结构体，批量刮削会把这个幻觉空结果计为
	// 成功，并把空提案塞进待审队列。
	if strings.TrimSpace(result.Title) == "" && strings.TrimSpace(result.Summary) == "" {
		return nil, nil
	}

	metadata := &SeriesMetadata{
		Title:      result.Title,
		Summary:    result.Summary,
		Publisher:  result.Publisher,
		Status:     sourceStatusCode(result.Status),
		Tags:       result.Tags,
		Rating:     result.Rating,
		Confidence: result.Confidence,
		Provider:   o.Name(),
	}

	return metadata, nil
}

func (o *OpenAIProvider) SearchMetadata(ctx context.Context, title string, limit, offset int) ([]*SeriesMetadata, int, error) {
	result, err := o.FetchSeriesMetadata(ctx, title)
	if err != nil {
		return nil, 0, err
	}
	// FetchSeriesMetadata 现在在「LLM 不了解该作品」时返回 (nil, nil)，这里必须先判 nil
	// 再取字段，否则会空指针 panic。
	if result == nil {
		return []*SeriesMetadata{}, 0, nil
	}
	return []*SeriesMetadata{result}, 1, nil
}

// GenerateRecommendations 请求LLM从候选列表中挑选合适的漫画并生成推荐理由
func (o *OpenAIProvider) GenerateRecommendations(ctx context.Context, userTags []string, candidates []CandidateSeries, limit int) ([]AIRecommendation, error) {
	if len(candidates) == 0 {
		return nil, nil
	}

	prompt := BuildRecommendationsPrompt(ctx, userTags, candidates, limit)

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

	prompt := BuildGroupingPrompt(ctx, seriesList)

	content, err := o.sendRequest(ctx, prompt, true)
	if err != nil {
		return nil, err
	}
	content = extractJSONString(content)

	var result AIGroupingResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, fmt.Errorf("openai: grouping response is not valid JSON: %w\nRaw: %s", err, content)
	}

	return result.NormalizedCollections(), nil
}

func (o *OpenAIProvider) TestLLM(ctx context.Context, prompt string) (string, error) {
	return o.sendRequest(ctx, prompt, false)
}
