// 业务说明：本文件提供一个零依赖、并发安全的失败尝试限流器，用于登录暴破防护，以及 OPDS/Mihon
// 的 HTTP Basic 鉴权的 bcrypt CPU-DoS 防护。按 key（IP / 用户名）统计失败次数，超阈值后进入指数退避
// 锁定期，锁定期内的请求被直接 429 拒绝，从而无需再跑昂贵的 bcrypt。设计上刻意不引入 httprate 等外部
// 依赖，并把「按用户名锁定」这类自定义逻辑收在一处。
//
// 代理注意：clientIP 优先取 X-Forwarded-For 首跳，其次退回 RemoteAddr。部署在反向代理之后时，
// 反代应写入真实客户端 IP；若反代未写 XFF，则同一反代后的所有客户端会共享一个 IP 桶（可能相互影响）。

package api

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// attemptLimiter 按 key 统计失败尝试并施加指数退避锁定。零值不可用，请用 newAttemptLimiter。
type attemptLimiter struct {
	mu       sync.Mutex
	entries  map[string]*attemptEntry
	max      int           // 达到该失败次数即开始锁定
	window   time.Duration // 统计窗口：距首次失败超过该时长则重置计数
	baseLock time.Duration // 触发锁定后的基础锁定时长
	maxLock  time.Duration // 锁定时长上限（同时兜底移位溢出）
	now      func() time.Time
}

type attemptEntry struct {
	failures  int
	firstAt   time.Time
	lockUntil time.Time
}

func newAttemptLimiter(max int, window, baseLock, maxLock time.Duration) *attemptLimiter {
	return &attemptLimiter{
		entries:  make(map[string]*attemptEntry),
		max:      max,
		window:   window,
		baseLock: baseLock,
		maxLock:  maxLock,
		now:      time.Now,
	}
}

// retryAfter 返回 key 当前是否处于锁定期；若是，返回剩余锁定时长。
func (l *attemptLimiter) retryAfter(key string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[key]
	if !ok {
		return 0, false
	}
	now := l.now()
	if e.lockUntil.After(now) {
		return e.lockUntil.Sub(now), true
	}
	return 0, false
}

// recordFailure 记录一次失败：窗口外则重置计数；达到阈值后按指数退避设置锁定期。
func (l *attemptLimiter) recordFailure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.pruneLocked(now)
	e, ok := l.entries[key]
	if !ok || now.Sub(e.firstAt) > l.window {
		e = &attemptEntry{firstAt: now}
		l.entries[key] = e
	}
	e.failures++
	if e.failures >= l.max {
		// 指数退避：每超出阈值一次，锁定时长翻倍，封顶 maxLock；移位溢出也归到 maxLock。
		shift := uint(e.failures - l.max)
		backoff := l.maxLock
		if shift < 62 {
			if b := l.baseLock << shift; b > 0 && b < l.maxLock {
				backoff = b
			}
		}
		e.lockUntil = now.Add(backoff)
	}
}

// recordSuccess 成功后清除该 key 的失败记录。
func (l *attemptLimiter) recordSuccess(key string) {
	l.mu.Lock()
	delete(l.entries, key)
	l.mu.Unlock()
}

// pruneLocked 在 map 较大时清理已过期条目（未锁定且窗口已过），避免被伪造 key 的攻击撑大内存。
// 调用方须持有 l.mu。
func (l *attemptLimiter) pruneLocked(now time.Time) {
	if len(l.entries) < 1024 {
		return
	}
	for k, e := range l.entries {
		if e.lockUntil.After(now) {
			continue
		}
		if now.Sub(e.firstAt) > l.window {
			delete(l.entries, k)
		}
	}
}

// clientIP 提取用于限流的客户端 IP：优先 X-Forwarded-For 首跳，其次 RemoteAddr 的主机部分。
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			xff = xff[:i]
		}
		if ip := strings.TrimSpace(xff); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// respondTooManyAttempts 写 429 + Retry-After（秒，向上取整、至少 1）并返回本地化提示。
func respondTooManyAttempts(w http.ResponseWriter, r *http.Request, retryAfter time.Duration) {
	secs := int(retryAfter / time.Second)
	if retryAfter%time.Second != 0 {
		secs++
	}
	if secs < 1 {
		secs = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	jsonError(w, http.StatusTooManyRequests, apiText(requestLocale(r), "auth.too_many_attempts"))
}
