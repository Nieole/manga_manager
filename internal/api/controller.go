package api

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/diskwork"
	"manga-manager/internal/external"
	"manga-manager/internal/koreader"
	"manga-manager/internal/metadata"
	"manga-manager/internal/proposal"
	"manga-manager/internal/runtimecfg"
	"manga-manager/internal/scanner"
	"manga-manager/internal/storageio"
	"manga-manager/internal/taskcontrol"
	"manga-manager/internal/taskstore"

	"github.com/go-chi/chi/v5"
	lru "github.com/hashicorp/golang-lru/v2"
	"golang.org/x/sync/singleflight"
)

type Controller struct {
	store database.Store
	// imageCache 必须按**总字节**限流（见 image_memory_cache.go），不能按条数——
	// 页图大小悬殊，按条数给不出稳定的内存上界，而它只是个加速缓存。
	imageCache *imageMemoryCache
	// 阅读路径上的两级只读缓存（书籍归档来源 + 归档页清单）已抽成独立组件
	// （page_archive.go 的 pageArchiveCache）：二者必须同时失效，收在一起才能把这条约束写下来。
	pageArchive        *pageArchiveCache
	progressWriteCache *lru.Cache[int64, cachedProgressWrite]
	// 仪表盘统计缓存已抽成独立组件（stats_cache.go）；Controller 仅持引用，失效经薄委托方法转发。
	stats   *statsCache
	scanner *scanner.Scanner
	config  *config.Manager
	// diskWork 是任务体发起**磁盘作业**要用的执行器。Controller 自己不再直接用它——
	// 它只经 newTaskEngine 交给引擎，由引擎装进每个任务的**运行句柄**。
	diskWork   *diskwork.Runner
	koreader   *koreader.Service
	external   *external.Manager
	proposals  *proposal.Service
	configPath string
	watcher    *scanner.FileWatcher

	// SSE 事件推送已抽成独立组件（sse_broker.go）；Controller 仅持引用做编排。
	sse *sseBroker

	// AI 阅读推荐缓存已抽成独立组件（recommendation_cache.go，按 locale + TTL + singleflight）；Controller 仅持引用。
	recommendations *recommendationCache
	// pageTranscodeGroup 合并同一 cacheKey 的并发页图转码：冷缓存时多客户端/预取请求同一页只解码+编码一次，
	// 其余等待者复用同一结果，避免重复 CPU 转码与重复归档读取。
	pageTranscodeGroup singleflight.Group

	// taskEngine 是任务子域的适配层：把启动入口、控制端点与**重启函数**注册表接到
	// internal/task 的领域引擎与 internal/taskstore 的落盘上（它自己不留任务表，理由见它的符号 doc）。
	// 任务方法仍是 Controller 方法，统一经 c.taskEngine 走（端点定义见 controller_tasks.go）。
	taskEngine *taskEngine

	// 缩略图重建的跨库进度聚合已抽成独立组件（rebuild_thumb_aggregator.go），自带互斥锁。
	rebuildThumbAgg *rebuildThumbAggregator

	openPath        func(string) error
	providerFactory func(string) metadata.Provider

	// 鉴权链路的进程内状态（账户存在性、Basic 凭据缓存、两个失败限流器）
	// 已抽成独立组件（auth_state.go）。
	auth *authState
	// untrustedProtoWarnOnce 保证「忽略了来自不可信对端的 X-Forwarded-Proto」这条告警每进程只打一次：
	// 该头由客户端可控，逐次打日志等于给了一个刷日志的口子。
	untrustedProtoWarnOnce sync.Once

	lifecycleOnce sync.Once
	shutdownOnce  sync.Once
	lifecycleMu   sync.Mutex
	done          chan struct{}
	closed        bool
	backgroundWG  sync.WaitGroup

	// franchise 合集重建的合并式调度已抽成独立组件（franchise_rebuilder.go）；Controller 仅持引用。
	franchiseRebuilder *franchiseRebuilder
}

type RunStatus struct {
	// RunID 是这一条**运行**的标识：同一个任务键在列表里可以出现多条，各是一次运行，
	// 而键只认得出「哪件事」，认不出「哪一次」。重试从此不再抹掉上一次，因此它是必需的。
	RunID int64 `json:"run_id"`
	// TaskID 是这次运行属于哪个**任务**：任务中心的两层要靠它对上——一帧推过来的运行属于清单里
	// 哪一行、展开的历次运行里该不该多出这一条，都只有这个字段答得出。
	TaskID  int64  `json:"task_id"`
	Key     string `json:"key"`
	Type    string `json:"type"`
	Scope   string `json:"scope"`
	ScopeID *int64 `json:"scope_id,omitempty"`
	// Variant 是身份四要素的第四项（见 TaskIdentity），引擎用它把**重启函数**按（类型，变体）分发。
	// 它与另外三项一样进 JSON：四要素分两处走的话，任何按快照判身份的地方都要再从别处补一项。
	// 四项一起是身份行上那条唯一约束的四列。
	Variant   TaskVariant `json:"variant,omitempty"`
	ScopeName string      `json:"scope_name,omitempty"`
	// Trigger 是**发起方**：这次运行是被什么叫来的（见 task.Trigger）。它不改变运行怎么跑，
	// 只回答「这活是谁叫来的」——半夜转盘的那条究竟是定时守护还是用户自己点的。
	Trigger string `json:"trigger,omitempty"`
	Status  string `json:"status"`
	Message string `json:"message"`
	// MessageCode/MessageParams 承载可本地化的任务消息：后端只发稳定 i18n 键 + 占位参数，由前端按当前
	// 语言渲染，Go 里因此不出现面向用户的文案字面量。
	//
	// Message **没有任何来源**，恒为空：文案只有 i18n 码一种，连**中断**那句也是码。它留在契约里
	// 只为前端那条「码缺失时的兜底」还在——一句写死在 Go 里的文案没有任何地方能翻译它。
	MessageCode   string            `json:"message_code,omitempty"`
	MessageParams map[string]string `json:"message_params,omitempty"`
	Error         string            `json:"error,omitempty"`
	Current       int               `json:"current"`
	Total         int               `json:"total"`
	Percent       *float64          `json:"percent,omitempty"`
	RatePerMinute float64           `json:"rate_per_minute,omitempty"`
	EtaSeconds    *int64            `json:"eta_seconds,omitempty"`
	CanCancel     bool              `json:"can_cancel"`
	CanPause      bool              `json:"can_pause"`
	CanResume     bool              `json:"can_resume"`
	Retryable     bool              `json:"retryable"`
	PausedAt      *time.Time        `json:"paused_at,omitempty"`
	// PauseReason 是这一次暂停原因（见 task.PauseReason）：用户按的是这条运行自己的暂停键，
	// 还是「全部暂停」。只在**已暂停**期间非空——它回答的是「谁把它按下的」，
	// 一条早已跑完的运行带着这句话只会误导。
	PauseReason string `json:"pause_reason,omitempty"`
	// ControlPausedMillis 是这个任务至今在**已暂停**里待过的累计毫秒数，由引擎在每次离开暂停
	// （恢复、取消、收尾）时把那一段折进来。它只有一个用途：从速率与 ETA 的分母里扣掉——
	// 暂停期间任务一条都没处理，把那段时长算成在干活会让两个数一路失真到终态。
	//
	// 它与重启入参里的 `paused_ms` 不是一回事：那个是**磁盘作业**为阅读让路而等掉的时长，
	// 期间任务本身仍在跑。累计只属于这一次**运行**——重试是新一次运行，分母从它自己的开始时刻重新计。
	// 不进 JSON：前端不显示它，它是运行行上的一列。
	ControlPausedMillis int64 `json:"-"`
	// CoalescedCount 是**合并**进这条**排队中**运行的额外发起次数：同一件事被反复发起时不新建
	// 运行，只把这个数加一。它要发出去，否则用户看到的是一条孤零零的排队，不知道它代表了几次发起。
	CoalescedCount int               `json:"coalesced_count,omitempty"`
	Phase          string            `json:"phase,omitempty"`
	CurrentItem    string            `json:"current_item,omitempty"`
	EffectiveLimit *TaskLimits       `json:"effective_limit,omitempty"`
	Metrics        map[string]int64  `json:"metrics,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
	Params         map[string]string `json:"params,omitempty"`
	// StartedAt 是这次运行**进入运行中**的时刻。**排队中**的运行还没开跑，因此它是 nil 而不是
	// 一个零值时刻——发零值出去的话，界面上那格「开始时间」会写着它公元 1 年就开始了。
	// 入队时刻不是开始时刻：排了一小时队的运行会被算成跑了一小时，速率与 ETA 一路失真。
	StartedAt  *time.Time `json:"started_at,omitempty"`
	UpdatedAt  time.Time  `json:"updated_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Sequence   int64      `json:"-"`
}

