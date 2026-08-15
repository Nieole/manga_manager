// 本文件提供一个零依赖、并发安全的失败尝试限流器，用于登录暴破防护与 OPDS/Mihon HTTP Basic
// 鉴权的 bcrypt CPU-DoS 防护：按 key（IP / 用户名）统计失败次数，超阈值后指数退避锁定，
// 锁定期内直接 429、不再跑 bcrypt；不引入 httprate 等外部依赖，自定义逻辑收在一处。
//
// 代理注意：clientIP 优先取 X-Forwarded-For 首跳，否则退回 RemoteAddr；反代未写 XFF 时，
// 同一反代后的所有客户端会共享一个 IP 桶。

package api

import (
	"net/http"
	"strconv"
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
	now := l.now()
	e, ok := l.entries[key]
	if !ok {
		// 表已满时 recordFailure 不再为新 key 建条目，所以这里查不到并不代表「没失败过」。
		// 与 hasRoomLocked 保持同一取向：撑满 = 正在被攻击，对陌生 key 直接按锁定处理。
		if len(l.entries) >= limiterMaxEntries {
			return l.baseLock, true
		}
		return 0, false
	}
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
		if !ok && !l.hasRoomLocked() {
			return // 表已满：对新 key fail-closed，见 hasRoomLocked 的注释
		}
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

// 限流器的容量与剪枝参数。
const (
	// limiterPruneThreshold 是触发剪枝的条目数。低于它完全不扫。
	limiterPruneThreshold = 1024
	// limiterPruneSample 是单次剪枝最多检查的条目数，让单次剪枝保持 O(1) 而不是 O(n)：
	// 持续造新 key 的攻击者会让「扫全表且只删过期项」的剪枝退化成整体 O(n²)，而剪枝是
	// **持锁**的，会把整条鉴权路径（含每次成功登录、每个 OPDS/Mihon 请求的 retryAfter）
	// 串行拖慢。定额采样下 Go 的 map 迭代起点随机，每次看一批不同的条目，过期条目会在
	// 若干次失败之内被陆续清掉——剪枝本就只是内存回收，不需要「一次清干净」。
	limiterPruneSample = 256
	// limiterMaxEntries 是条目数硬上限。到顶后不再接纳新 key（见 recordFailure）。
	limiterMaxEntries = 8192
)

// pruneLocked 定额清理已过期条目（未锁定且窗口已过）。调用方须持有 l.mu。
//
// **锁定中的条目永不清理**：清掉它等于把已经成立的锁定白送回去，攻击者据此就能重置指数退避。
// 锁定条目的数量天然有界——每条至少要 max 次失败才会产生，且到 maxLock 就过期——
// 所以不需要靠淘汰来控制它们。
func (l *attemptLimiter) pruneLocked(now time.Time) {
	if len(l.entries) < limiterPruneThreshold {
		return
	}
	checked := 0
	for k, e := range l.entries {
		if checked >= limiterPruneSample {
			break
		}
		checked++
		if e.lockUntil.After(now) {
			continue // 锁定中，绝不清理
		}
		if now.Sub(e.firstAt) > l.window {
			delete(l.entries, k)
		}
	}
}

// hasRoomLocked 报告是否还能接纳一个新 key。调用方须持有 l.mu。
//
// 到顶时对新 key **fail-closed**：不新建条目、直接当作已被限流。
// 这条选择是刻意的——表被撑满意味着正在被攻击，此时「拒绝新来的」比
// 「挤掉某个已成立的锁定去给新 key 腾地方」安全得多。代价是攻击期间正常用户
// 也可能被误拒，但那本就是限流该有的表现，而且是有界的（条目会随窗口过期）。
func (l *attemptLimiter) hasRoomLocked() bool {
	return len(l.entries) < limiterMaxEntries
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
