// 守「未完成首登改密的账号不得经协议侧使用」这条约束在非浏览器链路上的落地：
// authGate 之外还有 OPDS/Mihon 的 Basic 鉴权与 KOReader 同步链，只在一条路径上执行等于没执行。

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"

	"manga-manager/internal/database"
	"manga-manager/internal/koreader"
)

// protocolGateFixture 是三条协议链路共用的装配：两个站点账号（一个待改密、一个正常），
// 各自绑定一个 KOReader 账户，并把三套路由挂在同一棵路由树上。
type protocolGateFixture struct {
	controller  *Controller
	store       database.Store
	server      *httptest.Server
	pendingKey  string // 待改密账号名下 KOReader 账户的客户端密钥（md5）
	settledKey  string // 正常账号名下 KOReader 账户的客户端密钥（md5）
	bookID      int64
	pendingPass string
	settledPass string
}

const (
	protocolGatePendingUser = "pending-user"
	protocolGateSettledUser = "settled-user"
	protocolGatePendingPass = "initial-pass1"
	protocolGateSettledPass = "settled-pass1"
)

func newProtocolGateFixture(t *testing.T) *protocolGateFixture {
	t.Helper()
	c, store, _, rootDir := newTestController(t)
	ctx := context.Background()

	cfg := c.currentConfig()
	cfg.Protocols.OPDS.Enabled = true
	cfg.Protocols.Mihon.Enabled = true
	cfg.KOReader.Enabled = true
	cfg.KOReader.BasePath = "/koreader"
	c.config.Replace(&cfg)

	_, _, book := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)

	createUser := func(name, password string, mustChange bool) database.User {
		t.Helper()
		hash, err := hashPassword(password)
		if err != nil {
			t.Fatalf("hash password failed: %v", err)
		}
		u, err := store.CreateUser(ctx, database.CreateUserParams{
			Username:           name,
			PasswordHash:       hash,
			Role:               database.RoleRegular,
			MustChangePassword: mustChange,
		})
		if err != nil {
			t.Fatalf("create user %s failed: %v", name, err)
		}
		return u
	}
	// KOReader 同步链的凭据是账户自己的 sync_key，与站点口令无关；账户经 user_id 绑到站点账号。
	createKOAccount := func(name string, userID int64) string {
		t.Helper()
		syncKey, err := koreader.GenerateSyncKey()
		if err != nil {
			t.Fatalf("generate sync key failed: %v", err)
		}
		account, err := store.CreateKOReaderAccount(ctx, database.CreateKOReaderAccountParams{
			Username: name,
			SyncKey:  syncKey,
			Enabled:  true,
		})
		if err != nil {
			t.Fatalf("create koreader account %s failed: %v", name, err)
		}
		if err := store.SetKOReaderAccountUser(ctx, account.ID, userID); err != nil {
			t.Fatalf("bind koreader account %s failed: %v", name, err)
		}
		return koreader.HashKey(syncKey)
	}

	pending := createUser(protocolGatePendingUser, protocolGatePendingPass, true)
	settled := createUser(protocolGateSettledUser, protocolGateSettledPass, false)

	router := chi.NewRouter()
	c.SetupRoutes(router)
	c.SetupOPDSRoutes(router)
	c.SetupKOReaderRoutes(router)
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)

	return &protocolGateFixture{
		controller:  c,
		store:       store,
		server:      srv,
		pendingKey:  createKOAccount("pending-device", pending.ID),
		settledKey:  createKOAccount("settled-device", settled.ID),
		bookID:      book.ID,
		pendingPass: protocolGatePendingPass,
		settledPass: protocolGateSettledPass,
	}
}

