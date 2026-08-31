package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ============================================================
// MangaDex API 数据实体
// ============================================================

type mangadexSearchResult struct {
	Data  []mangadexManga `json:"data"`
	Total int             `json:"total"`
}

type mangadexManga struct {
	ID            string                 `json:"id"`
	Attributes    mangadexAttributes     `json:"attributes"`
	Relationships []mangadexRelationship `json:"relationships"`
}

type mangadexAttributes struct {
	Title                  map[string]string   `json:"title"`
	AltTitles              []map[string]string `json:"altTitles"`
	Description            map[string]string   `json:"description"`
	Status                 string              `json:"status"`
	Year                   int                 `json:"year"`
	LastVolume             string              `json:"lastVolume"`
	PublicationDemographic *string             `json:"publicationDemographic"`
	Tags                   []mangadexTag       `json:"tags"`
}

type mangadexTag struct {
	Attributes struct {
		Name map[string]string `json:"name"`
	} `json:"attributes"`
}

type mangadexRelationship struct {
	Type       string `json:"type"`
	Attributes struct {
		FileName string `json:"fileName"`
		Name     string `json:"name"`
	} `json:"attributes"`
}

// ============================================================
// MangaDexProvider 实现
// ============================================================

type MangaDexProvider struct {
	BaseURL    string
	httpClient *http.Client
}

// NewMangaDexProvider 构造无需密钥的 MangaDex 元数据 Provider（HTTP 超时 15s）。
func NewMangaDexProvider() *MangaDexProvider {
	return &MangaDexProvider{
		BaseURL: "https://api.mangadex.org",
		// 出站请求经进程级并发闸门（见 provider_budget.go）：多个刮削任务可以同时在跑，
		// 每个都各自 new 一个 Provider，不在这里收口就会把对同一数据源的请求速率成倍放大。
		httpClient: withProviderBudget(&http.Client{
			Timeout: 15 * time.Second,
		}, "MangaDex"),
	}
}

func (m *MangaDexProvider) Name() string {
	return "MangaDex"
}

func (m *MangaDexProvider) FetchSeriesMetadata(ctx context.Context, title string) (*SeriesMetadata, error) {
	results, _, err := m.SearchMetadata(ctx, title, 1, 0)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}
	return results[0], nil
}

func (m *MangaDexProvider) SearchMetadata(ctx context.Context, title string, limit, offset int) ([]*SeriesMetadata, int, error) {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	// includes[] 与 contentRating[] 是可重复的同名参数，直接写入 Values 的切片。
	q := url.Values{}
	q.Set("title", title)
	q.Set("limit", strconv.Itoa(limit))
	q.Set("offset", strconv.Itoa(offset))
	q["includes[]"] = []string{"cover_art", "author", "artist"}
	q["contentRating[]"] = []string{"safe", "suggestive", "erotica"}
	q.Set("order[relevance]", "desc")

	apiURL := m.BaseURL + "/manga?" + q.Encode()

	slog.Info("MangaDex search request", "url", apiURL, "title", title, "limit", limit, "offset", offset)

	// 有限次指数退避重试：仅对 429 与 5xx 重试，尊重 Retry-After；退避可被 context 取消打断。
	var result mangadexSearchResult
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
		if err != nil {
			return nil, 0, fmt.Errorf("mangadex: failed to create request: %w", err)
		}
		req.Header.Set("User-Agent", "MangaManager/1.0 (https://github.com/manga-manager)")
		req.Header.Set("Accept", "application/json")

		resp, err := m.httpClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, 0, ctx.Err()
			}
			return nil, 0, fmt.Errorf("mangadex: request failed: %w", sanitizeTransportError(err))
		}

		if resp.StatusCode == http.StatusOK {
			decErr := json.NewDecoder(resp.Body).Decode(&result)
			resp.Body.Close()
			if decErr != nil {
				return nil, 0, fmt.Errorf("mangadex: failed to decode response: %w", decErr)
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
			slog.Warn("MangaDex API throttled, backing off", "status", status, "attempt", attempt+1, "wait", wait.String(), "url", apiURL)
			if werr := sleepWithContext(ctx, wait); werr != nil {
				return nil, 0, werr
			}
			continue
		}

		safeBody := logUpstreamFailure("MangaDex API error", status, respBody, "", "url", apiURL)
		return nil, 0, fmt.Errorf("mangadex: API returned status %d: %s", status, safeBody)
	}

	if len(result.Data) == 0 {
		return nil, 0, nil // 未找到匹配结果
	}

	var metadatas []*SeriesMetadata
	for idx, item := range result.Data {
		metadatas = append(metadatas, m.convertToSeriesMetadata(item, idx))
	}

	return metadatas, result.Total, nil
}

