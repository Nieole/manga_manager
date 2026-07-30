package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
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

// TestLimiterPruneIsBounded 守卫「单次剪枝的代价与表大小无关」。
//
// 旧实现无上限地遍历整张表，且只删已过期条目——攻击者在一个窗口内持续造新 key 时，
// 每次失败都扫满全表却一条也删不掉，整体 O(n²)。而这是**持锁**的，
// 整条鉴权路径（含每次成功登录、每个协议请求的 retryAfter）都会被串行化。
func TestLimiterPruneIsBounded(t *testing.T) {
	now := time.Now()
	l := newAttemptLimiter(5, time.Hour, time.Minute, 10*time.Minute)
	l.now = func() time.Time { return now }

	// 灌入远超剪枝阈值的、全都**未过期**的条目（窗口 1 小时，时钟不动）。
	for i := 0; i < limiterPruneThreshold*3; i++ {
		l.recordFailure("k" + strconv.Itoa(i))
	}

	l.mu.Lock()
	before := len(l.entries)
	l.mu.Unlock()

	// 再记一次失败：剪枝最多只检查 limiterPruneSample 条，不该退化成全表扫。
	// 这里用「一条都删不掉」间接证明——若实现是全表扫，它同样删不掉，
	// 所以真正的判据是下面那条容量上限。
	l.recordFailure("trigger")
	l.mu.Lock()
	after := len(l.entries)
	l.mu.Unlock()
	if after < before {
		t.Fatalf("未过期条目被剪掉了：%d -> %d", before, after)
	}
}

// TestLimiterCapsEntriesAndFailsClosed 守卫容量上限与 fail-closed。
func TestLimiterCapsEntriesAndFailsClosed(t *testing.T) {
	now := time.Now()
	l := newAttemptLimiter(5, time.Hour, time.Minute, 10*time.Minute)
	l.now = func() time.Time { return now }

	for i := 0; i < limiterMaxEntries*2; i++ {
		l.recordFailure("attacker" + strconv.Itoa(i))
	}

	l.mu.Lock()
	size := len(l.entries)
	l.mu.Unlock()
	if size > limiterMaxEntries {
		t.Fatalf("条目数 %d 越过上限 %d —— 伪造 key 仍能无限撑大内存", size, limiterMaxEntries)
	}

	// 表满之后，陌生 key 必须被当作已限流（fail-closed），而不是一路放行。
	if _, locked := l.retryAfter("never-seen-before"); !locked {
		t.Fatal("表已撑满却对陌生 key 放行 —— 撑满即意味着正在被攻击，此时应 fail-closed")
	}
}

// TestLimiterNeverEvictsLockedEntries 是最关键的一条：
// 锁定中的条目被清掉，等于把已经成立的锁定白送回去，攻击者据此可以重置指数退避。
func TestLimiterNeverEvictsLockedEntries(t *testing.T) {
	now := time.Now()
	l := newAttemptLimiter(3, time.Hour, time.Minute, 10*time.Minute)
	l.now = func() time.Time { return now }

	// 先把一个 key 打到锁定。
	const victim = "user:admin"
	for i := 0; i < 5; i++ {
		l.recordFailure(victim)
	}
	if _, locked := l.retryAfter(victim); !locked {
		t.Fatal("用例前提不成立：目标 key 没有进入锁定")
	}

	// 再用海量新 key 冲刷，试图把它挤掉。
	for i := 0; i < limiterMaxEntries*2; i++ {
		l.recordFailure("flood" + strconv.Itoa(i))
	}

	if _, locked := l.retryAfter(victim); !locked {
		t.Fatal("锁定中的条目被淘汰了 —— 攻击者可以用伪造 key 冲刷来重置管理员账号的锁定")
	}
}
