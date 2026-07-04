// 业务说明：本文件是业务实现，属于元数据聚合链路，负责通过 AniList GraphQL API 获取漫画标题、简介、封面、评分、标签与作者信息。
// 它为系列详情补全、搜索候选与关系图谱提供英文/罗马音/原生标题等多语言来源，作为 Bangumi 之外的补充数据源。
// 维护时应关注 GraphQL 查询字段兼容、HTML 简介清洗、限流退避以及作者角色映射的正确性。

package metadata

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// AniList 的 GraphQL 端点：无需密钥，POST JSON 载荷 {"query": ..., "variables": ...}。
const anilistEndpoint = "https://graphql.anilist.co"

// anilistQuery 查询 MANGA 类型的分页结果，字段与映射逻辑一一对应。
const anilistQuery = `query ($search: String, $page: Int, $perPage: Int) {
  Page(page: $page, perPage: $perPage) {
    pageInfo { total }
    media(search: $search, type: MANGA) {
      id
      title { romaji english native }
      description(asHtml: false)
      coverImage { extraLarge large }
      averageScore
      genres
      tags { name rank }
      staff(perPage: 8) { edges { role node { name { full } } } }
      startDate { year month day }
      volumes
      status
      siteUrl
    }
  }
}`

// anilistHTMLTagRe 用于剥离简介中的 HTML 标签（如 <i>、<b> 等）。
var anilistHTMLTagRe = regexp.MustCompile(`<[^>]*>`)

// ============================================================
// AniList GraphQL 数据实体
// ============================================================

type anilistGraphQLRequest struct {
	Query     string           `json:"query"`
	Variables anilistVariables `json:"variables"`
}

type anilistVariables struct {
	Search  string `json:"search"`
	Page    int    `json:"page"`
	PerPage int    `json:"perPage"`
}

type anilistGraphQLResponse struct {
	Data struct {
		Page anilistPage `json:"Page"`
	} `json:"data"`
	Errors []anilistGraphQLError `json:"errors"`
}

type anilistGraphQLError struct {
	Message string `json:"message"`
}

type anilistPage struct {
	PageInfo struct {
		Total int `json:"total"`
	} `json:"pageInfo"`
	Media []anilistMedia `json:"media"`
}

type anilistMedia struct {
	ID    int `json:"id"`
	Title struct {
		Romaji  string `json:"romaji"`
		English string `json:"english"`
		Native  string `json:"native"`
	} `json:"title"`
	Description string `json:"description"`
	CoverImage  struct {
		ExtraLarge string `json:"extraLarge"`
		Large      string `json:"large"`
	} `json:"coverImage"`
	AverageScore int      `json:"averageScore"`
	Genres       []string `json:"genres"`
	Tags         []struct {
		Name string `json:"name"`
		Rank int    `json:"rank"`
	} `json:"tags"`
	Staff struct {
		Edges []struct {
			Role string `json:"role"`
			Node struct {
				Name struct {
					Full string `json:"full"`
				} `json:"name"`
			} `json:"node"`
		} `json:"edges"`
	} `json:"staff"`
	StartDate struct {
		Year  int `json:"year"`
		Month int `json:"month"`
		Day   int `json:"day"`
	} `json:"startDate"`
	Volumes int    `json:"volumes"`
	Status  string `json:"status"`
	SiteURL string `json:"siteUrl"`
}

// ============================================================
// AniListProvider 实现
// ============================================================

type AniListProvider struct {
	Endpoint   string
	httpClient *http.Client
}

