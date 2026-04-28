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
