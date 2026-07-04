// 业务说明：本文件是业务实现，属于后端 HTTP API 层，负责把前端请求转换为数据库、扫描器、图片处理和元数据服务调用。
// 它承载资料库浏览、阅读器取页、系列维护、任务进度、系统设置和静态资源缓存等对外业务契约。
// 维护时应重点关注请求参数校验、错误语义、缓存头、并发任务状态和前后端字段兼容性。

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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"manga-manager/internal/booksort"
	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/external"
	"manga-manager/internal/koreader"
	"manga-manager/internal/logger"
	"manga-manager/internal/metadata"
	"manga-manager/internal/parser"
	"manga-manager/internal/scanner"
	"manga-manager/internal/storageio"
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

	taskMutex    sync.Mutex
	tasks        map[string]TaskStatus
	taskRuntimes map[string]*TaskRuntime
	taskSeq      int64

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
	Key            string            `json:"key"`
	Type           string            `json:"type"`
	Scope          string            `json:"scope"`
	ScopeID        *int64            `json:"scope_id,omitempty"`
	ScopeName      string            `json:"scope_name,omitempty"`
	Status         string            `json:"status"`
	Message        string            `json:"message"`
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
		recommendationsCache:     make(map[string][]AIRecommendationResponse),
		recommendationsCacheTime: make(map[string]time.Time),
		openPath:                 openPathInDefaultFileManager,
	}
	if scan != nil {
		scan.SetBatchCallback(c.handleScannerBatchEvent)
		scan.SetScanMetricsCallback(c.handleScannerMetricsEvent)
		scan.SetScanProgressCallback(c.handleScannerProgressEvent)
	}

	c.recoverInterruptedTasks()

	c.runBackground(c.startBroker)
	c.runBackground(c.startDaemon)
	c.runBackground(c.startPageCacheJanitor)

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
		jsonError(w, http.StatusUnauthorized, "需要有效的访问令牌")
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

func inferTaskScope(taskType, key string) (string, *int64) {
	scope := "system"
	switch {
	case strings.Contains(taskType, "library"):
		scope = "library"
	case strings.Contains(taskType, "series"):
		scope = "series"
	}

	parts := strings.Split(key, "_")
	if len(parts) == 0 {
		return scope, nil
	}

	last := parts[len(parts)-1]
	id, err := strconv.ParseInt(last, 10, 64)
	if err != nil {
		return scope, nil
	}
	return scope, &id
}

func isRetryableTaskType(taskType string) bool {
	switch taskType {
	case "scan_library", "scan_series", "cleanup_library", "rebuild_index", "rebuild_thumbnails", "scrape", "ai_grouping", "rebuild_book_hashes", "rebuild_file_identities", "reconcile_koreader_progress":
		return true
	case "refresh_koreader_matching":
		return true
	default:
		return false
	}
}

func taskStatusFromRecord(record database.TaskRecord) TaskStatus {
	task := TaskStatus{
		Key:        record.Key,
		Type:       record.Type,
		Scope:      record.Scope,
		ScopeID:    record.ScopeID,
		ScopeName:  record.ScopeName,
		Status:     record.Status,
		Message:    record.Message,
		Error:      record.Error,
		Current:    record.Current,
		Total:      record.Total,
		CanCancel:  record.CanCancel,
		Retryable:  record.Retryable,
		Params:     record.Params,
		StartedAt:  record.StartedAt,
		UpdatedAt:  record.UpdatedAt,
		FinishedAt: record.FinishedAt,
		Sequence:   record.Sequence,
	}
	hydrateTaskStatusDerivedFields(&task)
	return task
}

func taskRecordFromStatus(task TaskStatus) database.TaskRecord {
	task.Params = taskParamsWithDerivedFields(task)
	return database.TaskRecord{
		Key:        task.Key,
		Type:       task.Type,
		Scope:      task.Scope,
		ScopeID:    task.ScopeID,
		ScopeName:  task.ScopeName,
		Status:     task.Status,
		Message:    task.Message,
		Error:      task.Error,
		Current:    task.Current,
		Total:      task.Total,
		CanCancel:  task.CanCancel,
		Retryable:  task.Retryable,
		Params:     task.Params,
		StartedAt:  task.StartedAt,
		UpdatedAt:  task.UpdatedAt,
		FinishedAt: task.FinishedAt,
		Sequence:   task.Sequence,
	}
}

func hydrateTaskStatusDerivedFields(task *TaskStatus) {
	if task == nil || task.Params == nil {
		enrichTaskProgress(task)
		return
	}
	task.Phase = firstNonEmptyTaskValue(task.Phase, task.Params["phase"])
	task.CurrentItem = firstNonEmptyTaskValue(task.CurrentItem, task.Params["current_item"])
	task.PauseReason = firstNonEmptyTaskValue(task.PauseReason, task.Params["pause_reason"])
	if raw := strings.TrimSpace(task.Params["can_pause"]); raw != "" {
		task.CanPause, _ = strconv.ParseBool(raw)
	}
	if raw := strings.TrimSpace(task.Params["can_resume"]); raw != "" {
		task.CanResume, _ = strconv.ParseBool(raw)
	}
	if pausedAt := strings.TrimSpace(task.Params["paused_at"]); task.PausedAt == nil && pausedAt != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, pausedAt); err == nil {
			task.PausedAt = &parsed
		}
	}

	for key, value := range task.Params {
		switch {
		case strings.HasPrefix(key, "metric."):
			if task.Metrics == nil {
				task.Metrics = make(map[string]int64)
			}
			if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
				task.Metrics[strings.TrimPrefix(key, "metric.")] = parsed
			}
		case strings.HasPrefix(key, "label."):
			if task.Labels == nil {
				task.Labels = make(map[string]string)
			}
			task.Labels[strings.TrimPrefix(key, "label.")] = value
		case strings.HasPrefix(key, "limit."):
			applyTaskLimitParam(task, strings.TrimPrefix(key, "limit."), value)
		}
	}
	enrichTaskProgress(task)
}

func applyTaskLimitParam(task *TaskStatus, key, value string) {
	if task.EffectiveLimit == nil {
		task.EffectiveLimit = &TaskLimits{}
	}
	parseInt := func() int {
		parsed, _ := strconv.Atoi(value)
		return parsed
	}
	parseBool := func() bool {
		parsed, _ := strconv.ParseBool(value)
		return parsed
	}
	switch key {
	case "scan_profile":
		task.EffectiveLimit.ScanProfile = value
	case "scanner_workers_configured":
		task.EffectiveLimit.ScannerWorkersConfigured = parseInt()
	case "scanner_workers_effective":
		task.EffectiveLimit.ScannerWorkersEffective = parseInt()
	case "storage_profile":
		task.EffectiveLimit.StorageProfile = value
	case "volume_key":
		task.EffectiveLimit.VolumeKey = value
	case "scan_concurrency":
		task.EffectiveLimit.ScanConcurrency = parseInt()
	case "archive_open_concurrency":
		task.EffectiveLimit.ArchiveOpenConcurrency = parseInt()
	case "cover_concurrency":
		task.EffectiveLimit.CoverConcurrency = parseInt()
	case "hash_concurrency":
		task.EffectiveLimit.HashConcurrency = parseInt()
	case "pause_background_when_reading":
		task.EffectiveLimit.PauseBackgroundWhenReading = parseBool()
	case "idle_only_heavy_tasks":
		task.EffectiveLimit.IdleOnlyHeavyTasks = parseBool()
	case "disable_same_disk_page_cache":
		task.EffectiveLimit.DisableSameDiskPageCache = parseBool()
	}
}

func taskParamsWithDerivedFields(task TaskStatus) map[string]string {
	params := make(map[string]string, len(task.Params)+24)
	for k, v := range task.Params {
		params[k] = v
	}
	put := func(key, value string) {
		if strings.TrimSpace(value) != "" {
			params[key] = value
		}
	}
	put("phase", task.Phase)
	put("current_item", task.CurrentItem)
	put("pause_reason", task.PauseReason)
	params["can_pause"] = strconv.FormatBool(task.CanPause)
	params["can_resume"] = strconv.FormatBool(task.CanResume)
	if task.PausedAt != nil {
		params["paused_at"] = task.PausedAt.Format(time.RFC3339Nano)
	}
	for key, value := range task.Metrics {
		params["metric."+key] = strconv.FormatInt(value, 10)
	}
	for key, value := range task.Labels {
		put("label."+key, value)
	}
	if task.EffectiveLimit != nil {
		limit := task.EffectiveLimit
		put("limit.scan_profile", limit.ScanProfile)
		params["limit.scanner_workers_configured"] = strconv.Itoa(limit.ScannerWorkersConfigured)
		params["limit.scanner_workers_effective"] = strconv.Itoa(limit.ScannerWorkersEffective)
		put("limit.storage_profile", limit.StorageProfile)
		put("limit.volume_key", limit.VolumeKey)
		params["limit.scan_concurrency"] = strconv.Itoa(limit.ScanConcurrency)
		params["limit.archive_open_concurrency"] = strconv.Itoa(limit.ArchiveOpenConcurrency)
		params["limit.cover_concurrency"] = strconv.Itoa(limit.CoverConcurrency)
		params["limit.hash_concurrency"] = strconv.Itoa(limit.HashConcurrency)
		params["limit.pause_background_when_reading"] = strconv.FormatBool(limit.PauseBackgroundWhenReading)
		params["limit.idle_only_heavy_tasks"] = strconv.FormatBool(limit.IdleOnlyHeavyTasks)
		params["limit.disable_same_disk_page_cache"] = strconv.FormatBool(limit.DisableSameDiskPageCache)
	}
	if len(params) == 0 {
		return nil
	}
	return params
}

func firstNonEmptyTaskValue(preferred, fallback string) string {
	if strings.TrimSpace(preferred) != "" {
		return preferred
	}
	return fallback
}

func enrichTaskProgress(task *TaskStatus) {
	if task == nil {
		return
	}
	if task.Total > 0 {
		percent := float64(task.Current) * 100 / float64(task.Total)
		if percent > 100 {
			percent = 100
		}
		task.Percent = &percent
	}
	elapsed := time.Since(task.StartedAt).Seconds()
	if task.Status != "running" && task.Status != "paused" && task.FinishedAt != nil {
		elapsed = task.FinishedAt.Sub(task.StartedAt).Seconds()
	}
	if elapsed > 0 && task.Current > 0 {
		task.RatePerMinute = float64(task.Current) * 60 / elapsed
		if task.Total > task.Current {
			eta := int64(float64(task.Total-task.Current) / task.RatePerMinute * 60)
			task.EtaSeconds = &eta
		}
	}
}

