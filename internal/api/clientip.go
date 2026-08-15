package api

import (
	"net"
	"net/http"
	"strings"

	"manga-manager/internal/config"
)

// clientIP 返回用于限流分桶的客户端 IP。
//
// 只有当直连对端（RemoteAddr）落在 server.trusted_proxies 声明的网段内时，
// 才采信 X-Forwarded-For / X-Real-IP。默认 trusted_proxies 为空 = 一律不信任转发头。
//
// 这条规则是必须的：限流以 IP 为键，无条件采信 XFF 意味着攻击者每个请求换一个伪造
// 头就是一个全新的桶，登录暴破防护与 Basic 鉴权的 bcrypt CPU-DoS 防护双双失效。
// 直连部署本就不该出现转发头；套了反代的部署把代理网段配上即可拿到真实 IP。
func (c *Controller) clientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	remote := remoteHost(r.RemoteAddr)
	if !c.trustsProxy(remote) {
		return remote
	}
	if forwarded := forwardedClientIP(r); forwarded != "" {
		return forwarded
	}
	return remote
}

// trustsForwardedHeaders 报告本次请求的转发头是否可信（直连对端落在 server.trusted_proxies 内）。
//
// 这是「代理头可不可信」这个概念的唯一出口：clientIP 用它决定要不要采信 X-Forwarded-For，
// 会话 Cookie 的 Secure 判定用它决定要不要采信 X-Forwarded-Proto。两者必须共用同一套
// trusted_proxies 判定，否则限流侧收紧了、Cookie 侧却仍可能无条件采信，分叉早晚重新长出来。
func (c *Controller) trustsForwardedHeaders(r *http.Request) bool {
	if r == nil {
		return false
	}
	return c.trustsProxy(remoteHost(r.RemoteAddr))
}

// trustsProxy 判断直连对端是否属于已声明的可信代理。
func (c *Controller) trustsProxy(remote string) bool {
	if remote == "" {
		return false
	}
	ip := net.ParseIP(remote)
	if ip == nil {
		return false
	}
	var cidrs []string
	if c != nil && c.config != nil {
		cfg := c.currentConfig()
		cidrs = cfg.Server.TrustedProxies
	}
	return ipInAnyCIDR(ip, cidrs)
}

// ipInAnyCIDR 报告 ip 是否落在任一 CIDR 内。无法解析的条目直接跳过（配置校验会另行报错）。
func ipInAnyCIDR(ip net.IP, cidrs []string) bool {
	for _, raw := range cidrs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		// 允许直接写单个 IP（等价于 /32 或 /128），省得用户为回环地址也要写掩码。
		if parsed := net.ParseIP(raw); parsed != nil {
			if parsed.Equal(ip) {
				return true
			}
			continue
		}
		_, network, err := net.ParseCIDR(raw)
		if err != nil || network == nil {
			continue
		}
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// forwardedClientIP 从转发头里取出最靠近客户端的那一跳。仅在对端可信时调用。
func forwardedClientIP(r *http.Request) string {
	if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
		first := xff
		if i := strings.IndexByte(xff, ','); i >= 0 {
			first = xff[:i]
		}
		if ip := normalizeIPCandidate(first); ip != "" {
			return ip
		}
	}
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		if ip := normalizeIPCandidate(realIP); ip != "" {
			return ip
		}
	}
	return ""
}

// normalizeIPCandidate 校验并归一化一个候选 IP 串；非法值返回空串，避免把任意字符串当作分桶键。
func normalizeIPCandidate(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// 有些代理会带端口（尤其 IPv6 的 [::1]:1234 形式）。
	if host, _, err := net.SplitHostPort(raw); err == nil {
		raw = host
	}
	raw = strings.Trim(raw, "[]")
	if net.ParseIP(raw) == nil {
		return ""
	}
	return raw
}

// remoteHost 从 RemoteAddr 里剥出主机部分。
func remoteHost(remoteAddr string) string {
	if remoteAddr == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return strings.Trim(remoteAddr, "[]")
	}
	return host
}

// 编译期确认 config 包已被引用（TrustedProxies 定义在此）。
var _ = config.SecretMask
