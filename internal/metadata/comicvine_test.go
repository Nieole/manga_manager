// 业务说明：本文件是 Comic Vine Provider 的解析回归测试，验证 apiKey 前置校验、deck 优先/description 清洗回退、
// 封面 super/medium 优先级、出版商与发行年份提取、总数透传，以及 stripComicVineHTML 纯逻辑。

package metadata

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestComicVineRequiresAPIKey(t *testing.T) {
	p := NewComicVineProvider("")
	_, _, err := p.SearchMetadata(context.Background(), "saga", 10, 0)
	if err == nil || !strings.Contains(err.Error(), "api key not configured") {
		t.Fatalf("expected api-key error, got %v", err)
	}
}

func TestComicVineSearchMetadataParsesFixture(t *testing.T) {
	body := `{
	  "error":"OK",
	  "number_of_total_results": 3,
	  "results":[
	    {"id":100,"name":"Saga","deck":"Space opera drama.","description":"<p>Long &amp; winding tale.</p>",
	     "image":{"super_url":"https://cv/super.jpg","medium_url":"https://cv/med.jpg"},
	     "count_of_issues":54,"start_year":"2012","site_detail_url":"https://comicvine.com/saga",
	     "publisher":{"name":"Image Comics"}}
	  ]
	}`
	var gotUA, gotKey string
	p := NewComicVineProvider("key-xyz")
	p.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		gotUA = req.Header.Get("User-Agent")
		gotKey = req.URL.Query().Get("api_key")
		return metaJSONResponse(req, body), nil
	})}

	results, total, err := p.SearchMetadata(context.Background(), "saga", 20, 0)
	if err != nil {
		t.Fatalf("SearchMetadata failed: %v", err)
	}
	if gotUA == "" {
		t.Error("expected non-empty User-Agent (Comic Vine rejects empty)")
	}
	if gotKey != "key-xyz" {
		t.Errorf("api_key query = %q, want injected key", gotKey)
	}
	if total != 3 || len(results) != 1 {
		t.Fatalf("expected total=3 len=1, got total=%d len=%d", total, len(results))
	}
	m := results[0]
	if m.Title != "Saga" {
		t.Errorf("Title = %q", m.Title)
	}
	if m.Summary != "Space opera drama." {
		t.Errorf("Summary = %q, want deck preferred", m.Summary)
	}
	if m.Publisher != "Image Comics" {
		t.Errorf("Publisher = %q", m.Publisher)
	}
	if m.CoverURL != "https://cv/super.jpg" {
		t.Errorf("CoverURL = %q, want super_url", m.CoverURL)
	}
	if m.ReleaseDate != "2012" || m.VolumeCount != 54 || m.SourceID != 100 {
		t.Errorf("date/vols/id = %q/%d/%d", m.ReleaseDate, m.VolumeCount, m.SourceID)
	}
	if m.SourceURL != "https://comicvine.com/saga" || m.Provider != "Comic Vine" {
		t.Errorf("SourceURL/Provider = %q/%q", m.SourceURL, m.Provider)
	}
	assertMetaFloat(t, "Rating", m.Rating, 0)
	assertMetaFloat(t, "Confidence", m.Confidence, 0.9)
}

func TestComicVineDeckFallbackAndMediumCover(t *testing.T) {
	p := NewComicVineProvider("k")
	// deck 为空 → 用清洗后的 description；super_url 缺失 → medium_url。
	item := comicvineVolume{
		ID:          7,
		Name:        "X",
		Deck:        "   ",
		Description: "<p>Long &amp; winding tale.</p>",
		Image:       &comicvineImage{MediumURL: "https://cv/med.jpg"},
	}
	got := p.convertToSeriesMetadata(item, 0)
	if got.Summary != "Long & winding tale." {
		t.Errorf("Summary = %q, want cleaned description", got.Summary)
	}
	if got.CoverURL != "https://cv/med.jpg" {
		t.Errorf("CoverURL = %q, want medium fallback", got.CoverURL)
	}
}

