// 本文件守卫「滑动续期对客户端也生效」：服务端把 expires_at 推后的同时，必须把同一份会话
// Cookie 以与登录时完全一致的属性重新下发。只续服务端不下发 Cookie 的话，浏览器那份 Max-Age
// 停在登录那一刻，天天在用的用户仍会在第 30 天被踢下线——声明的滑动续期对客户端是哑的。

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
)

// slidingRenewalPassword 是本文件夹具账户的口令（长度须过 minPasswordLen）。
const slidingRenewalPassword = "password1"

// newSlidingSessionFixture 建一个管理员并走真实登录，返回登录时下发的会话 Cookie。
// 用真实的 login 而非直接造 Cookie：登录那一份就是续期必须对齐的基准。
func newSlidingSessionFixture(t *testing.T, cookieSecure string) (*Controller, database.Store, *http.Cookie) {
	t.Helper()
	c, store, _, _ := newTestController(t)

	cfg := c.config.Snapshot()
	cfg.Server.CookieSecure = cookieSecure
	config.NormalizeConfig(&cfg)
	c.config.Replace(&cfg)

	hash, err := hashPassword(slidingRenewalPassword)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if _, err := store.CreateUser(context.Background(), database.CreateUserParams{
		Username: "alice", PasswordHash: hash, Role: database.RoleAdmin,
	}); err != nil {
		t.Fatalf("create admin: %v", err)
	}

	body, _ := json.Marshal(map[string]string{"username": "alice", "password": slidingRenewalPassword})
	rec := httptest.NewRecorder()
	c.login(rec, httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("login = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	cookie := findSessionCookie(rec.Result().Cookies())
	if cookie == nil {
		t.Fatal("登录响应里没有会话 Cookie")
	}
	return c, store, cookie
}

// findSessionCookie 从一组 Set-Cookie 里挑出会话 Cookie；没有则返回 nil。
func findSessionCookie(cookies []*http.Cookie) *http.Cookie {
	for _, ck := range cookies {
		if ck.Name == sessionCookieName {
			return ck
		}
	}
	return nil
}

// ageSession 把会话改成「上次活跃在 sessionTouchAfter 之前、且只剩 remaining 有效期」，
// 即真实世界里天天在用的老会话——下一个请求应当触发续期。
func ageSession(t *testing.T, store database.Store, token string, remaining time.Duration) time.Time {
	t.Helper()
	expires := time.Now().Add(remaining)
	if err := store.TouchSession(context.Background(), hashSessionID(token),
		time.Now().Add(-2*sessionTouchAfter), expires); err != nil {
		t.Fatalf("age session: %v", err)
	}
	return expires
}

// sessionExpiresAt 读回服务端记的会话过期时刻。
func sessionExpiresAt(t *testing.T, store database.Store, token string) time.Time {
	t.Helper()
	sess, _, err := store.GetSessionWithUser(context.Background(), hashSessionID(token), time.Now())
	if err != nil {
		t.Fatalf("read session: %v", err)
	}
	return sess.ExpiresAt
}

// serveThroughGate 把请求送过 authGate（包住 next），返回响应。
func serveThroughGate(c *Controller, next http.Handler, req *http.Request) *http.Response {
	rec := httptest.NewRecorder()
	c.authGate(next).ServeHTTP(rec, req)
	return rec.Result()
}

// okHandler 是被守卫的普通业务处理器替身。
func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
}

// authedRequest 造一个带会话 Cookie 的请求。
func authedRequest(method, path string, cookie *http.Cookie) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	req.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value})
	return req
}

