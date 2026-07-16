package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"manga-manager/internal/database"
)

// TestAttemptLimiterLockoutAndReset 覆盖限流器核心语义：达阈值即锁定、成功清零、窗口外重置、指数退避递增。
func TestAttemptLimiterLockoutAndReset(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	now := base
	l := newAttemptLimiter(3, 10*time.Minute, time.Minute, 15*time.Minute)
	l.now = func() time.Time { return now }

	// 未达阈值：不锁定。
	l.recordFailure("k")
	l.recordFailure("k")
	if _, locked := l.retryAfter("k"); locked {
		t.Fatal("should not be locked before reaching max failures")
	}
	// 第 3 次达阈值：锁定，剩余约 1 分钟（baseLock）。
	l.recordFailure("k")
	d, locked := l.retryAfter("k")
	if !locked || d <= 0 || d > time.Minute {
		t.Fatalf("expected ~1m lockout after 3 failures, got locked=%v d=%v", locked, d)
	}
	// 锁定期结束后解锁。
	now = now.Add(time.Minute + time.Second)
	if _, locked := l.retryAfter("k"); locked {
		t.Fatal("should unlock after lockout elapses")
	}
	// 指数退避：第 4 次失败锁定时长应翻倍（约 2 分钟）。
	l.recordFailure("k")
	d, locked = l.retryAfter("k")
	if !locked || d <= time.Minute || d > 2*time.Minute+time.Second {
		t.Fatalf("expected ~2m backoff on 4th failure, got locked=%v d=%v", locked, d)
	}

	// 成功清零：另一个 key 达阈值后 recordSuccess 应解锁。
	l.recordFailure("s")
	l.recordFailure("s")
	l.recordFailure("s")
	if _, locked := l.retryAfter("s"); !locked {
		t.Fatal("key s should be locked")
	}
	l.recordSuccess("s")
	if _, locked := l.retryAfter("s"); locked {
		t.Fatal("recordSuccess should clear the lock")
	}

	// 窗口外重置：一次失败后跨过窗口再失败，计数从头开始，不应立即锁定。
	l.recordSuccess("w")
	l.recordFailure("w")
	now = now.Add(11 * time.Minute)
	l.recordFailure("w")
	if _, locked := l.retryAfter("w"); locked {
		t.Fatal("failures separated beyond the window must not accumulate into a lockout")
	}
}

// TestLoginRateLimiting 通过真实路由验证登录暴破限流：连续失败达阈值后返回 429（带 Retry-After），
// 且锁定期内即便凭据正确也被拦截（证明是真正的锁定而非仅凭错误凭据判定）。
func TestLoginRateLimiting(t *testing.T) {
	c, store, _, _ := newTestController(t)
	adminHash, _ := hashPassword("password1")
	if _, err := store.CreateUser(context.Background(), database.CreateUserParams{Username: "admin", PasswordHash: adminHash, Role: database.RoleAdmin}); err != nil {
		t.Fatalf("create admin: %v", err)
	}

	r := chi.NewRouter()
	c.SetupRoutes(r)
	srv := httptest.NewServer(r)
	defer srv.Close()

	postLogin := func(password string) *http.Response {
		body, _ := json.Marshal(map[string]string{"username": "admin", "password": password})
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/login", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("login request failed: %v", err)
		}
		return resp
	}

	// 前 5 次错误密码：应为 401（loginLimiter max=5，第 5 次失败后锁定）。
	for i := 0; i < 5; i++ {
		resp := postLogin("wrongpass")
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d: want 401, got %d", i+1, resp.StatusCode)
		}
		resp.Body.Close()
	}

	// 第 6 次：来源 IP 已锁定 → 429，带 Retry-After。
	resp := postLogin("wrongpass")
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("after threshold want 429, got %d", resp.StatusCode)
	}
	if resp.Header.Get("Retry-After") == "" {
		t.Fatal("429 response must carry a Retry-After header")
	}
	resp.Body.Close()

	// 锁定期内即使密码正确也应 429（证明是锁定，而非放行正确凭据）。
	resp = postLogin("password1")
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("correct password during lockout should still 429, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}
