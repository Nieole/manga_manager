// 业务说明：本文件是业务回归测试，属于元数据聚合链路，负责从本地规则、外部站点和 AI Provider 获取漫画标题、简介、人物、标签与关系信息。
// 它通过自动化断言保护对应业务场景在扫描、读取、展示或配置变更后仍保持兼容。
// 维护时应让用例名称、测试数据和断言结果直接反映真实用户流程，而不是只覆盖实现细节。

package metadata

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestOpenAILegacyGenerateGroupingParsesCollections(t *testing.T) {
	provider := NewOpenAILegacyProvider("http://example.test/v1/chat/completions", "test-model", "", 5)
	provider.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/v1/chat/completions" {
				t.Fatalf("unexpected request path: %s", req.URL.Path)
			}
			body := `{"choices":[{"message":{"role":"assistant","content":"{\"collections\":[{\"name\":\"热血冒险\",\"description\":\"主角成长与冒险作品\",\"series_ids\":[1,2]}]}"}}]}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    req,
			}, nil
		}),
	}

	groups, err := provider.GenerateGrouping(context.Background(), []CandidateSeries{
		{ID: 1, Title: "海贼王", Summary: "冒险"},
		{ID: 2, Title: "火影忍者", Summary: "忍者成长"},
	})
	if err != nil {
		t.Fatalf("GenerateGrouping failed: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].Name != "热血冒险" || len(groups[0].SeriesIDs) != 2 || groups[0].SeriesIDs[1] != 2 {
		t.Fatalf("unexpected grouping payload: %+v", groups[0])
	}
}

func TestOpenAILegacySearchMetadataReturnsSingleResult(t *testing.T) {
	provider := NewOpenAILegacyProvider("http://example.test/v1/chat/completions", "test-model", "", 5)
	provider.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body := `{"choices":[{"message":{"role":"assistant","content":"{\"title\":\"海贼王\",\"summary\":\"草帽一伙的冒险\",\"confidence\":0.8}"}}]}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    req,
			}, nil
		}),
	}

	results, total, err := provider.SearchMetadata(context.Background(), "海贼王", 10, 0)
	if err != nil {
		t.Fatalf("SearchMetadata failed: %v", err)
	}
	if total != 1 || len(results) != 1 {
		t.Fatalf("expected single result, got total=%d len=%d", total, len(results))
	}
	if results[0].Title != "海贼王" || results[0].Summary != "草帽一伙的冒险" {
		t.Fatalf("unexpected metadata: %+v", results[0])
	}
}

func TestOpenAILegacySearchMetadataEmptyOnBlank(t *testing.T) {
	provider := NewOpenAILegacyProvider("http://example.test/v1/chat/completions", "test-model", "", 5)
	provider.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body := `{"choices":[{"message":{"role":"assistant","content":"{\"title\":\"\",\"summary\":\"\"}"}}]}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    req,
			}, nil
		}),
	}

	results, total, err := provider.SearchMetadata(context.Background(), "不存在的作品", 10, 0)
	if err != nil {
		t.Fatalf("SearchMetadata failed: %v", err)
	}
	if total != 0 || len(results) != 0 {
		t.Fatalf("expected empty result, got total=%d len=%d", total, len(results))
	}
}

func TestAIGroupingResultKeepsGroupsFallback(t *testing.T) {
	var result AIGroupingResult
	if err := json.Unmarshal([]byte(`{"groups":[{"name":"Classic","series_ids":[7]}]}`), &result); err != nil {
		t.Fatalf("unmarshal grouping failed: %v", err)
	}
	normalized := result.NormalizedCollections()
	if len(normalized) != 1 || normalized[0].Name != "Classic" || normalized[0].SeriesIDs[0] != 7 {
		t.Fatalf("unexpected normalized groups: %+v", normalized)
	}
}
