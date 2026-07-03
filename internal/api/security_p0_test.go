package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"manga-manager/internal/config"
)

// TestSetupRoutesAppliesAuth 通过真实的 SetupRoutes 路由树验证 requireAuth 确实接入了 /api 组
// （单元测试仅直接包了中间件，本用例覆盖“是否真正 Use 到路由树”这一集成缺口）。
func TestSetupRoutesAppliesAuth(t *testing.T) {
	c, _, _, _ := newTestController(t)

	cfg := c.currentConfig()
	cfg.Server.Auth.Enabled = true
	cfg.Server.Auth.Token = "secret"
	c.config.Replace(&cfg)

	r := chi.NewRouter()
	c.SetupRoutes(r)
	srv := httptest.NewServer(r)
	defer srv.Close()

	get := func(path, token string) int {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		if token != "" {
			req.Header.Set("X-API-Token", token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request %s failed: %v", path, err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	if code := get("/api/system/config", ""); code != http.StatusUnauthorized {
		t.Fatalf("management endpoint without token should 401 through real router, got %d", code)
	}
	if code := get("/api/system/config", "secret"); code != http.StatusOK {
		t.Fatalf("management endpoint with token should 200, got %d", code)
	}
	// Mihon 阅读协议前缀不应被管理鉴权 401 拦截（协议默认关闭时应为 404，而非 401）。
	if code := get("/api/mihon/v1/libraries", ""); code == http.StatusUnauthorized {
		t.Fatal("mihon reading path must not be blocked by management auth")
	}
}

// TestGetSystemConfigMasksSecrets 验证配置回显对 LLM api_key 与 server.auth.token 脱敏，
// 且响应正文不含明文（对应 P0 的信息泄露修复）。
func TestGetSystemConfigMasksSecrets(t *testing.T) {
	c, _, _, _ := newTestController(t)

	cfg := c.currentConfig()
	cfg.LLM.APIKey = "sk-super-secret"
	cfg.Server.Auth.Enabled = true
	cfg.Server.Auth.Token = "topsecrettoken"
	c.config.Replace(&cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/system/config", nil)
	rec := httptest.NewRecorder()
	c.getSystemConfig(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp SystemConfigResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Config.LLM.APIKey != config.SecretMask {
		t.Fatalf("api_key not masked: %q", resp.Config.LLM.APIKey)
	}
	if resp.Config.Server.Auth.Token != config.SecretMask {
		t.Fatalf("auth token not masked: %q", resp.Config.Server.Auth.Token)
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("sk-super-secret")) ||
		bytes.Contains(rec.Body.Bytes(), []byte("topsecrettoken")) {
		t.Fatal("response body leaked a plaintext secret")
	}
}

// TestUpdateSystemConfigPreservesMaskedSecret 验证前端回传脱敏占位符时真实密钥被保留，
// 回传新值时被更新。
func TestUpdateSystemConfigPreservesMaskedSecret(t *testing.T) {
	c, _, _, _ := newTestController(t)

	cfg := c.currentConfig()
	cfg.LLM.APIKey = "original-key"
	c.config.Replace(&cfg)

	// 1. 回传占位符（未改动密钥）-> 保留原值。
	post := c.currentConfig()
	post.LLM.APIKey = config.SecretMask
	body, _ := json.Marshal(post)
	rec := httptest.NewRecorder()
	c.updateSystemConfig(rec, httptest.NewRequest(http.MethodPost, "/api/system/config", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("update with masked key expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := c.currentConfig().LLM.APIKey; got != "original-key" {
		t.Fatalf("masked save should preserve key, got %q", got)
	}

	// 2. 回传新密钥 -> 更新。
	post2 := c.currentConfig()
	post2.LLM.APIKey = "brand-new-key"
	body2, _ := json.Marshal(post2)
	rec2 := httptest.NewRecorder()
	c.updateSystemConfig(rec2, httptest.NewRequest(http.MethodPost, "/api/system/config", bytes.NewReader(body2)))
	if rec2.Code != http.StatusOK {
		t.Fatalf("update with new key expected 200, got %d: %s", rec2.Code, rec2.Body.String())
	}
	if got := c.currentConfig().LLM.APIKey; got != "brand-new-key" {
		t.Fatalf("new key should be persisted, got %q", got)
	}
}

// TestRequireAuthMiddleware 验证可选令牌鉴权：默认关闭直通；启用后无/错令牌 401，
// 头/查询参数/Bearer 携带正确令牌放行；Mihon 阅读协议前缀始终绕过管理鉴权。
func TestRequireAuthMiddleware(t *testing.T) {
	c, _, _, _ := newTestController(t)

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := c.requireAuth(next)

	serve := func(req *http.Request) int {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	// 默认（Auth.Enabled=false）直通。
	if code := serve(httptest.NewRequest(http.MethodGet, "/api/libraries", nil)); code != http.StatusOK {
		t.Fatalf("auth disabled should pass, got %d", code)
	}

	cfg := c.currentConfig()
	cfg.Server.Auth.Enabled = true
	cfg.Server.Auth.Token = "secret"
	c.config.Replace(&cfg)

	if code := serve(httptest.NewRequest(http.MethodGet, "/api/libraries", nil)); code != http.StatusUnauthorized {
		t.Fatalf("missing token should 401, got %d", code)
	}

	wrong := httptest.NewRequest(http.MethodGet, "/api/libraries", nil)
	wrong.Header.Set("X-API-Token", "nope")
	if code := serve(wrong); code != http.StatusUnauthorized {
		t.Fatalf("wrong token should 401, got %d", code)
	}

	hdr := httptest.NewRequest(http.MethodGet, "/api/libraries", nil)
	hdr.Header.Set("X-API-Token", "secret")
	if code := serve(hdr); code != http.StatusOK {
		t.Fatalf("valid X-API-Token should pass, got %d", code)
	}

	bearer := httptest.NewRequest(http.MethodGet, "/api/libraries", nil)
	bearer.Header.Set("Authorization", "Bearer secret")
	if code := serve(bearer); code != http.StatusOK {
		t.Fatalf("valid Bearer token should pass, got %d", code)
	}

	if code := serve(httptest.NewRequest(http.MethodGet, "/api/libraries?token=secret", nil)); code != http.StatusOK {
		t.Fatalf("valid query token should pass, got %d", code)
	}

	// Mihon 阅读协议前缀绕过管理鉴权。
	if code := serve(httptest.NewRequest(http.MethodGet, "/api/mihon/v1/libraries", nil)); code != http.StatusOK {
		t.Fatalf("mihon path should bypass management auth, got %d", code)
	}
}

// TestValidateOutboundLLMTarget 验证 test-llm 的 SSRF scheme 加固。
func TestValidateOutboundLLMTarget(t *testing.T) {
	allowed := []string{"http://localhost:11434", "https://api.openai.com", ""}
	for _, target := range allowed {
		if err := validateOutboundLLMTarget(target, ""); err != nil {
			t.Fatalf("target %q should be allowed: %v", target, err)
		}
	}
	rejected := []string{"file:///etc/passwd", "gopher://internal", "ftp://host/x"}
	for _, target := range rejected {
		if err := validateOutboundLLMTarget(target, ""); err == nil {
			t.Fatalf("target %q should be rejected", target)
		}
	}
	// base_url 为空时回退到 endpoint 校验。
	if err := validateOutboundLLMTarget("", "file:///etc/passwd"); err == nil {
		t.Fatal("endpoint fallback with file scheme should be rejected")
	}
}
