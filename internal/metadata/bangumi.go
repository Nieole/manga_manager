package metadata

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// ============================================================
// Bangumi API v0 数据实体
// ============================================================

type bangumiSearchRequest struct {
	Keyword string        `json:"keyword"`
	Filter  bangumiFilter `json:"filter"`
}

type bangumiFilter struct {
	Type []int `json:"type"`
	NSFW bool  `json:"nsfw"`
}

type bangumiSearchResult struct {
	Data   []bangumiSubjectResult `json:"data"`
	Total  int                    `json:"total"`
	Limit  int                    `json:"limit"`
	Offset int                    `json:"offset"`
}

type bangumiSubjectResult struct {
	ID       int            `json:"id"`
	Name     string         `json:"name"`
	NameCN   string         `json:"name_cn"`
	Summary  string         `json:"summary"`
	Date     string         `json:"date"`
	Images   *bangumiImages `json:"images"`
	Image    string         `json:"image"`
	NSFW     bool           `json:"nsfw"`
	Rating   *bangumiRating `json:"rating"`
	Volumes  int            `json:"volumes"`
	Eps      int            `json:"eps"`
	Type     int            `json:"type"`
	Platform string         `json:"platform"`
	Tags     []bangumiTag   `json:"tags"`
	Series   bool           `json:"series"`
	Locked   bool           `json:"locked"`
	Infobox  []interface{}  `json:"infobox"`
}

type bangumiImages struct {
	Small  string `json:"small"`
	Large  string `json:"large"`
	Common string `json:"common"`
	Grid   string `json:"grid"`
	Medium string `json:"medium"`
}

type bangumiRating struct {
	Score float64        `json:"score"`
	Total int            `json:"total"`
	Count map[string]int `json:"count"`
	Rank  int            `json:"rank"`
}

type bangumiTag struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// ============================================================
// BangumiProvider 实现
// ============================================================

type BangumiProvider struct {
	ClientURL  string
	httpClient *http.Client
}

func NewBangumiProvider() *BangumiProvider {
	return &BangumiProvider{
		ClientURL: "https://api.bgm.tv",
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (b *BangumiProvider) Name() string {
	return "Bangumi"
}

func (b *BangumiProvider) FetchSeriesMetadata(ctx context.Context, title string) (*SeriesMetadata, error) {
	results, _, err := b.SearchMetadata(ctx, title, 1, 0)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}
	// 默认返回第一条（保持原有逻辑兼容性）
	return results[0], nil
}

func (b *BangumiProvider) SearchMetadata(ctx context.Context, title string, limit, offset int) ([]*SeriesMetadata, int, error) {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	reqBody := bangumiSearchRequest{
		Keyword: title,
		Filter: bangumiFilter{
			Type: []int{1}, // 1 = 书籍
			// NSFW: true,
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("bangumi: failed to marshal request: %w", err)
	}

	apiUrl := fmt.Sprintf("%s/v0/search/subjects?limit=%d&offset=%d",
		b.ClientURL, limit, offset)

	slog.Info("Bangumi search request (POST)", "url", apiUrl, "keyword", title, "limit", limit, "offset", offset)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiUrl, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, 0, fmt.Errorf("bangumi: failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "MangaManager/1.0 (https://github.com/manga-manager)")

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("bangumi: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		slog.Error("Bangumi API error", "status", resp.Status, "body", string(respBody), "url", apiUrl)
		return nil, 0, fmt.Errorf("bangumi: API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var result bangumiSearchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, 0, fmt.Errorf("bangumi: failed to decode response: %w", err)
	}

	if len(result.Data) == 0 {
		return nil, 0, nil // 未找到匹配结果
	}

	var metadatas []*SeriesMetadata
	for _, item := range result.Data {
		metadatas = append(metadatas, b.convertToSeriesMetadata(item))
	}

	return metadatas, result.Total, nil
}

func (b *BangumiProvider) convertToSeriesMetadata(best bangumiSubjectResult) *SeriesMetadata {
	// 优先使用中文名
	displayTitle := best.NameCN
	if displayTitle == "" {
		displayTitle = best.Name
	}

	// 提取封面 URL
	var coverURL string
	if best.Images != nil {
		if best.Images.Large != "" {
			coverURL = best.Images.Large
		} else if best.Images.Common != "" {
			coverURL = best.Images.Common
		} else if best.Images.Medium != "" {
			coverURL = best.Images.Medium
		}
	}
	if coverURL == "" && best.Image != "" {
		coverURL = best.Image
	}

	// 提取评分
	var rating float64
	if best.Rating != nil {
		rating = best.Rating.Score
	}

	// 提取标签名
	var tags []string
	for _, t := range best.Tags {
		if t.Name != "" {
			tags = append(tags, t.Name)
		}
	}

	// 推断出版商：尝试从 infobox 中解析
	publisher := extractPublisherFromInfobox(best.Infobox)

	return &SeriesMetadata{
		Title:         displayTitle,
		OriginalTitle: best.Name,
		Summary:       best.Summary,
		Publisher:     publisher,
		CoverURL:      coverURL,
		Rating:        rating,
		Tags:          tags,
		SourceID:      best.ID,
		ReleaseDate:   best.Date,
		VolumeCount:   best.Volumes,
	}
}

// extractPublisherFromInfobox 尝试从 Bangumi 的 infobox 异构数组中提取出版社信息
func extractPublisherFromInfobox(infobox []interface{}) string {
	for _, item := range infobox {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		key, _ := m["key"].(string)
		if key == "出版社" || key == "publisher" {
			if val, ok := m["value"].(string); ok {
				return val
			}
		}
	}
	return ""
}
