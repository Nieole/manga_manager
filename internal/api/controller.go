// 业务说明：本文件是业务实现，属于后端 HTTP API 层，负责把前端请求转换为数据库、扫描器、图片处理和元数据服务调用。
// 它承载资料库浏览、阅读器取页、系列维护、任务进度、系统设置和静态资源缓存等对外业务契约。
// 维护时应重点关注请求参数校验、错误语义、缓存头、并发任务状态和前后端字段兼容性。

package api

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/external"
	"manga-manager/internal/koreader"
	"manga-manager/internal/logger"
	"manga-manager/internal/metadata"
	"manga-manager/internal/parser"
	"manga-manager/internal/scanner"
	"manga-manager/internal/taskcontrol"

	"github.com/go-chi/chi/v5"
	lru "github.com/hashicorp/golang-lru/v2"
	"golang.org/x/sync/singleflight"
)

type Controller struct {
	store                database.Store
	imageCache           *lru.Cache[string, []byte]
	pageCache            *lru.Cache[string, []parser.PageMetadata]
	bookPageSourceCache  *lru.Cache[int64, cachedBookPageSource]
	progressWriteCache   *lru.Cache[int64, cachedProgressWrite]
	dashboardStatsMu     sync.RWMutex
	volatileStatsCache   *cachedVolatileStats
	dashboardStatsGen    int64
	structuralStatsMu    sync.RWMutex
	structuralStatsCache *cachedStructuralStats
	structuralStatsGen   int64
	scanner              *scanner.Scanner
	config               *config.Manager
	koreader             *koreader.Service
	external             *external.Manager
	configPath           string
	watcher              *scanner.FileWatcher

	// SSE Broker
	clients        map[chan string]bool
	newClients     chan chan string
	defunctClients chan chan string
	messages       chan string

	// AI Recommendations Cache
	recommendationsCache     map[string][]AIRecommendationResponse
	recommendationsCacheTime map[string]time.Time
	recommendationsMutex     sync.RWMutex
	// recommendationsGroup 合并同一 locale 的并发冷缓存/刷新请求，避免各自触发一次 LLM 推理。
	recommendationsGroup singleflight.Group
	// pageTranscodeGroup 合并同一 cacheKey 的并发页图转码：冷缓存时多客户端/预取请求同一页只解码+编码一次，
	// 其余等待者复用同一结果，避免重复 CPU 转码与重复归档读取。
	pageTranscodeGroup singleflight.Group

	taskMutex    sync.Mutex
	tasks        map[string]TaskStatus
	taskRuntimes map[string]*TaskRuntime
	taskSeq      int64
	// taskPersistPending 是待异步落盘的最新任务快照（按 key 合并），由 taskMutex 保护。进度更新只写内存+
	// 入此集合，专用 goroutine（startTaskPersister）节流批量写 SQLite，避免在临界区内同步写库阻塞任务
	// API 与系列详情页。taskPersistWake 在终态时唤醒该 goroutine 立即刷，缩短终态落库延迟（缓冲 1）。
	taskPersistPending map[string]TaskStatus
	taskPersistWake    chan struct{}
	// taskRelaunchers 是任务重试的注册表（taskType -> 重启函数），也是"可重试类型"的唯一事实来源，
	// 在 NewController 中构建，取代 retryTask 的中央 switch 与 isRetryableTaskType 两份硬编码清单。
	taskRelaunchers map[string]taskRelauncher

	rebuildThumbAggMu sync.Mutex
	rebuildThumbAgg   *rebuildThumbAggregator

	openPath        func(string) error
	providerFactory func(string) metadata.Provider

	lifecycleOnce sync.Once
	shutdownOnce  sync.Once
	lifecycleMu   sync.Mutex
	done          chan struct{}
	closed        bool
	backgroundWG  sync.WaitGroup

	// franchise 合集重建的合并式调度状态：把一串系列关联编辑合并成至多再跑一轮重建，
	// 避免每次增删改都启一个全图重建 goroutine 争抢 SQLite 写锁。
	franchiseRebuildMu      sync.Mutex
	franchiseRebuildRunning bool
	franchiseRebuildPending bool
}

