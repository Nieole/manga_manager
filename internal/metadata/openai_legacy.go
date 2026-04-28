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

// OpenAILegacyProvider 基于标准 OpenAI API 格式的元数据提供者（支持 OpenAI, DeepSeek, LM Studio 等 /v1/chat/completions 接口）
type OpenAILegacyProvider struct {
	Endpoint string
	Model    string
	APIKey   string
	client   *http.Client
}

func NewOpenAILegacyProvider(endpoint, model, apiKey string, timeout int) *OpenAILegacyProvider {
	if endpoint == "" {
		endpoint = "https://api.openai.com/v1"
	}
	if model == "" {
		model = "gpt-3.5-turbo"
	}
	if timeout <= 0 {
		timeout = 120
	}
	return &OpenAILegacyProvider{
		Endpoint: endpoint,
		Model:    model,
		APIKey:   apiKey,
		client: &http.Client{
			Timeout: time.Duration(timeout) * time.Second,
		},
	}
}

func (o *OpenAILegacyProvider) Name() string {
	return "OpenAI Compatible (v1/chat/completions)"
}

// openAILegacyMessage
type openAILegacyMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openAILegacyRequest OpenAI Chat Completions API 请求体
type openAILegacyRequest struct {
	Model    string                `json:"model"`
	Messages []openAILegacyMessage `json:"messages"`
	Stream   bool                  `json:"stream"`
}

// openAILegacyResponse OpenAI Chat Completions API 响应体
type openAILegacyResponse struct {
	ID      string `json:"id"`
	Choices []struct {
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// sendRequest 统一发送封装
func (o *OpenAILegacyProvider) sendRequest(ctx context.Context, prompt string, requireJSON bool) (string, error) {
	reqBody := openAILegacyRequest{
		Model: o.Model,
		Messages: []openAILegacyMessage{
			{
				Role:    "user",
				Content: prompt,
			},
		},
		Stream: false,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("openai-legacy: failed to marshal request: %w", err)
	}

	url := strings.TrimSpace(o.Endpoint)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("openai-legacy: failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if o.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.APIKey)
	}

	resp, err := o.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("openai-legacy: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("openai-legacy: API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var aiResp openAILegacyResponse
	if err := json.NewDecoder(resp.Body).Decode(&aiResp); err != nil {
		return "", fmt.Errorf("openai-legacy: failed to decode response: %w", err)
	}

	if len(aiResp.Choices) == 0 {
		return "", fmt.Errorf("openai-legacy: no choices found in response")
	}

	result := aiResp.Choices[0].Message.Content
	if result == "" {
		return "", fmt.Errorf("openai-legacy: empty content in response")
	}

	return result, nil
}

func (o *OpenAILegacyProvider) FetchSeriesMetadata(ctx context.Context, title string) (*SeriesMetadata, error) {
	prompt := BuildFetchMetadataPrompt(ctx, title)

	content, err := o.sendRequest(ctx, prompt, true)
	if err != nil {
		return nil, err
	}

	content = extractJSONString(content)

	var meta SeriesMetadata
	if err := json.Unmarshal([]byte(content), &meta); err != nil {
		return nil, fmt.Errorf("openai-legacy: failed to parse JSON output: %w\nOutput: %s", err, content)
	}
	meta.Status = NormalizeStatusCode(meta.Status)

	return &meta, nil
}

func (o *OpenAILegacyProvider) SearchMetadata(ctx context.Context, title string, limit, offset int) ([]*SeriesMetadata, int, error) {
	return nil, 0, fmt.Errorf("openai-legacy search metadata: not implemented")
}

func (o *OpenAILegacyProvider) GenerateRecommendations(ctx context.Context, userTags []string, candidates []CandidateSeries, limit int) ([]AIRecommendation, error) {
	prompt := BuildRecommendationsPrompt(ctx, userTags, candidates, limit)

	content, err := o.sendRequest(ctx, prompt, true)
	if err != nil {
		return nil, err
	}

	content = extractJSONString(content)

	var response struct {
		Recommendations []AIRecommendation `json:"recommendations"`
	}
	if err := json.Unmarshal([]byte(content), &response); err != nil {
		return nil, fmt.Errorf("openai-legacy: failed to parse recommendation response: %w\nOutput: %s", err, content)
	}

	return response.Recommendations, nil
}

func (o *OpenAILegacyProvider) GenerateGrouping(ctx context.Context, seriesList []CandidateSeries) ([]AIGroupCollection, error) {
	prompt := BuildGroupingPrompt(ctx, seriesList)

	content, err := o.sendRequest(ctx, prompt, true)
	if err != nil {
		return nil, err
	}

	content = extractJSONString(content)

	var response AIGroupingResult
	if err := json.Unmarshal([]byte(content), &response); err != nil {
		return nil, fmt.Errorf("openai-legacy: failed to parse grouping response: %w\nOutput: %s", err, content)
	}

	return response.NormalizedCollections(), nil
}

func (o *OpenAILegacyProvider) TestLLM(ctx context.Context, prompt string) (string, error) {
	return o.sendRequest(ctx, prompt, false)
}