func (c *Controller) persistTaskStatus(task TaskStatus) {
	if c.store == nil {
		return
	}
	if err := c.store.UpsertTask(context.Background(), taskRecordFromStatus(task)); err != nil {
		slog.Warn("Failed to persist task status", "task_key", task.Key, "error", err)
	}
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

func (c *Controller) handleScannerBatchEvent(action string) {
	c.invalidateDashboardStatsCache("scanner_" + action)
	if action == "scan_completed" {
		c.warmDashboardStatsCacheAsync("scanner_" + action)
	}
	c.PublishEvent(action)
}

func (c *Controller) handleScannerMetricsEvent(report scanner.ScanMetricsReport) {
	taskKey := ""
	switch report.Scope {
	case "series":
		taskKey = fmt.Sprintf("scan_series_%d", report.ID)
	default:
		taskKey = fmt.Sprintf("scan_library_%d", report.ID)
	}
	c.mergeTaskParams(taskKey, map[string]string{
		"storage_profile":          report.StorageProfile,
		"volume_key":               report.VolumeKey,
		"archive_open_concurrency": strconv.Itoa(report.ArchiveOpenConcurrency),
		"cover_concurrency":        strconv.Itoa(report.CoverConcurrency),
		"discovered_archives":      strconv.FormatInt(report.DiscoveredArchives, 10),
		"skipped_archives":         strconv.FormatInt(report.SkippedArchives, 10),
		"processed_archives":       strconv.FormatInt(report.ProcessedArchives, 10),
		"opened_archives":          strconv.FormatInt(report.OpenedArchives, 10),
		"hashed_files":             strconv.FormatInt(report.HashedFiles, 10),
		"queued_covers":            strconv.FormatInt(report.QueuedCovers, 10),
		"generated_covers":         strconv.FormatInt(report.GeneratedCovers, 10),
		"failed_archives":          strconv.FormatInt(report.FailedArchives, 10),
		"io_wait_ms":               strconv.FormatInt(report.IOWaitMillis, 10),
		"paused_ms":                strconv.FormatInt(report.PausedMillis, 10),
		"thumbnail_write_ms":       strconv.FormatInt(report.ThumbnailWriteMillis, 10),
		"duration_ms":              strconv.FormatInt(report.DurationMillis, 10),
	})
	c.mergeTaskParams("rebuild_thumbnails", map[string]string{
		"storage_profile":          report.StorageProfile,
		"volume_key":               report.VolumeKey,
		"archive_open_concurrency": strconv.Itoa(report.ArchiveOpenConcurrency),
		"cover_concurrency":        strconv.Itoa(report.CoverConcurrency),
	})
	c.mergeRunningTaskMetricSums("rebuild_thumbnails", map[string]int64{
		"discovered_archives": report.DiscoveredArchives,
		"skipped_archives":    report.SkippedArchives,
		"processed_archives":  report.ProcessedArchives,
		"opened_archives":     report.OpenedArchives,
		"hashed_files":        report.HashedFiles,
		"queued_covers":       report.QueuedCovers,
		"generated_covers":    report.GeneratedCovers,
		"failed_archives":     report.FailedArchives,
		"io_wait_ms":          report.IOWaitMillis,
		"paused_ms":           report.PausedMillis,
		"thumbnail_write_ms":  report.ThumbnailWriteMillis,
		"duration_ms":         report.DurationMillis,
	}, nil)
	c.fixateRebuildThumbBaseline(report)
}

func (c *Controller) handleScannerProgressEvent(report scanner.ScanProgressReport) {
	taskKey := ""
	switch report.Scope {
	case "series":
		taskKey = fmt.Sprintf("scan_series_%d", report.ID)
	default:
		taskKey = fmt.Sprintf("scan_library_%d", report.ID)
	}
	metrics := make(map[string]int64, len(report.Metrics))
	for key, value := range report.Metrics {
		metrics[key] = value
	}
	current := int(report.Current)
	total := int(report.Total)
	message := "扫描中"
	if report.CurrentItem != "" {
		message = fmt.Sprintf("扫描: %s", filepath.Base(report.CurrentItem))
	}
	c.updateTaskDetails(taskKey, current, total, message, report.Phase, report.CurrentItem, metrics, nil)

	// 若正在执行缩略图重建，按全局视角同步 rebuild_thumbnails 任务进度
	c.applyScannerProgressToRebuildThumbnails(report)
}

func (c *Controller) applyScannerProgressToRebuildThumbnails(report scanner.ScanProgressReport) {
	if report.Scope != "library" {
		return
	}
	c.rebuildThumbAggMu.Lock()
	agg := c.rebuildThumbAgg
	if agg == nil {
		c.rebuildThumbAggMu.Unlock()
		return
	}
	if agg.perLibPending == nil {
		agg.perLibPending = make(map[int64]map[string]int64)
	}
	if agg.finalizedCoverSeen == nil {
		agg.finalizedCoverSeen = make(map[int64]int64)
	}
	if _, finalized := agg.finalizedLibs[report.ID]; finalized {
		// 库 fixate 时已把当时的 generated_covers 计入 baseline。这里只把 progress 事件中
		// 新增的 generated_covers 增量补回 baseline，其它 metrics 不再变更。
		newSeen := report.Metrics["generated_covers"]
		if newSeen > agg.finalizedCoverSeen[report.ID] {
			agg.baseline["generated_covers"] += newSeen - agg.finalizedCoverSeen[report.ID]
			agg.finalizedCoverSeen[report.ID] = newSeen
		}
	} else {
		snapshot := make(map[string]int64, len(report.Metrics))
		for k, v := range report.Metrics {
			snapshot[k] = v
		}
		agg.perLibPending[report.ID] = snapshot
	}

	merged := make(map[string]int64, len(agg.baseline)+8)
	for k, v := range agg.baseline {
		merged[k] = v
	}
	for _, pending := range agg.perLibPending {
		for k, v := range pending {
			merged[k] += v
		}
	}
	currentLibName := agg.currentLibName
	currentLibPath := agg.currentLibPath
	doneLibs := agg.doneLibraries
	totalLibs := agg.totalLibraries
	c.rebuildThumbAggMu.Unlock()

	current, total := rebuildThumbProgressFromMetrics(merged)
	phase := report.Phase
	if phase == "" {
		phase = "reading_metadata"
	}
	currentItem := report.CurrentItem
	displayName := filepath.Base(report.CurrentItem)
	var message string
	switch {
	case phase == "queueing_covers" && displayName != "":
		message = fmt.Sprintf("生成缩略图: %s (已生成 %d)", displayName, merged["generated_covers"])
	case currentItem == "" && currentLibName != "":
		message = fmt.Sprintf("正在重建缩略图: %s (%d/%d 资源库)", currentLibName, doneLibs+1, totalLibs)
	case displayName != "" && currentLibName != "":
		message = fmt.Sprintf("[%s %d/%d] 重建: %s", currentLibName, doneLibs+1, totalLibs, displayName)
	case displayName != "":
		message = fmt.Sprintf("重建缩略图: %s", displayName)
	default:
		message = "正在重建缩略图"
	}
	if currentItem == "" {
		currentItem = currentLibPath
	}
	labels := map[string]string{
		"current_library": currentLibName,
	}
	c.updateTaskDetails("rebuild_thumbnails", current, total, message, phase, currentItem, merged, labels)
}

func (c *Controller) initRebuildThumbAggregator(totalLibraries int) {
	c.rebuildThumbAggMu.Lock()
	defer c.rebuildThumbAggMu.Unlock()
	c.rebuildThumbAgg = &rebuildThumbAggregator{
		totalLibraries:     totalLibraries,
		baseline:           make(map[string]int64),
		perLibPending:      make(map[int64]map[string]int64),
		finalizedLibs:      make(map[int64]struct{}),
		finalizedCoverSeen: make(map[int64]int64),
	}
}

func (c *Controller) releaseRebuildThumbAggregator() {
	c.rebuildThumbAggMu.Lock()
	c.rebuildThumbAgg = nil
	c.rebuildThumbAggMu.Unlock()
}

// trackRebuildThumbLibraryProgress 在 runGlobalScan 的库切换边界更新聚合器，
// current 是已完成库数（progress 回调 i 表示"开始第 i+1 个"，i+1 表示"完成第 i+1 个"）。
func (c *Controller) trackRebuildThumbLibraryProgress(current, total int, lib database.Library) {
	c.rebuildThumbAggMu.Lock()
	defer c.rebuildThumbAggMu.Unlock()
	if c.rebuildThumbAgg == nil {
		c.rebuildThumbAgg = &rebuildThumbAggregator{
			baseline:           make(map[string]int64),
			perLibPending:      make(map[int64]map[string]int64),
			finalizedLibs:      make(map[int64]struct{}),
			finalizedCoverSeen: make(map[int64]int64),
		}
	}
	c.rebuildThumbAgg.totalLibraries = total
	c.rebuildThumbAgg.doneLibraries = current
	c.rebuildThumbAgg.currentLibID = lib.ID
	c.rebuildThumbAgg.currentLibName = lib.Name
	c.rebuildThumbAgg.currentLibPath = lib.Path
}

// fixateRebuildThumbBaseline 在某个库扫描"主流程"完成时被调用（cover queue 仍可能在异步中），
// 此时把该库的最终 metrics 加到 baseline，并删除 perLibPending 中对应条目。
// 注意：cover queue 异步阶段的 generatedCovers 增量会通过 progress 事件继续更新该库的 perLibPending，
// 但因为我们已把 baseline 中累计了最终值，再次出现的 perLibPending 反映的是同一份 metrics 的最新值，
// 这意味着会双计。为避免双计，fixate 后忽略后续 perLibPending（直到 release 或下次扫描）。
func (c *Controller) fixateRebuildThumbBaseline(report scanner.ScanMetricsReport) {
	if report.Scope != "library" {
		return
	}
	c.rebuildThumbAggMu.Lock()
	agg := c.rebuildThumbAgg
	if agg == nil {
		c.rebuildThumbAggMu.Unlock()
		return
	}
	if agg.baseline == nil {
		agg.baseline = make(map[string]int64)
	}
	if agg.finalizedLibs == nil {
		agg.finalizedLibs = make(map[int64]struct{})
	}
	if agg.finalizedCoverSeen == nil {
		agg.finalizedCoverSeen = make(map[int64]int64)
	}
	delete(agg.perLibPending, report.ID)
	agg.finalizedLibs[report.ID] = struct{}{}
	agg.finalizedCoverSeen[report.ID] = report.GeneratedCovers
	agg.baseline["discovered_archives"] += report.DiscoveredArchives
	agg.baseline["skipped_archives"] += report.SkippedArchives
	agg.baseline["processed_archives"] += report.ProcessedArchives
	agg.baseline["opened_archives"] += report.OpenedArchives
	agg.baseline["hashed_files"] += report.HashedFiles
	agg.baseline["queued_covers"] += report.QueuedCovers
	agg.baseline["generated_covers"] += report.GeneratedCovers
	agg.baseline["failed_archives"] += report.FailedArchives
	agg.baseline["io_wait_ms"] += report.IOWaitMillis
	agg.baseline["paused_ms"] += report.PausedMillis
	agg.baseline["thumbnail_write_ms"] += report.ThumbnailWriteMillis
	merged := make(map[string]int64, len(agg.baseline)+len(agg.perLibPending))
	for k, v := range agg.baseline {
		merged[k] = v
	}
	for _, pending := range agg.perLibPending {
		for k, v := range pending {
			merged[k] += v
		}
	}
	totalLibs := agg.totalLibraries
	doneLibs := agg.doneLibraries
	c.rebuildThumbAggMu.Unlock()

	current, total := rebuildThumbProgressFromMetrics(merged)
	message := "正在重建缩略图"
	if totalLibs > 0 {
		message = fmt.Sprintf("已完成 %d/%d 资源库", doneLibs, totalLibs)
	}
	c.updateTaskDetails("rebuild_thumbnails", current, total, message, "queueing_covers", "", merged, nil)
}

// refreshRebuildThumbTaskFromAggregator 用聚合器中已记录的 metrics 立即刷新一次任务，
// 用于在 runGlobalScan 库切换边界（无 progress 事件携带 metrics 的时机）保持任务消息和当前库标签同步。
func (c *Controller) refreshRebuildThumbTaskFromAggregator(lib database.Library) {
	c.rebuildThumbAggMu.Lock()
	agg := c.rebuildThumbAgg
	if agg == nil {
		c.rebuildThumbAggMu.Unlock()
		return
	}
	merged := make(map[string]int64, len(agg.baseline)+8)
	for k, v := range agg.baseline {
		merged[k] = v
	}
	for _, pending := range agg.perLibPending {
		for k, v := range pending {
			merged[k] += v
		}
	}
	doneLibs := agg.doneLibraries
	totalLibs := agg.totalLibraries
	c.rebuildThumbAggMu.Unlock()

	current, total := rebuildThumbProgressFromMetrics(merged)
	var message string
	if totalLibs > 0 {
		message = fmt.Sprintf("正在重建缩略图: %s (%d/%d 资源库)", lib.Name, doneLibs+1, totalLibs)
	} else {
		message = fmt.Sprintf("正在重建缩略图: %s", lib.Name)
	}
	labels := map[string]string{"current_library": lib.Name}
	c.updateTaskDetails("rebuild_thumbnails", current, total, message, "reading_metadata", lib.Path, merged, labels)
}

// refreshRebuildThumbTaskMessage 在阶段切换（如等待封面队列收尾）时刷新任务消息和阶段，
// 但保留聚合器累计的 current/total（避免被旧的占位 total 重置成 100%）。
func (c *Controller) refreshRebuildThumbTaskMessage(message, phase string) {
	c.rebuildThumbAggMu.Lock()
	agg := c.rebuildThumbAgg
	if agg == nil {
		c.rebuildThumbAggMu.Unlock()
		return
	}
	merged := make(map[string]int64, len(agg.baseline)+8)
	for k, v := range agg.baseline {
		merged[k] = v
	}
	for _, pending := range agg.perLibPending {
		for k, v := range pending {
			merged[k] += v
		}
	}
	c.rebuildThumbAggMu.Unlock()

	current, total := rebuildThumbProgressFromMetrics(merged)
	c.updateTaskDetails("rebuild_thumbnails", current, total, message, phase, "", merged, nil)
}

// rebuildThumbProgressFromMetrics 把"重建缩略图"任务的进度展开成两阶段：
// 归档处理 (processed+skipped/discovered) 和封面生成 (generated/queued)，分别贡献分子分母。
// 这样归档全部入队时进度只走到 ~50%，cover queue 异步生成时进度继续推进，避免视觉上"过早 100%"。
func rebuildThumbProgressFromMetrics(merged map[string]int64) (int, int) {
	processedArchives := merged["processed_archives"] + merged["skipped_archives"]
	discoveredArchives := merged["discovered_archives"]
	if discoveredArchives < processedArchives {
		discoveredArchives = processedArchives
	}
	generatedCovers := merged["generated_covers"]
	queuedCovers := merged["queued_covers"]
	if queuedCovers < generatedCovers {
		queuedCovers = generatedCovers
	}
	current := int(processedArchives + generatedCovers)
	total := int(discoveredArchives + queuedCovers)
	if total < current {
		total = current
	}
	if total <= 0 {
		return current, -1
	}
	return current, total
}

func (c *Controller) startTask(key, taskType, message string, total int) bool {
	return c.startTaskWithOptions(key, taskType, message, total, false, false)
}

func (c *Controller) startCancelableTask(key, taskType, message string, total int) bool {
	return c.startTaskWithOptions(key, taskType, message, total, true, false)
}

func (c *Controller) startPausableCancelableTask(key, taskType, message string, total int) bool {
	return c.startTaskWithOptions(key, taskType, message, total, true, true)
}

func (c *Controller) startTaskWithOptions(key, taskType, message string, total int, canCancel bool, canPause bool) bool {
	c.taskMutex.Lock()
	defer c.taskMutex.Unlock()

	if c.tasks == nil {
		c.tasks = make(map[string]TaskStatus)
	}

	if existing, ok := c.tasks[key]; ok && taskIsActive(existing.Status) {
		return false
	}

	now := time.Now()
	c.taskSeq++
	scope, scopeID := inferTaskScope(taskType, key)
	task := TaskStatus{
		Key:       key,
		Type:      taskType,
		Scope:     scope,
		ScopeID:   scopeID,
		Status:    "running",
		Message:   message,
		Current:   0,
		Total:     total,
		CanCancel: canCancel,
		CanPause:  canPause,
		Retryable: isRetryableTaskType(taskType),
		StartedAt: now,
		UpdatedAt: now,
		Sequence:  c.taskSeq,
	}
	c.tasks[key] = task
	c.pruneTasksLocked()
	c.persistTaskStatus(task)
	c.publishTaskStatusLocked(task)
	return true
}

func taskIsActive(status string) bool {
	return status == "running" || status == "paused" || status == "cancelling"
}

func (c *Controller) newTaskContext(key string) (context.Context, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	gate := taskcontrol.NewPauseGate()
	taskCtx := taskcontrol.WithPauseGate(ctx, gate)

	c.taskMutex.Lock()
	if c.taskRuntimes == nil {
		c.taskRuntimes = make(map[string]*TaskRuntime)
	}
	c.taskRuntimes[key] = &TaskRuntime{
		Context:   taskCtx,
		Cancel:    cancel,
		PauseGate: gate,
		StartedAt: time.Now(),
	}
	c.taskMutex.Unlock()

	cleanup := func() {
		c.taskMutex.Lock()
		delete(c.taskRuntimes, key)
		c.taskMutex.Unlock()
	}

	return taskCtx, cleanup
}

func (c *Controller) updateTask(key string, current, total int, message string) {
	c.taskMutex.Lock()
	defer c.taskMutex.Unlock()

	task, ok := c.tasks[key]
	if !ok {
		return
	}
	task.Current = current
	if total >= 0 {
		task.Total = total
	}
	if message != "" {
		task.Message = message
	}
	task.UpdatedAt = time.Now()
	c.taskSeq++
	task.Sequence = c.taskSeq
	enrichTaskProgress(&task)
	c.tasks[key] = task
	c.persistTaskStatus(task)
	c.publishTaskStatusLocked(task)
}

func (c *Controller) updateTaskDetails(key string, current, total int, message, phase, currentItem string, metrics map[string]int64, labels map[string]string) {
	c.taskMutex.Lock()
	defer c.taskMutex.Unlock()

	task, ok := c.tasks[key]
	if !ok {
		return
	}
	if !taskIsActive(task.Status) {
		return
	}
	task.Current = current
	if total >= 0 {
		task.Total = total
	}
	if message != "" {
		task.Message = message
	}
	if phase != "" {
		task.Phase = phase
	}
	if currentItem != "" {
		task.CurrentItem = currentItem
	}
	if len(metrics) > 0 {
		if task.Metrics == nil {
			task.Metrics = make(map[string]int64, len(metrics))
		}
		for k, v := range metrics {
			task.Metrics[k] = v
		}
	}
	if len(labels) > 0 {
		if task.Labels == nil {
			task.Labels = make(map[string]string, len(labels))
		}
		for k, v := range labels {
			task.Labels[k] = v
		}
	}
	task.UpdatedAt = time.Now()
	c.taskSeq++
	task.Sequence = c.taskSeq
	enrichTaskProgress(&task)
	c.tasks[key] = task
	c.persistTaskStatus(task)
	c.publishTaskStatusLocked(task)
}

func (c *Controller) setTaskMetadata(key string, params map[string]string, scopeName string) {
	c.taskMutex.Lock()
	defer c.taskMutex.Unlock()

	task, ok := c.tasks[key]
	if !ok {
		return
	}
	task.Params = params
	if strings.TrimSpace(scopeName) != "" {
		task.ScopeName = scopeName
	}
	c.taskSeq++
	task.Sequence = c.taskSeq
	hydrateTaskStatusDerivedFields(&task)
	c.tasks[key] = task
	c.persistTaskStatus(task)
	c.publishTaskStatusLocked(task)
}

func (c *Controller) mergeTaskParams(key string, params map[string]string) {
	if len(params) == 0 {
		return
	}
	c.taskMutex.Lock()
	defer c.taskMutex.Unlock()

	task, ok := c.tasks[key]
	if !ok {
		return
	}
	if task.Params == nil {
		task.Params = make(map[string]string, len(params))
	}
	for k, v := range params {
		task.Params[k] = v
	}
	task.UpdatedAt = time.Now()
	c.taskSeq++
	task.Sequence = c.taskSeq
	hydrateTaskStatusDerivedFields(&task)
	c.tasks[key] = task
	c.persistTaskStatus(task)
	c.publishTaskStatusLocked(task)
}

func (c *Controller) mergeRunningTaskMetricSums(key string, increments map[string]int64, params map[string]string) {
	c.taskMutex.Lock()
	defer c.taskMutex.Unlock()

	task, ok := c.tasks[key]
	if !ok || task.Status != "running" {
		return
	}
	if task.Params == nil {
		task.Params = make(map[string]string, len(params)+len(increments))
	}
	for k, v := range params {
		if strings.TrimSpace(v) != "" {
			task.Params[k] = v
		}
	}
	for k, inc := range increments {
		if inc == 0 {
			continue
		}
		current, _ := strconv.ParseInt(task.Params[k], 10, 64)
		task.Params[k] = strconv.FormatInt(current+inc, 10)
		if task.Metrics == nil {
			task.Metrics = make(map[string]int64)
		}
		task.Metrics[k] += inc
	}
	task.UpdatedAt = time.Now()
	c.taskSeq++
	task.Sequence = c.taskSeq
	hydrateTaskStatusDerivedFields(&task)
	c.tasks[key] = task
	c.persistTaskStatus(task)
	c.publishTaskStatusLocked(task)
}

func (c *Controller) acquireTaskStorageToken(ctx context.Context, libraryPath string, kind storageio.WorkKind) (config.ResolvedStoragePolicy, func(), time.Duration, time.Duration, error) {
	policy := config.ResolveStoragePolicy(c.currentConfig(), libraryPath)
	limit := minPositiveStorageLimit(policy.IOPolicy.HashConcurrency, policy.IOPolicy.ArchiveOpenConcurrency)
	if kind == storageio.WorkKindCoverBuild {
		limit = minPositiveStorageLimit(policy.IOPolicy.CoverConcurrency, policy.IOPolicy.ArchiveOpenConcurrency)
	}
	if limit <= 0 || strings.TrimSpace(policy.VolumeKey) == "" {
		return policy, func() {}, 0, 0, nil
	}
	lease, err := storageio.Default.Acquire(ctx, storageio.Request{
		VolumeKey:        policy.VolumeKey,
		Limit:            limit,
		Kind:             kind,
		PauseWhenReading: policy.IOPolicy.PauseBackgroundWhenReading,
		IdleOnly:         policy.IOPolicy.IdleOnlyHeavyTasks,
	})
	if err != nil {
		return policy, nil, lease.Wait, lease.PausedWait, err
	}
	return policy, lease.Release, lease.Wait, lease.PausedWait, nil
}

func minPositiveStorageLimit(values ...int) int {
	limit := 0
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if limit == 0 || value < limit {
			limit = value
		}
	}
	return limit
}

func taskIOMetricsParams(metrics taskIOMetrics) map[string]string {
	params := map[string]string{
		"io_wait_ms":   strconv.FormatInt(metrics.IOWaitMillis, 10),
		"paused_ms":    strconv.FormatInt(metrics.PausedMillis, 10),
		"hashed_files": strconv.FormatInt(metrics.HashedFiles, 10),
	}
	if metrics.StorageProfile != "" {
		params["storage_profile"] = metrics.StorageProfile
	}
	if metrics.VolumeKey != "" {
		params["volume_key"] = metrics.VolumeKey
	}
	return params
}

func (c *Controller) taskLimitsForPath(path string, force bool) TaskLimits {
	cfg := c.currentConfig()
	profile := scanner.NormalizeScanProfile(cfg.Scanner.ScanProfile)
	if profile == scanner.ScanProfileRepair {
		force = true
	}
	_ = force
	policy := config.ResolveStoragePolicy(cfg, path)
	workers := cfg.Scanner.Workers
	if workers <= 0 {
		workers = runtime.NumCPU() * 2
	}
	limit := policy.IOPolicy.ScanConcurrency
	if profile != scanner.ScanProfileFast {
		limit = minPositiveStorageLimit(limit, policy.IOPolicy.ArchiveOpenConcurrency)
	}
	if profile == scanner.ScanProfileIdentity || profile == scanner.ScanProfileRepair {
		limit = minPositiveStorageLimit(limit, policy.IOPolicy.HashConcurrency)
	}
	effectiveWorkers := workers
	if limit > 0 && effectiveWorkers > limit {
		effectiveWorkers = limit
	}
	if effectiveWorkers < 1 {
		effectiveWorkers = 1
	}
	return TaskLimits{
		ScanProfile:                string(profile),
		ScannerWorkersConfigured:   cfg.Scanner.Workers,
		ScannerWorkersEffective:    effectiveWorkers,
		StorageProfile:             policy.StorageProfile,
		VolumeKey:                  policy.VolumeKey,
		ScanConcurrency:            policy.IOPolicy.ScanConcurrency,
		ArchiveOpenConcurrency:     policy.IOPolicy.ArchiveOpenConcurrency,
		CoverConcurrency:           policy.IOPolicy.CoverConcurrency,
		HashConcurrency:            policy.IOPolicy.HashConcurrency,
		PauseBackgroundWhenReading: policy.IOPolicy.PauseBackgroundWhenReading,
		IdleOnlyHeavyTasks:         policy.IOPolicy.IdleOnlyHeavyTasks,
		DisableSameDiskPageCache:   policy.IOPolicy.DisableSameDiskPageCache,
	}
}