// RunLive 是任务中心**实况区**的一帧：此刻仍会变化的那些运行，加上它们的汇总。
//
// 它不受列表筛选影响。实况区答的是「我的盘现在在干什么」——那是个全局问题，
// 而顶部那对全部暂停 / 全部恢复同样作用于全体运行，按筛选给出的答案会与按钮动到的那批对不上。
type RunLive struct {
	// Active 是**活动态**运行数，也就是占着运行槽位的那些；Queued 是**排队中**的条数。
	Active int `json:"active"`
	Queued int `json:"queued"`
	// Slots 是运行槽位上限，界面据此画出「槽位 n/N」。它恒为一个真的会被撞上的数——
	// 领域引擎把小于 1 的配置值改写成默认值，因此 0 不会发出去。
	Slots int `json:"slots"`
	// Paused 回答「有没有运行被暂停」，PausedAll 回答「全部暂停的闸门还关着吗」。
	//
	// 两个都要发，因为它们答的不是同一个问题：闸门关着而被暂停的那几条已经被取消或跑完时，
	// 前者是 false 而后者仍是 true，此时队列还被拦着——只按前者决定「全部恢复」的可按性，
	// 那个按钮会灰在唯一能重新放开队列的位置上。
	Paused    bool `json:"paused"`
	PausedAll bool `json:"paused_all"`
	// Runs 是常驻置顶的那批运行：**活动态**与**排队中**，仍会变化的都在里面。
	Runs []RunStatus `json:"runs"`
}

// TaskSummary 是**任务清单**上的一行：一个任务的身份、它的长期属性，与它最近一次运行。
//
// 历次运行不在这里：一屏几十个任务、每个几十次运行，一次全带回来是几千条。
// 展开某一行时按 task_id 单独取（见 taskFilters.TaskID）。
type TaskSummary struct {
	TaskID  int64       `json:"task_id"`
	Type    string      `json:"type"`
	Scope   string      `json:"scope"`
	ScopeID *int64      `json:"scope_id,omitempty"`
	Variant TaskVariant `json:"variant,omitempty"`
	// ScopeName 取自最近一次运行：显示名是**过渡期**字段，落在运行上而不是身份上
	// （见 task.Run.ScopeName）。一次都没跑过的任务因此没有显示名，界面回落到作用域加 id。
	ScopeName string `json:"scope_name,omitempty"`
	// Disabled / FailStreak / BackoffUntil 是**退避**与禁用那一组长期属性，写入方尚未存在，
	// 因此今天恒为零值。界面据此整块不显示，而不是画一个「连败 0」。
	Disabled      bool       `json:"disabled"`
	FailStreak    int        `json:"fail_streak"`
	LastSuccessAt *time.Time `json:"last_success_at,omitempty"`
	BackoffUntil  *time.Time `json:"backoff_until,omitempty"`
	// LastRun 是「上次跑成什么样」。一次运行都没有的任务为 nil。
	LastRun *RunStatus `json:"last_run,omitempty"`
}

type TaskRuntime struct {
	Context   context.Context
	Cancel    context.CancelFunc
	PauseGate *taskcontrol.PauseGate
	StartedAt time.Time
}

type TaskLimits struct {
	ScanProfile                string `json:"scan_profile,omitempty"`
	ScannerWorkersConfigured   int    `json:"scanner_workers_configured,omitempty"`
	ScannerWorkersEffective    int    `json:"scanner_workers_effective,omitempty"`
	StorageProfile             string `json:"storage_profile,omitempty"`
	VolumeKey                  string `json:"volume_key,omitempty"`
	ScanConcurrency            int    `json:"scan_concurrency,omitempty"`
	ArchiveOpenConcurrency     int    `json:"archive_open_concurrency,omitempty"`
	CoverConcurrency           int    `json:"cover_concurrency,omitempty"`
	HashConcurrency            int    `json:"hash_concurrency,omitempty"`
	PauseBackgroundWhenReading bool   `json:"pause_background_when_reading"`
	IdleOnlyHeavyTasks         bool   `json:"idle_only_heavy_tasks"`
	DisableSameDiskPageCache   bool   `json:"disable_same_disk_page_cache"`
}

type SystemCapabilitiesResponse struct {
	SupportedScanFormats     []string `json:"supported_scan_formats"`
	SupportedScanProfiles    []string `json:"supported_scan_profiles"`
	SupportedLogLevels       []string `json:"supported_log_levels"`
	SupportedStorageProfiles []string `json:"supported_storage_profiles"`
	DefaultScanFormats       string   `json:"default_scan_formats"`
	DefaultScanInterval      int      `json:"default_scan_interval"`
	SupportedLLMProviders    []string `json:"supported_llm_providers"`
	SupportedLLMAPIModes     []string `json:"supported_llm_api_modes"`
}

type SystemConfigResponse struct {
	Config       config.Config              `json:"config"`
	Validation   config.ValidationResult    `json:"validation"`
	Capabilities SystemCapabilitiesResponse `json:"capabilities"`
}

type SearchResult struct {
	Hits     []*SearchHit `json:"hits"`
	Total    uint64       `json:"total_hits"`
	MaxScore float64      `json:"max_score"`
}

type SearchHit struct {
	ID     string                 `json:"id"`
	Score  float64                `json:"score"`
	Fields map[string]interface{} `json:"fields,omitempty"`
}

const (
	// rebuildBookHashesTaskKey 与 lowPriorityBookHashTaskKey 是 rebuild_book_hashes 这个任务类型
	// 下两个**变体**的**任务键**：前者是用户在维护页发起的前台重建，后者是**资料库扫描**收尾串联的
	// 低优先级回填。键只管寻址，分回哪一条跑法由变体决定，见 buildTaskRelaunchers。
	rebuildBookHashesTaskKey     = "rebuild_book_hashes"
	lowPriorityBookHashTaskKey   = "background_book_hash_backfill"
	lowPriorityBookHashBatchSize = 32
	lowPriorityBookHashBatchGap  = 100 * time.Millisecond
	dashboardStatsCacheTTL       = 30 * time.Second
)

