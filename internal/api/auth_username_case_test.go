// 守用户名的大小写口径：账户身份按字节精确匹配，登录失败计数桶必须用同一把尺子分桶。

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"

	"manga-manager/internal/database"
)

// newUsernameCaseFixture 建两个仅大小写不同的账户，并返回一个「指定来源 IP 登录」的函数。
// 之所以要能换 IP：登录同时按 IP 与用户名两个桶计失败，不换 IP 的话 IP 桶会先锁死，
// 用例就分不清「被谁锁的」。
func newUsernameCaseFixture(t *testing.T) func(t *testing.T, ip, username, password string) int {
	t.Helper()
	c, store, _, _ := newTestController(t)
	// 采信 X-Forwarded-For 需要直连对端落在 trusted_proxies 内。
	cfg := c.currentConfig()
	cfg.Server.TrustedProxies = []string{"127.0.0.1/32", "::1/128"}
	c.config.Replace(&cfg)

	ctx := context.Background()
	for _, account := range []struct{ username, password string }{
		{"alice", "password1"},
		{"Alice", "password2"},
	} {
		hash, err := hashPassword(account.password)
		if err != nil {
			t.Fatalf("hashPassword: %v", err)
		}
		if _, err := store.CreateUser(ctx, database.CreateUserParams{
			Username: account.username, PasswordHash: hash, Role: database.RoleRegular,
		}); err != nil {
			t.Fatalf("create %q: %v", account.username, err)
		}
	}
	c.auth.markUsersExist()

	r := chi.NewRouter()
	c.SetupRoutes(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	return func(t *testing.T, ip, username, password string) int {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"username": username, "password": password})
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/login", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Forwarded-For", ip)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("login request: %v", err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
}

// TestLoginRateLimitBucketsFollowExactUsername 覆盖失败计数桶与账户身份的口径是否是同一把尺子。
func TestLoginRateLimitBucketsFollowExactUsername(t *testing.T) {
	// loginLimiter 的阈值：5 次失败即锁定。
	const failuresToLock = 5

	t.Run("大小写变体账户互不牵连", func(t *testing.T) {
		login := newUsernameCaseFixture(t)
		for i := 0; i < failuresToLock; i++ {
			if code := login(t, "203.0.113."+strconv.Itoa(i+1), "alice", "wrongpass"); code != http.StatusUnauthorized {
				t.Fatalf("第 %d 次错误口令应得 401，实得 %d", i+1, code)
			}
		}
		if code := login(t, "198.51.100.7", "Alice", "password2"); code != http.StatusOK {
			t.Fatalf("Alice 是另一个账户，却被 alice 的失败计数锁在门外：实得 %d", code)
		}
	})

	t.Run("同一用户名打满阈值仍然锁定", func(t *testing.T) {
		login := newUsernameCaseFixture(t)
		for i := 0; i < failuresToLock; i++ {
			if code := login(t, "203.0.113."+strconv.Itoa(i+1), "alice", "wrongpass"); code != http.StatusUnauthorized {
				t.Fatalf("第 %d 次错误口令应得 401，实得 %d", i+1, code)
			}
		}
		// 换一个来源 IP 用正确口令：还被拦下才说明锁的是用户名桶，限流没被改废。
		if code := login(t, "198.51.100.7", "alice", "password1"); code != http.StatusTooManyRequests {
			t.Fatalf("用户名桶应已锁定，正确口令也该得 429，实得 %d", code)
		}
	})

	t.Run("未触发限流时正常登录不受影响", func(t *testing.T) {
		login := newUsernameCaseFixture(t)
		if code := login(t, "198.51.100.7", "alice", "password1"); code != http.StatusOK {
			t.Fatalf("alice 正常登录应得 200，实得 %d", code)
		}
		if code := login(t, "198.51.100.8", "Alice", "password2"); code != http.StatusOK {
			t.Fatalf("Alice 正常登录应得 200，实得 %d", code)
		}
	})
}
