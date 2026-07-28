// 业务说明：本文件守卫会话 Cookie 的 Secure 标志判定。
//
// Secure 决定浏览器是否只在 HTTPS 上回传会话令牌。判错的两个方向都会出事：
// 漏判（该有而没有）让明文链路上的中间人拿到会话；误判（不该有却有）会让浏览器
// 在 HTTP 部署上直接丢弃 Cookie，登录彻底失效。而这个判定的唯一输入是一个**客户端可伪造**
// 的请求头，所以它属于安全边界，必须按「谁的声明可信」来决定，而不是见到头就采信。

package api

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"manga-manager/internal/config"
)

func TestSessionCookieSecure(t *testing.T) {
	cases := []struct {
		name           string
		cookieSecure   string
		trustedProxies []string
		remoteAddr     string
		headers        map[string]string
		tlsConn        bool
		want           bool
	}{
		{
			// 核心修复：无条件采信 X-Forwarded-Proto 等于让任意客户端自己决定 Secure 标志。
			name:       "auto: 未登记对端声明 https 一律不采信",
			remoteAddr: "203.0.113.9:5555",
			headers:    map[string]string{"X-Forwarded-Proto": "https"},
			want:       false,
		},
		{
			name:           "auto: 可信代理声明 https 才采信",
			trustedProxies: []string{"10.0.0.0/8"},
			remoteAddr:     "10.1.2.3:5555",
			headers:        map[string]string{"X-Forwarded-Proto": "https"},
			want:           true,
		},
		{
			// 判定顺序的锁：proxy→backend 之间常另有一段内部 TLS。若让 r.TLS 覆盖可信代理的声明，
			// 对外明文的部署会被误判成 https 而下发 Secure，浏览器随即丢弃 Cookie。
			name:           "auto: 可信代理说 http 时不被后端 TLS 覆盖",
			trustedProxies: []string{"10.0.0.0/8"},
			remoteAddr:     "10.1.2.3:5555",
			headers:        map[string]string{"X-Forwarded-Proto": "http"},
			tlsConn:        true,
			want:           false,
		},
		{
			name:           "auto: 可信代理未声明协议时回退到直连 TLS",
			trustedProxies: []string{"10.0.0.0/8"},
			remoteAddr:     "10.1.2.3:5555",
			tlsConn:        true,
			want:           true,
		},
		{
			name:           "auto: 可信代理未声明协议且非 TLS",
			trustedProxies: []string{"10.0.0.0/8"},
			remoteAddr:     "10.1.2.3:5555",
			want:           false,
		},
		{
			name:       "auto: 直连 TLS",
			remoteAddr: "203.0.113.9:5555",
			tlsConn:    true,
			want:       true,
		},
		{
			// 与 requestBaseURL 同口径：XFP 缺失时看 X-Forwarded-Scheme。
			// 只统一「信不信」而头名不统一，口径就仍然是两套。
			name:           "auto: 采信可信代理的 X-Forwarded-Scheme",
			trustedProxies: []string{"10.0.0.0/8"},
			remoteAddr:     "10.1.2.3:5555",
			headers:        map[string]string{"X-Forwarded-Scheme": "https"},
			want:           true,
		},
		{
			// TLS 在反代终结、又不想为此放宽 trusted_proxies 的部署，用这个出口。
			name:         "always: 明文直连也带 Secure",
			cookieSecure: config.CookieSecureAlways,
			remoteAddr:   "203.0.113.9:5555",
			want:         true,
		},
		{
			// 反代错误声明 https 而实际服务明文时的逃生口。
			name:           "never: 可信代理声明 https 也不带",
			cookieSecure:   config.CookieSecureNever,
			trustedProxies: []string{"10.0.0.0/8"},
			remoteAddr:     "10.1.2.3:5555",
			headers:        map[string]string{"X-Forwarded-Proto": "https"},
			tlsConn:        true,
			want:           false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			controller, _, _, _ := newTestController(t)
			cfg := controller.config.Snapshot()
			cfg.Server.TrustedProxies = tc.trustedProxies
			cfg.Server.CookieSecure = tc.cookieSecure
			config.NormalizeConfig(&cfg)
			controller.config.Replace(&cfg)

			req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
			req.RemoteAddr = tc.remoteAddr
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			if tc.tlsConn {
				req.TLS = &tls.ConnectionState{}
			}

			if got := controller.sessionCookieSecure(req); got != tc.want {
				t.Fatalf("sessionCookieSecure = %v, want %v", got, tc.want)
			}

			// 写入与清除必须给出同一个 Secure：属性不一致时浏览器可能不认为是同一个 Cookie，
			// 于是「登出」留下一个仍然有效的会话 Cookie。
			for label, emit := range map[string]func(w http.ResponseWriter){
				"set":   func(w http.ResponseWriter) { controller.setSessionCookie(w, req, "tok", time.Now().Add(time.Hour)) },
				"clear": func(w http.ResponseWriter) { controller.clearSessionCookie(w, req) },
			} {
				rec := httptest.NewRecorder()
				emit(rec)
				cookies := rec.Result().Cookies()
				if len(cookies) != 1 {
					t.Fatalf("%s: 期望恰好 1 个 Cookie，得到 %d 个", label, len(cookies))
				}
				if cookies[0].Secure != tc.want {
					t.Fatalf("%s: Cookie.Secure = %v, want %v", label, cookies[0].Secure, tc.want)
				}
			}
		})
	}
}

// TestCookieSecureNormalization 守卫配置归一化：非法值必须落到 auto 而不是留着一个
// 谁也不认识的字符串，让 sessionCookieSecure 的 switch 默默走 default。
func TestCookieSecureNormalization(t *testing.T) {
	cases := map[string]string{
		"":         config.CookieSecureAuto,
		"  AUTO  ": config.CookieSecureAuto,
		"Always":   config.CookieSecureAlways,
		"never":    config.CookieSecureNever,
		"alwyas":   config.CookieSecureAuto, // 拼错 —— 回落 auto 并在启动日志里告警
	}
	for raw, want := range cases {
		t.Run("raw="+raw, func(t *testing.T) {
			var cfg config.Config
			cfg.Server.CookieSecure = raw
			config.NormalizeConfig(&cfg)
			if got := cfg.Server.CookieSecure; got != want {
				t.Fatalf("normalize(%q) = %q, want %q", raw, got, want)
			}
		})
	}
}
