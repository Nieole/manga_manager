// 守协议侧（OPDS/Mihon）口令爆破的三条判据：账号级失败要跨 IP 生效，失败计数要按
// (来源 IP, 用户名) 分格，一次成功只清它自己那一格。任一条破了，站点口令在协议侧就是无限次可猜的。

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
	"golang.org/x/crypto/bcrypt"

	"manga-manager/internal/database"
)

const (
	bruteAdminUser = "alice"
	bruteAdminPass = "password1"
	bruteOtherUser = "bob"
	bruteOtherPass = "password2"
)

// bruteForceRig 是一套开着 OPDS 的真实路由装配，并让转发头可信以便逐次更换来源 IP。
type bruteForceRig struct {
	controller *Controller
	server     *httptest.Server
}

func newBruteForceRig(t *testing.T) *bruteForceRig {
	t.Helper()
	c, store, _, _ := newTestController(t)

	cfg := c.currentConfig()
	cfg.Protocols.OPDS.Enabled = true
	cfg.Server.TrustedProxies = []string{"127.0.0.1/32", "::1/128"}
	c.config.Replace(&cfg)

	ctx := context.Background()
	for _, account := range []struct{ username, password, role string }{
		{bruteAdminUser, bruteAdminPass, database.RoleAdmin},
		{bruteOtherUser, bruteOtherPass, database.RoleRegular},
	} {
		// 用最低 cost 建哈希：本用例要打几十次口令比对，判据是限流分桶而不是 KDF 强度。
		hash, err := bcrypt.GenerateFromPassword([]byte(account.password), bcrypt.MinCost)
		if err != nil {
			t.Fatalf("hash %q: %v", account.username, err)
		}
		if _, err := store.CreateUser(ctx, database.CreateUserParams{
			Username: account.username, PasswordHash: string(hash), Role: account.role,
		}); err != nil {
			t.Fatalf("建账号 %q 失败: %v", account.username, err)
		}
	}
	c.auth.markUsersExist()

	router := chi.NewRouter()
	c.SetupRoutes(router)
	c.SetupOPDSRoutes(router)
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return &bruteForceRig{controller: c, server: srv}
}

// opds 发一条带 Basic 凭据、声明来源 IP 的 OPDS 请求，返回状态码。
func (r *bruteForceRig) opds(t *testing.T, ip, username, password string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, r.server.URL+"/opds/v1.2/", nil)
	if err != nil {
		t.Fatalf("构造 OPDS 请求失败: %v", err)
	}
	req.SetBasicAuth(username, password)
	req.Header.Set("X-Forwarded-For", ip)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("OPDS 请求失败: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// webLogin 走网页登录入口，用来判定协议侧的锁定有没有溢出到 Web 侧。
func (r *bruteForceRig) webLogin(t *testing.T, ip, username, password string) int {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	req, err := http.NewRequest(http.MethodPost, r.server.URL+"/api/auth/login", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("构造登录请求失败: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", ip)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("登录请求失败: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// TestProtocolBruteForceIsThrottled 覆盖两种爆破姿势：换 IP 打同一账号、单 IP 上穿插自己的有效凭据。
// 两条都必须在有限次内撞上 429，否则站点口令在协议侧就是无限次可猜的。
func TestProtocolBruteForceIsThrottled(t *testing.T) {
	for _, tc := range []struct {
		name string
		// attack 发第 i 次猜测（从 0 起），返回状态码。
		attack   func(t *testing.T, rig *bruteForceRig, i int) int
		attempts int
		maxTries int // 允许攻击者在被拦下之前拿到的最大猜测次数
	}{
		{
			name: "每次更换来源 IP 猜同一个账号",
			attack: func(t *testing.T, rig *bruteForceRig, i int) int {
				return rig.opds(t, "203.0.113."+strconv.Itoa(i%256), bruteAdminUser, "guess"+strconv.Itoa(i))
			},
			attempts: 40,
			maxTries: 25,
		},
		{
			name: "单个 IP 上每 9 次穿插一条自己的有效凭据",
			attack: func(t *testing.T, rig *bruteForceRig, i int) int {
				if i > 0 && i%9 == 0 {
					if code := rig.opds(t, "198.51.100.9", bruteOtherUser, bruteOtherPass); code != http.StatusOK {
						t.Fatalf("第 %d 次穿插的有效凭据应 200，实得 %d", i, code)
					}
				}
				return rig.opds(t, "198.51.100.9", bruteAdminUser, "guess"+strconv.Itoa(i))
			},
			attempts: 40,
			maxTries: 15,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rig := newBruteForceRig(t)
			throttledAt := -1
			for i := 0; i < tc.attempts; i++ {
				code := tc.attack(t, rig, i)
				if code == http.StatusTooManyRequests {
					throttledAt = i + 1
					break
				}
				if code != http.StatusUnauthorized {
					t.Fatalf("第 %d 次错误口令应 401 或 429，实得 %d", i+1, code)
				}
			}
			if throttledAt < 0 {
				t.Fatalf("连续 %d 次错误口令一次都没被限流——协议侧站点口令可无限爆破", tc.attempts)
			}
			if throttledAt > tc.maxTries {
				t.Fatalf("第 %d 次才被限流，超过了允许的 %d 次", throttledAt, tc.maxTries)
			}
		})
	}
}

// TestProtocolThrottleBlastRadius 守限流的**波及范围**：一次成功只清它自己那一格，
// 一个账号被打也只锁它自己那一格；同一出口 IP 后面的其它家庭成员、以及本人的网页登录都不该受牵连。
func TestProtocolThrottleBlastRadius(t *testing.T) {
	const sharedIP = "192.0.2.44"

	for _, tc := range []struct {
		name  string
		check func(t *testing.T, rig *bruteForceRig)
	}{
		{
			name: "同一出口 IP 上另一个账号的正确口令仍然放行",
			check: func(t *testing.T, rig *bruteForceRig) {
				// 一台设备存着过期口令，反复重试到被锁。
				for i := 0; i < 20; i++ {
					if code := rig.opds(t, sharedIP, bruteAdminUser, "stale"+strconv.Itoa(i)); code == http.StatusTooManyRequests {
						break
					}
				}
				if code := rig.opds(t, sharedIP, bruteAdminUser, "stale-again"); code != http.StatusTooManyRequests {
					t.Fatalf("用例前提不成立：这台设备该被锁了，实得 %d", code)
				}
				if code := rig.opds(t, sharedIP, bruteOtherUser, bruteOtherPass); code != http.StatusOK {
					t.Fatalf("同一出口 IP 后面的另一个账号被连坐了，实得 %d", code)
				}
			},
		},
		{
			name: "协议侧的锁定不牵连本人的网页登录",
			check: func(t *testing.T, rig *bruteForceRig) {
				for i := 0; i < 40; i++ {
					if code := rig.opds(t, "203.0.113."+strconv.Itoa(i%256), bruteAdminUser, "guess"+strconv.Itoa(i)); code == http.StatusTooManyRequests {
						break
					}
				}
				// 设备拿旧口令自动重试会把账号打进协议侧锁定，网页端是用户唯一能自救的地方，
				// 它必须还开着。
				if code := rig.webLogin(t, "192.0.2.77", bruteAdminUser, bruteAdminPass); code != http.StatusOK {
					t.Fatalf("协议侧的失败把网页登录一起锁死了，实得 %d", code)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.check(t, newBruteForceRig(t))
		})
	}
}
