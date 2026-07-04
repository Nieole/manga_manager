// 业务说明：本文件是业务实现，属于元数据聚合链路，负责从 Comic Vine 开放 API 抓取漫画卷（volume）的标题、简介、出版商与封面信息。
// 它为系列详情补全和搜索候选提供英文漫画（欧美 comics）侧的数据来源，结果需交由人工审核而非直接覆盖数据库。
// 维护时应关注 Comic Vine 的严格限流（429）、必需的 User-Agent 头、HTML 描述清洗以及 apiKey 缺失时的降级。

package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Comic Vine 抓取的有限次指数退避重试参数：仅针对 429 与 5xx，尊重 Retry-After。
const (
	comicvineMaxRetries = 3
	comicvineBaseURL    = "https://comicvine.gamespot.com/api/search/"
)

// comicvineTagRegexp 用于剥离 description 字段中的 HTML 标签。
var comicvineTagRegexp = regexp.MustCompile(`<[^>]*>`)

// ============================================================
// Comic Vine API 数据实体
// ============================================================

type comicvineSearchResponse struct {
	Error                string            `json:"error"`
	NumberOfTotalResults int               `json:"number_of_total_results"`
	Results              []comicvineVolume `json:"results"`
}

type comicvineVolume struct {
	ID            int                 `json:"id"`
	Name          string              `json:"name"`
	Deck          string              `json:"deck"`
	Description   string              `json:"description"`
	Image         *comicvineImage     `json:"image"`
	CountOfIssues int                 `json:"count_of_issues"`
	StartYear     string              `json:"start_year"`
	SiteDetailURL string              `json:"site_detail_url"`
	Publisher     *comicvinePublisher `json:"publisher"`
}

type comicvineImage struct {
	SuperURL  string `json:"super_url"`
	MediumURL string `json:"medium_url"`
}

type comicvinePublisher struct {
	Name string `json:"name"`
}

// ============================================================
// ComicVineProvider 实现
// ============================================================

type ComicVineProvider struct {
	apiKey     string
	httpClient *http.Client
}

func NewComicVineProvider(apiKey string) *ComicVineProvider {
	return &ComicVineProvider{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (c *ComicVineProvider) Name() string {
	return "Comic Vine"
}

func (c *ComicVineProvider) FetchSeriesMetadata(ctx context.Context, title string) (*SeriesMetadata, error) {
	results, _, err := c.SearchMetadata(ctx, title, 1, 0)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}
	return results[0], nil
}

func (c *ComicVineProvider) SearchMetadata(ctx context.Context, title string, limit, offset int) ([]*SeriesMetadata, int, error) {
	if c.apiKey == "" {
		return nil, 0, fmt.Errorf("comicvine: api key not configured")
	}
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	query := url.Values{}
	query.Set("api_key", c.apiKey)
	query.Set("format", "json")
	query.Set("resources", "volume")
	query.Set("query", title)
	query.Set("limit", fmt.Sprintf("%d", limit))
	query.Set("offset", fmt.Sprintf("%d", offset))
	query.Set("field_list", "id,name,deck,description,image,count_of_issues,start_year,site_detail_url,publisher")

	apiURL := comicvineBaseURL + "?" + query.Encode()

	slog.Info("Comic Vine search request", "query", title, "limit", limit, "offset", offset)

	// 有限次指数退避重试：仅对 429 与 5xx 重试，尊重 Retry-After；退避可被 context 取消打断。
	var result comicvineSearchResponse
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
		if err != nil {
			return nil, 0, fmt.Errorf("comicvine: failed to create request: %w", err)
		}
		// Comic Vine 会拒绝空 User-Agent，必须显式设置。
		req.Header.Set("User-Agent", "MangaManager/1.0 (https://github.com/manga-manager)")
		req.Header.Set("Accept", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, 0, ctx.Err()
			}
			return nil, 0, fmt.Errorf("comicvine: request failed: %w", err)
		}

		if resp.StatusCode == http.StatusOK {
			decErr := json.NewDecoder(resp.Body).Decode(&result)
			resp.Body.Close()
			if decErr != nil {
				return nil, 0, fmt.Errorf("comicvine: failed to decode response: %w", decErr)
			}
			break
		}

		respBody, _ := io.ReadAll(resp.Body)
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
		status := resp.StatusCode
		resp.Body.Close()

		if (status == http.StatusTooManyRequests || status >= 500) && attempt < comicvineMaxRetries {
			wait := retryAfter
			if wait <= 0 {
				wait = backoffDelay(attempt)
			}
			slog.Warn("Comic Vine API throttled, backing off", "status", status, "attempt", attempt+1, "wait", wait.String())
			if werr := sleepWithContext(ctx, wait); werr != nil {
				return nil, 0, werr
			}
			continue
		}

		slog.Error("Comic Vine API error", "status", status, "body", string(respBody))
		return nil, 0, fmt.Errorf("comicvine: API returned status %d: %s", status, string(respBody))
	}

	if len(result.Results) == 0 {
		return nil, 0, nil // 未找到匹配结果
	}

	var metadatas []*SeriesMetadata
	for idx, item := range result.Results {
		metadatas = append(metadatas, c.convertToSeriesMetadata(item, idx))
	}

	return metadatas, result.NumberOfTotalResults, nil
}

func (c *ComicVineProvider) convertToSeriesMetadata(item comicvineVolume, rank int) *SeriesMetadata {
	// Summary：优先使用短描述 deck，否则清洗长描述 description 的 HTML。
	summary := strings.TrimSpace(item.Deck)
	if summary == "" {
		summary = stripComicVineHTML(item.Description)
	}

	var coverURL string
	if item.Image != nil {
		if item.Image.SuperURL != "" {
			coverURL = item.Image.SuperURL
		} else if item.Image.MediumURL != "" {
			coverURL = item.Image.MediumURL
		}
	}

	var publisher string
	if item.Publisher != nil {
		publisher = strings.TrimSpace(item.Publisher.Name)
	}

	confidence := 0.9 - float64(rank)*0.05
	if summary == "" {
		confidence -= 0.05
	}
	if confidence < 0.4 {
		confidence = 0.4
	}

	return &SeriesMetadata{
		Title:       item.Name,
		Summary:     summary,
		Publisher:   publisher,
		CoverURL:    coverURL,
		Rating:      0, // Comic Vine 不提供评分
		SourceID:    item.ID,
		SourceURL:   item.SiteDetailURL,
		Provider:    c.Name(),
		Confidence:  confidence,
		ReleaseDate: strings.TrimSpace(item.StartYear),
		VolumeCount: item.CountOfIssues,
	}
}

// stripComicVineHTML 去除 Comic Vine description 中的 HTML 标签并反转义实体，最后折叠空白。
func stripComicVineHTML(s string) string {
	if s == "" {
		return ""
	}
	s = comicvineTagRegexp.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}
