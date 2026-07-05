package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
)

// TestSetupRoutesEnforcesSession 通过真实的 SetupRoutes 路由树验证多用户 authGate 接入了 /api 组：
// 首启（尚无账户）直通 → 建首个管理员后锁定 → 未登录 401 → 登录拿 cookie/csrf 后放行 →
// 改写方法缺 CSRF 403、带 CSRF 通过；Mihon 前缀始终不被会话鉴权 401 拦截。
func TestSetupRoutesEnforcesSession(t *testing.T) {
	c, store, _, _ := newTestController(t)
	ctx := context.Background()

	r := chi.NewRouter()
	c.SetupRoutes(r)
	srv := httptest.NewServer(r)
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	do := func(method, path, csrf string, body []byte) *http.Response {
		var rd *bytes.Reader
		req, _ := http.NewRequest(method, srv.URL+path, nil)
		if body != nil {
			rd = bytes.NewReader(body)
			req, _ = http.NewRequest(method, srv.URL+path, rd)
			req.Header.Set("Content-Type", "application/json")
		}
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s failed: %v", method, path, err)
		}
		return resp
	}

	// 首启（尚无账户）：管理端点直通。
	if resp := do(http.MethodGet, "/api/system/config", "", nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("setup-mode GET should pass, got %d", resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	// 建首个管理员 → 站点锁定。
	adminHash, _ := hashPassword("password1")
	if _, err := store.CreateUser(ctx, database.CreateUserParams{Username: "admin", PasswordHash: adminHash, Role: database.RoleAdmin}); err != nil {
		t.Fatalf("create admin: %v", err)
	}

	// 有账户但未登录：401。
	if resp := do(http.MethodGet, "/api/system/config", "", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("locked GET without session should 401, got %d", resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	// 登录拿 cookie + csrf。
	loginBody, _ := json.Marshal(map[string]string{"username": "admin", "password": "password1"})
	resp := do(http.MethodPost, "/api/auth/login", "", loginBody)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login should 200, got %d", resp.StatusCode)
	}
	var lr authSessionResponse
	_ = json.NewDecoder(resp.Body).Decode(&lr)
	resp.Body.Close()
	if lr.CSRFToken == "" {
		t.Fatal("login should return a csrf token")
	}

	// 登录后（cookie 在 jar）：200。
	if resp := do(http.MethodGet, "/api/system/config", "", nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("authenticated GET should 200, got %d", resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	// 改写方法缺 CSRF：403。
	if resp := do(http.MethodPost, "/api/users", "", []byte(`{"username":"bob","password":"password1"}`)); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("mutating without csrf should 403, got %d", resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	// 带 CSRF 创建用户：200。
	if resp := do(http.MethodPost, "/api/users", lr.CSRFToken, []byte(`{"username":"bob","password":"password1","role":"regular"}`)); resp.StatusCode != http.StatusOK {
		t.Fatalf("mutating with csrf should 200, got %d", resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	// Mihon 前缀不应被会话鉴权 401（协议关闭时应为 404）。
	if resp := do(http.MethodGet, "/api/mihon/v1/libraries", "", nil); resp.StatusCode == http.StatusUnauthorized {
		t.Fatal("mihon reading path must not be blocked by session auth")
	} else {
		resp.Body.Close()
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

// TestAuthGateRoles 验证角色边界：普通用户可浏览（GET）但不能访问管理只读端点、
// 不能执行管理写操作；管理员则放行。
func TestAuthGateRoles(t *testing.T) {
	c, store, _, _ := newTestController(t)
	ctx := context.Background()

	adminHash, _ := hashPassword("password1")
	if _, err := store.CreateUser(ctx, database.CreateUserParams{Username: "admin", PasswordHash: adminHash, Role: database.RoleAdmin}); err != nil {
		t.Fatalf("create admin: %v", err)
	}
	regHash, _ := hashPassword("password1")
	if _, err := store.CreateUser(ctx, database.CreateUserParams{Username: "reg", PasswordHash: regHash, Role: database.RoleRegular}); err != nil {
		t.Fatalf("create regular: %v", err)
	}

	r := chi.NewRouter()
	c.SetupRoutes(r)
	srv := httptest.NewServer(r)
	defer srv.Close()

	login := func(user string) (*http.Client, string) {
		jar, _ := cookiejar.New(nil)
		cl := &http.Client{Jar: jar}
		body, _ := json.Marshal(map[string]string{"username": user, "password": "password1"})
		resp, err := cl.Post(srv.URL+"/api/auth/login", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("login %s: %v", user, err)
		}
		var lr authSessionResponse
		_ = json.NewDecoder(resp.Body).Decode(&lr)
		resp.Body.Close()
		return cl, lr.CSRFToken
	}

	regClient, regCSRF := login("reg")

	// 普通用户访问管理只读端点 → 403。
	if resp, _ := regClient.Get(srv.URL + "/api/system/config"); resp != nil {
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("regular GET system/config want 403 got %d", resp.StatusCode)
		}
		resp.Body.Close()
	}

	// 普通用户执行管理写操作（带合法 CSRF）→ 403。
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/users", bytes.NewReader([]byte(`{"username":"z","password":"password1"}`)))
	req.Header.Set("X-CSRF-Token", regCSRF)
	req.Header.Set("Content-Type", "application/json")
	if resp, err := regClient.Do(req); err == nil {
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("regular create user want 403 got %d", resp.StatusCode)
		}
		resp.Body.Close()
	}

	// 普通用户浏览端点 → 200。
	if resp, _ := regClient.Get(srv.URL + "/api/libraries"); resp != nil {
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("regular browse want 200 got %d", resp.StatusCode)
		}
		resp.Body.Close()
	}

	// 管理员访问管理只读端点 → 200。
	adminClient, _ := login("admin")
	if resp, _ := adminClient.Get(srv.URL + "/api/system/config"); resp != nil {
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("admin GET system/config want 200 got %d", resp.StatusCode)
		}
		resp.Body.Close()
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
