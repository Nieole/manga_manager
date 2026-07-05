package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"

	"manga-manager/internal/database"
)

// authTestServer 起一个真实路由的测试服务器。
func authTestServer(t *testing.T) (*Controller, *httptest.Server) {
	t.Helper()
	c, _, _, _ := newTestController(t)
	r := chi.NewRouter()
	c.SetupRoutes(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return c, srv
}

func newAuthClient(t *testing.T) *http.Client {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar}
}

// authDo 发送带可选 CSRF 头与 JSON 体的请求。
func authDo(t *testing.T, cl *http.Client, method, url, csrf string, body any) (*http.Response, []byte) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, url, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	resp, err := cl.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, data
}

func TestAuthSetupFlow(t *testing.T) {
	_, srv := authTestServer(t)
	cl := newAuthClient(t)

	// 初始：需要建管理员。
	resp, data := authDo(t, cl, http.MethodGet, srv.URL+"/api/auth/status", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status want 200 got %d", resp.StatusCode)
	}
	var st struct {
		SetupRequired bool `json:"setup_required"`
		Authenticated bool `json:"authenticated"`
	}
	_ = json.Unmarshal(data, &st)
	if !st.SetupRequired || st.Authenticated {
		t.Fatalf("fresh install should be setup_required & unauthenticated, got %+v", st)
	}

	// 密码过短被拒。
	if resp, _ := authDo(t, cl, http.MethodPost, srv.URL+"/api/auth/setup", "", map[string]string{"username": "admin", "password": "short"}); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("short password should 400 got %d", resp.StatusCode)
	}

	// 建管理员并自动登录。
	resp, data = authDo(t, cl, http.MethodPost, srv.URL+"/api/auth/setup", "", map[string]string{"username": "admin", "password": "password1", "display_name": "Boss"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("setup want 200 got %d: %s", resp.StatusCode, data)
	}
	var sr authSessionResponse
	_ = json.Unmarshal(data, &sr)
	if sr.User.Role != "admin" || sr.CSRFToken == "" {
		t.Fatalf("setup should return admin + csrf, got %+v", sr)
	}

	// 再次 setup 应 409。
	if resp, _ := authDo(t, newAuthClient(t), http.MethodPost, srv.URL+"/api/auth/setup", "", map[string]string{"username": "x", "password": "password1"}); resp.StatusCode != http.StatusConflict {
		t.Fatalf("second setup should 409 got %d", resp.StatusCode)
	}

	// 现在 status 显示已登录。
	_, data = authDo(t, cl, http.MethodGet, srv.URL+"/api/auth/status", "", nil)
	_ = json.Unmarshal(data, &st)
	if st.SetupRequired || !st.Authenticated {
		t.Fatalf("after setup should be authenticated & not setup_required, got %+v", st)
	}
}

func TestAuthLoginAndChangePassword(t *testing.T) {
	_, srv := authTestServer(t)
	admin := newAuthClient(t)
	// 建管理员。
	_, data := authDo(t, admin, http.MethodPost, srv.URL+"/api/auth/setup", "", map[string]string{"username": "admin", "password": "password1"})
	var sr authSessionResponse
	_ = json.Unmarshal(data, &sr)

	// 错误口令登录 401。
	if resp, _ := authDo(t, newAuthClient(t), http.MethodPost, srv.URL+"/api/auth/login", "", map[string]string{"username": "admin", "password": "wrongpass"}); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password should 401 got %d", resp.StatusCode)
	}

	// 改密：当前口令错 400。
	if resp, _ := authDo(t, admin, http.MethodPost, srv.URL+"/api/auth/change-password", sr.CSRFToken, map[string]string{"current_password": "nope", "new_password": "newpassword1"}); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("wrong current password should 400 got %d", resp.StatusCode)
	}
	// 改密成功。
	resp, data := authDo(t, admin, http.MethodPost, srv.URL+"/api/auth/change-password", sr.CSRFToken, map[string]string{"current_password": "password1", "new_password": "newpassword1"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("change password want 200 got %d: %s", resp.StatusCode, data)
	}

	// 新口令可登录，旧口令失效。
	if resp, _ := authDo(t, newAuthClient(t), http.MethodPost, srv.URL+"/api/auth/login", "", map[string]string{"username": "admin", "password": "password1"}); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old password should fail after change, got %d", resp.StatusCode)
	}
	if resp, _ := authDo(t, newAuthClient(t), http.MethodPost, srv.URL+"/api/auth/login", "", map[string]string{"username": "admin", "password": "newpassword1"}); resp.StatusCode != http.StatusOK {
		t.Fatalf("new password should log in, got %d", resp.StatusCode)
	}
}

