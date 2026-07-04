// 业务说明：本文件是业务实现，属于元数据聚合链路，负责从 MyAnimeList 官方 API 获取漫画标题、简介、评分、标签与作者信息。
// 它支撑系列详情、智能补全和搜索候选审核，为多数据源统一结构提供 MAL 侧内容质量。
// 维护时应关注 Provider 契约、Client-ID 鉴权、失败回退、限流退避与状态字段映射的稳定性。

package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// MyAnimeList 抓取的有限次指数退避重试参数：仅针对 429 与 5xx，尊重 Retry-After。
const (
	malMaxRetries   = 3
	malMaxDelay     = 30 * time.Second
	malDefaultLimit = 20
	malMaxLimit     = 100
	malAPIEndpoint  = "https://api.myanimelist.net/v2/manga"
)

// ============================================================
// MyAnimeList API v2 数据实体
// ============================================================

type malSearchResult struct {
	Data   []malNodeWrapper `json:"data"`
	Paging malPaging        `json:"paging"`
}

type malPaging struct {
	Next     string `json:"next"`
	Previous string `json:"previous"`
}

type malNodeWrapper struct {
	Node malMangaNode `json:"node"`
}

type malMangaNode struct {
	ID          int         `json:"id"`
	Title       string      `json:"title"`
	MainPicture *malPicture `json:"main_picture"`
	Synopsis    string      `json:"synopsis"`
	Mean        float64     `json:"mean"`
	Genres      []malGenre  `json:"genres"`
	Authors     []malAuthor `json:"authors"`
	NumVolumes  int         `json:"num_volumes"`
	Status      string      `json:"status"`
	StartDate   string      `json:"start_date"`
}

type malPicture struct {
	Large  string `json:"large"`
	Medium string `json:"medium"`
}

type malGenre struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type malAuthor struct {
	Node malAuthorNode `json:"node"`
	Role string        `json:"role"`
}