type TaskStatus struct {
	Key       string `json:"key"`
	Type      string `json:"type"`
	Scope     string `json:"scope"`
	ScopeID   *int64 `json:"scope_id,omitempty"`
	ScopeName string `json:"scope_name,omitempty"`
	Status    string `json:"status"`
	Message   string `json:"message"`
	// MessageCode/MessageParams 承载可本地化的任务消息：后端只发稳定 i18n 键 + 占位参数，由前端按当前语言
	// 渲染文案，避免在 Go 中散落面向用户的中文字面量。设置了 MessageCode 时 Message 置空；未迁移 i18n 的
	// 旧调用点仍直接用 Message，前端按 message_code 优先、Message 兜底渲染，两者可共存以支持增量迁移。
	MessageCode    string            `json:"message_code,omitempty"`
	MessageParams  map[string]string `json:"message_params,omitempty"`
	Error          string            `json:"error,omitempty"`
	Current        int               `json:"current"`
	Total          int               `json:"total"`
	Percent        *float64          `json:"percent,omitempty"`
	RatePerMinute  float64           `json:"rate_per_minute,omitempty"`
	EtaSeconds     *int64            `json:"eta_seconds,omitempty"`
	CanCancel      bool              `json:"can_cancel"`
	CanPause       bool              `json:"can_pause"`
	CanResume      bool              `json:"can_resume"`
	Retryable      bool              `json:"retryable"`
	PausedAt       *time.Time        `json:"paused_at,omitempty"`
	PauseReason    string            `json:"pause_reason,omitempty"`
	Phase          string            `json:"phase,omitempty"`
	CurrentItem    string            `json:"current_item,omitempty"`
	EffectiveLimit *TaskLimits       `json:"effective_limit,omitempty"`
	Metrics        map[string]int64  `json:"metrics,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
	Params         map[string]string `json:"params,omitempty"`
	StartedAt      time.Time         `json:"started_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
	FinishedAt     *time.Time        `json:"finished_at,omitempty"`
	Sequence       int64             `json:"-"`
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

const maxRetainedTasks = 200

const (
	lowPriorityBookHashTaskKey   = "background_book_hash_backfill"
	lowPriorityBookHashBatchSize = 32
	lowPriorityBookHashBatchGap  = 100 * time.Millisecond
	dashboardStatsCacheTTL       = 30 * time.Second
)

type taskIOMetrics struct {
	StorageProfile string
	VolumeKey      string
	IOWaitMillis   int64
	PausedMillis   int64
	HashedFiles    int64
}

// rebuildThumbAggregator 跟踪缩略图重建任务的聚合进度。
// runGlobalScan 按库依次扫描，但 cover 队列是异步的，相邻两个库的 cover job 可能交错。
// 因此 baseline 仅记录已确定 final 的库的累计 metrics；perLibPending 记录每个仍可能更新的库
// 当前的实时 metrics 快照；汇总到任务时取 baseline + sum(perLibPending)。
type rebuildThumbAggregator struct {
	totalLibraries int
	doneLibraries  int
	baseline       map[string]int64
	perLibPending  map[int64]map[string]int64
	finalizedLibs  map[int64]struct{}
	// finalizedCoverSeen[libID] = 该库 fixate 后从 progress 事件中观察到的 generated_covers 最大值，
	// 避免 cover queue 异步阶段对已 fixate 库的二次累计。
	finalizedCoverSeen map[int64]int64
	currentLibID       int64
	currentLibName     string
	currentLibPath     string
}

// cachedStructuralStats 缓存结构性统计（含 books 全表扫描），仅在扫描/库结构变化时失效。
// 阅读进度变化不会失效它，从而避免高频阅读触发 70w 行全表 COUNT/SUM。
type cachedStructuralStats struct {
	stats     database.DashboardStructuralStats
	expiresAt time.Time
}

// cachedVolatileStats 缓存随阅读进度高频变化的统计（走索引，代价低）。
type cachedVolatileStats struct {
	stats     database.DashboardVolatileStats
	expiresAt time.Time
}

func NewController(store database.Store, scan *scanner.Scanner, cfg *config.Manager, cfgPath string) *Controller {
	cache, _ := lru.New[string, []byte](256)
	pageCache, _ := lru.New[string, []parser.PageMetadata](128)
	bookPageSourceCache, _ := lru.New[int64, cachedBookPageSource](512)
	progressWriteCache, _ := lru.New[int64, cachedProgressWrite](2048)
	c := &Controller{
		store:                    store,
		imageCache:               cache,
		pageCache:                pageCache,
		bookPageSourceCache:      bookPageSourceCache,
		progressWriteCache:       progressWriteCache,
		scanner:                  scan,
		config:                   cfg,
		koreader:                 koreader.NewService(store, cfg),
		external:                 external.NewManager(store, 30*time.Minute),
		configPath:               cfgPath,
		clients:                  make(map[chan string]bool),
		newClients:               make(chan chan string),
		defunctClients:           make(chan chan string),
		messages:                 make(chan string, 64),
		tasks:                    make(map[string]TaskStatus),
		taskRuntimes:             make(map[string]*TaskRuntime),
		taskPersistPending:       make(map[string]TaskStatus),
		taskPersistWake:          make(chan struct{}, 1),
		recommendationsCache:     make(map[string][]AIRecommendationResponse),
		recommendationsCacheTime: make(map[string]time.Time),
		openPath:                 openPathInDefaultFileManager,
	}
	if scan != nil {
		scan.SetBatchCallback(c.handleScannerBatchEvent)
		scan.SetScanMetricsCallback(c.handleScannerMetricsEvent)
		scan.SetScanProgressCallback(c.handleScannerProgressEvent)
	}

	// 构建任务重试注册表：必须在任何任务创建（startTaskWithOptions 会经 isRetryableTaskType 查表）之前完成。
	c.taskRelaunchers = c.buildTaskRelaunchers()

	c.recoverInterruptedTasks()

	c.runBackground(c.startBroker)
	c.runBackground(c.startDaemon)
	c.runBackground(c.startPageCacheJanitor)
	c.runBackground(c.startTaskPersister)

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
					_ = fw.WatchLibrary(lib.ID, lib.Path)
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
		c.taskMutex.Lock()
		cancels := make([]context.CancelFunc, 0, len(c.taskRuntimes))
		pauses := make([]*taskcontrol.PauseGate, 0, len(c.taskRuntimes))
		for _, runtime := range c.taskRuntimes {
			if runtime == nil {
				continue
			}
			if runtime.PauseGate != nil {
				pauses = append(pauses, runtime.PauseGate)
			}
			if runtime.Cancel != nil {
				cancels = append(cancels, runtime.Cancel)
			}
		}
		c.taskMutex.Unlock()
		for _, gate := range pauses {
			gate.Resume()
		}
		for _, cancel := range cancels {
			cancel()
		}
	})
	c.backgroundWG.Wait()
}