func (c *Controller) setTaskEffectiveLimit(key string, limit TaskLimits) {
	c.taskMutex.Lock()
	defer c.taskMutex.Unlock()

	task, ok := c.tasks[key]
	if !ok {
		return
	}
	task.EffectiveLimit = &limit
	task.UpdatedAt = time.Now()
	c.taskSeq++
	task.Sequence = c.taskSeq
	hydrateTaskStatusDerivedFields(&task)
	c.tasks[key] = task
	c.persistTaskStatus(task)
	c.publishTaskStatusLocked(task)
}

func (c *Controller) finishTask(key, message string) {
	c.completeTask(key, "completed", message)
}

func (c *Controller) failTask(key, message string) {
	c.failTaskWithError(key, message, message)
}

func (c *Controller) completeTask(key, status, message string) {
	c.taskMutex.Lock()
	defer c.taskMutex.Unlock()

	task, ok := c.tasks[key]
	if !ok {
		return
	}
	now := time.Now()
	task.Status = status
	task.Message = message
	if status != "failed" {
		task.Error = ""
	}
	task.CanCancel = false
	task.CanPause = false
	task.CanResume = false
	task.PausedAt = nil
	task.PauseReason = ""
	if task.Total > 0 {
		task.Current = task.Total
	}
	task.UpdatedAt = now
	task.FinishedAt = &now
	c.taskSeq++
	task.Sequence = c.taskSeq
	c.tasks[key] = task
	delete(c.taskRuntimes, key)
	c.persistTaskStatus(task)
	c.publishTaskStatusLocked(task)
}

func (c *Controller) failTaskWithError(key, message, taskError string) {
	c.taskMutex.Lock()
	defer c.taskMutex.Unlock()

	task, ok := c.tasks[key]
	if !ok {
		return
	}
	now := time.Now()
	task.Status = "failed"
	task.Message = message
	task.Error = taskError
	task.CanCancel = false
	task.CanPause = false
	task.CanResume = false
	task.PausedAt = nil
	task.UpdatedAt = now
	task.FinishedAt = &now
	c.taskSeq++
	task.Sequence = c.taskSeq
	c.tasks[key] = task
	delete(c.taskRuntimes, key)
	c.persistTaskStatus(task)
	c.publishTaskStatusLocked(task)
}

func (c *Controller) publishTaskStatusLocked(task TaskStatus) {
	if c.messages == nil {
		return
	}
	payload, err := json.Marshal(task)
	if err != nil {
		slog.Warn("Failed to marshal task status", "task_key", task.Key, "error", err)
		return
	}
	select {
	case c.messages <- "task_progress:" + string(payload):
	default:
		slog.Warn("SSE message channel full, dropping task progress", "task_key", task.Key)
	}
}

func (c *Controller) pruneTasksLocked() {
	if len(c.tasks) <= maxRetainedTasks {
		return
	}

	items := make([]TaskStatus, 0, len(c.tasks))
	for _, task := range c.tasks {
		items = append(items, task)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Sequence == items[j].Sequence {
			if items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
				if items[i].StartedAt.Equal(items[j].StartedAt) {
					return items[i].Key > items[j].Key
				}
				return items[i].StartedAt.After(items[j].StartedAt)
			}
			return items[i].UpdatedAt.After(items[j].UpdatedAt)
		}
		return items[i].Sequence > items[j].Sequence
	})

	next := make(map[string]TaskStatus, len(items))
	for _, task := range items {
		if len(next) >= maxRetainedTasks {
			break
		}
		next[task.Key] = task
	}
	c.tasks = next
}

func (c *Controller) listTasks(w http.ResponseWriter, r *http.Request) {
	statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))
	scopeFilter := strings.TrimSpace(r.URL.Query().Get("scope"))
	typeFilter := strings.TrimSpace(r.URL.Query().Get("type"))
	scopeIDFilter := strings.TrimSpace(r.URL.Query().Get("scope_id"))
	queryFilter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	limit := 0
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	var scopeID *int64
	if scopeIDFilter != "" {
		if parsed, err := strconv.ParseInt(scopeIDFilter, 10, 64); err == nil {
			scopeID = &parsed
		}
	}

	items, err := c.listTaskStatuses(r.Context(), database.TaskFilters{
		Status:  statusFilter,
		Scope:   scopeFilter,
		Type:    typeFilter,
		ScopeID: scopeID,
		Query:   queryFilter,
		Limit:   limit,
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to list tasks")
		return
	}
	jsonResponse(w, http.StatusOK, items)
}

func (c *Controller) listTaskStatuses(ctx context.Context, filters database.TaskFilters) ([]TaskStatus, error) {
	records, err := c.store.ListTasks(ctx, filters)
	if err != nil {
		return nil, err
	}
	items := make([]TaskStatus, 0, len(records))
	seen := make(map[string]bool, len(records))
	for _, record := range records {
		task := taskStatusFromRecord(record)
		items = append(items, task)
		seen[task.Key] = true
	}

	c.taskMutex.Lock()
	if c.tasks == nil {
		c.tasks = make(map[string]TaskStatus)
	}
	for _, task := range c.tasks {
		if seen[task.Key] {
			continue
		}
		if filters.Status != "" && task.Status != filters.Status {
			continue
		}
		if filters.Scope != "" && task.Scope != filters.Scope {
			continue
		}
		if filters.Type != "" && task.Type != filters.Type {
			continue
		}
		if filters.ScopeID != nil && (task.ScopeID == nil || *task.ScopeID != *filters.ScopeID) {
			continue
		}
		if filters.Query != "" {
			haystack := strings.ToLower(task.Key + " " + task.Message + " " + task.Error)
			if !strings.Contains(haystack, strings.ToLower(filters.Query)) {
				continue
			}
		}
		items = append(items, task)
	}
	c.taskMutex.Unlock()

	sort.Slice(items, func(i, j int) bool {
		if items[i].Sequence == items[j].Sequence {
			if items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
				if items[i].StartedAt.Equal(items[j].StartedAt) {
					return items[i].Key > items[j].Key
				}
				return items[i].StartedAt.After(items[j].StartedAt)
			}
			return items[i].UpdatedAt.After(items[j].UpdatedAt)
		}
		return items[i].Sequence > items[j].Sequence
	})
	if filters.Limit > 0 && len(items) > filters.Limit {
		items = items[:filters.Limit]
	}
	return items, nil
}

func (c *Controller) clearTasks(w http.ResponseWriter, r *http.Request) {
	statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))
	scopeFilter := strings.TrimSpace(r.URL.Query().Get("scope"))
	typeFilter := strings.TrimSpace(r.URL.Query().Get("type"))
	scopeIDFilter := strings.TrimSpace(r.URL.Query().Get("scope_id"))
	var scopeID *int64
	if scopeIDFilter != "" {
		if parsed, err := strconv.ParseInt(scopeIDFilter, 10, 64); err == nil {
			scopeID = &parsed
		}
	}
	removed, err := c.store.DeleteTasks(r.Context(), database.TaskFilters{
		Status:  statusFilter,
		Scope:   scopeFilter,
		Type:    typeFilter,
		ScopeID: scopeID,
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to clear tasks")
		return
	}

	c.taskMutex.Lock()
	if c.tasks == nil {
		c.tasks = make(map[string]TaskStatus)
	}
	for key, task := range c.tasks {
		if statusFilter != "" && task.Status != statusFilter {
			continue
		}
		if scopeFilter != "" && task.Scope != scopeFilter {
			continue
		}
		if typeFilter != "" && task.Type != typeFilter {
			continue
		}
		if scopeID != nil && (task.ScopeID == nil || *task.ScopeID != *scopeID) {
			continue
		}
		if task.Status == "running" {
			continue
		}
		delete(c.tasks, key)
	}
	c.taskMutex.Unlock()

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"removed": removed,
	})
}

func (c *Controller) retryTask(w http.ResponseWriter, r *http.Request) {
	taskKey := chi.URLParam(r, "taskKey")
	if taskKey == "" {
		jsonError(w, http.StatusBadRequest, "Missing task key")
		return
	}

	c.taskMutex.Lock()
	task, ok := c.tasks[taskKey]
	c.taskMutex.Unlock()
	if !ok {
		records, err := c.store.ListTasks(r.Context(), database.TaskFilters{Query: taskKey, Limit: 20})
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "Failed to load task")
			return
		}
		for _, record := range records {
			if record.Key == taskKey {
				task = taskStatusFromRecord(record)
				ok = true
				break
			}
		}
		if !ok {
			jsonError(w, http.StatusNotFound, "Task not found")
			return
		}
	}
	if task.Status == "running" {
		jsonError(w, http.StatusConflict, "Task is already running")
		return
	}
	if !task.Retryable {
		jsonError(w, http.StatusConflict, "Task is not retryable")
		return
	}

	var err error
	switch task.Type {
	case "scan_library":
		if task.ScopeID == nil {
			err = fmt.Errorf("missing library id")
			break
		}
		lib, getErr := c.store.GetLibrary(r.Context(), *task.ScopeID)
		if getErr != nil {
			err = getErr
			break
		}
		force := task.Params != nil && task.Params["force"] == "true"
		if !c.launchLibraryScanTask(lib, force) {
			err = fmt.Errorf("task already running")
		}
	case "scan_series":
		if task.ScopeID == nil {
			err = fmt.Errorf("missing series id")
			break
		}
		force := task.Params != nil && task.Params["force"] == "true"
		if !c.launchSeriesScanTask(*task.ScopeID, force) {
			err = fmt.Errorf("task already running")
		}
	case "cleanup_library":
		if task.ScopeID == nil {
			err = fmt.Errorf("missing library id")
			break
		}
		if !c.launchCleanupLibraryTask(*task.ScopeID) {
			err = fmt.Errorf("task already running")
		}
	case "rebuild_index":
		err = c.launchRebuildIndexTask()
	case "rebuild_thumbnails":
		err = c.launchRebuildThumbnailsTask()
	case "scrape":
		err = c.retryScrapeTask(task)
	case "ai_grouping":
		if task.ScopeID == nil {
			err = fmt.Errorf("missing library id")
			break
		}
		if !c.launchAIGroupingTask(*task.ScopeID, "zh-CN") {
			err = fmt.Errorf("task already running")
		}
	case "rebuild_book_hashes":
		err = c.launchRebuildBookHashesTask()
	case "rebuild_file_identities":
		err = c.launchRebuildFileIdentitiesTask()
	case "reconcile_koreader_progress":
		err = c.launchReconcileKOReaderProgressTask()
	case "refresh_koreader_matching":
		err = c.launchRefreshKOReaderMatchingTask()
	default:
		err = fmt.Errorf("unsupported retry type")
	}

	if err != nil {
		jsonError(w, http.StatusConflict, fmt.Sprintf("Retry failed: %v", err))
		return
	}

	jsonResponse(w, http.StatusAccepted, map[string]string{"message": "Task retry queued"})
}

func (c *Controller) pauseTask(w http.ResponseWriter, r *http.Request) {
	taskKey := chi.URLParam(r, "taskKey")
	if taskKey == "" {
		jsonError(w, http.StatusBadRequest, "Missing task key")
		return
	}

	c.taskMutex.Lock()
	task, ok := c.tasks[taskKey]
	if !ok {
		c.taskMutex.Unlock()
		jsonError(w, http.StatusNotFound, "Task not found")
		return
	}
	if task.Status != "running" {
		c.taskMutex.Unlock()
		jsonError(w, http.StatusConflict, "Task is not running")
		return
	}
	if !task.CanPause {
		c.taskMutex.Unlock()
		jsonError(w, http.StatusConflict, "Task cannot be paused")
		return
	}
	runtime := c.taskRuntimes[taskKey]
	if runtime == nil || runtime.PauseGate == nil {
		c.taskMutex.Unlock()
		jsonError(w, http.StatusConflict, "Task pause gate is not available")
		return
	}
	now := time.Now()
	runtime.PauseGate.Pause()
	task.Status = "paused"
	task.CanPause = false
	task.CanResume = true
	task.PausedAt = &now
	task.PauseReason = "manual_pause"
	task.Message = "任务已暂停，等待继续执行"
	task.UpdatedAt = now
	c.taskSeq++
	task.Sequence = c.taskSeq
	enrichTaskProgress(&task)
	c.tasks[taskKey] = task
	c.persistTaskStatus(task)
	c.publishTaskStatusLocked(task)
	c.taskMutex.Unlock()

	jsonResponse(w, http.StatusAccepted, map[string]string{"message": "Task pause requested"})
}

func (c *Controller) resumeTask(w http.ResponseWriter, r *http.Request) {
	taskKey := chi.URLParam(r, "taskKey")
	if taskKey == "" {
		jsonError(w, http.StatusBadRequest, "Missing task key")
		return
	}

	c.taskMutex.Lock()
	task, ok := c.tasks[taskKey]
	if !ok {
		c.taskMutex.Unlock()
		jsonError(w, http.StatusNotFound, "Task not found")
		return
	}
	if task.Status != "paused" {
		c.taskMutex.Unlock()
		jsonError(w, http.StatusConflict, "Task is not paused")
		return
	}
	runtime := c.taskRuntimes[taskKey]
	if runtime == nil || runtime.PauseGate == nil {
		c.taskMutex.Unlock()
		jsonError(w, http.StatusConflict, "Task pause gate is not available")
		return
	}
	runtime.PauseGate.Resume()
	task.Status = "running"
	task.CanPause = true
	task.CanResume = false
	task.PausedAt = nil
	task.PauseReason = ""
	task.Message = "任务已继续执行"
	task.UpdatedAt = time.Now()
	c.taskSeq++
	task.Sequence = c.taskSeq
	enrichTaskProgress(&task)
	c.tasks[taskKey] = task
	c.persistTaskStatus(task)
	c.publishTaskStatusLocked(task)
	c.taskMutex.Unlock()

	jsonResponse(w, http.StatusAccepted, map[string]string{"message": "Task resumed"})
}

func (c *Controller) cancelTask(w http.ResponseWriter, r *http.Request) {
	taskKey := chi.URLParam(r, "taskKey")
	if taskKey == "" {
		jsonError(w, http.StatusBadRequest, "Missing task key")
		return
	}

	c.taskMutex.Lock()
	task, ok := c.tasks[taskKey]
	if !ok {
		c.taskMutex.Unlock()
		jsonError(w, http.StatusNotFound, "Task not found")
		return
	}
	if task.Status != "running" && task.Status != "paused" {
		c.taskMutex.Unlock()
		jsonError(w, http.StatusConflict, "Task is not running")
		return
	}
	if !task.CanCancel {
		c.taskMutex.Unlock()
		jsonError(w, http.StatusConflict, "Task cannot be cancelled")
		return
	}
	runtime := c.taskRuntimes[taskKey]
	if runtime == nil || runtime.Cancel == nil {
		c.taskMutex.Unlock()
		jsonError(w, http.StatusConflict, "Task cancellation is not available")
		return
	}

	runtime.Cancel()
	if runtime.PauseGate != nil {
		runtime.PauseGate.Resume()
	}
	task.CanCancel = false
	task.CanPause = false
	task.CanResume = false
	task.Status = "cancelling"
	task.Message = "正在取消任务..."
	task.UpdatedAt = time.Now()
	c.taskSeq++
	task.Sequence = c.taskSeq
	c.tasks[taskKey] = task
	c.persistTaskStatus(task)
	c.publishTaskStatusLocked(task)
	c.taskMutex.Unlock()

	jsonResponse(w, http.StatusAccepted, map[string]string{"message": "Task cancellation requested"})
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

func (c *Controller) searchBooks(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	target := r.URL.Query().Get("target") // "all", "series", "book"
	if target == "" {
		target = "all"
	}

	if query == "" {
		jsonResponse(w, http.StatusOK, map[string]interface{}{"hits": []interface{}{}})
		return
	}

	if target == "series" {
		res, err := c.searchSeriesWithSQLite(r.Context(), query, 20)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "Search failed")
			return
		}
		normalizeSearchScores(res)
		jsonResponse(w, http.StatusOK, res)
		return
	}

	if target == "book" || target == "all" || target == "title" {
		res, err := c.searchBooksWithSQLite(r.Context(), query, target, 20)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "Search failed")
			return
		}
		normalizeSearchScores(res)
		jsonResponse(w, http.StatusOK, res)
		return
	}

	jsonError(w, http.StatusBadRequest, "Invalid search target")
}

func (c *Controller) searchSeriesWithSQLite(ctx context.Context, query string, limit int32) (*SearchResult, error) {
	res := &SearchResult{}
	c.mergeSeriesSearchFallback(ctx, res, query, "series", limit)
	return res, nil
}

func (c *Controller) searchBooksWithSQLite(ctx context.Context, query, target string, limit int32) (*SearchResult, error) {
	res := &SearchResult{}
	if target == "all" {
		if err := c.mergeBookSearchHits(ctx, res, query, limit); err != nil {
			return nil, err
		}
		c.mergeSeriesSearchFallback(ctx, res, query, "all", limit)
		return res, nil
	}
	if err := c.mergeBookSearchHits(ctx, res, query, limit); err != nil {
		return nil, err
	}
	return res, nil
}

func (c *Controller) mergeBookSearchHits(ctx context.Context, res *SearchResult, query string, limit int32) error {
	if res == nil || strings.TrimSpace(query) == "" {
		return nil
	}
	rows, err := c.store.SearchGlobalBooks(ctx, query, limit)
	if err != nil {
		return err
	}
	for _, hit := range rows {
		title := hit.Name
		if hit.Title.Valid && hit.Title.String != "" {
			title = hit.Title.String
		}
		seriesName := hit.SeriesName
		if hit.SeriesTitle.Valid && hit.SeriesTitle.String != "" {
			seriesName = hit.SeriesTitle.String
		}
		coverPath := ""
		if hit.CoverPath.Valid {
			coverPath = hit.CoverPath.String
		}
		score := hit.Score
		if score <= 0 {
			score = 1
		}
		docID := "b_" + strconv.FormatInt(hit.ID, 10)
		res.Hits = append(res.Hits, &SearchHit{
			ID:    docID,
			Score: score,
			Fields: map[string]interface{}{
				"id":          docID,
				"title":       title,
				"series_name": seriesName,
				"type":        "book",
				"cover_path":  coverPath,
			},
		})
		if score > res.MaxScore {
			res.MaxScore = score
		}
	}
	if uint64(len(res.Hits)) > res.Total {
		res.Total = uint64(len(res.Hits))
	}
	return nil
}