// TestSessionSlidingRenewalReissuesCookie 锁定滑动续期的客户端可见性：服务端续期的同一次请求里
// 必须重下发会话 Cookie，且与登录时同令牌、同属性、同过期时刻。
func TestSessionSlidingRenewalReissuesCookie(t *testing.T) {
	t.Run("续期请求把 Cookie 的过期时间跟着推后", func(t *testing.T) {
		c, store, login := newSlidingSessionFixture(t, "")
		staleExpires := ageSession(t, store, login.Value, 10*24*time.Hour)

		resp := serveThroughGate(c, okHandler(), authedRequest(http.MethodGet, "/api/auth/me", login))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		renewed := findSessionCookie(resp.Cookies())
		if renewed == nil {
			t.Fatal("续期后的响应没有重下发会话 Cookie——浏览器那份 Max-Age 仍停在登录那一刻")
		}
		if renewed.Value != login.Value {
			t.Fatalf("续期不应轮换令牌: got %q, want %q", renewed.Value, login.Value)
		}
		// 客户端过期时刻必须跟着服务端走：两者分叉正是这个 bug 的形态。
		serverExpires := sessionExpiresAt(t, store, login.Value)
		if !serverExpires.After(staleExpires) {
			t.Fatalf("服务端未续期: expires_at %v, 续期前 %v", serverExpires, staleExpires)
		}
		if diff := renewed.Expires.Sub(serverExpires); diff > 2*time.Second || diff < -2*time.Second {
			t.Fatalf("Cookie 过期时刻与服务端不同步: cookie %v, db %v", renewed.Expires, serverExpires)
		}
		// Max-Age 应是完整的一轮有效期，而不是续期前剩下的那点。
		if want := int(sessionTTL.Seconds()); renewed.MaxAge > want || renewed.MaxAge < want-120 {
			t.Fatalf("Cookie MaxAge = %d, want ≈ %d", renewed.MaxAge, want)
		}
	})

	t.Run("重下发的 Cookie 属性与登录时完全一致", func(t *testing.T) {
		// cookie_secure=always 让 Secure 取到 true，属性一致性才是被真正检验过的。
		c, store, login := newSlidingSessionFixture(t, config.CookieSecureAlways)
		ageSession(t, store, login.Value, 10*24*time.Hour)

		resp := serveThroughGate(c, okHandler(), authedRequest(http.MethodGet, "/api/auth/me", login))
		renewed := findSessionCookie(resp.Cookies())
		if renewed == nil {
			t.Fatal("续期后的响应没有重下发会话 Cookie")
		}
		if !login.Secure {
			t.Fatal("夹具期望登录 Cookie 带 Secure")
		}
		// 属性不一致会产生两份同名 Cookie，浏览器发哪份取决于 path/domain，行为不可预期。
		for _, attr := range []struct {
			name      string
			got, want any
		}{
			{"Path", renewed.Path, login.Path},
			{"Domain", renewed.Domain, login.Domain},
			{"Secure", renewed.Secure, login.Secure},
			{"HttpOnly", renewed.HttpOnly, login.HttpOnly},
			{"SameSite", renewed.SameSite, login.SameSite},
		} {
			if attr.got != attr.want {
				t.Errorf("Cookie.%s = %v, 登录时为 %v", attr.name, attr.got, attr.want)
			}
		}
	})

	t.Run("图片流响应同样重下发", func(t *testing.T) {
		// 阅读期几乎只有取页/缩略图请求，把图片排除等于在最该续期的场景里继续哑火；
		// 这些端点是 private + must-revalidate，共享缓存不会替别人存下这个 Set-Cookie。
		c, store, login := newSlidingSessionFixture(t, "")
		ageSession(t, store, login.Value, 10*24*time.Hour)

		thumbDir := config.ThumbnailDir(c.currentConfig())
		if err := os.MkdirAll(thumbDir, 0o755); err != nil {
			t.Fatalf("mkdir thumbnails: %v", err)
		}
		if err := os.WriteFile(filepath.Join(thumbDir, "cover.webp"), []byte("fake-image"), 0o644); err != nil {
			t.Fatalf("write thumbnail: %v", err)
		}
		req := requestWithRouteParam(http.MethodGet, "/api/thumbnails/cover.webp", nil, "*", "cover.webp")
		req.AddCookie(&http.Cookie{Name: login.Name, Value: login.Value})

		resp := serveThroughGate(c, http.HandlerFunc(c.serveThumbnailImage), req)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		if got := resp.Header.Get("Cache-Control"); got != pageImageCacheControl {
			t.Fatalf("Cache-Control = %q, want %q（不是 private 就不该挂 Set-Cookie）", got, pageImageCacheControl)
		}
		if findSessionCookie(resp.Cookies()) == nil {
			t.Fatal("图片流响应没有重下发会话 Cookie")
		}
	})

	t.Run("SSE 建连的响应头里带上续期 Cookie", func(t *testing.T) {
		// SSE 只在建连这一次经过 authGate，之后连接一直开着；续期必须搭上建连的那组响应头。
		c, store, login := newSlidingSessionFixture(t, "")
		ageSession(t, store, login.Value, 10*24*time.Hour)
		c.sse.closeClients() // 让 serveHTTP 写完响应头即返回，不必真挂一条长连接

		resp := serveThroughGate(c, http.HandlerFunc(c.sse.serveHTTP), authedRequest(http.MethodGet, "/api/events", login))
		if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
			t.Fatalf("Content-Type = %q, want text/event-stream", got)
		}
		if findSessionCookie(resp.Cookies()) == nil {
			t.Fatal("SSE 建连响应没有重下发会话 Cookie")
		}
	})
}