func TestAuthUserManagementGuards(t *testing.T) {
	c, srv := authTestServer(t)
	admin := newAuthClient(t)
	_, data := authDo(t, admin, http.MethodPost, srv.URL+"/api/auth/setup", "", map[string]string{"username": "admin", "password": "password1"})
	var sr authSessionResponse
	_ = json.Unmarshal(data, &sr)
	adminID := sr.User.ID

	// 创建一个普通用户。
	resp, data := authDo(t, admin, http.MethodPost, srv.URL+"/api/users", sr.CSRFToken, map[string]string{"username": "reader", "password": "password1", "role": "regular"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create user want 200 got %d: %s", resp.StatusCode, data)
	}
	var created struct {
		ID                 int64  `json:"id"`
		Role               string `json:"role"`
		MustChangePassword bool   `json:"must_change_password"`
	}
	_ = json.Unmarshal(data, &created)
	if created.Role != "regular" || !created.MustChangePassword {
		t.Fatalf("admin-created user should be regular + must_change_password, got %+v", created)
	}

	// 列表应含 2 人。
	_, data = authDo(t, admin, http.MethodGet, srv.URL+"/api/users", "", nil)
	var list []map[string]any
	_ = json.Unmarshal(data, &list)
	if len(list) != 2 {
		t.Fatalf("want 2 users got %d", len(list))
	}

	// 用户名重复 409。
	if resp, _ := authDo(t, admin, http.MethodPost, srv.URL+"/api/users", sr.CSRFToken, map[string]string{"username": "reader", "password": "password1"}); resp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate username should 409 got %d", resp.StatusCode)
	}

	// 不能删除自己。
	if resp, _ := authDo(t, admin, http.MethodDelete, srv.URL+"/api/users/"+itoa(adminID), sr.CSRFToken, nil); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("delete self should 403 got %d", resp.StatusCode)
	}

	// 不能降级最后一个管理员。
	if resp, _ := authDo(t, admin, http.MethodPatch, srv.URL+"/api/users/"+itoa(adminID), sr.CSRFToken, map[string]string{"role": "regular"}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("demote last admin should 403 got %d", resp.StatusCode)
	}

	// 删除普通用户成功。
	if resp, _ := authDo(t, admin, http.MethodDelete, srv.URL+"/api/users/"+itoa(created.ID), sr.CSRFToken, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("delete regular user want 200 got %d", resp.StatusCode)
	}
	_ = c
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

// TestProtocolBasicAuth 验证阅读协议（Mihon）的 HTTP Basic 鉴权：首启直通 → 建账户后要求凭据；
// 无/错凭据 401，正确凭据 200。
func TestProtocolBasicAuth(t *testing.T) {
	c, store, _, _ := newTestController(t)
	ctx := context.Background()

	cfg := c.currentConfig()
	cfg.Protocols.Mihon.Enabled = true
	c.config.Replace(&cfg)

	r := chi.NewRouter()
	c.SetupRoutes(r)
	srv := httptest.NewServer(r)
	defer srv.Close()
	url := srv.URL + "/api/mihon/v1/libraries"

	get := func(setCreds func(*http.Request)) int {
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		if setCreds != nil {
			setCreds(req)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	// 首启（无账户）：直通。
	if code := get(nil); code != http.StatusOK {
		t.Fatalf("setup-mode protocol GET should pass, got %d", code)
	}

	// 建账户后锁定。
	hash, _ := hashPassword("password1")
	if _, err := store.CreateUser(ctx, database.CreateUserParams{Username: "reader", PasswordHash: hash, Role: database.RoleRegular}); err != nil {
		t.Fatalf("create user: %v", err)
	}

	if code := get(nil); code != http.StatusUnauthorized {
		t.Fatalf("no creds should 401, got %d", code)
	}
	if code := get(func(req *http.Request) { req.SetBasicAuth("reader", "wrong") }); code != http.StatusUnauthorized {
		t.Fatalf("wrong password should 401, got %d", code)
	}
	if code := get(func(req *http.Request) { req.SetBasicAuth("reader", "password1") }); code != http.StatusOK {
		t.Fatalf("valid basic creds should 200, got %d", code)
	}
}