type malAuthorNode struct {
	ID        int    `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

// ============================================================
// MyAnimeListProvider 实现
// ============================================================

type MyAnimeListProvider struct {
	clientID   string
	httpClient *http.Client
}

// NewMyAnimeListProvider 构造 MAL Provider，clientID 用于 X-MAL-CLIENT-ID 鉴权头。
func NewMyAnimeListProvider(clientID string) *MyAnimeListProvider {
	return &MyAnimeListProvider{
		clientID: clientID,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (m *MyAnimeListProvider) Name() string {
	return "MyAnimeList"
}

func (m *MyAnimeListProvider) FetchSeriesMetadata(ctx context.Context, title string) (*SeriesMetadata, error) {
	results, _, err := m.SearchMetadata(ctx, title, 1, 0)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}
	return results[0], nil
}

func (m *MyAnimeListProvider) SearchMetadata(ctx context.Context, title string, limit, offset int) ([]*SeriesMetadata, int, error) {
	if m.clientID == "" {
		return nil, 0, fmt.Errorf("myanimelist: client id not configured")
	}
	if limit <= 0 {
		limit = malDefaultLimit
	}
	if limit > malMaxLimit {
		limit = malMaxLimit
	}
	if offset < 0 {
		offset = 0
	}

	query := url.Values{}
	query.Set("q", title)
	query.Set("limit", fmt.Sprintf("%d", limit))
	query.Set("offset", fmt.Sprintf("%d", offset))
	query.Set("fields", "id,title,main_picture,synopsis,mean,genres,authors{first_name,last_name},num_volumes,status,start_date")
	apiURL := malAPIEndpoint + "?" + query.Encode()

	slog.Info("MyAnimeList search request", "keyword", title, "limit", limit, "offset", offset)

	// 有限次指数退避重试：仅对 429 与 5xx 重试，尊重 Retry-After；退避可被 context 取消打断。
	var result malSearchResult
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
		if err != nil {
			return nil, 0, fmt.Errorf("myanimelist: failed to create request: %w", err)
		}
		req.Header.Set("X-MAL-CLIENT-ID", m.clientID)

		resp, err := m.httpClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, 0, ctx.Err()
			}
			return nil, 0, fmt.Errorf("myanimelist: request failed: %w", err)
		}

		if resp.StatusCode == http.StatusOK {
			decErr := json.NewDecoder(resp.Body).Decode(&result)
			resp.Body.Close()
			if decErr != nil {
				return nil, 0, fmt.Errorf("myanimelist: failed to decode response: %w", decErr)
			}
			break
		}

		respBody, _ := io.ReadAll(resp.Body)
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
		status := resp.StatusCode
		resp.Body.Close()

		if (status == http.StatusTooManyRequests || status >= 500) && attempt < malMaxRetries {
			wait := retryAfter
			if wait <= 0 {
				wait = backoffDelay(attempt)
			}
			if wait > malMaxDelay {
				wait = malMaxDelay
			}
			slog.Warn("MyAnimeList API throttled, backing off", "status", status, "attempt", attempt+1, "wait", wait.String())
			if werr := sleepWithContext(ctx, wait); werr != nil {
				return nil, 0, werr
			}
			continue
		}

		slog.Error("MyAnimeList API error", "status", status, "body", string(respBody))
		return nil, 0, fmt.Errorf("myanimelist: API returned status %d: %s", status, string(respBody))
	}

	if len(result.Data) == 0 {
		return nil, 0, nil // 未找到匹配结果
	}

	metadatas := make([]*SeriesMetadata, 0, len(result.Data))
	for idx, item := range result.Data {
		metadatas = append(metadatas, m.convertToSeriesMetadata(item.Node, idx))
	}

	// MAL 不返回精确总数：用 offset + 本页数量近似；若有下一页则 +1 提示还有更多。
	total := offset + len(result.Data)
	if result.Paging.Next != "" {
		total++
	}

	return metadatas, total, nil
}

func (m *MyAnimeListProvider) convertToSeriesMetadata(node malMangaNode, rank int) *SeriesMetadata {
	// 封面：优先 large，否则 medium
	var coverURL string
	if node.MainPicture != nil {
		if node.MainPicture.Large != "" {
			coverURL = node.MainPicture.Large
		} else if node.MainPicture.Medium != "" {
			coverURL = node.MainPicture.Medium
		}
	}

	// 标签：取所有 genre 名称
	var tags []string
	for _, g := range node.Genres {
		if g.Name != "" {
			tags = append(tags, g.Name)
		}
	}

	// 作者：拼接姓名并按 role 归类
	var authors []SeriesAuthor
	for _, a := range node.Authors {
		name := strings.TrimSpace(strings.TrimSpace(a.Node.FirstName) + " " + strings.TrimSpace(a.Node.LastName))
		if name == "" {
			continue
		}
		authors = append(authors, SeriesAuthor{Name: name, Role: mapMALAuthorRole(a.Role)})
	}

	confidence := 0.9 - float64(rank)*0.05
	if node.Synopsis == "" {
		confidence -= 0.05
	}
	if confidence < 0.4 {
		confidence = 0.4
	}

	return &SeriesMetadata{
		Title:       node.Title,
		Summary:     node.Synopsis,
		Status:      mapMALStatus(node.Status),
		CoverURL:    coverURL,
		Rating:      node.Mean,
		Tags:        tags,
		Authors:     authors,
		SourceID:    node.ID,
		SourceURL:   fmt.Sprintf("https://myanimelist.net/manga/%d", node.ID),
		Provider:    m.Name(),
		Confidence:  confidence,
		ReleaseDate: node.StartDate,
		VolumeCount: node.NumVolumes,
	}
}

// mapMALAuthorRole 将 MAL 的作者 role 归类为内部统一角色。
// 含 Story/Author/Original → Writer；含 Art → Penciller；其它默认 Writer。
func mapMALAuthorRole(role string) string {
	lower := strings.ToLower(role)
	switch {
	case strings.Contains(lower, "story"), strings.Contains(lower, "author"), strings.Contains(lower, "original"):
		return "Writer"
	case strings.Contains(lower, "art"):
		return "Penciller"
	default:
		return "Writer"
	}
}

// mapMALStatus 将 MAL 的连载状态映射为内部小写状态，未知留空。
func mapMALStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "currently_publishing":
		return "ongoing"
	case "finished":
		return "completed"
	case "on_hiatus":
		return "hiatus"
	case "discontinued":
		return "cancelled"
	default:
		return ""
	}
}