// controllerCacheSizes 是各内存 LRU 的容量。抽成参数是为了让白盒测试用极小容量构造，
// 从而能在几条数据内触发淘汰路径，而不必为此复制一份装配逻辑。
type controllerCacheSizes struct {
	// imageBytes 是页图内存缓存的**字节**预算（不是条数）。
	imageBytes     int64
	page           int
	bookPageSource int
	progressWrite  int
}

func defaultControllerCacheSizes() controllerCacheSizes {
	return controllerCacheSizes{imageBytes: defaultImageCacheBytes, page: 128, bookPageSource: 512, progressWrite: 2048}
}

// newControllerCore 完成 Controller 的**装配**：建组件、接扫描器回调、填任务重试注册表。
// 它不启动任何后台 goroutine，也不碰文件监听——那些属于「运行」而非「装配」，由 NewController 负责。
//
// 白盒测试构造 Controller 必须调用这里，不得手工拼装字段：手工拼装在新增组件字段时容易漏掉，
// 两条路径共用同一份装配后，这类遗漏在结构上不可能出现。
func newControllerCore(store database.Store, scan *scanner.Scanner, cfg *config.Manager, cfgPath string, sizes controllerCacheSizes) *Controller {
	cache := newImageMemoryCache(sizes.imageBytes)
	progressWriteCache, _ := lru.New[int64, cachedProgressWrite](sizes.progressWrite)
	c := &Controller{
		store:              store,
		stats:              newStatsCache(),
		imageCache:         cache,
		pageArchive:        newPageArchiveCache(sizes.bookPageSource, sizes.page),
		progressWriteCache: progressWriteCache,
		scanner:            scan,
		config:             cfg,
		external:           external.NewManager(store, 30*time.Minute),
		proposals:          proposal.NewService(proposalDB{Store: store}),
		configPath:         cfgPath,
		sse:                newSSEBroker(),
		recommendations:    newRecommendationCache(24 * time.Hour),
		rebuildThumbAgg:    newRebuildThumbAggregator(),
		openPath:           openPathInDefaultFileManager,
		auth:               newAuthState(),
	}
	if scan != nil {
		scan.SetBatchCallback(c.handleScannerBatchEvent)
	}

	// diskWork 读的是 c 的当前配置快照，须在 c 构造完成后建立。
	c.diskWork = diskwork.NewRunner(c.currentConfig, storageio.Default)

	c.koreader = koreader.NewService(store, cfg)

	// franchiseRebuilder 注入 c 的领域重建方法与生命周期后台登记器（须在 c 构造完成后设置）。
	c.franchiseRebuilder = newFranchiseRebuilder(c.RebuildFranchiseCollections, c.runBackground)

	// taskEngine 依赖 c 的 SSE 投递与后台运行能力，同样须在 c 构造完成后建立。
	// 任务快照只投给管理员：它带着作用域显示名、重启入参与失败原因，与任务列表接口
	// （对普通用户 403）是同一份数据，两条路口径不同就等于那条 403 不存在。
	c.taskEngine = newTaskEngine(taskEngineConfig{
		Store:         taskstore.New(store.DB()),
		Publish:       c.sse.publishAdmin,
		RunBackground: c.runBackground,
		DiskWork:      c.diskWork,
		Slots:         c.taskSlots,
	})
	// 构建任务重试注册表：必须在任何任务创建（admitTaskLocked 会经 isRetryableTask 查表）之前完成。
	c.taskEngine.relaunchers = c.buildTaskRelaunchers()

	return c
}

func NewController(store database.Store, scan *scanner.Scanner, cfg *config.Manager, cfgPath string) *Controller {
	c := newControllerCore(store, scan, cfg, cfgPath, defaultControllerCacheSizes())

	c.taskEngine.markInterrupted(context.Background())

	c.runBackground(func() { c.sse.run(c.lifecycleDone()) })
	c.runBackground(c.startDaemon)
	c.runBackground(c.startPageCacheJanitor)
	c.runBackground(c.startSessionJanitor)

	// 初始化文件系统监控
	fw, err := scanner.NewFileWatcher(scan)
	if err != nil {
		slog.Warn("Failed to create file watcher", "error", err)
	} else {
		c.watcher = fw
		fw.Start(c.PublishEvent)
		// 为现有库开启监听
		c.runBackground(func() {
			libs, err := store.ListLibraries(context.Background())
			if err != nil {
				slog.Warn("Failed to list libraries for watcher", "error", err)
				return
			}
			for _, lib := range libs {
				if lib.ScanMode == "watch" {
					_ = fw.WatchLibrary(lib.ID, lib.Path, lib.ScanFormats)
				}
			}
		})
	}

	return c
}

func (c *Controller) lifecycleDone() <-chan struct{} {
	c.lifecycleOnce.Do(func() {
		c.done = make(chan struct{})
	})
	return c.done
}

func (c *Controller) runBackground(fn func()) {
	c.lifecycleDone()
	c.lifecycleMu.Lock()
	if c.closed {
		c.lifecycleMu.Unlock()
		return
	}
	c.backgroundWG.Add(1)
	c.lifecycleMu.Unlock()
	go func() {
		defer c.backgroundWG.Done()
		// 后台任务的 panic 不经过 middleware.Recoverer（那只覆盖 HTTP handler goroutine），
		// 未捕获会直接终止整个进程。这里统一兜底：记录 panic 与栈后让服务继续可用。
		// 任务体这一路另有兜底：引擎在 runTaskGoroutine 里把对应任务一并置为失败态。
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("Background task panicked", "panic", rec, "stack", string(debug.Stack()))
			}
		}()
		fn()
	}()
}

func (c *Controller) Close() {
	c.lifecycleDone()
	c.shutdownOnce.Do(func() {
		c.lifecycleMu.Lock()
		c.closed = true
		close(c.done)
		c.lifecycleMu.Unlock()
		if c.watcher != nil {
			c.watcher.Stop()
		}
		c.taskEngine.stopAllRuntimes()
	})
	c.backgroundWG.Wait()
}

func (c *Controller) currentConfig() config.Config {
	if c.config == nil {
		return config.Config{}
	}
	return c.config.Snapshot()
}

// taskSlots 读此刻生效的全局并发上限。
//
// 每次判定现读一遍配置快照，不在装配期取一个数收进引擎：上限在设置里可改，而改完要对
// **新的放行**生效——收成一个数的话，调大上限要重启进程才算数。已经在跑的不受影响：
// 引擎只在准入与放行时读它。
func (c *Controller) taskSlots() int {
	return c.currentConfig().Tasks.MaxConcurrentRuns
}

func (c *Controller) protocolEnabled(protocol string) bool {
	cfg := c.currentConfig()
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "opds":
		return cfg.Protocols.OPDS.Enabled
	case "mihon":
		return cfg.Protocols.Mihon.Enabled
	default:
		return false
	}
}