// basic 发一条带 HTTP Basic 凭据的请求（OPDS / Mihon 走这条），返回状态码与响应头。
func (f *protocolGateFixture) basic(t *testing.T, method, path, user, password string, body []byte) (int, http.Header) {
	t.Helper()
	var req *http.Request
	var err error
	if body == nil {
		req, err = http.NewRequest(method, f.server.URL+path, nil)
	} else {
		req, err = http.NewRequest(method, f.server.URL+path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	if err != nil {
		t.Fatalf("build %s %s failed: %v", method, path, err)
	}
	req.SetBasicAuth(user, password)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s failed: %v", method, path, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode, resp.Header
}

// kosync 发一条带 kosync 头凭据的请求（KOReader 走这条）。
func (f *protocolGateFixture) kosync(t *testing.T, method, path, user, key string, body []byte) int {
	t.Helper()
	var req *http.Request
	var err error
	if body == nil {
		req, err = http.NewRequest(method, f.server.URL+path, nil)
	} else {
		req, err = http.NewRequest(method, f.server.URL+path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	if err != nil {
		t.Fatalf("build %s %s failed: %v", method, path, err)
	}
	req.Header.Set("x-auth-user", user)
	req.Header.Set("x-auth-key", key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s failed: %v", method, path, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// changePasswordOverSession 走真实的浏览器流程改密：登录 → 带 CSRF 调 change-password。
func (f *protocolGateFixture) changePasswordOverSession(t *testing.T, username, current, next string) {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	loginBody, _ := json.Marshal(map[string]string{"username": username, "password": current})
	resp, err := client.Post(f.server.URL+"/api/auth/login", "application/json", bytes.NewReader(loginBody))
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	var login authSessionResponse
	_ = json.NewDecoder(resp.Body).Decode(&login)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login should 200, got %d", resp.StatusCode)
	}

	changeBody, _ := json.Marshal(map[string]string{"current_password": current, "new_password": next})
	req, _ := http.NewRequest(http.MethodPost, f.server.URL+"/api/auth/change-password", bytes.NewReader(changeBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", login.CSRFToken)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("change-password failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("change-password should 200, got %d", resp.StatusCode)
	}
}

func (f *protocolGateFixture) mihonProgressPath() string {
	return "/api/mihon/v1/books/" + strconv.FormatInt(f.bookID, 10) + "/progress"
}

// TestProtocolAuthRejectsPendingPasswordChange 锁住强制改密在协议侧的执行。
//
// 强制改密不能只在浏览器会话那条路径上执行：协议侧漏掉这条校验，未完成首登改密的账号
// 就能经协议客户端绕过该约束，长期读写全库。读写一并拒绝——初始口令的分发方式本身
// 就在这条约束的假设之外，只拒写等于默认「读全库」无所谓。
func TestProtocolAuthRejectsPendingPasswordChange(t *testing.T) {
	f := newProtocolGateFixture(t)

	t.Run("OPDS 目录对待改密账号关闭", func(t *testing.T) {
		code, header := f.basic(t, http.MethodGet, "/opds/v1.2/", protocolGatePendingUser, f.pendingPass, nil)
		if code != http.StatusUnauthorized {
			t.Fatalf("OPDS 根目录应 401，实得 %d", code)
		}
		// 阅读器基本不显示 Basic 鉴权的响应体，realm 是这条拒绝唯一的可见解释，
		// 它退回默认文案就等于用户只看到一句「认证失败」。
		if challenge := header.Get("WWW-Authenticate"); challenge != basicAuthChallengePasswordChange {
			t.Fatalf("realm 应说明真实原因，实得 %q", challenge)
		}
	})

	t.Run("Mihon 读取对待改密账号关闭", func(t *testing.T) {
		if code, _ := f.basic(t, http.MethodGet, "/api/mihon/v1/libraries", protocolGatePendingUser, f.pendingPass, nil); code != http.StatusUnauthorized {
			t.Fatalf("Mihon 资料库列表应 401，实得 %d", code)
		}
	})

	t.Run("Mihon 写进度对待改密账号关闭", func(t *testing.T) {
		body := []byte(`{"page":3}`)
		if code, _ := f.basic(t, http.MethodPost, f.mihonProgressPath(), protocolGatePendingUser, f.pendingPass, body); code != http.StatusUnauthorized {
			t.Fatalf("Mihon 进度上报应 401，实得 %d", code)
		}
	})

	t.Run("KOReader 同步链对待改密账号关闭", func(t *testing.T) {
		if code := f.kosync(t, http.MethodGet, "/koreader/users/auth", "pending-device", f.pendingKey, nil); code != http.StatusForbidden {
			t.Fatalf("KOReader 鉴权应 403，实得 %d", code)
		}
		body := []byte(`{"document":"abc","progress":"1","percentage":0.1,"device":"kindle"}`)
		if code := f.kosync(t, http.MethodPut, "/koreader/syncs/progress", "pending-device", f.pendingKey, body); code != http.StatusForbidden {
			t.Fatalf("KOReader 进度推送应 403，实得 %d", code)
		}
	})

	t.Run("反向判据：正常账号三条链路都不受影响", func(t *testing.T) {
		if code, _ := f.basic(t, http.MethodGet, "/opds/v1.2/", protocolGateSettledUser, f.settledPass, nil); code != http.StatusOK {
			t.Fatalf("正常账号的 OPDS 根目录应 200，实得 %d", code)
		}
		if code, _ := f.basic(t, http.MethodGet, "/api/mihon/v1/libraries", protocolGateSettledUser, f.settledPass, nil); code != http.StatusOK {
			t.Fatalf("正常账号的 Mihon 资料库列表应 200，实得 %d", code)
		}
		if code, _ := f.basic(t, http.MethodPost, f.mihonProgressPath(), protocolGateSettledUser, f.settledPass, []byte(`{"page":3}`)); code != http.StatusOK {
			t.Fatalf("正常账号的 Mihon 进度上报应 200，实得 %d", code)
		}
		if code := f.kosync(t, http.MethodGet, "/koreader/users/auth", "settled-device", f.settledKey, nil); code != http.StatusOK {
			t.Fatalf("正常账号的 KOReader 鉴权应 200，实得 %d", code)
		}
	})

	t.Run("反向判据：改密之后协议侧立刻可用", func(t *testing.T) {
		const newPass = "changed-pass1"
		f.changePasswordOverSession(t, protocolGatePendingUser, f.pendingPass, newPass)

		if code, _ := f.basic(t, http.MethodGet, "/opds/v1.2/", protocolGatePendingUser, newPass, nil); code != http.StatusOK {
			t.Fatalf("改密后 OPDS 根目录应 200，实得 %d", code)
		}
		if code, _ := f.basic(t, http.MethodGet, "/api/mihon/v1/libraries", protocolGatePendingUser, newPass, nil); code != http.StatusOK {
			t.Fatalf("改密后 Mihon 资料库列表应 200，实得 %d", code)
		}
		if code := f.kosync(t, http.MethodGet, "/koreader/users/auth", "pending-device", f.pendingKey, nil); code != http.StatusOK {
			t.Fatalf("改密后 KOReader 鉴权应 200，实得 %d", code)
		}
	})
}