// mergeSeriesSearchFallback uses SQLite FTS5 (trigram) series search. Series metadata lives
// in SQLite, and the FTS triggers keep name/title indexed with substring semantics that match
// manga titles well (this replaced the former Bleve-based full-text engine).
func (c *Controller) mergeSeriesSearchFallback(ctx context.Context, res *SearchResult, query, target string, limit int32) {
	if res == nil || strings.TrimSpace(query) == "" || (target != "all" && target != "series") {
		return
	}

	seen := make(map[string]struct{}, len(res.Hits))
	for _, hit := range res.Hits {
		seen[hit.ID] = struct{}{}
	}

	rows, err := c.store.SearchGlobalSeries(ctx, query, limit)
	if err != nil {
		slog.Warn("mergeSeriesSearchFallback: series lookup failed", "error", err)
		return
	}

	added := 0
	for _, hit := range rows {
		row := hit.SearchSeriesPagedRow
		docID := "s_" + strconv.FormatInt(row.ID, 10)
		if _, ok := seen[docID]; ok {
			continue
		}
		title := row.Name
		if row.Title.Valid && row.Title.String != "" {
			title = row.Title.String
		}
		coverPath := ""
		if row.CoverPath.Valid {
			coverPath = row.CoverPath.String
		}
		score := hit.Score
		if score <= 0 {
			score = 1
		}
		res.Hits = append(res.Hits, &SearchHit{
			ID:    docID,
			Score: score,
			Fields: map[string]interface{}{
				"id":          docID,
				"title":       title,
				"series_name": row.Name,
				"type":        "series",
				"cover_path":  coverPath,
			},
		})
		if score > res.MaxScore {
			res.MaxScore = score
		}
		seen[docID] = struct{}{}
		added++
		if target == "series" && len(res.Hits) >= int(limit) {
			break
		}
		if target == "all" && added >= int(limit) {
			break
		}
	}

	if uint64(len(res.Hits)) > res.Total {
		res.Total = uint64(len(res.Hits))
	}
	if res.MaxScore <= 0 && len(res.Hits) > 0 {
		res.MaxScore = 1
		for _, hit := range res.Hits {
			if hit.Score <= 0 {
				hit.Score = 1
			}
		}
	}
}

// normalizeSearchScores 将命中得分按本次结果的最高分缩放到 [0,1]，最佳匹配为 1.0。
func normalizeSearchScores(res *SearchResult) {
	if res == nil || len(res.Hits) == 0 || res.MaxScore <= 0 {
		return
	}
	for _, hit := range res.Hits {
		hit.Score = hit.Score / res.MaxScore
	}
}

// hydrateSearchCovers 用数据库中的最新封面覆盖搜索命中文档的 cover_path 字段。
// 文档 ID 形如 "b_<bookID>" / "s_<seriesID>"。
func (c *Controller) hydrateSearchCovers(ctx context.Context, res *SearchResult) {
	if res == nil || len(res.Hits) == 0 {
		return
	}

	var bookIDs, seriesIDs []int64
	for _, hit := range res.Hits {
		id, kind, ok := parseSearchDocID(hit.ID)
		if !ok {
			continue
		}
		switch kind {
		case "b":
			bookIDs = append(bookIDs, id)
		case "s":
			seriesIDs = append(seriesIDs, id)
		}
	}

	bookCovers := make(map[int64]string, len(bookIDs))
	if len(bookIDs) > 0 {
		if rows, err := c.store.GetBookCoverPathsByIDs(ctx, bookIDs); err == nil {
			for _, row := range rows {
				bookCovers[row.ID] = row.CoverPath
			}
		} else {
			slog.Warn("hydrateSearchCovers: book covers lookup failed", "error", err)
		}
	}

	seriesCovers := make(map[int64]string, len(seriesIDs))
	if len(seriesIDs) > 0 {
		if rows, err := c.store.GetSeriesCoverPathsByIDs(ctx, seriesIDs); err == nil {
			for _, row := range rows {
				seriesCovers[row.ID] = row.CoverPath
			}
		} else {
			slog.Warn("hydrateSearchCovers: series covers lookup failed", "error", err)
		}
	}

	for _, hit := range res.Hits {
		id, kind, ok := parseSearchDocID(hit.ID)
		if !ok {
			continue
		}
		if hit.Fields == nil {
			hit.Fields = map[string]interface{}{}
		}
		switch kind {
		case "b":
			if cover, found := bookCovers[id]; found {
				hit.Fields["cover_path"] = cover
			}
		case "s":
			if cover, found := seriesCovers[id]; found {
				hit.Fields["cover_path"] = cover
			}
		}
	}
}

// parseSearchDocID 解析搜索文档 ID（"b_123" / "s_45"），返回数值 ID 与类型前缀。
func parseSearchDocID(docID string) (int64, string, bool) {
	idx := strings.IndexByte(docID, '_')
	if idx <= 0 || idx == len(docID)-1 {
		return 0, "", false
	}
	id, err := strconv.ParseInt(docID[idx+1:], 10, 64)
	if err != nil {
		return 0, "", false
	}
	return id, docID[:idx], true
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

func (c *Controller) deleteLibrary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	libraryID, err := parseID(r, "libraryId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid library ID")
		return
	}

	if lib, err := c.store.GetLibrary(ctx, libraryID); err == nil && c.watcher != nil {
		c.watcher.UnwatchLibrary(lib.Path)
	}

	err = c.store.DeleteLibrary(ctx, libraryID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to delete library")
		return
	}

	jsonResponse(w, http.StatusOK, map[string]string{"status": "deleted"})
}

type CreateLibraryRequest struct {
	Name                string `json:"name"`
	Path                string `json:"path"`
	ScanMode            string `json:"scan_mode"`
	KOReaderSyncEnabled *bool  `json:"koreader_sync_enabled"`
	ScanInterval        int64  `json:"scan_interval"`
	ScanFormats         string `json:"scan_formats"`
}

func (c *Controller) validateLibraryRequest(ctx context.Context, libraryID *int64, req CreateLibraryRequest) []config.ValidationIssue {
	issues := make([]config.ValidationIssue, 0)
	if strings.TrimSpace(req.Name) == "" {
		issues = append(issues, config.ValidationIssue{Field: "name", Message: "名称不能为空。", Severity: "error"})
	}
	if strings.TrimSpace(req.Path) == "" {
		issues = append(issues, config.ValidationIssue{Field: "path", Message: "路径不能为空。", Severity: "error"})
	} else {
		info, err := os.Stat(req.Path)
		if err != nil {
			issues = append(issues, config.ValidationIssue{Field: "path", Message: "路径不存在或不可访问。", Severity: "error"})
		} else if !info.IsDir() {
			issues = append(issues, config.ValidationIssue{Field: "path", Message: "这里只能选择目录。", Severity: "error"})
		}
	}

	if req.ScanInterval <= 0 {
		issues = append(issues, config.ValidationIssue{Field: "scan_interval", Message: "扫描间隔至少为 1 分钟。", Severity: "error"})
	}

	normalizedFormats := config.ParseScanFormats(req.ScanFormats)
	if len(normalizedFormats) == 0 {
		issues = append(issues, config.ValidationIssue{Field: "scan_formats", Message: "至少保留一个受支持的扫描格式。", Severity: "error"})
	}

	libs, err := c.store.ListLibraries(ctx)
	if err == nil {
		cleanTarget := filepath.Clean(req.Path)
		for _, lib := range libs {
			if libraryID != nil && lib.ID == *libraryID {
				continue
			}
			if filepath.Clean(lib.Path) == cleanTarget {
				issues = append(issues, config.ValidationIssue{Field: "path", Message: "这个目录已经被其他资源库使用。", Severity: "error"})
				break
			}
		}
	}

	return issues
}

func (c *Controller) createLibrary(w http.ResponseWriter, r *http.Request) {
	var req CreateLibraryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	if req.Name == "" || req.Path == "" {
		jsonError(w, http.StatusBadRequest, "Name and Path are required")
		return
	}

	if req.ScanInterval <= 0 {
		req.ScanInterval = config.DefaultScanInterval
	}
	req.ScanFormats = config.NormalizeScanFormatsCSV(req.ScanFormats)

	ctx := r.Context()
	if issues := c.validateLibraryRequest(ctx, nil, req); len(issues) > 0 {
		jsonResponse(w, http.StatusUnprocessableEntity, map[string]interface{}{
			"error":      "Library validation failed",
			"validation": config.ValidationResult{Valid: false, Issues: issues},
		})
		return
	}
	libParams := database.CreateLibraryParams{
		Name:                req.Name,
		Path:                req.Path,
		ScanMode:            req.ScanMode,
		KoreaderSyncEnabled: req.KOReaderSyncEnabled == nil || *req.KOReaderSyncEnabled,
		ScanInterval:        req.ScanInterval,
		ScanFormats:         req.ScanFormats,
	}

	createdLib, err := c.store.CreateLibrary(ctx, libParams)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to create library")
		return
	}
	c.invalidateDashboardStatsCache("library_created")

	if createdLib.ScanMode == "watch" && c.watcher != nil {
		_ = c.watcher.WatchLibrary(createdLib.ID, createdLib.Path)
	}

	// 触发异步扫描任务，不阻塞前端 API 响应
	c.runBackground(func() {
		// 使用独立 context 避免跟随请求自动取消，创建库默认全量
		defer c.purgeReadingPathCaches()
		err := c.scanner.ScanLibrary(context.Background(), createdLib.ID, req.Path, false)
		if err != nil {
			// 在生产环境需要接入日志中心打印
			_ = err
			c.invalidateDashboardStatsCache("library_initial_scan_failed")
			return
		}
		c.warmDashboardStatsCacheAsync("library_initial_scan_completed")
	})

	jsonResponse(w, http.StatusCreated, createdLib)
}

type UpdateLibraryRequest struct {
	Name                string `json:"name"`
	Path                string `json:"path"`
	ScanMode            string `json:"scan_mode"`
	KOReaderSyncEnabled *bool  `json:"koreader_sync_enabled"`
	ScanInterval        int64  `json:"scan_interval"`
	ScanFormats         string `json:"scan_formats"`
}

func (c *Controller) updateLibrary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	libraryID, err := parseID(r, "libraryId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid library ID")
		return
	}

	var req UpdateLibraryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	if req.Name == "" || req.Path == "" {
		jsonError(w, http.StatusBadRequest, "Name and Path are required")
		return
	}
	existingLib, err := c.store.GetLibrary(ctx, libraryID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "Library not found")
		return
	}

	if req.ScanInterval <= 0 {
		req.ScanInterval = config.DefaultScanInterval
	}
	req.ScanFormats = config.NormalizeScanFormatsCSV(req.ScanFormats)
	koreaderSyncEnabled := existingLib.KoreaderSyncEnabled
	if req.KOReaderSyncEnabled != nil {
		koreaderSyncEnabled = *req.KOReaderSyncEnabled
	}

	validateReq := CreateLibraryRequest{
		Name:                req.Name,
		Path:                req.Path,
		ScanMode:            req.ScanMode,
		KOReaderSyncEnabled: &koreaderSyncEnabled,
		ScanInterval:        req.ScanInterval,
		ScanFormats:         req.ScanFormats,
	}
	if issues := c.validateLibraryRequest(ctx, &libraryID, validateReq); len(issues) > 0 {
		jsonResponse(w, http.StatusUnprocessableEntity, map[string]interface{}{
			"error":      "Library validation failed",
			"validation": config.ValidationResult{Valid: false, Issues: issues},
		})
		return
	}

	libParams := database.UpdateLibraryParams{
		ID:                  libraryID,
		Name:                req.Name,
		Path:                req.Path,
		ScanMode:            req.ScanMode,
		KoreaderSyncEnabled: koreaderSyncEnabled,
		ScanInterval:        req.ScanInterval,
		ScanFormats:         req.ScanFormats,
	}

	updatedLib, err := c.store.UpdateLibrary(ctx, libParams)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to update library")
		return
	}
	c.invalidateDashboardStatsCache("library_updated")

	if c.watcher != nil {
		c.watcher.UnwatchLibrary(existingLib.Path)
		if updatedLib.ScanMode == "watch" {
			_ = c.watcher.WatchLibrary(updatedLib.ID, updatedLib.Path)
		}
	}

	jsonResponse(w, http.StatusOK, updatedLib)
}

func (c *Controller) launchLibraryScanTask(lib database.Library, force bool) bool {
	taskKey := fmt.Sprintf("scan_library_%d", lib.ID)
	if !c.startPausableCancelableTask(taskKey, "scan_library", fmt.Sprintf("开始扫描资源库: %s", lib.Name), 0) {
		return false
	}
	limits := c.taskLimitsForPath(lib.Path, force)
	storagePolicy := config.ResolveStoragePolicy(c.currentConfig(), lib.Path)
	c.setTaskMetadata(taskKey, map[string]string{
		"force":                    strconv.FormatBool(force),
		"scan_profile":             c.currentConfig().Scanner.ScanProfile,
		"storage_profile":          storagePolicy.StorageProfile,
		"volume_key":               storagePolicy.VolumeKey,
		"archive_open_concurrency": strconv.Itoa(storagePolicy.IOPolicy.ArchiveOpenConcurrency),
		"cover_concurrency":        strconv.Itoa(storagePolicy.IOPolicy.CoverConcurrency),
	}, lib.Name)
	c.setTaskEffectiveLimit(taskKey, limits)
	taskCtx, cleanupCancel := c.newTaskContext(taskKey)

	c.runBackground(func() {
		defer c.purgeReadingPathCaches()
		err := c.scanner.ScanLibrary(taskCtx, lib.ID, lib.Path, force)
		cleanupCancel()
		if errors.Is(err, context.Canceled) {
			c.invalidateDashboardStatsCache("scan_library_cancelled")
			c.completeTask(taskKey, "cancelled", fmt.Sprintf("资源库扫描已取消: %s", lib.Name))
			return
		}
		if err != nil {
			c.invalidateDashboardStatsCache("scan_library_failed")
			c.failTaskWithError(taskKey, fmt.Sprintf("资源库扫描失败: %v", err), err.Error())
			return
		}
		c.finishTask(taskKey, fmt.Sprintf("资源库扫描完成: %s", lib.Name))
		c.warmDashboardStatsCacheAsync("scan_library_completed")
		c.launchLowPriorityBookHashBackfillTask("scan_library")
	})

	return true
}

func (c *Controller) scanLibrary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	libID, err := parseID(r, "libraryId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid library ID")
		return
	}

	lib, err := c.store.GetLibrary(ctx, libID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "Library not found")
		return
	}

	forceParam := r.URL.Query().Get("force")
	isForce := forceParam == "true"
	if !c.launchLibraryScanTask(lib, isForce) {
		jsonResponse(w, http.StatusConflict, map[string]string{"error": "A library scan is already running"})
		return
	}

	jsonResponse(w, http.StatusOK, map[string]string{"status": "Scan initiated"})
}

func (c *Controller) launchSeriesScanTask(seriesID int64, force bool) bool {
	taskKey := fmt.Sprintf("scan_series_%d", seriesID)
	if !c.startPausableCancelableTask(taskKey, "scan_series", fmt.Sprintf("开始扫描系列 #%d", seriesID), 0) {
		return false
	}
	scopeName := ""
	if series, err := c.store.GetSeries(context.Background(), seriesID); err == nil {
		if series.Title.Valid && strings.TrimSpace(series.Title.String) != "" {
			scopeName = series.Title.String
		} else {
			scopeName = series.Name
		}
	}
	storagePolicy := config.ResolvedStoragePolicy{}
	if series, err := c.store.GetSeries(context.Background(), seriesID); err == nil {
		if lib, libErr := c.store.GetLibrary(context.Background(), series.LibraryID); libErr == nil {
			storagePolicy = config.ResolveStoragePolicy(c.currentConfig(), lib.Path)
			c.setTaskEffectiveLimit(taskKey, c.taskLimitsForPath(lib.Path, force))
		}
	}
	c.setTaskMetadata(taskKey, map[string]string{
		"force":                    strconv.FormatBool(force),
		"scan_profile":             c.currentConfig().Scanner.ScanProfile,
		"storage_profile":          storagePolicy.StorageProfile,
		"volume_key":               storagePolicy.VolumeKey,
		"archive_open_concurrency": strconv.Itoa(storagePolicy.IOPolicy.ArchiveOpenConcurrency),
		"cover_concurrency":        strconv.Itoa(storagePolicy.IOPolicy.CoverConcurrency),
	}, scopeName)
	taskCtx, cleanupCancel := c.newTaskContext(taskKey)

	c.runBackground(func() {
		defer c.purgeReadingPathCaches()
		err := c.scanner.ScanSeries(taskCtx, seriesID, force)
		cleanupCancel()
		if errors.Is(err, context.Canceled) {
			c.invalidateDashboardStatsCache("scan_series_cancelled")
			c.completeTask(taskKey, "cancelled", fmt.Sprintf("系列扫描已取消 #%d", seriesID))
			return
		}
		if err != nil {
			slog.Error("ScanSeries Failed", "seriesId", seriesID, "error", err)
			c.invalidateDashboardStatsCache("scan_series_failed")
			c.failTaskWithError(taskKey, fmt.Sprintf("系列扫描失败: %v", err), err.Error())
			return
		}
		c.finishTask(taskKey, fmt.Sprintf("系列扫描完成 #%d", seriesID))
		c.warmDashboardStatsCacheAsync("scan_series_completed")
		c.launchLowPriorityBookHashBackfillTask("scan_series")
	})

	return true
}

func (c *Controller) scanSeries(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}

	forceParam := r.URL.Query().Get("force")
	isForce := forceParam == "true"
	if !c.launchSeriesScanTask(seriesID, isForce) {
		jsonResponse(w, http.StatusConflict, map[string]string{"error": "A series scan is already running"})
		return
	}

	jsonResponse(w, http.StatusOK, map[string]string{"status": "Scan initiated"})
}

func (c *Controller) getSeriesByLibrary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	libID, err := parseID(r, "libraryId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid library ID")
		return
	}

	series, err := c.store.ListSeriesByLibrary(ctx, libID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to fetch series")
		return
	}

	if series == nil {
		series = []database.ListSeriesByLibraryRow{}
	}
	jsonResponse(w, http.StatusOK, series)
}

