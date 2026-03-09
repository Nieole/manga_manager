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

	url := strings.TrimRight(o.Endpoint, "/")
	if !strings.HasSuffix(url, "/v1/responses") {
		url += "/v1/responses"
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
	prompt := BuildFetchMetadataPrompt(title)

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

	prompt := BuildRecommendationsPrompt(userTags, candidates, limit)

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

	prompt := BuildGroupingPrompt(seriesList)

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

func (o *OpenAIProvider) TestLLM(ctx context.Context, prompt string) (string, error) {
	return o.sendRequest(ctx, prompt, false)
}
