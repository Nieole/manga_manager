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

func (o *OllamaProvider) SearchMetadata(ctx context.Context, title string) ([]*SeriesMetadata, error) {
	result, err := o.FetchSeriesMetadata(ctx, title)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	return []*SeriesMetadata{result}, nil
}
