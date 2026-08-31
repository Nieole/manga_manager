// 本文件是业务回归测试，覆盖 Provider 交出上游失败时的脱敏与截断——错误串进 HTTP 响应体
// （刮削失败提示、AI 推荐 500），日志行进 manga_manager.log。「不夹带密钥」是安全属性，
// 「不倾泻上游整段响应」既是安全属性也是可用性属性：一行超长日志会打死日志查看端点。

package metadata

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
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

// captureSlog 把包内的 slog 输出接到内存，返回读取已写行的取用函数。
func captureSlog(t *testing.T) func() []string {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return func() []string {
		return strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	}
}

// upstreamErrorPageTransport 让 Provider 收到一页远超 bufio.Scanner 默认 64KB 上限的 HTML 错误页，
// 这正是 CDN / WAF 拦截时的常态响应。
func upstreamErrorPageTransport(body string) roundTripFunc {
	return func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	}
}

// TestProviderLogsNeverEmitOversizedUpstreamBody 锁住制造侧：上游回一页超大 HTML 错误页时，
// 落盘的那一行必须已经截断。未截断的行会让 /api/system/logs 在日志轮转之前一直读不出后面的内容，
// 管理员恰好在最需要日志时看不到能解释这次失败的那几条。
func TestProviderLogsNeverEmitOversizedUpstreamBody(t *testing.T) {
	errorPage := "<html><body>" + strings.Repeat("<div>blocked by the edge gateway</div>", 4096) + "</body></html>"
	if len(errorPage) <= 64*1024 {
		t.Fatalf("用例数据没到 64KB（%d 字节），起不到复现作用", len(errorPage))
	}

	cases := []struct {
		name   string
		search func(rt roundTripFunc) error
	}{
		{"anilist", func(rt roundTripFunc) error {
			_, _, err := newAniListWithTransport(rt).SearchMetadata(context.Background(), "berserk", 5, 0)
			return err
		}},
		{"mangadex", func(rt roundTripFunc) error {
			_, _, err := newMangaDexWithTransport(rt).SearchMetadata(context.Background(), "berserk", 5, 0)
			return err
		}},
		{"bangumi", func(rt roundTripFunc) error {
			p := NewBangumiProvider()
			p.httpClient = &http.Client{Transport: rt}
			_, _, err := p.SearchMetadata(context.Background(), "berserk", 5, 0)
			return err
		}},
		{"myanimelist", func(rt roundTripFunc) error {
			p := NewMyAnimeListProvider("client-id")
			p.httpClient = &http.Client{Transport: rt}
			_, _, err := p.SearchMetadata(context.Background(), "berserk", 5, 0)
			return err
		}},
		{"comicvine", func(rt roundTripFunc) error {
			p := NewComicVineProvider("cv-secret-token")
			p.httpClient = &http.Client{Transport: rt}
			_, _, err := p.SearchMetadata(context.Background(), "berserk", 5, 0)
			return err
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name+"：超长上游错误页不写成超长日志行", func(t *testing.T) {
			lines := captureSlog(t)
			err := tc.search(upstreamErrorPageTransport(errorPage))
			if err == nil {
				t.Fatal("上游回 400 时必须报错")
			}

			for _, line := range lines() {
				if len(line) > maxUpstreamBodyInError*2 {
					t.Fatalf("日志写出了 %d 字节的一行，日志端点读到它就会降级丢内容", len(line))
				}
			}
			// 错误串与日志行共用同一份截断结果，标记必须两边都在。
			if !strings.Contains(err.Error(), "…(truncated)") {
				t.Fatalf("错误串没走统一出口的截断：%q", err.Error())
			}
		})
	}
}

// TestUpstreamBodiesNeverReachSlogDirectly 是给将来新增 Provider 的护栏：包内不许把上游响应体
// 直接拼进 slog，一律走 errors.go 的 logUpstreamFailure。命名与注释挡不住——本包五家 Provider
// 的错误分支长得几乎一样，抄一处漏掉截断，日志端点就整个失效。
func TestUpstreamBodiesNeverReachSlogDirectly(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("读取包目录失败: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "errors.go" {
			continue
		}
		content, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", name, err)
		}
		for i, line := range strings.Split(string(content), "\n") {
			if strings.Contains(line, "slog.") && strings.Contains(line, "respBody") {
				t.Errorf("%s:%d 把上游响应体直接拼进了 slog：改调 logUpstreamFailure，"+
					"它会先截断再脱敏，并把同一份结果交回来拼错误串\n\t%s", name, i+1, strings.TrimSpace(line))
			}
		}
	}
}