// 清理失效资源记录
func (c *Controller) launchCleanupLibraryTask(libraryID int64) bool {
	taskKey := fmt.Sprintf("cleanup_library_%d", libraryID)
	if !c.startTask(taskKey, "cleanup_library", fmt.Sprintf("开始清理资源库 #%d", libraryID), 1) {
		return false
	}
	scopeName := ""
	if lib, err := c.store.GetLibrary(context.Background(), libraryID); err == nil {
		scopeName = lib.Name
	}
	c.setTaskMetadata(taskKey, nil, scopeName)

	c.runBackground(func() {
		c.updateTaskDetails(taskKey, 0, 1, fmt.Sprintf("开始清理资源库 #%d", libraryID), "scanning_records", "", nil, nil)
		err := c.scanner.CleanupLibrary(context.Background(), libraryID)
		if err != nil {
			slog.Error("Failed to cleanup library", "library_id", libraryID, "error", err)
			c.failTaskWithError(taskKey, fmt.Sprintf("资源库清理失败: %v", err), err.Error())
			return
		}
		c.finishTask(taskKey, fmt.Sprintf("资源库清理完成 #%d", libraryID))
	})

	return true
}

func (c *Controller) cleanupLibrary(w http.ResponseWriter, r *http.Request) {
	libraryID, err := parseID(r, "libraryId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid library ID")
		return
	}
	if !c.launchCleanupLibraryTask(libraryID) {
		jsonResponse(w, http.StatusConflict, map[string]string{"error": "A library cleanup is already running"})
		return
	}

	jsonResponse(w, http.StatusOK, map[string]string{"status": "Cleanup initiated"})
}

func (c *Controller) searchSeriesPaged(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	libIDStr := r.URL.Query().Get("libraryId")
	libID, err := strconv.ParseInt(libIDStr, 10, 64)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid library ID")
		return
	}

	limitStr := r.URL.Query().Get("limit")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = 50
	}

	pageStr := r.URL.Query().Get("page")
	page, err := strconv.Atoi(pageStr)
	if err != nil || page <= 0 {
		page = 1
	}
	offset := (page - 1) * limit

	var tags []string
	if tagsParam := r.URL.Query().Get("tags"); tagsParam != "" {
		tags = strings.Split(tagsParam, ",")
	}

	var authors []string
	if authorsParam := r.URL.Query().Get("authors"); authorsParam != "" {
		authors = strings.Split(authorsParam, ",")
	}

	status := r.URL.Query().Get("status")
	letter := r.URL.Query().Get("letter")
	sortBy := r.URL.Query().Get("sortBy")
	keyword := r.URL.Query().Get("q")
	cursor := strings.TrimSpace(r.URL.Query().Get("cursor"))

	if cursor != "" {
		series, nextCursor, hasMore, err := c.store.SearchSeriesCursor(ctx, libID, keyword, letter, status, tags, authors, int32(limit), sortBy, cursor)
		if err != nil {
			slog.Error("SearchSeriesCursor Failed", "error", err)
			jsonError(w, http.StatusBadRequest, "Invalid cursor")
			return
		}
		if series == nil {
			series = []database.SearchSeriesPagedRow{}
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{
			"items":       series,
			"total":       0,
			"page":        page,
			"limit":       limit,
			"next_cursor": nextCursor,
			"has_more":    hasMore,
		})
		return
	}

	series, total, err := c.store.SearchSeriesPaged(ctx, libID, keyword, letter, status, tags, authors, int32(limit), int32(offset), sortBy)
	if err != nil {
		slog.Error("SearchSeriesPaged Failed", "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to fetch series")
		return
	}

	if series == nil {
		series = []database.SearchSeriesPagedRow{}
	}
	hasMore := page*limit < total
	nextCursor := ""
	if hasMore && len(series) > 0 && database.SeriesSearchSortSupportsCursor(sortBy) {
		nextCursor = database.NextSeriesSearchCursor(sortBy, series[len(series)-1])
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"items":       series,
		"total":       total,
		"page":        page,
		"limit":       limit,
		"next_cursor": nextCursor,
		"has_more":    hasMore,
	})
}

// getRecentReadSeries 返回该资源库下含有书籍最新阅读记录的系列
func (c *Controller) getRecentReadSeries(w http.ResponseWriter, r *http.Request) {
	libIDStr := r.URL.Query().Get("libraryId")
	if libIDStr == "" {
		jsonError(w, http.StatusBadRequest, "libraryId is required")
		return
	}
	libID, err := strconv.ParseInt(libIDStr, 10, 64)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid libraryId")
		return
	}

	limitStr := r.URL.Query().Get("limit")
	limit := int64(10) // 默认读取 10 条
	if limitStr != "" {
		if l, err := strconv.ParseInt(limitStr, 10, 64); err == nil && l > 0 {
			limit = l
		}
	}

	ctx := r.Context()
	arg := database.GetRecentReadSeriesParams{
		LibraryID:   libID,
		LibraryID_2: libID,
		Limit:       limit,
	}

	items, err := c.store.GetRecentReadSeries(ctx, arg)
	if err != nil {
		slog.Error("GetRecentReadSeries Failed", "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to fetch recent read series")
		return
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"items": items,
	})
}

func (c *Controller) getSeriesInfo(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}
	series, err := c.store.GetSeries(r.Context(), seriesID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "Series not found")
		return
	}
	jsonResponse(w, http.StatusOK, series)
}

func (c *Controller) openSeriesDirectory(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}

	series, err := c.store.GetSeries(r.Context(), seriesID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "Series not found")
		return
	}

	path := strings.TrimSpace(series.Path)
	if path == "" {
		jsonError(w, http.StatusBadRequest, "Series directory is not available")
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Series directory does not exist")
		return
	}
	if !info.IsDir() {
		jsonError(w, http.StatusBadRequest, "Series path is not a directory")
		return
	}

	opener := c.openPath
	if opener == nil {
		opener = openPathInDefaultFileManager
	}
	if err := opener(path); err != nil {
		slog.Error("OpenSeriesDirectory Failed", "series_id", seriesID, "path", path, "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to open series directory")
		return
	}

	jsonResponse(w, http.StatusOK, map[string]any{"success": true})
}

type UpdateAuthorRequest struct {
	Name string `json:"name"`
	Role string `json:"role"`
}

type UpdateLinkRequest struct {
	Name string `json:"name"`
	Url  string `json:"url"`
}

type UpdateSeriesRequest struct {
	Title        string                `json:"title"`
	Summary      string                `json:"summary"`
	Publisher    string                `json:"publisher"`
	Status       string                `json:"status"`
	Rating       float64               `json:"rating"`
	Language     string                `json:"language"`
	LockedFields string                `json:"locked_fields"`
	Tags         []string              `json:"tags"`
	Authors      []UpdateAuthorRequest `json:"authors"`
	Links        []UpdateLinkRequest   `json:"links"`
}

func (c *Controller) updateSeriesInfo(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}

	var req UpdateSeriesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	currentSeries, err := c.store.GetSeries(r.Context(), seriesID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "Series not found")
		return
	}

	err = c.store.ExecTx(r.Context(), func(q *database.Queries) error {
		_, err := q.UpdateSeriesMetadata(r.Context(), database.UpdateSeriesMetadataParams{
			Title:        sql.NullString{String: req.Title, Valid: req.Title != ""},
			Summary:      sql.NullString{String: req.Summary, Valid: req.Summary != ""},
			Publisher:    sql.NullString{String: req.Publisher, Valid: req.Publisher != ""},
			Status:       sql.NullString{String: req.Status, Valid: req.Status != ""},
			Rating:       sql.NullFloat64{Float64: req.Rating, Valid: req.Rating > 0},
			Language:     sql.NullString{String: req.Language, Valid: req.Language != ""},
			LockedFields: sql.NullString{String: req.LockedFields, Valid: true},
			NameInitial:  database.SeriesInitial(req.Title, currentSeries.Name),
			ID:           seriesID,
		})
		if err != nil {
			return err
		}

		if req.Tags != nil {
			_ = q.ClearSeriesTags(r.Context(), seriesID)
			for _, t := range req.Tags {
				if strings.TrimSpace(t) == "" {
					continue
				}
				if inserted, err := q.UpsertTag(r.Context(), t); err == nil {
					_ = q.LinkSeriesTag(r.Context(), database.LinkSeriesTagParams{SeriesID: seriesID, TagID: inserted.ID})
				}
			}
		}

		if req.Authors != nil {
			_ = q.ClearSeriesAuthors(r.Context(), seriesID)
			for _, a := range req.Authors {
				if strings.TrimSpace(a.Name) == "" {
					continue
				}
				if inserted, err := q.UpsertAuthor(r.Context(), database.UpsertAuthorParams{Name: a.Name, Role: a.Role}); err == nil {
					_ = q.LinkSeriesAuthor(r.Context(), database.LinkSeriesAuthorParams{SeriesID: seriesID, AuthorID: inserted.ID})
				}
			}
		}

		if req.Links != nil {
			_ = q.ClearSeriesLinks(r.Context(), seriesID)
			for _, link := range req.Links {
				if strings.TrimSpace(link.Name) == "" || strings.TrimSpace(link.Url) == "" {
					continue
				}
				_, _ = q.LinkSeriesLink(r.Context(), database.LinkSeriesLinkParams{
					SeriesID: seriesID,
					Name:     link.Name,
					Url:      link.Url,
				})
			}
		}

		return q.RefreshSeriesStats(r.Context(), seriesID)
	})

	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to update series metadata")
		return
	}

	// Fetch updated details for response
	updated, _ := c.store.GetSeries(r.Context(), seriesID)
	jsonResponse(w, http.StatusOK, updated)
}

func (c *Controller) getSeriesTags(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}
	tags, err := c.store.GetTagsForSeries(r.Context(), seriesID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to get tags")
		return
	}
	jsonResponse(w, http.StatusOK, tags)
}

func (c *Controller) getSeriesAuthors(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}
	authors, err := c.store.GetAuthorsForSeries(r.Context(), seriesID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to get authors")
		return
	}
	jsonResponse(w, http.StatusOK, authors)
}

func (c *Controller) getSeriesLinks(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}
	links, err := c.store.GetLinksForSeries(r.Context(), seriesID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to get links")
		return
	}
	if links == nil {
		links = []database.SeriesLink{}
	}
	jsonResponse(w, http.StatusOK, links)
}

type SeriesContextResponse struct {
	Series            database.Series         `json:"series"`
	Books             []database.Book         `json:"books"`
	Tags              []database.Tag          `json:"tags"`
	Authors           []database.Author       `json:"authors"`
	Links             []database.SeriesLink   `json:"links"`
	Volumes           []SeriesVolumeSummary   `json:"volumes"`
	Relations         []SeriesRelation        `json:"relations"`
	MetadataReview    metadataReviewResponse  `json:"metadata_review"`
	MetadataSummary   SeriesMetadataSummary   `json:"metadata_summary"`
	FailedTasks       []TaskStatus            `json:"failed_tasks"`
	FailedTaskSummary SeriesFailedTaskSummary `json:"failed_task_summary"`
	Continue          SeriesContinue          `json:"continue"`
}

// SeriesContinue 描述用户在某系列内的续读位置，用于资源库 / 详情页 CTA。
type SeriesContinue struct {
	NextUnreadBookID int64      `json:"next_unread_book_id,omitempty"`
	LastReadBookID   int64      `json:"last_read_book_id,omitempty"`
	LastReadPage     int64      `json:"last_read_page,omitempty"`
	LastReadAt       *time.Time `json:"last_read_at,omitempty"`
	TotalBooks       int        `json:"total_books"`
	ReadBooks        int        `json:"read_books"`
	TotalPages       int64      `json:"total_pages"`
	ReadPages        int64      `json:"read_pages"`
}

type SeriesVolumeSummary struct {
	Name        string          `json:"name"`
	BookCount   int             `json:"book_count"`
	TotalPages  int64           `json:"total_pages"`
	ReadPages   int64           `json:"read_pages"`
	CoverBookID int64           `json:"cover_book_id,omitempty"`
	CoverPath   sql.NullString  `json:"cover_path"`
	UpdatedAt   time.Time       `json:"updated_at"`
	Books       []database.Book `json:"books,omitempty"`
}

type SeriesFailedTaskSummary struct {
	Count    int        `json:"count"`
	LatestAt *time.Time `json:"latest_at,omitempty"`
}

type SeriesMetadataSummary struct {
	PendingReviewCount int `json:"pending_review_count"`
	ProvenanceCount    int `json:"provenance_count"`
}

func (c *Controller) getSeriesContext(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}

	ctx := r.Context()

	// 1. 获取系列基本信息
	series, err := c.store.GetSeries(ctx, seriesID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "Series not found")
		return
	}

	// 2. 获取书籍列表
	books, err := c.store.ListBooksBySeries(ctx, seriesID)
	if err != nil {
		slog.Error("Failed to fetch books for context", "series_id", seriesID, "error", err)
	}
	if books == nil {
		books = []database.Book{}
	}
	sortBooksForReading(books)

	// 3. 标签
	tags, err := c.store.GetTagsForSeries(ctx, seriesID)
	if err != nil {
		slog.Error("Failed to fetch tags for context", "series_id", seriesID, "error", err)
	}
	if tags == nil {
		tags = []database.Tag{}
	}

	// 4. 作者
	authors, err := c.store.GetAuthorsForSeries(ctx, seriesID)
	if err != nil {
		slog.Error("Failed to fetch authors for context", "series_id", seriesID, "error", err)
	}
	if authors == nil {
		authors = []database.Author{}
	}

	// 5. 链接
	links, err := c.store.GetLinksForSeries(ctx, seriesID)
	if err != nil {
		slog.Error("Failed to fetch links for context", "series_id", seriesID, "error", err)
	}
	if links == nil {
		links = []database.SeriesLink{}
	}

	relations, err := c.loadSeriesRelations(ctx, seriesID)
	if err != nil {
		slog.Error("Failed to fetch relations for context", "series_id", seriesID, "error", err)
		relations = []SeriesRelation{}
	}

	metadataReview, err := c.loadSeriesMetadataReview(ctx, seriesID)
	if err != nil {
		slog.Error("Failed to fetch metadata review for context", "series_id", seriesID, "error", err)
		metadataReview = emptyMetadataReviewResponse()
	}

	failedTasks, err := c.listTaskStatuses(ctx, database.TaskFilters{
		Status:  "failed",
		Scope:   "series",
		ScopeID: &seriesID,
		Limit:   5,
	})
	if err != nil {
		slog.Error("Failed to fetch failed tasks for context", "series_id", seriesID, "error", err)
		failedTasks = []TaskStatus{}
	}
	if failedTasks == nil {
		failedTasks = []TaskStatus{}
	}

	jsonResponse(w, http.StatusOK, SeriesContextResponse{
		Series:            series,
		Books:             books,
		Tags:              tags,
		Authors:           authors,
		Links:             links,
		Volumes:           buildSeriesVolumeSummaries(books, false),
		Relations:         relations,
		MetadataReview:    metadataReview,
		MetadataSummary:   summarizeSeriesMetadata(metadataReview),
		FailedTasks:       failedTasks,
		FailedTaskSummary: summarizeFailedTasks(failedTasks),
		Continue:          buildSeriesContinue(books),
	})
}

func (c *Controller) getSeriesContinueEndpoint(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}

	ctx := r.Context()
	if _, err := c.store.GetSeries(ctx, seriesID); err != nil {
		jsonError(w, http.StatusNotFound, "Series not found")
		return
	}

	books, err := c.store.ListBooksBySeries(ctx, seriesID)
	if err != nil {
		slog.Error("Failed to fetch books for continue", "series_id", seriesID, "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to compute continue position")
		return
	}
	sortBooksForReading(books)
	jsonResponse(w, http.StatusOK, buildSeriesContinue(books))
}

// buildSeriesContinue 假设 books 已按阅读顺序排序。
// 规则：
//   - next_unread_book_id：第一本未完成的书（last_read_page < page_count，含完全未读）。
//   - last_read_book_id：last_read_at 最大的书，用作"上次读到这里"。
//   - 全部读完时 next_unread 为 0；用户可前端落到 first 或 last。
func buildSeriesContinue(books []database.Book) SeriesContinue {
	out := SeriesContinue{TotalBooks: len(books)}
	var latestAt *time.Time
	for i := range books {
		book := books[i]
		out.TotalPages += book.PageCount
		readPages := int64(0)
		if book.LastReadPage.Valid {
			readPages = book.LastReadPage.Int64
			if book.PageCount > 0 && readPages > book.PageCount {
				readPages = book.PageCount
			}
		}
		out.ReadPages += readPages
		isFinished := book.PageCount > 0 && readPages >= book.PageCount
		if isFinished {
			out.ReadBooks++
		}
		if out.NextUnreadBookID == 0 && !isFinished {
			out.NextUnreadBookID = book.ID
		}
		if book.LastReadAt.Valid {
			at := book.LastReadAt.Time
			if latestAt == nil || at.After(*latestAt) {
				captured := at
				latestAt = &captured
				out.LastReadBookID = book.ID
				out.LastReadPage = readPages
			}
		}
	}
	if latestAt != nil {
		out.LastReadAt = latestAt
	}
	return out
}

func buildSeriesVolumeSummaries(books []database.Book, includeBooks bool) []SeriesVolumeSummary {
	type volumeAccumulator struct {
		summary SeriesVolumeSummary
		books   []database.Book
	}
	volumeMap := make(map[string]*volumeAccumulator)
	for _, book := range books {
		volumeName := strings.TrimSpace(book.Volume)
		if volumeName == "" {
			continue
		}
		acc, ok := volumeMap[volumeName]
		if !ok {
			acc = &volumeAccumulator{summary: SeriesVolumeSummary{Name: volumeName}}
			volumeMap[volumeName] = acc
		}
		acc.summary.BookCount++
		acc.summary.TotalPages += book.PageCount
		if book.LastReadPage.Valid {
			readPages := book.LastReadPage.Int64
			if book.PageCount > 0 && readPages > book.PageCount {
				readPages = book.PageCount
			}
			acc.summary.ReadPages += readPages
		}
		if acc.summary.CoverBookID == 0 && book.CoverPath.Valid && strings.TrimSpace(book.CoverPath.String) != "" {
			acc.summary.CoverBookID = book.ID
			acc.summary.CoverPath = book.CoverPath
			acc.summary.UpdatedAt = book.UpdatedAt
		}
		if includeBooks {
			acc.books = append(acc.books, book)
		}
	}
	items := make([]SeriesVolumeSummary, 0, len(volumeMap))
	for _, acc := range volumeMap {
		if includeBooks {
			acc.summary.Books = acc.books
		}
		items = append(items, acc.summary)
	}
	sort.Slice(items, func(i, j int) bool {
		return booksort.CompareLabels(items[i].Name, items[j].Name) < 0
	})
	return items
}