func (c *Controller) requireProtocolEnabled(protocol string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !c.protocolEnabled(protocol) {
				http.NotFound(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// 说明：历史上的可选共享令牌鉴权（requireAuth / extractAPIToken）已随多用户改造退役——
// /api 组现由 authGate（Cookie session + CSRF + 角色，见 auth_controller.go）统一守卫。
// 对应的 Server.Auth 配置字段也已删除——保留一个没有任何代码校验的鉴权开关，只会让
// 管理员在启动日志看到「令牌鉴权已启用」而误以为站点已加固。

// constantTimeTokenMatch 用恒定时间比较避免令牌校验的时序侧信道（现用于 CSRF 令牌比对）。
func constantTimeTokenMatch(provided, expected string) bool {
	if provided == "" || expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}

// validateOutboundLLMTarget 对 test-llm 的出站目标做 SSRF 加固：仅允许 http/https scheme，
// 拒绝 file://、gopher:// 等危险协议。由于本服务默认支持本机 Ollama（localhost），此处不封锁
// 私有网段/回环地址，未鉴权部署时应配合 server.auth 开启，或置于受信内网/反向代理之后。
func validateOutboundLLMTarget(baseURL, endpoint string) error {
	target := strings.TrimSpace(baseURL)
	if target == "" {
		target = strings.TrimSpace(endpoint)
	}
	if target == "" {
		return nil
	}
	u, err := url.Parse(target)
	if err != nil {
		return fmt.Errorf("无效的目标地址: %v", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return nil
	default:
		return fmt.Errorf("不支持的目标协议 %q，仅允许 http/https", u.Scheme)
	}
}

func (c *Controller) persistConfig(cfg *config.Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}
	config.NormalizeConfig(cfg)

	if err := os.MkdirAll(filepath.Dir(cfg.Database.Path), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.Cache.Dir, 0o755); err != nil {
		return err
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	// 原子写：避免保存过程中崩溃留下半截 config.yaml 导致下次启动解析失败。
	// 权限用 ConfigFilePerm(0600)：这条路径写的是 RestoreMaskedSecrets 回填后的**真实**密钥，
	// 是明文 api_key 真正落盘的地方（首次生成时配置里还没有密钥）。
	if err := config.AtomicWriteFile(c.configPath, data, config.ConfigFilePerm); err != nil {
		return err
	}
	c.config.Replace(cfg)
	// 与文件热重载走同一条 runtimecfg.Apply：重建 parser 池 / images 处理器并设置日志级别，使经 UI 保存的
	// archive_pool_size / max_ai_concurrency 立即生效，不依赖文件监听回环（监听器失效时也能生效）。
	if err := runtimecfg.Apply(cfg); err != nil {
		return err
	}
	return nil
}

func (c *Controller) systemCapabilities() SystemCapabilitiesResponse {
	return SystemCapabilitiesResponse{
		SupportedScanFormats:     append([]string{}, config.SupportedScanFormats...),
		SupportedScanProfiles:    append([]string{}, config.SupportedScanProfiles...),
		SupportedLogLevels:       append([]string{}, config.SupportedLogLevels...),
		SupportedStorageProfiles: append([]string{}, config.SupportedStorageProfiles...),
		DefaultScanFormats:       config.DefaultScanFormatsCSV,
		DefaultScanInterval:      config.DefaultScanInterval,
		SupportedLLMProviders:    []string{"ollama", "openai"},
		SupportedLLMAPIModes:     []string{"responses", "chat_completions"},
	}
}

func (c *Controller) buildSystemConfigResponse(cfg config.Config) SystemConfigResponse {
	return SystemConfigResponse{
		Config:       cfg,
		Validation:   config.ValidateConfig(&cfg),
		Capabilities: c.systemCapabilities(),
	}
}

func openPathInDefaultFileManager(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("explorer.exe", path)
	case "linux":
		cmd = exec.Command("xdg-open", path)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	if err := cmd.Start(); err != nil {
		return err
	}
	// 必须有配对的 Wait：Start 之后不 Wait，子进程退出后会在 Unix 内核里留下僵尸表项，
	// 而 Go 运行时不会替我们收割——每点一次「在文件管理器中打开」就泄漏一个。
	//
	// 但**不能同步等**：这些命令拉起的是长期存活的 GUI 进程（Finder / Explorer / 文件管理器），
	// 同步 Wait 会把 HTTP 请求一直挂到用户关掉那个窗口为止。
	//
	// 也刻意不走 runBackground：那会把这个 goroutine 计入 backgroundWG，于是优雅停机要等
	// 用户关闭文件管理器才能完成。这个 goroutine 的唯一职责就是收尸，进程退出时随之消失即可。
	//
	// （对应地也不用 exec.CommandContext + 超时兜底：那会在超时后杀掉用户正开着的文件管理器。）
	go func() {
		_ = cmd.Wait() // explorer.exe 惯例返回非零退出码，与成功与否无关，丢弃即可
	}()
	return nil
}

// startPageCacheJanitor 周期性地把磁盘页缓存修剪到配置的容量上限（单 goroutine 串行，经
// runBackground 登记 backgroundWG，关闭时会退出）。
func (c *Controller) startPageCacheJanitor() {
	c.enforcePageCacheBudget() // 启动兜底
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-c.lifecycleDone():
			return
		case <-ticker.C:
			c.enforcePageCacheBudget()
		}
	}
}

func (c *Controller) startDaemon() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	// 记录各个资料库的上次扫描时间
	lastScan := make(map[int64]time.Time)

	for {
		select {
		case <-c.lifecycleDone():
			return
		case <-ticker.C:
		}

		libs, err := c.store.ListLibraries(context.Background())
		if err != nil {
			slog.Error("Daemon failed to fetch libraries", "error", err)
			continue
		}

		now := time.Now()
		for _, lib := range libs {
			if lib.ScanMode != "interval" {
				continue
			}

			interval := time.Duration(lib.ScanInterval) * time.Minute
			last, ok := lastScan[lib.ID]
			// 如果从未记录（或刚启动），则在首次 Tick 时也会直接触发，或者也可选择不直接触发。目前假定超过间隔就触发。
			if !ok || now.Sub(last) >= interval {
				lastScan[lib.ID] = now
				slog.Info("Triggering auto-scan for library from Daemon", "library_id", lib.ID, "path", lib.Path)
				c.runBackground(func() {
					id, path := lib.ID, lib.Path
					defer c.purgeReadingPathCaches()
					err := c.scanner.ScanLibrary(context.Background(), id, path, false, nil)
					// 「已有扫描在跑」不是故障：定时守护与手动扫描本就可能撞车，
					// 下一个 tick 会再试，不必按错误刷屏。
					if errors.Is(err, scanner.ErrScanAlreadyRunning) {
						slog.Info("Auto-scan skipped, another scan is in progress", "library_id", id)
						return
					}
					if err != nil {
						slog.Error("Auto-scan failed", "library_id", id, "error", err)
						c.invalidateDashboardStatsCache("auto_scan_failed")
						return
					}
					c.warmDashboardStatsCacheAsync("auto_scan_completed")
				})
			}
		}
	}
}

// PublishEvent 供 Scanner / FileWatcher 等外部投递事件消息；委托给 sseBroker（保留此方法以维持外部 API）。
func (c *Controller) PublishEvent(event string) {
	c.sse.publish(event)
}

// serveEvents 是 /api/events 的处理器：按当前用户的角色决定这条订阅能收到哪一档事件。
//
// 整条端点不设成管理员专属，是因为普通用户界面依赖它——刷新帧是「管理员扫完库，我这边的列表
// 自己更新」的唯一通道；关掉它等于拿一个泄露换一个功能故障，还会让前端订阅一个必然 401 的端点。
// 取不到用户（首启尚无账户）时按普通用户处理，宁可少发不可错发。
func (c *Controller) serveEvents(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r.Context())
	c.sse.serveHTTP(w, r, ok && user.IsAdmin())
}