// touchFailingStore 让滑动续期落库失败，用于验证「服务端没续成就不下发」。
type touchFailingStore struct {
	database.Store
	calls int
}

func (s *touchFailingStore) TouchSession(context.Context, string, time.Time, time.Time) error {
	s.calls++
	return errors.New("touch failed")
}

// TestSessionSlidingRenewalSkipsCookie 锁定不重下发的判据：下发只搭在真正发生的那次续期上，
// 其余响应一律不带 Set-Cookie。
func TestSessionSlidingRenewalSkipsCookie(t *testing.T) {
	t.Run("未到续期节奏的请求不重下发", func(t *testing.T) {
		// 刚登录的会话 last_seen_at 是当下，不触发续期——这是主要的节流，
		// 每会话每小时至多一个 Set-Cookie 而不是每个请求一个。
		c, _, login := newSlidingSessionFixture(t, "")

		resp := serveThroughGate(c, okHandler(), authedRequest(http.MethodGet, "/api/auth/me", login))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		if ck := findSessionCookie(resp.Cookies()); ck != nil {
			t.Fatalf("同一小时内的请求不应重下发 Cookie, got %v", ck)
		}
	})

	t.Run("续期落库失败时不重下发", func(t *testing.T) {
		// 下发了却没续成，浏览器那份就比服务端活得久：用户拿着一个看起来没过期的 Cookie 撞 401。
		c, store, login := newSlidingSessionFixture(t, "")
		ageSession(t, store, login.Value, 10*24*time.Hour)
		failing := &touchFailingStore{Store: store}
		c.store = failing

		resp := serveThroughGate(c, okHandler(), authedRequest(http.MethodGet, "/api/auth/me", login))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200（续期失败不应影响本次请求）", resp.StatusCode)
		}
		if failing.calls != 1 {
			t.Fatalf("TouchSession 调用次数 = %d, want 1", failing.calls)
		}
		if ck := findSessionCookie(resp.Cookies()); ck != nil {
			t.Fatalf("续期落库失败时不应重下发 Cookie, got %v", ck)
		}
	})

	t.Run("鉴权未通过的响应不重下发", func(t *testing.T) {
		// CSRF 校验在续期之前拦下请求：被拒的响应不该顺手延长会话。
		c, store, login := newSlidingSessionFixture(t, "")
		ageSession(t, store, login.Value, 10*24*time.Hour)

		resp := serveThroughGate(c, okHandler(), authedRequest(http.MethodPost, "/api/users", login))
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", resp.StatusCode)
		}
		if ck := findSessionCookie(resp.Cookies()); ck != nil {
			t.Fatalf("CSRF 被拒的响应不应重下发 Cookie, got %v", ck)
		}
	})

	t.Run("未登录的 401 不重下发", func(t *testing.T) {
		c, _, _ := newSlidingSessionFixture(t, "")

		resp := serveThroughGate(c, okHandler(), httptest.NewRequest(http.MethodGet, "/api/auth/me", nil))
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", resp.StatusCode)
		}
		if ck := findSessionCookie(resp.Cookies()); ck != nil {
			t.Fatalf("未登录的响应不应下发会话 Cookie, got %v", ck)
		}
	})
}