func NewAniListProvider() *AniListProvider {
	return &AniListProvider{
		Endpoint: anilistEndpoint,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (a *AniListProvider) Name() string {
	return "AniList"
}

func (a *AniListProvider) FetchSeriesMetadata(ctx context.Context, title string) (*SeriesMetadata, error) {
	results, _, err := a.SearchMetadata(ctx, title, 1, 0)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}
	return results[0], nil
}

func (a *AniListProvider) SearchMetadata(ctx context.Context, title string, limit, offset int) ([]*SeriesMetadata, int, error) {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	page := offset/limit + 1

	reqBody := anilistGraphQLRequest{
		Query: anilistQuery,
		Variables: anilistVariables{
			Search:  title,
			Page:    page,
			PerPage: limit,
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("anilist: failed to marshal request: %w", err)
	}

	slog.Info("AniList search request (POST)", "url", a.Endpoint, "search", title, "page", page, "perPage", limit)

	// 有限次指数退避重试：仅对 429 与 5xx 重试，尊重 Retry-After（AniList 通常缺省时用退避）；退避可被 context 取消打断。
	var result anilistGraphQLResponse
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.Endpoint, bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, 0, fmt.Errorf("anilist: failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "MangaManager/1.0 (https://github.com/manga-manager)")

		resp, err := a.httpClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, 0, ctx.Err()
			}
			return nil, 0, fmt.Errorf("anilist: request failed: %w", err)
		}

		if resp.StatusCode == http.StatusOK {
			decErr := json.NewDecoder(resp.Body).Decode(&result)
			resp.Body.Close()
			if decErr != nil {
				return nil, 0, fmt.Errorf("anilist: failed to decode response: %w", decErr)
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
			slog.Warn("AniList API throttled, backing off", "status", status, "attempt", attempt+1, "wait", wait.String(), "url", a.Endpoint)
			if werr := sleepWithContext(ctx, wait); werr != nil {
				return nil, 0, werr
			}
			continue
		}

		slog.Error("AniList API error", "status", status, "body", string(respBody), "url", a.Endpoint)
		return nil, 0, fmt.Errorf("anilist: API returned status %d: %s", status, string(respBody))
	}

	if len(result.Errors) > 0 {
		return nil, 0, fmt.Errorf("anilist: GraphQL error: %s", result.Errors[0].Message)
	}

	if len(result.Data.Page.Media) == 0 {
		return nil, 0, nil // 未找到匹配结果
	}

	var metadatas []*SeriesMetadata
	for idx, item := range result.Data.Page.Media {
		metadatas = append(metadatas, a.convertToSeriesMetadata(item, idx))
	}

	return metadatas, result.Data.Page.PageInfo.Total, nil
}

func (a *AniListProvider) convertToSeriesMetadata(m anilistMedia, rank int) *SeriesMetadata {
	// 标题：english 优先，其次 romaji，再次 native。
	displayTitle := m.Title.English
	if displayTitle == "" {
		displayTitle = m.Title.Romaji
	}
	if displayTitle == "" {
		displayTitle = m.Title.Native
	}

	// 原名：native 优先，缺失回退 romaji。
	originalTitle := m.Title.Native
	if originalTitle == "" {
		originalTitle = m.Title.Romaji
	}

	summary := anilistStripHTML(m.Description)

	// 封面：extraLarge 优先否则 large。
	coverURL := m.CoverImage.ExtraLarge
	if coverURL == "" {
		coverURL = m.CoverImage.Large
	}

	// 评分：averageScore 为 0–100，统一换算到 0–10。
	rating := float64(m.AverageScore) / 10.0

	// 标签：genres 全部 + tags 中 rank>=60 的名字。
	var tags []string
	for _, g := range m.Genres {
		if g != "" {
			tags = append(tags, g)
		}
	}
	for _, t := range m.Tags {
		if t.Name != "" && t.Rank >= 60 {
			tags = append(tags, t.Name)
		}
	}

	authors := anilistExtractAuthors(m)

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
		CoverURL:      coverURL,
		Status:        anilistMapStatus(m.Status),
		Rating:        rating,
		Tags:          tags,
		Authors:       authors,
		SourceID:      m.ID,
		SourceURL:     m.SiteURL,
		Provider:      a.Name(),
		Confidence:    confidence,
		ReleaseDate:   anilistReleaseDate(m.StartDate.Year, m.StartDate.Month, m.StartDate.Day),
		VolumeCount:   m.Volumes,
	}
}

// anilistExtractAuthors 依据 staff.role 映射参与人员角色：
// 含 "Story"/"Original" → Writer；含 "Art" → Penciller（"Story & Art" 会同时加两条）。
func anilistExtractAuthors(m anilistMedia) []SeriesAuthor {
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
	for _, edge := range m.Staff.Edges {
		name := edge.Node.Name.Full
		roleLower := strings.ToLower(edge.Role)
		if strings.Contains(roleLower, "story") || strings.Contains(roleLower, "original") {
			addAuthor(name, "Writer")
		}
		if strings.Contains(roleLower, "art") {
			addAuthor(name, "Penciller")
		}
	}
	return authors
}

// anilistMapStatus 将 AniList 的发布状态映射为内部小写状态，未知返回空串。
func anilistMapStatus(status string) string {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "RELEASING":
		return "ongoing"
	case "FINISHED":
		return "completed"
	case "HIATUS":
		return "hiatus"
	case "CANCELLED":
		return "cancelled"
	default:
		return ""
	}
}

// anilistReleaseDate 由 startDate 拼出 "YYYY-MM-DD"，缺失的月/日部分省略；无年份返回空串。
func anilistReleaseDate(year, month, day int) string {
	if year <= 0 {
		return ""
	}
	s := fmt.Sprintf("%04d", year)
	if month > 0 {
		s += fmt.Sprintf("-%02d", month)
		if day > 0 {
			s += fmt.Sprintf("-%02d", day)
		}
	}
	return s
}

// anilistStripHTML 清洗简介中的 HTML：将换行标签转为换行，剥离其余标签并反转义实体。
func anilistStripHTML(s string) string {
	if s == "" {
		return ""
	}
	replacer := strings.NewReplacer(
		"<br>", "\n",
		"<br/>", "\n",
		"<br />", "\n",
		"<BR>", "\n",
		"<BR/>", "\n",
		"<BR />", "\n",
	)
	s = replacer.Replace(s)
	s = anilistHTMLTagRe.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	return strings.TrimSpace(s)
}