func (m *MangaDexProvider) convertToSeriesMetadata(item mangadexManga, rank int) *SeriesMetadata {
	attr := item.Attributes

	// 标题：优先英文标题，其次 title map 任意值，最后回退别名首项。
	displayTitle := firstLocalizedTitle(attr.Title, "en")
	if displayTitle == "" {
		displayTitle = firstAltTitle(attr.AltTitles)
	}

	// 原名：优先别名中的日文（ja 或 ja-ro）。
	originalTitle := altTitleByLang(attr.AltTitles, "ja", "ja-ro")

	// 简介：优先英文，其次任意语言。
	summary := firstLocalizedTitle(attr.Description, "en")

	// 封面：拼接原图地址（不加尺寸后缀）。
	var coverURL string
	for _, rel := range item.Relationships {
		if rel.Type == "cover_art" && rel.Attributes.FileName != "" {
			coverURL = fmt.Sprintf("https://uploads.mangadex.org/covers/%s/%s", item.ID, rel.Attributes.FileName)
			break
		}
	}

	// 标签：取英文标签名。
	var tags []string
	for _, t := range attr.Tags {
		if name := strings.TrimSpace(t.Attributes.Name["en"]); name != "" {
			tags = append(tags, name)
		}
	}

	// 作者/绘师：author→Writer，artist→Penciller，按角色+姓名去重。
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
	for _, rel := range item.Relationships {
		switch rel.Type {
		case "author":
			addAuthor(rel.Attributes.Name, "Writer")
		case "artist":
			addAuthor(rel.Attributes.Name, "Penciller")
		}
	}

	// 发行年份：仅在 year 非 0 时填年份字符串。
	var releaseDate string
	if attr.Year != 0 {
		releaseDate = strconv.Itoa(attr.Year)
	}

	// 册数：解析 lastVolume，失败填 0。
	volumeCount := 0
	if v, err := strconv.Atoi(strings.TrimSpace(attr.LastVolume)); err == nil {
		volumeCount = v
	}

	// 置信度：随排名递减，简介为空再减，下限 0.4。
	confidence := 0.9 - float64(rank)*0.05
	if summary == "" {
		confidence -= 0.05
	}
	if confidence < 0.4 {
		confidence = 0.4
	}

	return &SeriesMetadata{
		Title:         displayTitle,
		OriginalTitle: originalTitle,
		Summary:       summary,
		Status:        attr.Status,
		CoverURL:      coverURL,
		Rating:        0, // 基础接口无聚合评分
		Tags:          tags,
		Authors:       authors,
		SourceID:      0, // MangaDex 使用 UUID，int 字段填 0
		SourceURL:     fmt.Sprintf("https://mangadex.org/title/%s", item.ID),
		Provider:      m.Name(),
		Confidence:    confidence,
		ReleaseDate:   releaseDate,
		VolumeCount:   volumeCount,
	}
}

// firstLocalizedTitle 从多语言 map 中取值：优先给定语言，否则返回任意非空值。
func firstLocalizedTitle(m map[string]string, preferred ...string) string {
	for _, k := range preferred {
		if v := strings.TrimSpace(m[k]); v != "" {
			return v
		}
	}
	// 首选语言都没命中时的兜底必须是确定性的：Go 的 map 迭代顺序随机，直接 range 会让
	// 同一条目每次搜索返回不同的标题/简介，用户看到的结果凭空变来变去。按 key 排序后取首个。
	return firstValueBySortedKey(m)
}

// firstValueBySortedKey 按 key 字典序返回首个非空值，保证同一份 map 每次得到同一结果。
func firstValueBySortedKey(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if s := strings.TrimSpace(m[k]); s != "" {
			return s
		}
	}
	return ""
}

// altTitleByLang 遍历别名列表，返回首个命中给定语言的标题。
func altTitleByLang(altTitles []map[string]string, langs ...string) string {
	for _, lang := range langs {
		for _, alt := range altTitles {
			if v := strings.TrimSpace(alt[lang]); v != "" {
				return v
			}
		}
	}
	return ""
}

// firstAltTitle 返回别名列表中首个非空标题，用作主标题的兜底。
func firstAltTitle(altTitles []map[string]string) string {
	// 同样按 key 排序取值：别名 map 的迭代顺序随机会让兜底标题在多次搜索间跳变。
	for _, alt := range altTitles {
		if s := firstValueBySortedKey(alt); s != "" {
			return s
		}
	}
	return ""
}