func summarizeFailedTasks(tasks []TaskStatus) SeriesFailedTaskSummary {
	summary := SeriesFailedTaskSummary{Count: len(tasks)}
	for _, task := range tasks {
		if summary.LatestAt == nil || task.UpdatedAt.After(*summary.LatestAt) {
			updatedAt := task.UpdatedAt
			summary.LatestAt = &updatedAt
		}
	}
	return summary
}

func summarizeSeriesMetadata(review metadataReviewResponse) SeriesMetadataSummary {
	return SeriesMetadataSummary{
		PendingReviewCount: len(review.Reviews),
		ProvenanceCount:    len(review.Provenance),
	}
}

func (c *Controller) getAllTags(w http.ResponseWriter, r *http.Request) {
	tags, err := c.store.GetAllTags(r.Context())
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to fetch all tags")
		return
	}
	if tags == nil {
		tags = []database.Tag{}
	}
	jsonResponse(w, http.StatusOK, tags)
}

func parseFacetSearchLimit(r *http.Request) int {
	limit := 30
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}
	if limit < 1 {
		return 30
	}
	if limit > 100 {
		return 100
	}
	return limit
}

func (c *Controller) searchTags(w http.ResponseWriter, r *http.Request) {
	items, err := c.store.SearchTags(r.Context(), r.URL.Query().Get("q"), parseFacetSearchLimit(r))
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to search tags")
		return
	}
	if items == nil {
		items = []database.Tag{}
	}
	jsonResponse(w, http.StatusOK, items)
}

func (c *Controller) getAllAuthors(w http.ResponseWriter, r *http.Request) {
	authors, err := c.store.GetAllAuthors(r.Context())
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to fetch all authors")
		return
	}
	if authors == nil {
		authors = []database.Author{}
	}
	jsonResponse(w, http.StatusOK, authors)
}

func (c *Controller) searchAuthors(w http.ResponseWriter, r *http.Request) {
	items, err := c.store.SearchAuthors(r.Context(), r.URL.Query().Get("q"), parseFacetSearchLimit(r))
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to search authors")
		return
	}
	if items == nil {
		items = []database.Author{}
	}
	jsonResponse(w, http.StatusOK, items)
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

func cloneDashboardStats(stats *database.DashboardStats) *database.DashboardStats {
	if stats == nil {
		return nil
	}
	cloned := *stats
	if stats.LibrarySizes != nil {
		cloned.LibrarySizes = append([]database.LibrarySize(nil), stats.LibrarySizes...)
	}
	return &cloned
}

// invalidateDashboardStatsCache 失效全部统计缓存（结构性 + 阅读类）。
// 用于扫描/库结构变化等会改变 total_books/total_pages 的场景。
func (c *Controller) invalidateDashboardStatsCache(reason string) {
	c.structuralStatsMu.Lock()
	c.structuralStatsGen++
	c.structuralStatsCache = nil
	c.structuralStatsMu.Unlock()

	c.dashboardStatsMu.Lock()
	c.dashboardStatsGen++
	c.volatileStatsCache = nil
	c.dashboardStatsMu.Unlock()
	if reason != "" {
		slog.Debug("Invalidated dashboard stats cache", "reason", reason)
	}
}

// invalidateVolatileStatsCache 仅失效阅读类统计缓存（read_books/active_days）。
// 用于阅读进度更新等高频场景——这些操作不改变结构性统计，避免触发 books 全表扫描。
func (c *Controller) invalidateVolatileStatsCache(reason string) {
	c.dashboardStatsMu.Lock()
	c.dashboardStatsGen++
	c.volatileStatsCache = nil
	c.dashboardStatsMu.Unlock()
	if reason != "" {
		slog.Debug("Invalidated volatile dashboard stats cache", "reason", reason)
	}
}

func (c *Controller) warmDashboardStatsCacheAsync(reason string) {
	c.runBackground(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := c.loadDashboardStats(ctx); err != nil {
			slog.Debug("Failed to warm dashboard stats cache", "reason", reason, "error", err)
		}
	})
}

func (c *Controller) loadStructuralStats(ctx context.Context) (*database.DashboardStructuralStats, error) {
	now := time.Now()
	c.structuralStatsMu.RLock()
	if c.structuralStatsCache != nil && now.Before(c.structuralStatsCache.expiresAt) {
		stats := c.structuralStatsCache.stats
		c.structuralStatsMu.RUnlock()
		return &stats, nil
	}
	generation := c.structuralStatsGen
	c.structuralStatsMu.RUnlock()

	stats, err := c.store.GetDashboardStructuralStats(ctx)
	if err != nil {
		return nil, err
	}
	if stats == nil {
		stats = &database.DashboardStructuralStats{}
	}
	c.structuralStatsMu.Lock()
	if generation == c.structuralStatsGen {
		c.structuralStatsCache = &cachedStructuralStats{
			stats:     *stats,
			expiresAt: now.Add(dashboardStatsCacheTTL),
		}
	}
	c.structuralStatsMu.Unlock()
	return stats, nil
}

func (c *Controller) loadVolatileStats(ctx context.Context) (*database.DashboardVolatileStats, error) {
	now := time.Now()
	c.dashboardStatsMu.RLock()
	if c.volatileStatsCache != nil && now.Before(c.volatileStatsCache.expiresAt) {
		stats := c.volatileStatsCache.stats
		c.dashboardStatsMu.RUnlock()
		return &stats, nil
	}
	generation := c.dashboardStatsGen
	c.dashboardStatsMu.RUnlock()

	stats, err := c.store.GetDashboardVolatileStats(ctx)
	if err != nil {
		return nil, err
	}
	if stats == nil {
		stats = &database.DashboardVolatileStats{}
	}
	c.dashboardStatsMu.Lock()
	if generation == c.dashboardStatsGen {
		c.volatileStatsCache = &cachedVolatileStats{
			stats:     *stats,
			expiresAt: now.Add(dashboardStatsCacheTTL),
		}
	}
	c.dashboardStatsMu.Unlock()
	return stats, nil
}

func (c *Controller) loadDashboardStats(ctx context.Context) (*database.DashboardStats, error) {
	if c.store == nil {
		return nil, errors.New("store is not configured")
	}

	structural, err := c.loadStructuralStats(ctx)
	if err != nil {
		return nil, err
	}
	volatile, err := c.loadVolatileStats(ctx)
	if err != nil {
		return nil, err
	}

	stats := &database.DashboardStats{
		TotalSeries:  structural.TotalSeries,
		TotalBooks:   structural.TotalBooks,
		TotalPages:   structural.TotalPages,
		LibrarySizes: structural.LibrarySizes,
		ReadBooks:    volatile.ReadBooks,
		ActiveDays7:  volatile.ActiveDays7,
	}
	return cloneDashboardStats(stats), nil
}

func (c *Controller) getNextBook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bookID, err := parseID(r, "bookId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid book ID")
		return
	}

	currentBook, err := c.store.GetBook(ctx, bookID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "No next book")
		return
	}
	books, err := c.store.ListBooksBySeries(ctx, currentBook.SeriesID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "No next book")
		return
	}
	sortBooksForReading(books)
	for i := range books {
		if books[i].ID == currentBook.ID && i+1 < len(books) {
			jsonResponse(w, http.StatusOK, books[i+1])
			return
		}
	}

	jsonError(w, http.StatusNotFound, "No next book")
}

func (c *Controller) getPrevBook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bookID, err := parseID(r, "bookId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid book ID")
		return
	}

	currentBook, err := c.store.GetBook(ctx, bookID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "No previous book")
		return
	}
	books, err := c.store.ListBooksBySeries(ctx, currentBook.SeriesID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "No previous book")
		return
	}
	sortBooksForReading(books)
	for i := range books {
		if books[i].ID == currentBook.ID && i > 0 {
			jsonResponse(w, http.StatusOK, books[i-1])
			return
		}
	}

	jsonError(w, http.StatusNotFound, "No previous book")
}

func sortBooksForReading(books []database.Book) {
	sort.SliceStable(books, func(i, j int) bool {
		return booksort.CompareBooks(books[i], books[j]) < 0
	})
}

type UpdateProgressRequest struct {
	Page int64 `json:"page"`
}

const progressWriteThrottleWindow = 2 * time.Second

type cachedProgressWrite struct {
	page      int64
	updatedAt time.Time
}

func (c *Controller) updateBookProgress(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bookID, err := parseID(r, "bookId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid book ID")
		return
	}

	var req UpdateProgressRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	if req.Page <= 0 {
		req.Page = 1
	}

	if c.progressWriteCache != nil {
		if cached, ok := c.progressWriteCache.Get(bookID); ok && cached.page == req.Page && time.Since(cached.updatedAt) < progressWriteThrottleWindow {
			jsonResponse(w, http.StatusOK, map[string]string{"status": "Progress unchanged"})
			return
		}
	}

	// 校验页码合法性
	book, err := c.store.GetBook(ctx, bookID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "Book not found")
		return
	}

	validPage := req.Page
	if validPage > book.PageCount {
		validPage = book.PageCount
	}
	if validPage < 1 {
		validPage = 1
	}

	previousPage := int64(0)
	if book.LastReadPage.Valid {
		previousPage = book.LastReadPage.Int64
	}
	if book.LastReadPage.Valid && previousPage == validPage && book.LastReadAt.Valid && time.Since(book.LastReadAt.Time) < progressWriteThrottleWindow {
		if c.progressWriteCache != nil {
			c.progressWriteCache.Add(bookID, cachedProgressWrite{page: validPage, updatedAt: time.Now()})
		}
		jsonResponse(w, http.StatusOK, map[string]string{"status": "Progress unchanged"})
		return
	}

	params := database.UpdateBookProgressParams{
		LastReadPage: sql.NullInt64{Int64: validPage, Valid: true},
		LastReadAt:   sql.NullTime{Time: time.Now(), Valid: true},
		ID:           bookID,
	}

	if err := c.store.UpdateBookProgress(ctx, params); err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to update progress")
		return
	}
	c.invalidateVolatileStatsCache("book_progress")
	if c.progressWriteCache != nil {
		c.progressWriteCache.Add(bookID, cachedProgressWrite{page: validPage, updatedAt: time.Now()})
	}

	// 阅读活动只记录前进页，避免 Webtoon 滚动和重复上报刷高活动写入。
	if validPage > previousPage {
		if err := c.store.LogReadingActivity(ctx, database.LogReadingActivityParams{BookID: bookID, PagesRead: validPage}); err != nil {
			slog.Error("Failed to log reading activity", "book_id", bookID, "error", err)
		}
	} else if !book.LastReadPage.Valid {
		if err := c.store.LogReadingActivity(ctx, database.LogReadingActivityParams{BookID: bookID, PagesRead: validPage}); err != nil {
			slog.Error("Failed to log reading activity", "book_id", bookID, "error", err)
		}
	}

	jsonResponse(w, http.StatusOK, map[string]string{"status": "Progress updated"})
}

type BulkSyncProgressItem struct {
	BookID    int64      `json:"book_id"`
	Page      int64      `json:"page"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

type BulkSyncProgressRequest struct {
	Items []BulkSyncProgressItem `json:"items"`
}

type BulkSyncProgressResultItem struct {
	BookID  int64  `json:"book_id"`
	Status  string `json:"status"` // updated | skipped_stale | skipped_unchanged | not_found | invalid
	Page    int64  `json:"page,omitempty"`
	Message string `json:"message,omitempty"`
}

// bulkSyncBookProgress 接受多本书的离线进度并按 updated_at 解决冲突。
// 离线 / 在线恢复时 useReaderOffline 调用，避免逐条 POST 的峰值写入。
func (c *Controller) bulkSyncBookProgress(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req BulkSyncProgressRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	if len(req.Items) == 0 {
		jsonResponse(w, http.StatusOK, map[string]interface{}{
			"updated": 0,
			"results": []BulkSyncProgressResultItem{},
		})
		return
	}

	results := make([]BulkSyncProgressResultItem, 0, len(req.Items))
	updatedCount := 0

	for _, item := range req.Items {
		if item.BookID <= 0 {
			results = append(results, BulkSyncProgressResultItem{
				BookID:  item.BookID,
				Status:  "invalid",
				Message: "book_id is required",
			})
			continue
		}

		book, err := c.store.GetBook(ctx, item.BookID)
		if err != nil {
			results = append(results, BulkSyncProgressResultItem{
				BookID: item.BookID,
				Status: "not_found",
			})
			continue
		}

		validPage := item.Page
		if validPage <= 0 {
			validPage = 1
		}
		if book.PageCount > 0 && validPage > book.PageCount {
			validPage = book.PageCount
		}

		// updated_at 冲突解决：若客户端时间戳 < 数据库 last_read_at，认为本地数据已陈旧，跳过。
		// 没有 updated_at 时按顺序覆盖（与单本 updateBookProgress 行为一致）。
		if item.UpdatedAt != nil && book.LastReadAt.Valid && item.UpdatedAt.Before(book.LastReadAt.Time) {
			results = append(results, BulkSyncProgressResultItem{
				BookID:  item.BookID,
				Status:  "skipped_stale",
				Page:    book.LastReadPage.Int64,
				Message: "server has newer progress",
			})
			continue
		}

		// 与单本端点对齐的相同页节流。
		previousPage := int64(0)
		if book.LastReadPage.Valid {
			previousPage = book.LastReadPage.Int64
		}
		if book.LastReadPage.Valid && previousPage == validPage {
			results = append(results, BulkSyncProgressResultItem{
				BookID: item.BookID,
				Status: "skipped_unchanged",
				Page:   validPage,
			})
			continue
		}

		readAt := time.Now()
		if item.UpdatedAt != nil {
			readAt = *item.UpdatedAt
		}
		params := database.UpdateBookProgressParams{
			LastReadPage: sql.NullInt64{Int64: validPage, Valid: true},
			LastReadAt:   sql.NullTime{Time: readAt, Valid: true},
			ID:           item.BookID,
		}
		if err := c.store.UpdateBookProgress(ctx, params); err != nil {
			slog.Error("bulk sync progress update failed", "book_id", item.BookID, "error", err)
			results = append(results, BulkSyncProgressResultItem{
				BookID:  item.BookID,
				Status:  "invalid",
				Message: "update failed",
			})
			continue
		}
		updatedCount++

		if c.progressWriteCache != nil {
			c.progressWriteCache.Add(item.BookID, cachedProgressWrite{page: validPage, updatedAt: time.Now()})
		}

		// 仅前进的页码记录活动，与单本接口策略一致。
		if validPage > previousPage {
			if err := c.store.LogReadingActivity(ctx, database.LogReadingActivityParams{BookID: item.BookID, PagesRead: validPage}); err != nil {
				slog.Error("Failed to log reading activity", "book_id", item.BookID, "error", err)
			}
		} else if !book.LastReadPage.Valid {
			if err := c.store.LogReadingActivity(ctx, database.LogReadingActivityParams{BookID: item.BookID, PagesRead: validPage}); err != nil {
				slog.Error("Failed to log reading activity", "book_id", item.BookID, "error", err)
			}
		}

		results = append(results, BulkSyncProgressResultItem{
			BookID: item.BookID,
			Status: "updated",
			Page:   validPage,
		})
	}

	if updatedCount > 0 {
		c.invalidateVolatileStatsCache("bulk_progress_sync")
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"updated": updatedCount,
		"results": results,
	})
}

type UpsertReadingBookmarkRequest struct {
	Page int64  `json:"page"`
	Note string `json:"note"`
}

func (c *Controller) listReadingBookmarks(w http.ResponseWriter, r *http.Request) {
	bookID, err := parseID(r, "bookId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid book ID")
		return
	}
	if _, err := c.store.GetBook(r.Context(), bookID); err != nil {
		jsonError(w, http.StatusNotFound, "Book not found")
		return
	}

	items, err := c.store.ListReadingBookmarks(r.Context(), bookID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to load reading bookmarks")
		return
	}
	if items == nil {
		items = []database.ReadingBookmark{}
	}
	jsonResponse(w, http.StatusOK, items)
}

func (c *Controller) upsertReadingBookmark(w http.ResponseWriter, r *http.Request) {
	bookID, err := parseID(r, "bookId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid book ID")
		return
	}

	var req UpsertReadingBookmarkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	book, err := c.store.GetBook(r.Context(), bookID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "Book not found")
		return
	}
	page := req.Page
	if page < 1 {
		page = 1
	}
	if book.PageCount > 0 && page > book.PageCount {
		page = book.PageCount
	}

	item, err := c.store.UpsertReadingBookmark(r.Context(), database.UpsertReadingBookmarkParams{
		BookID: bookID,
		Page:   page,
		Note:   strings.TrimSpace(req.Note),
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to save reading bookmark")
		return
	}
	jsonResponse(w, http.StatusOK, item)
}

func (c *Controller) deleteReadingBookmark(w http.ResponseWriter, r *http.Request) {
	bookID, err := parseID(r, "bookId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid book ID")
		return
	}
	bookmarkID, err := parseID(r, "bookmarkId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid bookmark ID")
		return
	}
	affected, err := c.store.DeleteReadingBookmark(r.Context(), database.DeleteReadingBookmarkParams{
		ID:     bookmarkID,
		BookID: bookID,
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to delete reading bookmark")
		return
	}
	if affected == 0 {
		jsonError(w, http.StatusNotFound, "Bookmark not found")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "Bookmark deleted"})
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

func (c *Controller) browseDirs(w http.ResponseWriter, r *http.Request) {
	reqPath := r.URL.Query().Get("path")

	// 如果没有传路径，返回系统根目录
	if reqPath == "" {
		if runtime.GOOS == "windows" {
			reqPath = "C:\\"
		} else {
			reqPath = "/"
		}
	}

	// 确保路径存在且是目录
	info, err := os.Stat(reqPath)
	if err != nil || !info.IsDir() {
		jsonError(w, http.StatusBadRequest, "Path is not a valid directory")
		return
	}

	entries, err := os.ReadDir(reqPath)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Cannot read directory")
		return
	}

	type DirEntry struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}

	var dirs []DirEntry
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		// 跳过隐藏文件夹
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		dirs = append(dirs, DirEntry{
			Name: entry.Name(),
			Path: filepath.Join(reqPath, entry.Name()),
		})
	}

	sort.Slice(dirs, func(i, j int) bool {
		return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name)
	})

	// Windows 盘符探测
	var drives []DirEntry
	if runtime.GOOS == "windows" {
		for letter := 'A'; letter <= 'Z'; letter++ {
			drivePath := string(letter) + ":\\"
			if fi, err := os.Stat(drivePath); err == nil && fi.IsDir() {
				drives = append(drives, DirEntry{
					Name: string(letter) + ":",
					Path: drivePath,
				})
			}
		}
	}

	result := struct {
		Current string     `json:"current"`
		Parent  string     `json:"parent"`
		Dirs    []DirEntry `json:"dirs"`
		Drives  []DirEntry `json:"drives,omitempty"`
	}{
		Current: reqPath,
		Parent:  filepath.Dir(reqPath),
		Dirs:    dirs,
		Drives:  drives,
	}

	if result.Dirs == nil {
		result.Dirs = []DirEntry{}
	}

	jsonResponse(w, http.StatusOK, result)
}

func (c *Controller) enrichConfigWithDatabase(ctx context.Context, cfg *config.Config) {
	libs, err := c.store.ListLibraries(ctx)
	if err == nil {
		cfg.Library.Paths = make([]string, 0, len(libs))
		for _, lib := range libs {
			cfg.Library.Paths = append(cfg.Library.Paths, lib.Path)
		}
	}
}

func (c *Controller) getSystemConfig(w http.ResponseWriter, r *http.Request) {
	cfg := c.currentConfig()
	c.enrichConfigWithDatabase(r.Context(), &cfg)
	// 回显前脱敏：LLM api_key、server.auth.token 等敏感字段以占位符返回，不向客户端泄露明文。
	jsonResponse(w, http.StatusOK, c.buildSystemConfigResponse(config.MaskSecrets(cfg)))
}

func (c *Controller) getSystemCapabilities(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, http.StatusOK, c.systemCapabilities())
}

func (c *Controller) updateSystemConfig(w http.ResponseWriter, r *http.Request) {
	var newCfg config.Config
	if err := json.NewDecoder(r.Body).Decode(&newCfg); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid configuration format")
		return
	}
	// 前端保存整份配置时会把未改动的密钥以占位符形式回传，用当前值回填，避免真实密钥被占位符覆盖。
	config.RestoreMaskedSecrets(&newCfg, c.currentConfig())
	config.NormalizeConfig(&newCfg)

	validation := config.ValidateConfig(&newCfg)
	if !validation.Valid {
		jsonResponse(w, http.StatusUnprocessableEntity, map[string]interface{}{
			"error":      "Configuration validation failed",
			"validation": validation,
		})
		return
	}
	if err := c.persistConfig(&newCfg); err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to persist configuration")
		return
	}

	c.enrichConfigWithDatabase(r.Context(), &newCfg)

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"message":    "配置已成功保存。大部分设定会立刻生效。",
		"config":     config.MaskSecrets(newCfg),
		"validation": validation,
	})
}

func (c *Controller) testLLMConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider    string `json:"provider"`
		APIMode     string `json:"api_mode"`
		BaseURL     string `json:"base_url"`
		RequestPath string `json:"request_path"`
		Endpoint    string `json:"endpoint"`
		Model       string `json:"model"`
		APIKey      string `json:"api_key"`
		Prompt      string `json:"prompt"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if req.Prompt == "" {
		req.Prompt = "Hello, this is a test from Manga Manager."
	}
	if req.BaseURL == "" && req.Endpoint != "" {
		tmpCfg := &config.Config{}
		tmpCfg.LLM.Provider = req.Provider
		tmpCfg.LLM.Endpoint = req.Endpoint
		config.NormalizeConfig(tmpCfg)
		req.BaseURL = tmpCfg.LLM.BaseURL
		req.RequestPath = tmpCfg.LLM.RequestPath
		req.APIMode = tmpCfg.LLM.APIMode
	}

	cfg := c.currentConfig()
	// 前端可能回传脱敏占位符（未改动密钥）：用当前存储的真实密钥替换，避免用占位符去测试。
	if req.APIKey == config.SecretMask {
		req.APIKey = cfg.LLM.APIKey
	}
	// SSRF 加固：拒绝非 http(s) 的出站目标协议。
	if err := validateOutboundLLMTarget(req.BaseURL, req.Endpoint); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	provider := metadata.NewAIProvider(req.Provider, req.APIMode, req.BaseURL, req.RequestPath, req.Model, req.APIKey, cfg.LLM.Timeout)
	response, err := provider.TestLLM(r.Context(), req.Prompt)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("LLM Test failed: %v", err))
		return
	}

	jsonResponse(w, http.StatusOK, map[string]string{
		"response": response,
	})
}