// eventPrefix 截取事件前缀用于日志，避免输出整段 JSON
func eventPrefix(event string) string {
	if i := strings.IndexByte(event, ':'); i >= 0 {
		return event[:i]
	}
	if len(event) > 32 {
		return event[:32]
	}
	return event
}

func (c *Controller) SetupRoutes(r chi.Router) {
	r.Route("/api", func(r chi.Router) {
		// 多用户会话鉴权（Cookie session + CSRF + 角色）。必须在挂载任何子路由之前 Use，
		// 中间件内部会放行公开鉴权端点（/api/auth/status|setup|login）与阅读协议前缀 /api/mihon/，
		// 且在「尚无账户」的首启阶段直通（首次建管理员前站点无数据需保护）。
		r.Use(c.authGate)
		c.setupMihonRoutes(r)

		// 鉴权与账户管理
		r.Get("/auth/status", c.authStatus)
		r.Post("/auth/setup", c.setupAdmin)
		r.Post("/auth/login", c.login)
		r.Post("/auth/logout", c.logout)
		r.Get("/auth/me", c.authMe)
		r.Post("/auth/change-password", c.changePassword)
		r.Get("/users", c.listUsers)
		r.Post("/users", c.createUser)
		r.Patch("/users/{userId}", c.updateUser)
		r.Post("/users/{userId}/password", c.resetUserPassword)
		r.Delete("/users/{userId}", c.deleteUser)

		r.Get("/events", c.serveEvents)
		r.Get("/search", c.searchBooks)
		r.Get("/libraries", c.getLibraries)
		r.Post("/libraries", c.createLibrary)
		r.Put("/libraries/{libraryId}", c.updateLibrary)
		r.Get("/libraries/{libraryId}/franchise", c.getLibraryFranchiseGraph)
		r.Post("/libraries/{libraryId}/scan", c.scanLibrary)
		r.Post("/libraries/{libraryId}/external-libraries/session", c.createExternalLibrarySession)
		r.Get("/libraries/{libraryId}/external-libraries/session/{sessionId}", c.getExternalLibrarySession)
		r.Get("/libraries/{libraryId}/external-libraries/session/{sessionId}/series", c.getExternalLibrarySeries)
		r.Post("/libraries/{libraryId}/external-libraries/session/{sessionId}/transfer", c.transferToExternalLibrary)
		r.Post("/libraries/{libraryId}/scrape", c.scrapeLibrary)
		r.Post("/libraries/{libraryId}/ai-grouping", c.aiGroupingLibrary)
		r.Post("/libraries/{libraryId}/cleanup", c.cleanupLibrary)
		r.Delete("/libraries/{libraryId}", c.deleteLibrary)
		r.Get("/browse-dirs", c.browseDirs)
		r.Get("/metadata/search", c.searchMetadata)
		r.Get("/metadata/providers", c.listProviders)
		r.Get("/recommendations", c.getRecommendations)
		r.Get("/health/report", c.getHealthReport)
		r.Get("/metadata/reviews", c.listMetadataReviewInbox)
		r.Post("/metadata/reviews/bulk-apply", c.bulkApplyMetadataReviews)
		r.Post("/metadata/reviews/bulk-reject", c.bulkRejectMetadataReviews)
		r.Get("/ai-grouping/reviews", c.listAIGroupingReviews)
		r.Get("/reviews/inbox", c.listReviewInbox)
		r.Get("/reviews/inbox/summary", c.getReviewInboxSummary)
		r.Post("/ai-grouping/reviews/{reviewId}/apply", c.applyAIGroupingReview)
		r.Post("/ai-grouping/reviews/{reviewId}/reject", c.rejectAIGroupingReview)
		r.Put("/ai-grouping/reviews/{reviewId}/collections/{collectionId}", c.updateAIGroupingReviewCollection)
		r.Post("/ai-grouping/reviews/{reviewId}/collections/{collectionId}/apply", c.applyAIGroupingReviewCollection)
		r.Post("/ai-grouping/reviews/{reviewId}/collections/{collectionId}/reject", c.rejectAIGroupingReviewCollection)
		r.Get("/series/{seriesId}/metadata-review", c.listSeriesMetadataReview)
		r.Post("/metadata/reviews/{reviewId}/apply", c.applyMetadataReview)
		r.Post("/metadata/reviews/{reviewId}/reject", c.rejectMetadataReview)
		r.Route("/series", func(r chi.Router) {
			r.Post("/bulk-update", c.bulkUpdateSeries)
			r.Post("/bulk-edit", c.bulkEditSeries)
			r.Post("/bulk-progress", c.bulkUpdateSeriesProgress)
			r.Get("/search", c.searchSeriesPaged)
			r.Get("/recent-read", c.getRecentReadSeries)
			r.Get("/{libraryId}", c.getSeriesByLibrary)
			r.Get("/info/{seriesId}", c.getSeriesInfo)
			r.Put("/info/{seriesId}", c.updateSeriesInfo)
			r.Post("/{seriesId}/open-dir", c.openSeriesDirectory)
			r.Post("/{seriesId}/rescan", c.scanSeries)
			r.Post("/{seriesId}/scrape", c.scrapeSeriesMetadata)
			r.Get("/{seriesId}/scrape-search", c.scrapeSearchMetadata)
			r.Post("/{seriesId}/scrape-apply", c.applyScrapedMetadata)
			r.Get("/{seriesId}/tags", c.getSeriesTags)
			r.Get("/{seriesId}/custom-fields", c.getSeriesCustomFields)
			r.Put("/{seriesId}/custom-fields", c.replaceSeriesCustomFields)
			r.Get("/{seriesId}/review", c.getSeriesReview)
			r.Put("/{seriesId}/review", c.putSeriesReview)
			r.Delete("/{seriesId}/review", c.deleteSeriesReview)
			r.Get("/{seriesId}/authors", c.getSeriesAuthors)
			r.Get("/{seriesId}/links", c.getSeriesLinks)
			r.Get("/{seriesId}/context", c.getSeriesContext)
			r.Get("/{seriesId}/continue", c.getSeriesContinueEndpoint)
			r.Get("/{seriesId}/comicinfo.zip", c.exportSeriesComicInfoArchive)
			r.Post("/{seriesId}/comicinfo", c.writeSeriesComicInfo)
		})

		r.Route("/books", func(r chi.Router) {
			r.Get("/duplicates", c.getDuplicateBooks)
			r.Post("/remove", c.removeBooks)
			r.Post("/bulk-progress", c.bulkUpdateBookProgress)
			r.Post("/bulk-progress/sync", c.bulkSyncBookProgress)
			r.Post("/{bookId}/progress", c.updateBookProgress)
			r.Post("/{bookId}/reading-time", c.addBookReadingTime)
			r.Get("/{bookId}/comicinfo.xml", c.exportBookComicInfo)
			r.Post("/{bookId}/comicinfo", c.writeBookComicInfo)
			r.Post("/{bookId}/cover", c.setBookCoverFromPage)
			r.Post("/{bookId}/cover/upload", c.uploadBookCover)
			r.Get("/{bookId}/file", c.serveBookFile)
			r.Get("/{bookId}/bookmarks", c.listReadingBookmarks)
			r.Post("/{bookId}/bookmarks", c.upsertReadingBookmark)
			r.Delete("/{bookId}/bookmarks/{bookmarkId}", c.deleteReadingBookmark)
			r.Get("/{seriesId}", c.getBooksBySeries)
		})

		r.Route("/tags", func(r chi.Router) {
			r.Get("/all", c.getAllTags)
			r.Get("/search", c.searchTags)
			r.Patch("/{tagId}", c.renameTag)
			r.Post("/{tagId}/merge", c.mergeTag)
			r.Delete("/{tagId}", c.deleteTag)
		})

		r.Route("/authors", func(r chi.Router) {
			r.Get("/all", c.getAllAuthors)
			r.Get("/search", c.searchAuthors)
		})

		r.Get("/system/config", c.getSystemConfig)
		r.Get("/system/capabilities", c.getSystemCapabilities)
		r.Get("/system/client-connections", c.getClientConnections)
		r.Get("/system/performance", c.getSystemPerformance)
		r.Get("/system/storage-io", c.getStorageIODiagnostics)
		r.Post("/system/config", c.updateSystemConfig)
		r.Get("/system/logs", c.getSystemLogs)
		r.Get("/system/page-cache", c.getPageCacheStats)
		r.Delete("/system/page-cache", c.clearPageCache)
		r.Get("/system/tasks", c.listTasks)
		r.Delete("/system/tasks", c.clearTasks)
		r.Get("/system/tasks/live", c.getTaskCenterLive)
		r.Get("/system/tasks/summary", c.listTaskSummaries)
		r.Post("/system/tasks/pause-all", c.pauseAllTasks)
		r.Post("/system/tasks/resume-all", c.resumeAllTasks)
		// 重试作用在**任务**上（再发起一次同一件事），因此仍按**任务键**寻址；
		// 暂停 / 恢复 / 取消作用在**运行**上，按运行 id 寻址——同一个键此刻可以有两条仍会变化的
		// 运行（一条在跑、一条排队），按键寻址答不出用户按的是哪一条（关键决定 15）。
		r.Post("/system/tasks/{taskKey}/retry", c.retryTask)
		r.Post("/system/runs/{runID}/pause", c.pauseRun)
		r.Post("/system/runs/{runID}/resume", c.resumeRun)
		r.Post("/system/runs/{runID}/cancel", c.cancelRun)
		r.Get("/system/koreader", c.getKOReaderSettings)
		r.Get("/system/koreader/accounts", c.listKOReaderAccounts)
		r.Get("/system/koreader/unmatched", c.listKOReaderUnmatched)
		r.Get("/system/koreader/devices", c.getKOReaderDeviceDiagnostics)
		r.Post("/system/koreader", c.updateKOReaderSettings)
		r.Post("/system/koreader/accounts", c.createKOReaderAccount)
		r.Post("/system/koreader/accounts/{accountId}/rotate-key", c.rotateKOReaderAccountKey)
		r.Post("/system/koreader/accounts/{accountId}/toggle", c.toggleKOReaderAccount)
		r.Delete("/system/koreader/accounts/{accountId}", c.deleteKOReaderAccount)
		r.Post("/system/koreader/apply-matching", c.applyKOReaderMatching)
		r.Post("/system/koreader/rebuild-hashes", c.rebuildKOReaderHashes)
		r.Post("/system/koreader/reconcile", c.reconcileKOReaderProgress)
		r.Delete("/system/koreader/progress/{progressId}", c.resetKOReaderProgress)
		r.Post("/system/rebuild-index", c.rebuildIndex)
		r.Post("/system/rebuild-initials", c.rebuildInitials)
		r.Post("/system/rebuild-franchises", c.rebuildFranchiseCollectionsHandler)
		r.Post("/system/rebuild-thumbnails", c.rebuildThumbnails)
		r.Post("/system/cleanup-thumbnails", c.cleanupThumbnails)
		r.Post("/system/rebuild-file-identities", c.rebuildFileIdentities)
		r.Post("/system/batch-scrape", c.batchScrapeAllSeries)
		r.Post("/system/test-llm", c.testLLMConfig)

		// 统计看板
		r.Get("/stats/dashboard", c.getDashboardStats)
		r.Get("/stats/activity-heatmap", c.getActivityHeatmap)
		r.Get("/stats/recent-read", c.getRecentReadAll)
		r.Get("/stats/recommendations", c.getRecommendations)
		// 深度统计（第 6 项，每用户）
		r.Get("/stats/streak", c.getReadingStreak)
		r.Get("/stats/reading-time", c.getReadingTimeStats)
		r.Get("/stats/period", c.getPeriodStats)

		// 合集管理
		r.Route("/collections", func(r chi.Router) {
			r.Get("/", c.listCollections)
			r.Post("/", c.createCollection)
			r.Put("/{collectionId}", c.updateCollection)
			r.Delete("/{collectionId}", c.deleteCollection)
			r.Get("/{collectionId}/series", c.getCollectionSeries)
			r.Post("/{collectionId}/series", c.addSeriesToCollection)
			r.Delete("/{collectionId}/series/{seriesId}", c.removeSeriesFromCollection)
		})
		r.Get("/collection-views", c.listCollectionViews)
		r.Get("/collection-views/smart/{filterId}/series", c.getSmartCollectionSeries)
		r.Get("/collection-views/smart/{filterId}/snapshot-preview", c.previewSmartCollectionSnapshot)
		r.Post("/collection-views/smart/{filterId}/snapshot", c.snapshotSmartCollection)

		r.Route("/libraries/{libraryId}/smart-filters", func(r chi.Router) {
			r.Get("/", c.listSmartFilters)
			r.Post("/", c.upsertSmartFilter)
		})
		r.Put("/smart-filters/{filterId}", c.updateSmartFilter)
		r.Delete("/smart-filters/{filterId}", c.deleteSmartFilter)

		// 有序阅读清单
		r.Route("/reading-lists", func(r chi.Router) {
			r.Get("/", c.listReadingLists)
			r.Post("/", c.createReadingList)
			r.Put("/{listId}", c.updateReadingList)
			r.Delete("/{listId}", c.deleteReadingList)
			r.Get("/{listId}/items", c.listReadingListItems)
			r.Post("/{listId}/items", c.addReadingListItem)
			r.Post("/{listId}/items/reorder", c.reorderReadingListItems)
			r.Delete("/{listId}/items/{itemId}", c.removeReadingListItem)
		})

		// 系列关联
		r.Get("/series/{seriesId}/relations", c.getSeriesRelations)
		r.Get("/series/{seriesId}/franchise", c.getSeriesFranchise)
		r.Post("/series/{seriesId}/relations", c.createSeriesRelation)
		r.Delete("/relations/{relationId}", c.deleteSeriesRelation)
		r.Put("/relations/{relationId}", c.updateSeriesRelation)

		// 独立路径，避免与 /books/{seriesId} 通配符冲突
		r.Get("/book-info/{bookId}", c.getBookInfo)
		r.Get("/book-next/{bookId}", c.getNextBook)
		r.Get("/book-prev/{bookId}", c.getPrevBook)

		r.Route("/pages", func(r chi.Router) {
			r.Get("/{bookId}", c.getPagesByBook)
			r.Get("/{bookId}/{pageNumber}", c.servePageImage)
		})

		r.Route("/covers", func(r chi.Router) {
			r.Get("/{bookId}", c.serveCoverImage)
		})

		// 通用静态直接下发，适配首卷封面作为系列代表图（支持二级哈希子目录）
		r.Get("/thumbnails/*", c.serveThumbnailImage)
	})
}

