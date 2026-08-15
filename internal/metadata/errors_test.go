// 本文件是业务回归测试，覆盖 Provider 对外错误的脱敏与截断。
// 这些错误会被写进 HTTP 响应体（刮削失败提示、AI 推荐 500），属于面向用户的输出，
// 因此「不夹带密钥」「不倾泻上游整段响应」是安全属性而非风格问题。

package metadata

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestSanitizeTransportErrorDropsRequestURL 锁住 P1：Comic Vine 的凭据在 query 里，
// http.Client.Do 失败时返回的 *url.Error 会把完整 URL 打进错误串，于是一次 DNS 失败
// 或超时就足以让明文 api_key 出现在 500 响应体中，任何已登录用户都能读到。
func TestSanitizeTransportErrorDropsRequestURL(t *testing.T) {
	const secret = "super-secret-api-key"
	raw := &url.Error{
		Op:  "Get",
		URL: "https://comicvine.gamespot.com/api/search/?api_key=" + secret + "&query=x",
		Err: errors.New("dial tcp: lookup failed"),
	}

	sanitized := sanitizeTransportError(raw)
	if strings.Contains(sanitized.Error(), secret) {
		t.Fatalf("sanitized error still leaks the api key: %s", sanitized.Error())
	}
	// 底层原因必须保留，否则排障时看不出到底是超时还是连接被拒。
	if !strings.Contains(sanitized.Error(), "dial tcp") {
		t.Fatalf("sanitized error lost the underlying cause: %s", sanitized.Error())
	}
}

func TestSanitizeTransportErrorPassesThroughOtherErrors(t *testing.T) {
	plain := errors.New("boom")
	if got := sanitizeTransportError(plain); got != plain {
		t.Fatalf("non-url errors should pass through unchanged, got %v", got)
	}
	if sanitizeTransportError(nil) != nil {
		t.Fatal("nil should stay nil")
	}
}

func TestTruncateUpstreamBodyKeepsValidUTF8(t *testing.T) {
	long := strings.Repeat("中", maxUpstreamBodyInError) // 每字 3 字节，必然超限
	got := truncateUpstreamBody([]byte(long))

	if len(got) > maxUpstreamBodyInError+len("…(truncated)") {
		t.Fatalf("truncated body still too long: %d bytes", len(got))
	}
	if !strings.HasSuffix(got, "…(truncated)") {
		t.Fatalf("expected a truncation marker, got %q", got[len(got)-20:])
	}
	// 关键：不能切在多字节字符中间产出乱码。
	if strings.Contains(strings.TrimSuffix(got, "…(truncated)"), "�") {
		t.Fatal("truncation produced invalid UTF-8")
	}

	short := []byte("ok")
	if truncateUpstreamBody(short) != "ok" {
		t.Fatal("short bodies should pass through unchanged")
	}
}

// TestComicVineSearchDoesNotLeakAPIKeyOnTransportFailure 是端到端版本：
// 用一个立刻关闭连接的服务端制造 transport 失败，断言返回的错误不含密钥。
func TestComicVineSearchDoesNotLeakAPIKeyOnTransportFailure(t *testing.T) {
	const secret = "cv-secret-token"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close() // 立刻关掉：后续请求必然 transport 失败

	provider := NewComicVineProvider(secret)
	provider.httpClient = &http.Client{Timeout: 2 * time.Second}
	provider.baseURL = srv.URL

	_, _, err := provider.SearchMetadata(context.Background(), "any", 5, 0)
	if err == nil {
		t.Fatal("expected the request to fail against a closed server")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaks the api key to the caller: %s", err.Error())
	}
}

// TestComicVineSurfacesAPIErrorField 锁住「Key 失效被误报成没找到」。
// Comic Vine 对 Key 失效/配额耗尽一律回 HTTP 200，真正的失败写在 body 的 error 字段里。
func TestComicVineSurfacesAPIErrorField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":"Invalid API Key","number_of_total_results":0,"results":[]}`))
	}))
	defer srv.Close()

	provider := NewComicVineProvider("whatever")
	provider.httpClient = srv.Client()
	provider.baseURL = srv.URL

	_, _, err := provider.SearchMetadata(context.Background(), "any", 5, 0)
	if err == nil {
		t.Fatal("an invalid API key must surface as an error, not as 'no results found'")
	}
	if !strings.Contains(err.Error(), "Invalid API Key") {
		t.Fatalf("expected the upstream error message to be surfaced, got %v", err)
	}
}

func TestComicVineTreatsOKErrorFieldAsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":"OK","number_of_total_results":0,"results":[]}`))
	}))
	defer srv.Close()

	provider := NewComicVineProvider("whatever")
	provider.httpClient = srv.Client()
	provider.baseURL = srv.URL

	results, total, err := provider.SearchMetadata(context.Background(), "any", 5, 0)
	if err != nil {
		t.Fatalf("error=\"OK\" is the success marker, not a failure: %v", err)
	}
	if len(results) != 0 || total != 0 {
		t.Fatalf("expected an empty result set, got %d/%d", len(results), total)
	}
}
