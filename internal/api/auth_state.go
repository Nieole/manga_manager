// 本文件把鉴权链路的进程内状态（账户是否已存在、Basic 凭据缓存、两个失败限流器）
// 从 Controller 抽成独立组件：这些状态彼此耦合——改密必须同时失效 basicAuthCache，
// 否则旧口令还能在协议侧用满 TTL；限流与缓存又共用同一个「客户端 IP」概念——
// 分散在 Controller 里，这些关系没地方统一维护。

package api

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"sync/atomic"
	"time"
)

// basicAuthCacheTTL 是 Basic 凭据校验结果的缓存时长。
// 取值是「省下 bcrypt」与「改密后旧口令最多还能用多久」之间的折衷；
// 改密路径会主动失效对应条目，所以这个 TTL 只影响没走改密的情形。
const basicAuthCacheTTL = 5 * time.Minute

type basicAuthEntry struct {
	userID    int64
	expiresAt time.Time
}

// authState 持有鉴权链路的全部进程内状态。零值不可用，须经 newAuthState 构造。
type authState struct {
	// usersPresent 缓存「站点已存在账户」这一事实：一旦创建了首个管理员即恒为 true，
	// authGate 据此判断是否处于「首启/尚无账户」的直通模式，避免每个请求都 COUNT(users)。
	// 账户体系保证至少留一个管理员，故该标志一旦置真不再回退。
	usersPresent atomic.Bool

	// basicAuthCache: key = 用户名+口令的哈希 → basicAuthEntry。
	// 用 sync.Map 而非带锁 map：读多写少，且条目数等于活跃协议客户端数（个位到几十）。
	basicAuthCache sync.Map

	loginLimiter *attemptLimiter
	// basicAuthLimiter 限流阅读协议 Basic 鉴权的失败尝试，兼作 bcrypt 的 CPU-DoS 防护。
	basicAuthLimiter *attemptLimiter
}

func newAuthState() *authState {
	return &authState{
		// 登录：15 分钟窗口内累计 5 次失败即锁定，基础 1 分钟、指数退避、封顶 15 分钟。
		loginLimiter: newAttemptLimiter(5, 15*time.Minute, time.Minute, 15*time.Minute),
		// 协议 Basic：更宽松些（客户端每次请求都带凭据），5 分钟窗口内 10 次失败锁定，封顶 10 分钟。
		basicAuthLimiter: newAttemptLimiter(10, 5*time.Minute, 30*time.Second, 10*time.Minute),
	}
}

// ---- 站点账户存在性 ----

func (a *authState) usersExist() bool {
	return a != nil && a.usersPresent.Load()
}

func (a *authState) markUsersExist() {
	if a != nil {
		a.usersPresent.Store(true)
	}
}

// ---- Basic 凭据缓存 ----

func basicAuthCacheKey(username, password string) string {
	sum := sha256.Sum256([]byte(username + "\x00" + password))
	return hex.EncodeToString(sum[:])
}

// lookupBasicAuth 返回该凭据对应的用户 id；未命中或已过期返回 ok=false。
func (a *authState) lookupBasicAuth(username, password string, now time.Time) (int64, bool) {
	if a == nil {
		return 0, false
	}
	key := basicAuthCacheKey(username, password)
	v, ok := a.basicAuthCache.Load(key)
	if !ok {
		return 0, false
	}
	entry, ok := v.(basicAuthEntry)
	if !ok || !entry.expiresAt.After(now) {
		a.basicAuthCache.Delete(key)
		return 0, false
	}
	return entry.userID, true
}

func (a *authState) rememberBasicAuth(username, password string, userID int64, now time.Time) {
	if a == nil {
		return
	}
	a.basicAuthCache.Store(basicAuthCacheKey(username, password),
		basicAuthEntry{userID: userID, expiresAt: now.Add(basicAuthCacheTTL)})
}

// invalidateBasicAuthForUser 清掉某用户在 Basic 缓存里的全部条目。
//
// 缓存键是 (用户名+口令) 的哈希，改密后旧口令对应的条目仍然有效：不清的话，最长
// basicAuthCacheTTL 之内旧密码在 OPDS/Mihon 上照样能登录——对「改密以踢掉泄露的凭据」
// 这个动机来说是致命的。条目数很少，Range 全表清理的开销可忽略。
func (a *authState) invalidateBasicAuthForUser(userID int64) {
	if a == nil {
		return
	}
	a.basicAuthCache.Range(func(key, value any) bool {
		if e, ok := value.(basicAuthEntry); ok && e.userID == userID {
			a.basicAuthCache.Delete(key)
		}
		return true
	})
}