func (c *Controller) serveThumbnailImage(w http.ResponseWriter, r *http.Request) {
	thumbDir := config.ThumbnailDir(c.currentConfig())
	filename := chi.URLParam(r, "*")
	fullPath := filepath.Join(thumbDir, filename)
	w.Header().Set("Cache-Control", pageImageCacheControl)
	// 图片资源不依赖 Origin，清除 CORS 中间件写入的 Vary: Origin，
	// 否则浏览器以 (URL+Origin) 为缓存 key，同源 <img> 请求无法命中缓存。
	w.Header().Del("Vary")

	if info, err := os.Stat(fullPath); err == nil && !info.IsDir() {
		etag := weakETag(fmt.Sprintf("thumbnail-%s-%d-%d", filename, info.ModTime().UnixNano(), info.Size()))
		w.Header().Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}

	http.ServeFile(w, r, fullPath)
}

// jsonResponse 是 JSON API 的唯一出口，也是宿主机绝对路径按角色净化的唯一落点：
// 非管理员的响应一律先过 redactHostPaths，新增端点因此默认就是安全的，无需逐个记得裁。
func jsonResponse(w http.ResponseWriter, status int, data interface{}) {
	if !responseKeepsHostPaths(w) {
		data = redactHostPaths(data)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// 响应头已 WriteHeader 发出，此时若编码/写出失败（多为客户端已断开）已无可挽救的动作，显式忽略。
	_ = json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, status int, message string) {
	jsonResponse(w, status, map[string]string{"error": message})
}

// getLibraries 返回资料库列表。整条端点对已登录用户开放——普通用户的侧栏、仪表盘与整理页
// 都靠它拿 id 与 name；响应里的宿主机绝对路径由 jsonResponse 按角色统一裁掉（见 redactHostPaths），
// 只有管理员的编辑弹窗拿得到真值。
func (c *Controller) getLibraries(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	libs, err := c.store.ListLibraries(ctx)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to fetch libraries")
		return
	}

	if libs == nil {
		libs = []database.Library{} // 保证 JSON 数组非 null
	}
	jsonResponse(w, http.StatusOK, libs)
}

