// 守客户端 IP 的可信来源判定：只有直连对端落在 trusted_proxies 网段内才采信转发头。
// 客户端 IP 是限流的分桶键，取错等于限流失效——这里锁的是安全属性而非工具函数行为。

package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"manga-manager/internal/config"
)

func newClientIPController(t *testing.T, trusted ...string) *Controller {
	t.Helper()
	cfg := &config.Config{}
	cfg.Server.TrustedProxies = trusted
	return &Controller{config: config.NewManager(cfg)}
}

// TestClientIPIgnoresForwardedHeadersFromUntrustedPeer 是本批次的核心断言。
//
// 限流以客户端 IP 为键：无条件采信 X-Forwarded-For 会让攻击者每个请求换一个伪造
// IP 就开出一个全新的桶，登录暴破防护与 OPDS/Mihon 的 bcrypt CPU-DoS 防护双双失效。
func TestClientIPIgnoresForwardedHeadersFromUntrustedPeer(t *testing.T) {
	c := newClientIPController(t) // trusted_proxies 为空 = 谁都不信

	req := httptest.NewRequest(http.MethodGet, "/api/auth/login", nil)
	req.RemoteAddr = "203.0.113.9:51234"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	req.Header.Set("X-Real-IP", "5.6.7.8")

	if got := c.clientIP(req); got != "203.0.113.9" {
		t.Fatalf("untrusted peer must not be able to spoof its bucket key, got %q", got)
	}
}

func TestClientIPTrustsForwardedHeadersFromConfiguredProxy(t *testing.T) {
	c := newClientIPController(t, "10.0.0.0/8")

	req := httptest.NewRequest(http.MethodGet, "/api/auth/login", nil)
	req.RemoteAddr = "10.1.2.3:4444"
	req.Header.Set("X-Forwarded-For", "198.51.100.7, 10.1.2.3")

	if got := c.clientIP(req); got != "198.51.100.7" {
		t.Fatalf("trusted proxy should surface the real client IP, got %q", got)
	}
}

func TestClientIPFallsBackWhenForwardedValueIsGarbage(t *testing.T) {
	c := newClientIPController(t, "10.0.0.0/8")

	req := httptest.NewRequest(http.MethodGet, "/api/auth/login", nil)
	req.RemoteAddr = "10.1.2.3:4444"
	// 非法值不能被当成分桶键——否则可信代理后的客户端仍能用任意字符串开新桶。
	req.Header.Set("X-Forwarded-For", "not-an-ip")

	if got := c.clientIP(req); got != "10.1.2.3" {
		t.Fatalf("garbage forwarded value must fall back to peer address, got %q", got)
	}
}

func TestClientIPAcceptsBareIPInTrustedProxies(t *testing.T) {
	// 允许直接写单个 IP，省得为回环地址也要写掩码。
	c := newClientIPController(t, "127.0.0.1")

	req := httptest.NewRequest(http.MethodGet, "/api/auth/login", nil)
	req.RemoteAddr = "127.0.0.1:9999"
	req.Header.Set("X-Real-IP", "198.51.100.7")

	if got := c.clientIP(req); got != "198.51.100.7" {
		t.Fatalf("bare-IP trusted proxy entry should work, got %q", got)
	}
}

// TestAdminOnlyPathsIncludeBrowseDirs 锁住目录浏览端点的角色边界。
//
// /api/browse-dirs 是 GET，会落到 authorize 的「读方法一律放行」分支里：任意已登录的
// 普通账号（设计上只该浏览漫画与记录本人进度）都能从 ?path=/ 起逐级枚举宿主机目录树。
func TestAdminOnlyPathsIncludeBrowseDirs(t *testing.T) {
	adminOnly := []string{
		"/api/browse-dirs",
		"/api/system/config",
		"/api/users",
		"/api/users/3",
		// 外部库会话：读侧不得落进「读方法一律放行」分支，否则普通账号能拿到
		// 带服务器绝对路径的会话快照。这是 isAdminOnlyPath 唯一的单元级守卫。
		"/api/libraries/1/external-libraries/session/1-2",
		"/api/libraries/1/external-libraries/scan",
	}
	for _, p := range adminOnly {
		if !isAdminOnlyPath(p) {
			t.Fatalf("%s must be admin-only", p)
		}
	}
	// 注意规则用的是带斜杠的 "/api/libraries/" 前缀，"/api/libraries" 本身不受影响。
	for _, p := range []string{"/api/libraries", "/api/libraries/1", "/api/series/search", "/api/books/1/progress"} {
		if isAdminOnlyPath(p) {
			t.Fatalf("%s should not be admin-only", p)
		}
	}
}

// TestPasswordChangeGateAllowsOnlySelfServiceEndpoints 锁住服务端的强制改密：
// must_change_password 不能只由前端 AuthGate 拦截，否则非浏览器客户端可无限期使用初始密码。
func TestPasswordChangeGateAllowsOnlySelfServiceEndpoints(t *testing.T) {
	for _, p := range []string{"/api/auth/me", "/api/auth/logout", "/api/auth/change-password", "/api/auth/status"} {
		if !isPasswordChangeAllowedPath(p) {
			t.Fatalf("%s should stay reachable while a password change is pending", p)
		}
	}
	for _, p := range []string{"/api/libraries", "/api/series/search", "/api/system/config"} {
		if isPasswordChangeAllowedPath(p) {
			t.Fatalf("%s must be blocked until the initial password is changed", p)
		}
	}
}

// TestRegularWritablePathsAreOnlyPerUserWrites 锁住「哪些写操作不需要管理员」这条线。
//
// 前端按它决定系列详情、资料库、合集等页面上的动作渲不渲染给普通用户；这条线一旦漂移而前端
// 不跟，用户看到的就是一排点下去吃 403 的按钮。名字里带 favorite / collection / reading-list
// 的都不是每用户数据——那几张表没有 user_id，写的是站点级共享内容。
func TestRegularWritablePathsAreOnlyPerUserWrites(t *testing.T) {
	perUser := []string{
		"/api/books/1/progress",
		"/api/books/1/reading-time",
		"/api/books/1/bookmarks",
		"/api/books/bulk-progress",
		"/api/series/bulk-progress",
		"/api/series/1/review",
	}
	for _, p := range perUser {
		if !isRegularWritablePath(p) {
			t.Fatalf("%s is a per-user write and must stay open to regular users", p)
		}
	}
	adminOnlyWrites := []string{
		"/api/series/info/1",
		"/api/series/1/rescan",
		"/api/series/1/scrape",
		"/api/series/1/scrape-apply",
		"/api/series/1/open-dir",
		"/api/series/1/comicinfo",
		"/api/series/1/custom-fields",
		"/api/series/1/relations",
		"/api/relations/1",
		"/api/series/bulk-update",
		"/api/series/bulk-edit",
		"/api/books/1/comicinfo",
		"/api/books/1/cover",
		"/api/books/1/cover/upload",
		"/api/metadata/reviews/1/apply",
		"/api/metadata/reviews/1/reject",
		"/api/collections/1/series",
		"/api/reading-lists/1/items",
		"/api/smart-filters/1",
	}
	for _, p := range adminOnlyWrites {
		if isRegularWritablePath(p) {
			t.Fatalf("%s writes site-wide state and must stay admin-only", p)
		}
	}
}