// 触发扫描全库，作为通用的挂载工具
func (c *Controller) triggerGlobalScan(ctx context.Context) {
	libs, err := c.store.ListLibraries(ctx)
	if err == nil {
		for _, lib := range libs {
			go func(lib database.Library) {
				defer c.purgeReadingPathCaches()
				c.scanner.ScanLibrary(ctx, lib.ID, lib.Path, true)
			}(lib)
		}
	}
}

// clearAllCoverPaths 把数据库中 books 与 series_stats 的 cover_path 字段清空，
// 用于"重建缩略图缓存"任务在删盘后强制让 scanner 重新生成所有缩略图。
func (c *Controller) clearAllCoverPaths(ctx context.Context) error {
	if err := c.store.ClearAllBookCoverPaths(ctx); err != nil {
		return fmt.Errorf("clear book cover paths: %w", err)
	}
	if err := c.store.ClearAllSeriesStatsCoverPaths(ctx); err != nil {
		return fmt.Errorf("clear series cover paths: %w", err)
	}
	return nil
}

func (c *Controller) runGlobalScan(ctx context.Context, force bool, progress func(current, total int, lib database.Library)) error {
	libs, err := c.store.ListLibraries(ctx)
	if err != nil {
		return err
	}
	total := len(libs)
	for i, lib := range libs {
		if err := taskcontrol.Wait(ctx); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if progress != nil {
			progress(i, total, lib)
		}
		if err := c.scanner.ScanLibrary(ctx, lib.ID, lib.Path, force); err != nil {
			return err
		}
		c.purgeReadingPathCaches()
		if progress != nil {
			progress(i+1, total, lib)
		}
	}
	return nil
}

func (c *Controller) launchRebuildIndexTask() error {
	if !c.startTask("rebuild_index", "rebuild_index", "开始重建搜索索引", 1) {
		return fmt.Errorf("task already running")
	}
	c.setTaskMetadata("rebuild_index", nil, "系统")

	if err := c.store.RebuildSeriesSearchIndex(context.Background()); err != nil {
		c.failTaskWithError("rebuild_index", fmt.Sprintf("SQLite series search index rebuild failed: %v", err), err.Error())
		return err
	}
	if err := c.store.RebuildBookSearchIndex(context.Background()); err != nil {
		c.failTaskWithError("rebuild_index", fmt.Sprintf("SQLite book search index rebuild failed: %v", err), err.Error())
		return err
	}

	go c.triggerGlobalScan(context.Background())
	c.finishTask("rebuild_index", "搜索索引已重建，正在后台重建索引数据")
	return nil
}

func (c *Controller) rebuildIndex(w http.ResponseWriter, r *http.Request) {
	if err := c.launchRebuildIndexTask(); err != nil {
		if strings.Contains(err.Error(), "already running") {
			jsonResponse(w, http.StatusConflict, map[string]string{"error": "A search index rebuild is already running"})
			return
		}
		jsonError(w, http.StatusInternalServerError, "Failed to rebuild search index")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"message": "搜索索引已在线重建，并已触发全库重新建立索引。"})
}

func (c *Controller) launchRebuildThumbnailsTask() error {
	if !c.startPausableCancelableTask("rebuild_thumbnails", "rebuild_thumbnails", "开始重建缩略图", 0) {
		return fmt.Errorf("task already running")
	}
	policy := config.ResolveStoragePolicy(c.currentConfig(), "")
	c.setTaskMetadata("rebuild_thumbnails", map[string]string{
		"storage_profile":   policy.StorageProfile,
		"volume_key":        policy.VolumeKey,
		"cover_concurrency": strconv.Itoa(policy.IOPolicy.CoverConcurrency),
		"execution_mode":    "low_impact",
	}, "系统")
	c.setTaskEffectiveLimit("rebuild_thumbnails", c.taskLimitsForPath("", true))
	taskCtx, cleanupCancel := c.newTaskContext("rebuild_thumbnails")

	thumbDir := filepath.Join(".", "data", "thumbnails")
	cfg := c.currentConfig()
	if cfg.Cache.Dir != "" {
		thumbDir = cfg.Cache.Dir
	}

	c.runBackground(func() {
		defer cleanupCancel()
		defer c.releaseRebuildThumbAggregator()
		c.initRebuildThumbAggregator(0)
		c.updateTaskDetails("rebuild_thumbnails", 0, 0, "正在清理缩略图缓存", "clearing_cache", thumbDir, nil, nil)
		if err := os.RemoveAll(thumbDir); err != nil {
			c.failTaskWithError("rebuild_thumbnails", fmt.Sprintf("清理缩略图缓存失败: %v", err), err.Error())
			return
		}
		if err := taskcontrol.Wait(taskCtx); errors.Is(err, context.Canceled) {
			c.completeTask("rebuild_thumbnails", "cancelled", "缩略图重建已取消")
			return
		}
		if err := os.MkdirAll(thumbDir, 0o755); err != nil {
			c.failTaskWithError("rebuild_thumbnails", fmt.Sprintf("创建缩略图缓存目录失败: %v", err), err.Error())
			return
		}
		c.updateTaskDetails("rebuild_thumbnails", 0, -1, "正在清空封面索引", "clearing_cache", "", nil, nil)
		if err := c.clearAllCoverPaths(taskCtx); err != nil {
			c.failTaskWithError("rebuild_thumbnails", fmt.Sprintf("清空封面索引失败: %v", err), err.Error())
			return
		}
		c.updateTaskDetails("rebuild_thumbnails", 0, -1, "缩略图缓存已清空，正在按低冲击策略重建", "reading_metadata", "", nil, nil)
		err := c.runGlobalScan(taskCtx, true, func(current, total int, lib database.Library) {
			c.trackRebuildThumbLibraryProgress(current, total, lib)
			c.refreshRebuildThumbTaskFromAggregator(lib)
		})
		if errors.Is(err, context.Canceled) {
			c.completeTask("rebuild_thumbnails", "cancelled", "缩略图重建已取消")
			return
		}
		if err != nil {
			c.failTaskWithError("rebuild_thumbnails", fmt.Sprintf("缩略图重建失败: %v", err), err.Error())
			return
		}
		c.refreshRebuildThumbTaskMessage("正在等待封面队列收尾", "queueing_covers")
		if err := c.scanner.WaitForCoverQueue(taskCtx); errors.Is(err, context.Canceled) {
			c.completeTask("rebuild_thumbnails", "cancelled", "缩略图重建已取消")
			return
		} else if err != nil {
			c.failTaskWithError("rebuild_thumbnails", fmt.Sprintf("等待缩略图队列失败: %v", err), err.Error())
			return
		}
		c.finishTask("rebuild_thumbnails", "缩略图缓存已按低冲击策略重建完成")
		c.warmDashboardStatsCacheAsync("rebuild_thumbnails_completed")
	})
	c.PublishEvent("refresh_thumbnails")
	return nil
}

func (c *Controller) rebuildThumbnails(w http.ResponseWriter, r *http.Request) {
	if err := c.launchRebuildThumbnailsTask(); err != nil {
		jsonResponse(w, http.StatusConflict, map[string]string{"error": "A thumbnail rebuild is already running"})
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"message": "当前的所有缩略图缓存已彻底撕毁，后台已发起全量静默遍历来重制封面。"})
}

func (c *Controller) launchCleanupThumbnailsTask() error {
	if !c.startPausableCancelableTask("cleanup_thumbnails", "cleanup_thumbnails", "开始清理未使用的缩略图", 0) {
		return fmt.Errorf("task already running")
	}
	taskCtx, cleanupCancel := c.newTaskContext("cleanup_thumbnails")
	c.setTaskMetadata("cleanup_thumbnails", nil, "系统")

	go c.runBackground(func() {
		defer cleanupCancel()

		c.updateTaskDetails("cleanup_thumbnails", 0, -1, "正在扫描未使用的缩略图...", "cleanup", "", nil, nil)

		err := c.scanner.CleanupThumbnails(taskCtx, func(deleted, scanned int, msg string) {
			c.updateTaskDetails("cleanup_thumbnails", deleted, scanned, msg, "cleanup", "", nil, nil)
		})

		if errors.Is(err, context.Canceled) {
			c.completeTask("cleanup_thumbnails", "cancelled", "清理缩略图已取消")
			return
		}
		if err != nil {
			c.failTaskWithError("cleanup_thumbnails", fmt.Sprintf("清理缩略图失败: %v", err), err.Error())
			return
		}
		c.finishTask("cleanup_thumbnails", "缩略图清理完成")
	})
	return nil
}

func (c *Controller) cleanupThumbnails(w http.ResponseWriter, r *http.Request) {
	if err := c.launchCleanupThumbnailsTask(); err != nil {
		jsonResponse(w, http.StatusConflict, map[string]string{"error": "A thumbnail cleanup is already running"})
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"message": "已在后台启动无效封面资源清理任务。"})
}

func (c *Controller) launchRebuildFileIdentitiesTask() error {
	if !c.startPausableCancelableTask("rebuild_file_identities", "rebuild_file_identities", "开始重建文件身份索引", 0) {
		return fmt.Errorf("task already running")
	}
	c.setTaskMetadata("rebuild_file_identities", map[string]string{"profile": "quick_hash"}, "系统")
	c.setTaskEffectiveLimit("rebuild_file_identities", c.taskLimitsForPath("", true))
	taskCtx, cleanupCancel := c.newTaskContext("rebuild_file_identities")

	c.runBackground(func() {
		defer cleanupCancel()
		updated, total, err := c.runRebuildFileIdentities(taskCtx, 500, func(current, total int, message string, metrics taskIOMetrics) {
			c.updateTaskDetails("rebuild_file_identities", current, total, message, "hashing", "", map[string]int64{
				"hashed_files": metrics.HashedFiles,
				"io_wait_ms":   metrics.IOWaitMillis,
				"paused_ms":    metrics.PausedMillis,
			}, map[string]string{
				"storage_profile": metrics.StorageProfile,
				"volume_key":      metrics.VolumeKey,
			})
			c.mergeTaskParams("rebuild_file_identities", taskIOMetricsParams(metrics))
		})
		if errors.Is(err, context.Canceled) {
			c.completeTask("rebuild_file_identities", "cancelled", "文件身份索引重建已取消")
			return
		}
		if err != nil {
			c.failTaskWithError("rebuild_file_identities", fmt.Sprintf("文件身份索引重建失败: %v", err), err.Error())
			return
		}
		c.finishTask("rebuild_file_identities", fmt.Sprintf("文件身份索引重建完成，已更新 %d / %d 本书籍", updated, total))
	})
	return nil
}

func (c *Controller) runRebuildFileIdentities(ctx context.Context, limit int, progress func(current, total int, message string, metrics taskIOMetrics)) (int, int, error) {
	if limit <= 0 {
		limit = 500
	}
	missingCount, err := c.store.CountBooksMissingQuickHash(ctx)
	if err != nil {
		return 0, 0, err
	}

	total := int(missingCount)
	updated := 0
	metrics := taskIOMetrics{}
	var afterID int64
	for {
		if err := taskcontrol.Wait(ctx); err != nil {
			return updated, total, err
		}
		books, err := c.store.ListBooksMissingQuickHashBatch(ctx, afterID, limit)
		if err != nil {
			return updated, total, err
		}
		if len(books) == 0 {
			break
		}

		for _, book := range books {
			if err := taskcontrol.Wait(ctx); err != nil {
				return updated, total, err
			}
			policy, releaseToken, waited, paused, tokenErr := c.acquireTaskStorageToken(ctx, book.LibraryPath, storageio.WorkKindIdentityHash)
			if tokenErr != nil {
				return updated, total, tokenErr
			}
			if waited > 0 {
				metrics.IOWaitMillis += waited.Milliseconds()
			}
			if paused > 0 {
				metrics.PausedMillis += paused.Milliseconds()
			}
			metrics.StorageProfile = policy.StorageProfile
			metrics.VolumeKey = policy.VolumeKey
			quickHash, err := koreader.FingerprintQuickFile(book.Path)
			releaseToken()
			metrics.HashedFiles++
			if err != nil {
				slog.Warn("Failed to quick-fingerprint book", "book_id", book.ID, "path", book.Path, "error", err)
				afterID = book.ID
				continue
			}
			if err := c.store.UpdateBookIdentity(ctx, database.UpdateBookIdentityParams{
				ID:        book.ID,
				QuickHash: quickHash,
			}); err != nil {
				return updated, total, err
			}

			updated++
			afterID = book.ID
			if progress != nil {
				progress(updated, total, fmt.Sprintf("已重建 %d / %d 本书籍的 quick_hash", updated, total), metrics)
			}
		}
	}
	return updated, total, nil
}