func parseID(r *http.Request, param string) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, param), 10, 64)
}

func (c *Controller) getBooksBySeries(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}

	books, err := c.store.ListBooksBySeries(ctx, seriesID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to fetch books")
		return
	}

	if books == nil {
		books = []database.Book{}
	}
	c.overlayUserProgress(ctx, c.currentUserID(r), books)
	sortBooksForReading(books)
	jsonResponse(w, http.StatusOK, books)
}

func (c *Controller) getBookInfo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bookID, err := parseID(r, "bookId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid book ID")
		return
	}

	book, err := c.store.GetBook(ctx, bookID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "Book not found")
		return
	}

	c.overlayUserProgressOne(ctx, c.currentUserID(r), &book)
	jsonResponse(w, http.StatusOK, book)
}

type BulkUpdateSeriesRequest struct {
	SeriesIDs  []int64 `json:"series_ids"`
	IsFavorite *bool   `json:"is_favorite"`
}

func (c *Controller) bulkUpdateSeries(w http.ResponseWriter, r *http.Request) {
	var req BulkUpdateSeriesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	if len(req.SeriesIDs) == 0 {
		jsonResponse(w, http.StatusOK, map[string]string{"message": "No series updated"})
		return
	}

	ctx := r.Context()
	for _, id := range req.SeriesIDs {
		if req.IsFavorite != nil {
			err := c.store.UpdateSeriesFavorite(ctx, database.UpdateSeriesFavoriteParams{
				IsFavorite: *req.IsFavorite,
				ID:         id,
			})
			if err != nil {
				slog.Error("Failed to bulk update series favorite", "series_id", id, "error", err)
			}
		}
	}

	jsonResponse(w, http.StatusOK, map[string]string{"message": "Bulk update completed"})
}

// BulkEditSeriesRequest 批量增量编辑多个系列的元数据；未提供的字段（nil/空）不改。
type BulkEditSeriesRequest struct {
	SeriesIDs  []int64  `json:"series_ids"`
	AddTags    []string `json:"add_tags"`
	RemoveTags []string `json:"remove_tags"`
	Status     *string  `json:"status"`
	Publisher  *string  `json:"publisher"`
}

func (c *Controller) bulkEditSeries(w http.ResponseWriter, r *http.Request) {
	var req BulkEditSeriesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	if len(req.SeriesIDs) == 0 {
		jsonResponse(w, http.StatusOK, map[string]interface{}{"updated": 0})
		return
	}

	err := c.store.BulkEditSeries(r.Context(), req.SeriesIDs, database.BulkSeriesEdit{
		AddTags:    req.AddTags,
		RemoveTags: req.RemoveTags,
		Status:     req.Status,
		Publisher:  req.Publisher,
	})
	if err != nil {
		slog.Error("Failed to bulk edit series", "count", len(req.SeriesIDs), "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to bulk edit series")
		return
	}

	c.invalidateDashboardStatsCache("bulk_edit")
	jsonResponse(w, http.StatusOK, map[string]interface{}{"updated": len(req.SeriesIDs)})
}

type BulkUpdateBookProgressRequest struct {
	BookIDs []int64 `json:"book_ids"`
	IsRead  bool    `json:"is_read"` // true=标为已读(最大页码), false=标为未读(1)
}

type BulkUpdateSeriesProgressRequest struct {
	SeriesIDs []int64 `json:"series_ids"`
	IsRead    bool    `json:"is_read"` // true=标为已读(最大页码), false=标为未读(清空阅读记录)
}