func TestComicVineConfidenceFloorAndNoSummaryPenalty(t *testing.T) {
	p := NewComicVineProvider("k")
	// 无 deck 无 description → summary 空 → 0.9-0.05 = 0.85（rank0）。
	got := p.convertToSeriesMetadata(comicvineVolume{ID: 1, Name: "Y"}, 0)
	assertMetaFloat(t, "Confidence(no summary)", got.Confidence, 0.85)
	// 高排名 → 下限 0.4。
	got2 := p.convertToSeriesMetadata(comicvineVolume{ID: 2, Name: "Z", Deck: "has summary"}, 20)
	assertMetaFloat(t, "Confidence floor", got2.Confidence, 0.4)
}

func TestStripComicVineHTML(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"<b>Hello</b> &amp;  world", "Hello & world"},
		{"<p>one</p><p>two</p>", "one two"},
		{"   spaced   text   ", "spaced text"},
	}
	for _, c := range cases {
		if got := stripComicVineHTML(c.in); got != c.want {
			t.Errorf("stripComicVineHTML(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestComicVineDoesNotLeakAPIKeyOnRedirect 守卫「跟随重定向时不外泄凭据」。
//
// Comic Vine 只支持在 query 里传 api_key（官方文档没有任何 header 认证方式），所以密钥
// 必然出现在请求 URL 上。而 Go 的 http.Client 在跟随重定向时会把**上一跳的完整 URL**
// 自动填进 Referer——stripSensitiveHeaders 只清 Authorization/Cookie 一类，Referer 不在其中。
// 上游或中间代理回一次跨站 302，明文密钥就进了第三方的访问日志。
//
// 这里起两台真实的 httptest 服务器走完整重定向链路：单机断言「Referer 非空」是骗不过去的，
// 必须看下游**实际收到**了什么。
func TestComicVineDoesNotLeakAPIKeyOnRedirect(t *testing.T) {
	const secret = "cv-secret-token"

	var downstreamReferer string
	var downstreamQuery string
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downstreamReferer = r.Header.Get("Referer")
		downstreamQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":"OK","number_of_total_results":0,"results":[]}`))
	}))
	defer downstream.Close()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, downstream.URL+"/next", http.StatusFound)
	}))
	defer upstream.Close()

	p := NewComicVineProvider(secret)
	p.baseURL = upstream.URL + "/api/search/"
	if _, _, err := p.SearchMetadata(context.Background(), "saga", 20, 0); err != nil {
		t.Fatalf("SearchMetadata failed: %v", err)
	}

	if downstreamReferer == "" {
		t.Fatal("下游没收到 Referer —— 说明请求根本没走到重定向目标，用例失去意义")
	}
	if strings.Contains(downstreamReferer, secret) {
		t.Fatalf("Referer 把 api_key 泄漏给了重定向目标: %q", downstreamReferer)
	}
	// 重定向目标 URL 本身不带 query（Location 里没写），所以下游按 HTTP 语义拿不到 api_key
	// ——这恰恰是威胁成立的原因：第三方从 URL 里拿不到，却能从 Referer 里白拿。
	// 断言这一点，是为了确认本用例覆盖的确实是「query 之外的泄漏通道」。
	if strings.Contains(downstreamQuery, secret) {
		t.Fatalf("重定向目标的 query 里出现了 api_key（%q），本用例应覆盖的是 Referer 通道", downstreamQuery)
	}
}

// TestComicVineRedactsAPIKeyInUpstreamError 守卫「上游错误响应体不把凭据带给用户」。
// 网关的错误页常把被请求的完整 URI 原样回显，而这段串会写进 HTTP 响应体交给前端。
func TestComicVineRedactsAPIKeyInUpstreamError(t *testing.T) {
	const secret = "cv-secret-token"

	p := NewComicVineProvider(secret)
	p.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		echoed := "<html>403 Forbidden: " + req.URL.RequestURI() + "</html>"
		return &http.Response{
			StatusCode: http.StatusForbidden,
			Body:       io.NopCloser(strings.NewReader(echoed)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})}

	_, _, err := p.SearchMetadata(context.Background(), "saga", 20, 0)
	if err == nil {
		t.Fatal("expected an error for 403")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("错误串把 api_key 透传给了调用方（进而进入 HTTP 响应体）: %v", err)
	}
	if !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("期望脱敏占位符出现在错误串里，实际: %v", err)
	}
}