func (c *Controller) launchLowPriorityBookHashBackfillTask(reason string) bool {
	cfg := c.currentConfig()
	if !cfg.KOReader.Enabled || cfg.KOReader.MatchMode != config.KOReaderMatchModeBinaryHash {
		return false
	}

	missingCount, err := c.store.CountBooksMissingIdentity(context.Background(), config.KOReaderMatchModeBinaryHash)
	if err != nil {
		slog.Warn("Failed to count missing full hashes for background backfill", "error", err)
		return false
	}
	if missingCount == 0 {
		return false
	}

	if !c.startPausableCancelableTask(lowPriorityBookHashTaskKey, "rebuild_book_hashes", "开始后台低优先级补算 KOReader 二进制哈希", int(missingCount)) {
		return false
	}
	c.setTaskMetadata(lowPriorityBookHashTaskKey, map[string]string{
		"match_mode": config.KOReaderMatchModeBinaryHash,
		"profile":    "full_hash_low_priority",
		"reason":     reason,
	}, "系统")
	c.setTaskEffectiveLimit(lowPriorityBookHashTaskKey, c.taskLimitsForPath("", true))
	taskCtx, cleanupCancel := c.newTaskContext(lowPriorityBookHashTaskKey)

	c.runBackground(func() {
		updated, total, err := c.runBackfillFullHashesLowPriority(taskCtx, lowPriorityBookHashBatchSize, lowPriorityBookHashBatchGap, func(current, total int, message string, metrics taskIOMetrics) {
			c.updateTaskDetails(lowPriorityBookHashTaskKey, current, total, message, "hashing", "", map[string]int64{
				"hashed_files": metrics.HashedFiles,
				"io_wait_ms":   metrics.IOWaitMillis,
				"paused_ms":    metrics.PausedMillis,
			}, map[string]string{
				"storage_profile": metrics.StorageProfile,
				"volume_key":      metrics.VolumeKey,
			})
			c.mergeTaskParams(lowPriorityBookHashTaskKey, taskIOMetricsParams(metrics))
		})
		cleanupCancel()
		if errors.Is(err, context.Canceled) {
			c.completeTask(lowPriorityBookHashTaskKey, "cancelled", "后台 KOReader 二进制哈希补算已取消")
			return
		}
		if err != nil {
			c.failTaskWithError(lowPriorityBookHashTaskKey, fmt.Sprintf("后台 KOReader 二进制哈希补算失败: %v", err), err.Error())
			return
		}
		c.finishTask(lowPriorityBookHashTaskKey, fmt.Sprintf("后台 KOReader 二进制哈希补算完成，已更新 %d / %d 本书籍", updated, total))
	})
	return true
}

func (c *Controller) runBackfillFullHashesLowPriority(ctx context.Context, limit int, batchGap time.Duration, progress func(current, total int, message string, metrics taskIOMetrics)) (int, int, error) {
	if limit <= 0 {
		limit = lowPriorityBookHashBatchSize
	}
	missingCount, err := c.store.CountBooksMissingIdentity(ctx, config.KOReaderMatchModeBinaryHash)
	if err != nil {
		return 0, 0, err
	}

	total := int(missingCount)
	updated := 0
	metrics := taskIOMetrics{}
	var afterID int64
	for {
		if err := taskcontrol.Wait(ctx); err != nil {
			return updated, total, err
		}
		books, err := c.store.ListBooksMissingIdentityBatch(ctx, config.KOReaderMatchModeBinaryHash, afterID, limit)
		if err != nil {
			return updated, total, err
		}
		if len(books) == 0 {
			break
		}

		for _, book := range books {
			if err := taskcontrol.Wait(ctx); err != nil {
				return updated, total, err
			}
			policy, releaseToken, waited, paused, tokenErr := c.acquireTaskStorageToken(ctx, book.LibraryPath, storageio.WorkKindIdentityHash)
			if tokenErr != nil {
				return updated, total, tokenErr
			}
			if waited > 0 {
				metrics.IOWaitMillis += waited.Milliseconds()
			}
			if paused > 0 {
				metrics.PausedMillis += paused.Milliseconds()
			}
			metrics.StorageProfile = policy.StorageProfile
			metrics.VolumeKey = policy.VolumeKey
			fileHash, err := koreader.FingerprintFile(book.Path)
			releaseToken()
			metrics.HashedFiles++
			if err != nil {
				slog.Warn("Failed to backfill full book hash", "book_id", book.ID, "path", book.Path, "error", err)
				afterID = book.ID
				continue
			}
			if err := c.store.UpdateBookIdentity(ctx, database.UpdateBookIdentityParams{
				ID:       book.ID,
				FileHash: fileHash,
			}); err != nil {
				return updated, total, err
			}

			updated++
			afterID = book.ID
			if progress != nil {
				progress(updated, total, fmt.Sprintf("后台低优先级补算 %d / %d 本书籍的 full hash", updated, total), metrics)
			}
		}

		if batchGap > 0 {
			if err := taskcontrol.Wait(ctx); err != nil {
				return updated, total, err
			}
			timer := time.NewTimer(batchGap)
			select {
			case <-timer.C:
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return updated, total, ctx.Err()
			}
		}
	}
	return updated, total, nil
}

func (c *Controller) rebuildFileIdentities(w http.ResponseWriter, r *http.Request) {
	if err := c.launchRebuildFileIdentitiesTask(); err != nil {
		jsonResponse(w, http.StatusConflict, map[string]string{"error": "A file identity rebuild is already running"})
		return
	}
	jsonResponse(w, http.StatusAccepted, map[string]string{"message": "文件身份索引重建已启动"})
}

// getDashboardStats 返回全局统计看板数据
func (c *Controller) getDashboardStats(w http.ResponseWriter, r *http.Request) {
	stats, err := c.loadDashboardStats(r.Context())
	if err != nil {
		slog.Error("GetDashboardStats failed", "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to get dashboard stats")
		return
	}
	jsonResponse(w, http.StatusOK, stats)
}

// getActivityHeatmap 返回近 N 周每日阅读页数热力数据
func (c *Controller) getActivityHeatmap(w http.ResponseWriter, r *http.Request) {
	weeksStr := r.URL.Query().Get("weeks")
	weeks := 16 // 默认 16 周
	if w, err := strconv.Atoi(weeksStr); err == nil && w > 0 && w <= 52 {
		weeks = w
	}

	offset := fmt.Sprintf("-%d days", weeks*7)
	rows, err := c.store.GetActivityHeatmap(r.Context(), offset)
	if err != nil {
		slog.Error("GetActivityHeatmap failed", "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to get activity heatmap")
		return
	}
	data := make([]database.ActivityDay, 0, len(rows))
	for _, row := range rows {
		count := 0
		if row.PageCount.Valid {
			count = int(row.PageCount.Float64)
		}
		data = append(data, database.ActivityDay{Date: row.Date, PageCount: count})
	}
	jsonResponse(w, http.StatusOK, data)
}

// getRecentReadAll 返回跨库的最近阅读记录（用于 Dashboard 首页）
func (c *Controller) getRecentReadAll(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := int64(20)
	if limitStr != "" {
		if l, err := strconv.ParseInt(limitStr, 10, 64); err == nil && l > 0 {
			limit = l
		}
	}

	items, err := c.store.GetRecentReadAll(r.Context(), limit)
	if err != nil {
		slog.Error("GetRecentReadAll failed", "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to get recent reads")
		return
	}

	sequels, err := c.store.GetContinueReadingSequels(r.Context())
	if err != nil {
		slog.Error("GetContinueReadingSequels failed", "error", err)
		// Ignore error and continue with items
	}

	type DashboardContinueItem struct {
		SeriesID           int64       `json:"series_id"`
		SeriesName         string      `json:"series_name"`
		BookID             int64       `json:"book_id"`
		BookName           string      `json:"book_name"`
		BookTitle          interface{} `json:"book_title"`
		CoverPath          string      `json:"cover_path"`
		LastReadPage       interface{} `json:"last_read_page"`
		LastReadAt         interface{} `json:"last_read_at"`
		PageCount          int64       `json:"page_count"`
		IsSequelSuggestion bool        `json:"is_sequel_suggestion,omitempty"`
		RelationType       string      `json:"relation_type,omitempty"`
		SourceSeriesName   string      `json:"source_series_name,omitempty"`
	}

	result := make([]DashboardContinueItem, 0, len(items)+len(sequels))
	for _, item := range items {
		cover := item.CoverPath
		result = append(result, DashboardContinueItem{
			SeriesID:     item.SeriesID,
			SeriesName:   item.SeriesName,
			BookID:       item.BookID,
			BookName:     item.BookName,
			BookTitle:    item.BookTitle,
			CoverPath:    cover,
			LastReadPage: item.LastReadPage,
			LastReadAt:   item.LastReadAt,
			PageCount:    int64(item.PageCount),
		})
	}

	for _, s := range sequels {
		cover := ""
		if s.CoverPath.Valid {
			cover = s.CoverPath.String
		}
		result = append(result, DashboardContinueItem{
			SeriesID:           s.SeriesID,
			SeriesName:         s.SeriesName,
			BookID:             0,
			BookName:           "",
			BookTitle:          nil,
			CoverPath:          cover,
			LastReadPage:       nil,
			LastReadAt:         nil,
			PageCount:          0,
			IsSequelSuggestion: true,
			RelationType:       s.RelationType,
			SourceSeriesName:   s.SourceSeriesName,
		})
	}

	jsonResponse(w, http.StatusOK, result)
}

type AIRecommendationResponse struct {
	SeriesID  int64  `json:"series_id"`
	Reason    string `json:"reason"`
	Title     string `json:"title"`
	CoverPath string `json:"cover_path"`
}

// getRecommendations 基于本地阅读历史的综合 LLM 推荐
func (c *Controller) getRecommendations(w http.ResponseWriter, r *http.Request) {
	locale := requestLocale(r)
	forceRefresh := r.URL.Query().Get("refresh") == "true"

	if !forceRefresh && c.cachedRecommendations(locale) != nil {
		jsonResponse(w, http.StatusOK, c.cachedRecommendations(locale))
		return
	}

	// 合并同一 locale 的并发冷缓存/刷新请求，只触发一次 LLM 推理。用 context.WithoutCancel 解绑
	// leader 的请求取消，避免 leader 客户端断开波及所有搭车的 follower（超时仍由 LLM Timeout 控制）。
	flightCtx := metadata.WithLocale(context.WithoutCancel(r.Context()), locale)
	v, err, _ := c.recommendationsGroup.Do(locale, func() (any, error) {
		if !forceRefresh {
			if cached := c.cachedRecommendations(locale); cached != nil {
				return cached, nil // 等待期间已被其他 leader 填充
			}
		}
		return c.computeRecommendations(flightCtx, locale)
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "AI inference failed: "+err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, v.([]AIRecommendationResponse))
}

// cachedRecommendations 返回未过期的缓存推荐（无有效缓存时返回 nil）。
func (c *Controller) cachedRecommendations(locale string) []AIRecommendationResponse {
	c.recommendationsMutex.RLock()
	defer c.recommendationsMutex.RUnlock()
	cache := c.recommendationsCache[locale]
	if time.Since(c.recommendationsCacheTime[locale]) < 24*time.Hour && len(cache) > 0 {
		return cache
	}
	return nil
}

// computeRecommendations 拉候选、调 LLM 生成推荐并回填缓存。由 getRecommendations 经 singleflight 调用，
// 保证同一 locale 的并发请求只执行一次。
func (c *Controller) computeRecommendations(ctx context.Context, locale string) ([]AIRecommendationResponse, error) {
	// 1. 获取用户最常看的 10 个标签
	tagRows, err := c.store.GetTopReadingTags(ctx, 10)
	var userTags []string
	if err == nil {
		for _, tr := range tagRows {
			userTags = append(userTags, tr.Name)
		}
	}

	// 2. 随机获取 20 本可能有兴趣的候选漫画
	candidateRows, err := c.store.GetCandidateSeriesForAI(ctx, 20)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch candidates from database: %w", err)
	}

	var candidates []metadata.CandidateSeries
	var candidatesMap = make(map[int64]database.GetCandidateSeriesForAIRow)
	for _, cr := range candidateRows {
		title := cr.Title.String
		if title == "" {
			title = cr.Name
		}
		summary := cr.Summary.String
		candidatesMap[cr.ID] = cr
		candidates = append(candidates, metadata.CandidateSeries{
			ID:      cr.ID,
			Title:   title,
			Summary: summary,
		})
	}

	if len(candidates) == 0 {
		return []AIRecommendationResponse{}, nil // 没有候选则不推荐，空结果不缓存
	}

	// 3. 构建 Provider
	cfg := c.currentConfig()
	provider := metadata.NewAIProvider(cfg.LLM.Provider, cfg.LLM.APIMode, cfg.LLM.BaseURL, cfg.LLM.RequestPath, cfg.LLM.Model, cfg.LLM.APIKey, cfg.LLM.Timeout)

	// 4. 交给 LLM 甄选并产出理
	recList, err := provider.GenerateRecommendations(ctx, userTags, candidates, 3)
	if err != nil {
		slog.Error("LLM failed to generate recommendations", "error", err)
		return nil, err
	}

	// 5. 组合最终回包数据
	var finalRecs []AIRecommendationResponse
	for _, rec := range recList {
		cRow, ok := candidatesMap[rec.SeriesID]
		if !ok {
			continue // AI幻觉
		}
		title := cRow.Title.String
		if title == "" {
			title = cRow.Name
		}
		coverPath := ""
		if cRow.CoverPath.Valid {
			coverPath = cRow.CoverPath.String
		}
		finalRecs = append(finalRecs, AIRecommendationResponse{
			SeriesID:  rec.SeriesID,
			Reason:    rec.Reason,
			Title:     title,
			CoverPath: coverPath,
		})
	}

	// Update cache
	c.recommendationsMutex.Lock()
	c.recommendationsCache[locale] = finalRecs
	c.recommendationsCacheTime[locale] = time.Now()
	c.recommendationsMutex.Unlock()

	return finalRecs, nil
}

// aiGroupingLibrary 扫描资料库中没有集合的系列，利用 LLM 进行智能分组
func (c *Controller) launchAIGroupingTask(libID int64, locale string) bool {
	taskKey := fmt.Sprintf("ai_grouping_library_%d", libID)
	if !c.startPausableCancelableTask(taskKey, "ai_grouping", "AI 智能分组开始...", 1) {
		return false
	}
	scopeName := ""
	if lib, err := c.store.GetLibrary(context.Background(), libID); err == nil {
		scopeName = lib.Name
	}
	c.setTaskMetadata(taskKey, nil, scopeName)
	taskCtx, cleanupCancel := c.newTaskContext(taskKey)

	c.runBackground(func() {
		defer cleanupCancel()
		libraryID, taskLocale := libID, locale
		ctx := metadata.WithLocale(taskCtx, taskLocale)

		c.updateTaskDetails(taskKey, 0, 1, "正在读取待分组系列", "collecting_series", "", nil, nil)
		seriesRows, err := c.store.GetSeriesWithoutCollection(ctx, libraryID)
		if errors.Is(err, context.Canceled) {
			c.completeTask(taskKey, "cancelled", "AI 智能分组已取消")
			return
		}
		if err != nil {
			slog.Error("Failed to fetch series for grouping", "error", err)
			c.failTaskWithError(taskKey, "AI 分组失败 (数据库获取异常)", err.Error())
			return
		}

		slog.Info("AI grouping: fetched candidate series", "library_id", libraryID, "count", len(seriesRows))

		if len(seriesRows) == 0 {
			c.finishTask(taskKey, "此库中所有作品已分组完成")
			return
		}
		if err := taskcontrol.Wait(ctx); errors.Is(err, context.Canceled) {
			c.completeTask(taskKey, "cancelled", "AI 智能分组已取消")
			return
		}

		chunkSize := 50
		if len(seriesRows) > chunkSize {
			seriesRows = seriesRows[:chunkSize]
		}

		var candidates []metadata.CandidateSeries
		for _, row := range seriesRows {
			title := row.Title.String
			if title == "" {
				title = row.Name
			}
			candidates = append(candidates, metadata.CandidateSeries{
				ID:      row.ID,
				Title:   title,
				Summary: row.Summary.String,
			})
		}

		cfg := c.currentConfig()
		provider := metadata.NewAIProvider(cfg.LLM.Provider, cfg.LLM.APIMode, cfg.LLM.BaseURL, cfg.LLM.RequestPath, cfg.LLM.Model, cfg.LLM.APIKey, cfg.LLM.Timeout)
		c.updateTaskDetails(taskKey, 0, 1, "正在请求 AI 分组", "requesting_provider", "", map[string]int64{
			"candidate_series": int64(len(candidates)),
		}, map[string]string{
			"provider": provider.Name(),
		})
		collections, err := provider.GenerateGrouping(ctx, candidates)
		if errors.Is(err, context.Canceled) {
			c.completeTask(taskKey, "cancelled", "AI 智能分组已取消")
			return
		}
		if err != nil {
			slog.Error("Failed to generate grouping", "error", err)
			c.failTaskWithError(taskKey, fmt.Sprintf("AI 分组失败: %s", err.Error()), err.Error())
			return
		}

		c.updateTaskDetails(taskKey, 1, 1, "正在写入 AI 分组审阅", "queueing_review", "", nil, nil)
		review, reviewCollections, err := c.createAIGroupingReview(ctx, libraryID, provider.Name(), candidates, collections)
		if errors.Is(err, context.Canceled) {
			c.completeTask(taskKey, "cancelled", "AI 智能分组已取消")
			return
		}
		if err != nil {
			slog.Error("Failed to create AI grouping review", "library_id", libraryID, "error", err)
			c.failTaskWithError(taskKey, "AI 分组审核生成失败", err.Error())
			return
		}
		if reviewCollections == 0 {
			c.finishTask(taskKey, "AI 智能分组未生成可审核的合集")
			return
		}

		c.finishTask(taskKey, fmt.Sprintf("AI 智能分组审核已生成 (审核单 #%d，%d 个候选合集)", review.ID, reviewCollections))
		c.PublishEvent("refresh")
	})

	return true
}

func (c *Controller) aiGroupingLibrary(w http.ResponseWriter, r *http.Request) {
	libID, err := parseID(r, "libraryId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid library ID")
		return
	}
	if !c.launchAIGroupingTask(libID, requestLocale(r)) {
		jsonResponse(w, http.StatusConflict, map[string]string{"error": "An AI grouping task is already running for this library"})
		return
	}

	jsonResponse(w, http.StatusAccepted, map[string]string{"message": "AI 分组审核任务已提交至后台"})
}

func (c *Controller) rebuildInitials(w http.ResponseWriter, r *http.Request) {
	if err := c.store.BackfillSeriesInitials(r.Context()); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "success"})
}
