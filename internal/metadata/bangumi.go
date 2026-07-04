// 业务说明：本文件是业务实现，属于元数据聚合链路，负责从本地规则、外部站点和 AI Provider 获取漫画标题、简介、人物、标签与关系信息。
// 它支撑系列详情、智能补全、关系图谱和搜索索引的内容质量。
// 维护时应关注 Provider 契约、失败回退、限流、提示词稳定性和人工审核数据不被覆盖。

package metadata

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Bangumi 抓取的有限次指数退避重试参数：仅针对 429 与 5xx，尊重 Retry-After。
const (
	bangumiMaxRetries     = 3
	bangumiRetryBaseDelay = 1 * time.Second
	bangumiRetryMaxDelay  = 30 * time.Second
)

// backoffDelay 返回第 attempt 次重试的退避时长（1s/2s/4s…），封顶 bangumiRetryMaxDelay。
func backoffDelay(attempt int) time.Duration {
	d := bangumiRetryBaseDelay << attempt
	if d > bangumiRetryMaxDelay || d <= 0 {
		return bangumiRetryMaxDelay
	}
	return d
}

// parseRetryAfter 解析 Retry-After 头（秒数或 HTTP-date），无法解析或为负时返回 0。
func parseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs < 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// sleepWithContext 可被 context 取消打断的睡眠，用于退避期间响应任务取消/超时。
func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

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

	// 有限次指数退避重试：仅对 429 与 5xx 重试，尊重 Retry-After；退避可被 context 取消打断。
	// body 为 bytes.Reader，每次重试都必须重建 request。
	var result bangumiSearchResult
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiUrl, bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, 0, fmt.Errorf("bangumi: failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "MangaManager/1.0 (https://github.com/manga-manager)")

		resp, err := b.httpClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, 0, ctx.Err()
			}
			return nil, 0, fmt.Errorf("bangumi: request failed: %w", err)
		}

		if resp.StatusCode == http.StatusOK {
			decErr := json.NewDecoder(resp.Body).Decode(&result)
			resp.Body.Close()
			if decErr != nil {
				return nil, 0, fmt.Errorf("bangumi: failed to decode response: %w", decErr)
			}
			break
		}

		respBody, _ := io.ReadAll(resp.Body)
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
		status := resp.StatusCode
		resp.Body.Close()

		if (status == http.StatusTooManyRequests || status >= 500) && attempt < bangumiMaxRetries {
			wait := retryAfter
			if wait <= 0 {
				wait = backoffDelay(attempt)
			}
			if wait > bangumiRetryMaxDelay {
				wait = bangumiRetryMaxDelay
			}
			slog.Warn("Bangumi API throttled, backing off", "status", status, "attempt", attempt+1, "wait", wait.String(), "url", apiUrl)
			if werr := sleepWithContext(ctx, wait); werr != nil {
				return nil, 0, werr
			}
			continue
		}

		slog.Error("Bangumi API error", "status", status, "body", string(respBody), "url", apiUrl)
		return nil, 0, fmt.Errorf("bangumi: API returned status %d: %s", status, string(respBody))
	}

	if len(result.Data) == 0 {
		return nil, 0, nil // 未找到匹配结果
	}

	var metadatas []*SeriesMetadata
	for idx, item := range result.Data {
		metadatas = append(metadatas, b.convertToSeriesMetadata(item, idx))
	}

	return metadatas, result.Total, nil
}

func (b *BangumiProvider) convertToSeriesMetadata(best bangumiSubjectResult, rank int) *SeriesMetadata {
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
	authors := extractAuthorsFromInfobox(best.Infobox)
	confidence := 0.92 - float64(rank)*0.04
	if confidence < 0.55 {
		confidence = 0.55
	}
	if best.Summary == "" {
		confidence -= 0.08
	}
	if publisher == "" {
		confidence -= 0.03
	}
	if len(tags) == 0 {
		confidence -= 0.03
	}
	if confidence < 0.35 {
		confidence = 0.35
	}

	return &SeriesMetadata{
		Title:         displayTitle,
		OriginalTitle: best.Name,
		Summary:       best.Summary,
		Publisher:     publisher,
		CoverURL:      coverURL,
		Rating:        rating,
		Tags:          tags,
		Authors:       authors,
		SourceID:      best.ID,
		SourceURL:     fmt.Sprintf("https://bgm.tv/subject/%d", best.ID),
		Provider:      b.Name(),
		Confidence:    confidence,
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

// extractAuthorsFromInfobox 从 Bangumi infobox 抽取作者/作画/原作等参与人员
func extractAuthorsFromInfobox(infobox []interface{}) []SeriesAuthor {
	roleMap := map[string]string{
		"作者":     "Writer",
		"原作":     "Writer",
		"漫画":     "Penciller",
		"作画":     "Penciller",
		"插图":     "Cover",
		"插画":     "Cover",
		"出品":     "Editor",
		"编辑":     "Editor",
		"编剧":     "Writer",
		"author": "Writer",
		"writer": "Writer",
		"artist": "Penciller",
	}
	var authors []SeriesAuthor
	seen := make(map[string]struct{})
	addAuthor := func(name, role string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		key := role + "|" + name
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		authors = append(authors, SeriesAuthor{Name: name, Role: role})
	}
	for _, item := range infobox {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		key, _ := m["key"].(string)
		role, ok := roleMap[strings.ToLower(strings.TrimSpace(key))]
		if !ok {
			role, ok = roleMap[strings.TrimSpace(key)]
		}
		if !ok {
			continue
		}
		switch v := m["value"].(type) {
		case string:
			for _, name := range strings.FieldsFunc(v, func(r rune) bool {
				return r == ',' || r == '、' || r == '/' || r == '\n'
			}) {
				addAuthor(name, role)
			}
		case []interface{}:
			for _, entry := range v {
				switch entry := entry.(type) {
				case string:
					addAuthor(entry, role)
				case map[string]interface{}:
					if name, ok := entry["v"].(string); ok {
						addAuthor(name, role)
					} else if name, ok := entry["name"].(string); ok {
						addAuthor(name, role)
					}
				}
			}
		}
	}
	return authors
}