func (c *Controller) bulkUpdateBookProgress(w http.ResponseWriter, r *http.Request) {
	var req BulkUpdateBookProgressRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	if len(req.BookIDs) == 0 {
		jsonResponse(w, http.StatusOK, map[string]string{"message": "No books updated"})
		return
	}
	if len(req.BookIDs) > maxBulkReadStateBooks {
		jsonError(w, http.StatusBadRequest, "Too many books in one request")
		return
	}

	ctx := r.Context()
	// 已登录用户走每用户进度：一次事务内标记全部书并按系列刷新 user_series_progress。
	if uid := c.currentUserID(r); uid > 0 {
		if err := c.store.SetUserBooksReadState(ctx, uid, req.BookIDs, req.IsRead, time.Now()); err != nil {
			jsonError(w, http.StatusInternalServerError, "Failed to update progress")
			return
		}
		c.invalidateVolatileStatsCache("bulk_book_progress")
		jsonResponse(w, http.StatusOK, map[string]interface{}{"message": "Bulk progress update completed", "updated": len(req.BookIDs)})
		return
	}

	// 旧全局路径（首启尚无账户 / 单元测试）。按系列分组：每系列一个事务，内部逐书写入后只刷新一次
	// series_stats，避免走 store 包装器时每本书都隐式触发一次全系列统计重算（O(N^2) 聚合 + 逐条 autocommit）。
	booksBySeries := make(map[int64][]database.Book)
	orderedSeries := make([]int64, 0)
	for _, id := range req.BookIDs {
		book, err := c.store.GetBook(ctx, id)
		if err != nil {
			slog.Error("Failed to load book for bulk progress update", "book_id", id, "error", err)
			continue
		}
		if _, seen := booksBySeries[book.SeriesID]; !seen {
			orderedSeries = append(orderedSeries, book.SeriesID)
		}
		booksBySeries[book.SeriesID] = append(booksBySeries[book.SeriesID], book)
	}
	updated := 0
	for _, seriesID := range orderedSeries {
		books := booksBySeries[seriesID]
		if err := c.applySeriesBooksReadStateTx(ctx, seriesID, books, req.IsRead); err != nil {
			slog.Error("Failed to bulk update book progress", "series_id", seriesID, "error", err)
			continue
		}
		updated += len(books)
	}
	if updated > 0 {
		c.invalidateVolatileStatsCache("bulk_book_progress")
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{"message": "Bulk progress update completed", "updated": updated})
}

func (c *Controller) bulkUpdateSeriesProgress(w http.ResponseWriter, r *http.Request) {
	var req BulkUpdateSeriesProgressRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	if len(req.SeriesIDs) == 0 {
		jsonResponse(w, http.StatusOK, map[string]interface{}{"message": "No series updated", "updated": 0})
		return
	}
	// 系列 id 数本身就要限：每个都会被展开成该系列的全部书。
	if len(req.SeriesIDs) > maxBulkReadStateSeries {
		jsonError(w, http.StatusBadRequest, "Too many series in one request")
		return
	}

	ctx := r.Context()
	uid := c.currentUserID(r)
	updated := 0
	// 已登录用户：汇集所有系列的书，一次性走每用户读态写入。
	if uid > 0 {
		var bookIDs []int64
		for _, seriesID := range req.SeriesIDs {
			books, err := c.store.ListBooksBySeries(ctx, seriesID)
			if err != nil {
				slog.Error("Failed to load books for bulk series progress update", "series_id", seriesID, "error", err)
				continue
			}
			for _, b := range books {
				bookIDs = append(bookIDs, b.ID)
			}
		}
		// 展开后的书数同样要挡：系列 id 数在上限内，展开结果仍可能是整库量级。
		if len(bookIDs) > maxBulkReadStateBooks {
			jsonError(w, http.StatusBadRequest, "Selected series expand to too many books")
			return
		}
		if err := c.store.SetUserBooksReadState(ctx, uid, bookIDs, req.IsRead, time.Now()); err != nil {
			jsonError(w, http.StatusInternalServerError, "Failed to update progress")
			return
		}
		if len(bookIDs) > 0 {
			c.invalidateVolatileStatsCache("bulk_series_progress")
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{"message": "Bulk series progress update completed", "updated": len(bookIDs)})
		return
	}

	// 旧全局路径。
	for _, seriesID := range req.SeriesIDs {
		books, err := c.store.ListBooksBySeries(ctx, seriesID)
		if err != nil {
			slog.Error("Failed to load books for bulk series progress update", "series_id", seriesID, "error", err)
			continue
		}
		if len(books) == 0 {
			continue
		}
		if err := c.applySeriesBooksReadStateTx(ctx, seriesID, books, req.IsRead); err != nil {
			slog.Error("Failed to bulk update series progress", "series_id", seriesID, "error", err)
			continue
		}
		updated += len(books)
	}
	if updated > 0 {
		c.invalidateVolatileStatsCache("bulk_series_progress")
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{"message": "Bulk series progress update completed", "updated": updated})
}

// applySeriesBooksReadStateTx 在单个事务内更新一个系列下若干书的阅读状态，并在写完后只刷新一次
// series_stats。用 tx 绑定的原始 q.UpdateBookProgress（绕开 SqlStore 包装器的逐书隐式全量刷新），
// 把整系列标记已读/未读从「每本书 3 次 autocommit + 一次全量聚合」收敛为「一个事务 + 一次刷新」。
func (c *Controller) applySeriesBooksReadStateTx(ctx context.Context, seriesID int64, books []database.Book, isRead bool) error {
	return c.store.ExecTx(ctx, func(q *database.Queries) error {
		for _, book := range books {
			if err := applyBookReadStateTx(ctx, q, book, isRead); err != nil {
				return err
			}
		}
		return q.RefreshSeriesStats(ctx, seriesID)
	})
}

// applyBookReadStateTx 在事务内更新单本书的阅读状态（已读=最大页码并记阅读活动，未读=清空进度），
// 使用事务绑定的原始 q 方法，不做逐书统计刷新。
func applyBookReadStateTx(ctx context.Context, q *database.Queries, book database.Book, isRead bool) error {
	page := int64(1)
	validPage := false
	readAt := sql.NullTime{Valid: false}

	if isRead {
		if book.PageCount > 0 {
			page = book.PageCount
		} else {
			page = 99999
		}
		validPage = true
		readAt = sql.NullTime{Time: time.Now(), Valid: true}
	}

	if err := q.UpdateBookProgress(ctx, database.UpdateBookProgressParams{
		LastReadPage: sql.NullInt64{Int64: page, Valid: validPage},
		LastReadAt:   readAt,
		ID:           book.ID,
	}); err != nil {
		return err
	}

	if isRead && validPage {
		if err := q.LogReadingActivity(ctx, database.LogReadingActivityParams{BookID: book.ID, PagesRead: page, Date: database.ActivityDayKey(time.Now())}); err != nil {
			slog.Error("Failed to log reading activity", "book_id", book.ID, "error", err)
		}
	}
	return nil
}

func (c *Controller) getPagesByBook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bookID, err := parseID(r, "bookId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid Book ID")
		return
	}

	book, err := c.store.GetBook(ctx, bookID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "Book not found")
		return
	}

	pagesInfo, err := c.listBookArchivePages(ctx, book)
	if err != nil {
		writeStorageFailure(w, c.diagnoseStorageFailure(ctx, storageTargetFromBook(book), err), "list_pages")
		return
	}

	type PageResponse struct {
		Number int64  `json:"number"`
		URL    string `json:"url"`
	}

	var pages []PageResponse
	for i := range pagesInfo {
		pages = append(pages, PageResponse{
			Number: int64(i + 1),
			URL:    fmt.Sprintf("/api/books/page/%d/%d", bookID, i+1),
		})
	}

	jsonResponse(w, http.StatusOK, pages)
}

// ShutdownNotify 让 Controller 在 HTTP 停机开始时先切断 SSE 长连接。
//
// 供 main 注册到 http.Server.RegisterOnShutdown：Shutdown 会在开始时同步调用它，
// 之后才去排空在途请求。没有这一步，任何开着页面的浏览器标签都会让 Shutdown
// 一直等到 20 秒超时——SSE 是长连接，「在途请求排空」对它永远不会自然完成。
func (c *Controller) ShutdownNotify() {
	if c == nil {
		return
	}
	c.sse.closeClients()
}
