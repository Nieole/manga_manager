// 业务说明：本文件是业务回归测试，属于后端服务启动入口，负责装配配置、数据库、扫描器、HTTP 路由和静态资源服务。
// 它通过自动化断言保护对应业务场景在扫描、读取、展示或配置变更后仍保持兼容。
// 维护时应让用例名称、测试数据和断言结果直接反映真实用户流程，而不是只覆盖实现细节。

package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestStaticCacheControl(t *testing.T) {
	t.Run("index html stays revalidatable", func(t *testing.T) {
		if got := staticCacheControl("/index.html"); got != "no-cache" {
			t.Fatalf("expected index cache-control no-cache, got %q", got)
		}
	})

	t.Run("hashed assets are long-lived", func(t *testing.T) {
		got := staticCacheControl("/assets/index-CRCnYWro.js")
		want := "public, max-age=31536000, immutable"
		if got != want {
			t.Fatalf("expected asset cache-control %q, got %q", want, got)
		}
	})

	t.Run("root level files stay revalidatable", func(t *testing.T) {
		if got := staticCacheControl("/favicon.ico"); got != "no-cache" {
			t.Fatalf("expected root asset cache-control no-cache, got %q", got)
		}
	})
}

func TestSetStaticResponseHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	setStaticResponseHeaders(rec, "/assets/index-CRCnYWro.js")

	if got := rec.Header().Get("Content-Type"); got != "application/javascript" {
		t.Fatalf("expected javascript content-type, got %q", got)
	}

	wantCacheControl := "public, max-age=31536000, immutable"
	if got := rec.Header().Get("Cache-Control"); got != wantCacheControl {
		t.Fatalf("expected cache-control %q, got %q", wantCacheControl, got)
	}
}

func TestWriteStaticContentETag(t *testing.T) {
	content := []byte("console.log('ok')")
	req := httptest.NewRequest(http.MethodGet, "/assets/index-CRCnYWro.js", nil)
	rec := httptest.NewRecorder()

	writeStaticContent(rec, req, "/assets/index-CRCnYWro.js", content)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("expected static response to include ETag")
	}
	if rec.Body.String() != string(content) {
		t.Fatalf("unexpected body: %q", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/assets/index-CRCnYWro.js", nil)
	req.Header.Set("If-None-Match", etag)
	rec = httptest.NewRecorder()
	writeStaticContent(rec, req, "/assets/index-CRCnYWro.js", content)
	if rec.Code != http.StatusNotModified {
		t.Fatalf("expected matching etag 304, got %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("ETag") != etag {
		t.Fatalf("expected 304 ETag %q, got %q", etag, rec.Header().Get("ETag"))
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("expected 304 body to be empty, got %q", rec.Body.String())
	}
}

func TestStaticETagIncludesPath(t *testing.T) {
	content := []byte("same bytes")
	if staticETag("/index.html", content) == staticETag("/assets/index.js", content) {
		t.Fatal("expected static ETag to include the requested path")
	}
}

// TestEnvOrDefault 验证 config/data-dir 参数的默认值可被环境变量覆盖：
// 空(或纯空白)env 用默认值，非空 env（去空白后）优先。
func TestEnvOrDefault(t *testing.T) {
	const key = "MANGA_MANAGER_TEST_ENVOR"
	t.Setenv(key, "")
	if got := envOrDefault(key, "fallback"); got != "fallback" {
		t.Fatalf("empty env should use default, got %q", got)
	}
	t.Setenv(key, "  from-env  ")
	if got := envOrDefault(key, "fallback"); got != "from-env" {
		t.Fatalf("non-empty env should win (trimmed), got %q", got)
	}
}

// TestAbsOrSelf 验证相对路径被解析为绝对路径（与 cwd 解耦），且绝对路径保持稳定。
func TestAbsOrSelf(t *testing.T) {
	got := absOrSelf("data")
	if !filepath.IsAbs(got) {
		t.Fatalf("expected absolute path, got %q", got)
	}
	if again := absOrSelf(got); again != got {
		t.Fatalf("absolute path should be stable, got %q want %q", again, got)
	}
}