func (c *Controller) recoverInterruptedTasks() {
	if c.store == nil {
		return
	}
	count, err := c.store.MarkInterruptedTasks(context.Background(), database.MarkInterruptedTasksParams{
		Message: "任务因服务重启而中断，可重试",
		Error:   "任务因服务重启而中断，可重试",
	})
	if err != nil {
		slog.Warn("Failed to recover interrupted tasks", "error", err)
		return
	}
	if count > 0 {
		slog.Info("Recovered interrupted tasks", "count", count)
	}
}

func (c *Controller) currentConfig() config.Config {
	if c.config == nil {
		return config.Config{}
	}
	return c.config.Snapshot()
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

// requireAuth 是可选的管理 API 令牌鉴权中间件。默认（server.auth.enabled=false 或 token 为空）
// 为直通，行为与历史无鉴权版本完全一致；启用后，管理端点要求携带匹配 token 的令牌
// （X-API-Token 头、Authorization: Bearer，或 token 查询参数——后者便于 EventSource/<img> 等
// 无法自定义请求头的场景）。阅读协议 Mihon（/api/mihon/）不经此中间件，保持自身的协议开关与鉴权模型；
// OPDS/KOReader 挂载在根路由、本就不在 /api 组内。
func (c *Controller) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg := c.currentConfig()
		if !cfg.Server.Auth.Enabled || cfg.Server.Auth.Token == "" {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/mihon/") {
			next.ServeHTTP(w, r)
			return
		}
		if constantTimeTokenMatch(extractAPIToken(r), cfg.Server.Auth.Token) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="manga-manager"`)
		jsonError(w, http.StatusUnauthorized, apiText(requestLocale(r), "auth.token_required"))
	})
}

// extractAPIToken 依次从 X-API-Token 头、Authorization: Bearer、token 查询参数中取令牌。
func extractAPIToken(r *http.Request) string {
	if t := strings.TrimSpace(r.Header.Get("X-API-Token")); t != "" {
		return t
	}
	if auth := strings.TrimSpace(r.Header.Get("Authorization")); auth != "" {
		if after, ok := strings.CutPrefix(auth, "Bearer "); ok {
			return strings.TrimSpace(after)
		}
	}
	return strings.TrimSpace(r.URL.Query().Get("token"))
}

// constantTimeTokenMatch 用恒定时间比较避免令牌校验的时序侧信道。
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
	if err := config.AtomicWriteFile(c.configPath, data, 0644); err != nil {
		return err
	}
	c.config.Replace(cfg)
	if err := logger.SetLevel(cfg.Logging.Level); err != nil {
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

	return cmd.Start()
}

func (c *Controller) startBroker() {
	for {
		select {
		case <-c.lifecycleDone():
			return
		case s := <-c.newClients:
			c.clients[s] = true
		case s := <-c.defunctClients:
			if _, ok := c.clients[s]; ok {
				delete(c.clients, s)
				close(s)
			}
		case msg := <-c.messages:
			for s := range c.clients {
				select {
				case s <- msg:
				default:
					// 客户端 buffer 已满（默认 64 条），说明该消费者卡死或网络背压。
					// 主动断开它的 channel，sseHandler 会在下一轮 select 收到关闭信号并退出，
					// 浏览器端 EventSource 会按 retry 间隔自动重连。
					slog.Warn("SSE client backpressure, dropping client connection")
					delete(c.clients, s)
					close(s)
				}
			}
		}
	}
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
					err := c.scanner.ScanLibrary(context.Background(), id, path, false)
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

// PublishEvent 供 Scanner 外部等调用投递事件消息
func (c *Controller) PublishEvent(event string) {
	if c.messages == nil {
		return
	}
	select {
	case c.messages <- event:
	default:
		slog.Warn("SSE broker channel full, dropping event", "event_prefix", eventPrefix(event))
	}
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
		// 可选管理 API 鉴权（默认关闭时为直通）。必须在挂载任何子路由之前 Use，
		// 中间件内部会放行 /api/mihon/ 等阅读协议前缀。
		r.Use(c.requireAuth)
		c.setupMihonRoutes(r)

		r.Get("/events", c.sseHandler)
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
			r.Get("/{seriesId}/authors", c.getSeriesAuthors)
			r.Get("/{seriesId}/links", c.getSeriesLinks)
			r.Get("/{seriesId}/context", c.getSeriesContext)
			r.Get("/{seriesId}/continue", c.getSeriesContinueEndpoint)
			r.Get("/{seriesId}/comicinfo.zip", c.exportSeriesComicInfoArchive)
		})

		r.Route("/books", func(r chi.Router) {
			r.Post("/bulk-progress", c.bulkUpdateBookProgress)
			r.Post("/bulk-progress/sync", c.bulkSyncBookProgress)
			r.Post("/{bookId}/progress", c.updateBookProgress)
			r.Get("/{bookId}/comicinfo.xml", c.exportBookComicInfo)
			r.Get("/{bookId}/file", c.serveBookFile)
			r.Get("/{bookId}/bookmarks", c.listReadingBookmarks)
			r.Post("/{bookId}/bookmarks", c.upsertReadingBookmark)
			r.Delete("/{bookId}/bookmarks/{bookmarkId}", c.deleteReadingBookmark)
			r.Get("/{seriesId}", c.getBooksBySeries)
		})

		r.Route("/tags", func(r chi.Router) {
			r.Get("/all", c.getAllTags)
			r.Get("/search", c.searchTags)
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
		r.Post("/system/storage-io/pause", c.pauseStorageIO)
		r.Post("/system/storage-io/resume", c.resumeStorageIO)
		r.Post("/system/config", c.updateSystemConfig)
		r.Get("/system/logs", c.getSystemLogs)
		r.Get("/system/page-cache", c.getPageCacheStats)
		r.Delete("/system/page-cache", c.clearPageCache)
		r.Get("/system/tasks", c.listTasks)
		r.Delete("/system/tasks", c.clearTasks)
		r.Post("/system/tasks/{taskKey}/retry", c.retryTask)
		r.Post("/system/tasks/{taskKey}/pause", c.pauseTask)
		r.Post("/system/tasks/{taskKey}/resume", c.resumeTask)
		r.Post("/system/tasks/{taskKey}/cancel", c.cancelTask)
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
	cfg := c.currentConfig()
	thumbDir := filepath.Join(".", "data", "thumbnails")
	if cfg.Cache.Dir != "" {
		thumbDir = cfg.Cache.Dir
	}
	filename := chi.URLParam(r, "*")
	fullPath := filepath.Join(thumbDir, filename)
	w.Header().Set("Cache-Control", "public, max-age=31536000")
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

func jsonResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, status int, message string) {
	jsonResponse(w, status, map[string]string{"error": message})
}

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

	ctx := r.Context()
	// 按系列分组：每系列一个事务，内部逐书写入后只刷新一次 series_stats，
	// 避免走 store 包装器时每本书都隐式触发一次全系列统计重算（O(N^2) 聚合 + 逐条 autocommit）。
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

	ctx := r.Context()
	updated := 0
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

// applyBookReadStateTx 在事务内更新单本书的阅读状态，语义与旧的 updateBookReadState 一致
// （已读=最大页码并记阅读活动，未读=清空进度），但使用事务绑定的原始 q 方法、不做逐书统计刷新。
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
		if err := q.LogReadingActivity(ctx, database.LogReadingActivityParams{BookID: book.ID, PagesRead: page}); err != nil {
			slog.Error("Failed to log reading activity", "book_id", book.ID, "error", err)
		}
	}
	return nil
}

func (c *Controller) sseHandler(w http.ResponseWriter, r *http.Request) {
	// 设置 SSE 需要响应头
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// 允许跨域及凭证支持长链接
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, _ := w.(http.Flusher)

	// 提示客户端断线重连间隔（毫秒），并立刻刷一次响应头
	if _, err := w.Write([]byte("retry: 5000\n\n")); err != nil {
		return
	}
	if flusher != nil {
		flusher.Flush()
	}

	// 注册客户端通道
	messageChan := make(chan string, 64)
	c.newClients <- messageChan

	// 监听从客户端意外断开链接
	notify := r.Context().Done()
	go func() {
		<-notify
		c.defunctClients <- messageChan
	}()

	// 心跳：每 25 秒发送一次 SSE 注释行，避免反向代理（nginx/cloudflare 等）
	// 在长时间空闲时切断空连接。注释行以 `:` 开头，浏览器会忽略。
	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case msg, open := <-messageChan:
			if !open {
				return // 连接已从服务端侧切断（例如 broker 检测到客户端积压）
			}
			if _, err := w.Write([]byte("data: " + msg + "\n\n")); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		case <-heartbeat.C:
			if _, err := w.Write([]byte(": ping\n\n")); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		case <-notify:
			return
		}
	}
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
		jsonError(w, http.StatusInternalServerError, "Failed to read pages")
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
