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
	Format string `json:"format,omitempty"`
}

// ollamaResponse Ollama /api/generate 响应体
type ollamaResponse struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

// sendRequest 统一的 Ollama HTTP 请求封装
func (o *OllamaProvider) sendRequest(ctx context.Context, prompt string, requireJSON bool) (string, error) {
	reqBody := ollamaRequest{
		Model:  o.Model,
		Prompt: prompt,
		Stream: false,
	}
	if requireJSON {
		reqBody.Format = "json"
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("ollama: failed to marshal request: %w", err)
	}

	url := strings.TrimRight(o.Endpoint, "/") + "/api/generate"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("ollama: failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama: request failed (is Ollama running at %s?): %w", o.Endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("ollama: API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var ollamaResp ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return "", fmt.Errorf("ollama: failed to decode response: %w", err)
	}

	return ollamaResp.Response, nil
}

func (o *OllamaProvider) FetchSeriesMetadata(ctx context.Context, title string) (*SeriesMetadata, error) {
	prompt := BuildFetchMetadataPrompt(title)

	content, err := o.sendRequest(ctx, prompt, true)
	if err != nil {
		return nil, err
	}

	// 解析 LLM 返回的 JSON
	var result llmMetadataResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, fmt.Errorf("ollama: LLM response is not valid JSON: %w\nRaw: %s", err, content)
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

// GenerateRecommendations 请求LLM从候选列表中挑选合适的漫画并生成推荐理由
func (o *OllamaProvider) GenerateRecommendations(ctx context.Context, userTags []string, candidates []CandidateSeries, limit int) ([]AIRecommendation, error) {
	if len(candidates) == 0 {
		return nil, nil
	}

	prompt := BuildRecommendationsPrompt(userTags, candidates, limit)

	content, err := o.sendRequest(ctx, prompt, true)
	if err != nil {
		return nil, fmt.Errorf("ollama: recommendation request failed: %w", err)
	}

	var result aiRecommendationResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, fmt.Errorf("ollama: recommendation response is not valid JSON: %w\nRaw: %s", err, content)
	}

	return result.Recommendations, nil
}

// GenerateGrouping 请求LLM对系列分类归档
func (o *OllamaProvider) GenerateGrouping(ctx context.Context, seriesList []CandidateSeries) ([]AIGroupCollection, error) {
	if len(seriesList) == 0 {
		return nil, nil
	}

	prompt := BuildGroupingPrompt(seriesList)

	content, err := o.sendRequest(ctx, prompt, true)
	if err != nil {
		return nil, fmt.Errorf("ollama: grouping request failed: %w", err)
	}

	var result AIGroupingResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, fmt.Errorf("ollama: grouping response is not valid JSON: %w\nRaw: %s", err, content)
	}

	return result.Collections, nil
}

func (o *OllamaProvider) TestLLM(ctx context.Context, prompt string) (string, error) {
	return o.sendRequest(ctx, prompt, false)
}
