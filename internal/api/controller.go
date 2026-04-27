package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
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

	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/external"
	"manga-manager/internal/koreader"
	"manga-manager/internal/logger"
	"manga-manager/internal/metadata"
	"manga-manager/internal/parser"
	"manga-manager/internal/scanner"
	"manga-manager/internal/search"

	"github.com/go-chi/chi/v5"
	lru "github.com/hashicorp/golang-lru/v2"
)

type Controller struct {
	store      database.Store
	imageCache *lru.Cache[string, []byte]
	scanner    *scanner.Scanner
	engine     *search.Engine
	config     *config.Manager
	koreader   *koreader.Service
	external   *external.Manager
	configPath string
	watcher    *scanner.FileWatcher

	// SSE Broker
	clients        map[chan string]bool
	newClients     chan chan string
	defunctClients chan chan string
	messages       chan string

	// AI Recommendations Cache
	recommendationsCache     map[string][]AIRecommendationResponse
	recommendationsCacheTime map[string]time.Time
	recommendationsMutex     sync.RWMutex

	taskMutex sync.Mutex
	tasks     map[string]TaskStatus
	taskSeq   int64

	openPath func(string) error
}

type TaskStatus struct {
	Key        string            `json:"key"`
	Type       string            `json:"type"`
	Scope      string            `json:"scope"`
	ScopeID    *int64            `json:"scope_id,omitempty"`
	ScopeName  string            `json:"scope_name,omitempty"`
	Status     string            `json:"status"`
	Message    string            `json:"message"`
	Error      string            `json:"error,omitempty"`
	Current    int               `json:"current"`
	Total      int               `json:"total"`
	CanCancel  bool              `json:"can_cancel"`
	Retryable  bool              `json:"retryable"`
	Params     map[string]string `json:"params,omitempty"`
	StartedAt  time.Time         `json:"started_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
	FinishedAt *time.Time        `json:"finished_at,omitempty"`
	Sequence   int64             `json:"-"`
}

type SystemCapabilitiesResponse struct {
	SupportedScanFormats  []string `json:"supported_scan_formats"`
	SupportedLogLevels    []string `json:"supported_log_levels"`
	DefaultScanFormats    string   `json:"default_scan_formats"`
	DefaultScanInterval   int      `json:"default_scan_interval"`
	SupportedLLMProviders []string `json:"supported_llm_providers"`
	SupportedLLMAPIModes  []string `json:"supported_llm_api_modes"`
}

type SystemConfigResponse struct {
	Config       config.Config              `json:"config"`
	Validation   config.ValidationResult    `json:"validation"`
	Capabilities SystemCapabilitiesResponse `json:"capabilities"`
}

const maxRetainedTasks = 200

func NewController(store database.Store, scan *scanner.Scanner, engine *search.Engine, cfg *config.Manager, cfgPath string) *Controller {
	cache, _ := lru.New[string, []byte](256)
	c := &Controller{
		store:                    store,
		imageCache:               cache,
		scanner:                  scan,
		engine:                   engine,
		config:                   cfg,
		koreader:                 koreader.NewService(store, cfg),
		external:                 external.NewManager(store, 30*time.Minute),
		configPath:               cfgPath,
		clients:                  make(map[chan string]bool),
		newClients:               make(chan chan string),
		defunctClients:           make(chan chan string),
		messages:                 make(chan string, 64),
		tasks:                    make(map[string]TaskStatus),
		recommendationsCache:     make(map[string][]AIRecommendationResponse),
		recommendationsCacheTime: make(map[string]time.Time),
		openPath:                 openPathInDefaultFileManager,
	}

	go c.startBroker()
	go c.startDaemon()

	// 初始化文件系统监控
	fw, err := scanner.NewFileWatcher(scan)
	if err != nil {
		slog.Warn("Failed to create file watcher", "error", err)
	} else {
		c.watcher = fw
		fw.Start(c.PublishEvent)
		// 为现有库开启监听
		go func() {
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
		}()
	}

	return c
}

func (c *Controller) currentConfig() config.Config {
	if c.config == nil {
		return config.Config{}
	}
	return c.config.Snapshot()
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
	if err := os.WriteFile(c.configPath, data, 0644); err != nil {
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
		SupportedScanFormats:  append([]string{}, config.SupportedScanFormats...),
		SupportedLogLevels:    append([]string{}, config.SupportedLogLevels...),
		DefaultScanFormats:    config.DefaultScanFormatsCSV,
		DefaultScanInterval:   config.DefaultScanInterval,
		SupportedLLMProviders: []string{"ollama", "openai"},
		SupportedLLMAPIModes:  []string{"responses", "chat_completions"},
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
	case "scan_library", "scan_series", "cleanup_library", "rebuild_index", "rebuild_thumbnails", "scrape", "ai_grouping", "rebuild_book_hashes", "reconcile_koreader_progress":
		return true
	case "refresh_koreader_matching":
		return true
	default:
		return false
	}
}

func (c *Controller) startBroker() {
	for {
		select {
		case s := <-c.newClients:
			c.clients[s] = true
		case s := <-c.defunctClients:
			delete(c.clients, s)
			close(s)
		case msg := <-c.messages:
			for s := range c.clients {
				select {
				case s <- msg:
				default: // 如果客户端积压，抛弃或在此断开逻辑
				}
			}
		}
	}
}

func (c *Controller) startDaemon() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	// 记录各个资料库的上次扫描时间
	lastScan := make(map[int64]time.Time)

	for {
		<-ticker.C

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
				go func(id int64, path string) {
					err := c.scanner.ScanLibrary(context.Background(), id, path, false)
					if err != nil {
						slog.Error("Auto-scan failed", "library_id", id, "error", err)
					}
				}(lib.ID, lib.Path)
			}
		}
	}
}

// PublishEvent 供 Scanner 外部等调用投递事件消息
func (c *Controller) PublishEvent(event string) {
	if c.messages == nil {
		return
	}
	c.messages <- event
}

func (c *Controller) startTask(key, taskType, message string, total int) bool {
	c.taskMutex.Lock()
	defer c.taskMutex.Unlock()

	if c.tasks == nil {
		c.tasks = make(map[string]TaskStatus)
	}

	if existing, ok := c.tasks[key]; ok && existing.Status == "running" {
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
		CanCancel: false,
		Retryable: isRetryableTaskType(taskType),
		StartedAt: now,
		UpdatedAt: now,
		Sequence:  c.taskSeq,
	}
	c.tasks[key] = task
	c.pruneTasksLocked()
	c.publishTaskStatusLocked(task)
	return true
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
	c.tasks[key] = task
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
	c.tasks[key] = task
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
	if task.Total > 0 {
		task.Current = task.Total
	}
	task.UpdatedAt = now
	task.FinishedAt = &now
	c.taskSeq++
	task.Sequence = c.taskSeq
	c.tasks[key] = task
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
	task.UpdatedAt = now
	task.FinishedAt = &now
	c.taskSeq++
	task.Sequence = c.taskSeq
	c.tasks[key] = task
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
	c.taskMutex.Lock()
	defer c.taskMutex.Unlock()

	if c.tasks == nil {
		c.tasks = make(map[string]TaskStatus)
	}

	items := make([]TaskStatus, 0, len(c.tasks))
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

	for _, task := range c.tasks {
		if statusFilter != "" && task.Status != statusFilter {
			continue
		}
		if scopeFilter != "" && task.Scope != scopeFilter {
			continue
		}
		if typeFilter != "" && task.Type != typeFilter {
			continue
		}
		if scopeID != nil {
			if task.ScopeID == nil || *task.ScopeID != *scopeID {
				continue
			}
		}
		if queryFilter != "" {
			haystack := strings.ToLower(task.Key + " " + task.Message + " " + task.Error)
			if !strings.Contains(haystack, queryFilter) {
				continue
			}
		}
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
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	jsonResponse(w, http.StatusOK, items)
}

func (c *Controller) clearTasks(w http.ResponseWriter, r *http.Request) {
	c.taskMutex.Lock()
	defer c.taskMutex.Unlock()

	if c.tasks == nil {
		c.tasks = make(map[string]TaskStatus)
	}

	statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))
	scopeFilter := strings.TrimSpace(r.URL.Query().Get("scope"))
	removed := 0

	for key, task := range c.tasks {
		if statusFilter != "" && task.Status != statusFilter {
			continue
		}
		if scopeFilter != "" && task.Scope != scopeFilter {
			continue
		}
		delete(c.tasks, key)
		removed++
	}

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
		jsonError(w, http.StatusNotFound, "Task not found")
		return
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

func (c *Controller) SetupRoutes(r chi.Router) {
	r.Route("/api", func(r chi.Router) {
		c.setupMihonRoutes(r)

		r.Get("/events", c.sseHandler)
		r.Get("/search", c.searchBooks)
		r.Get("/libraries", c.getLibraries)
		r.Post("/libraries", c.createLibrary)
		r.Put("/libraries/{libraryId}", c.updateLibrary)
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
		r.Route("/series", func(r chi.Router) {
			r.Post("/bulk-update", c.bulkUpdateSeries)
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
			r.Get("/{seriesId}/comicinfo.zip", c.exportSeriesComicInfoArchive)
		})

		r.Route("/books", func(r chi.Router) {
			r.Post("/bulk-progress", c.bulkUpdateBookProgress)
			r.Post("/{bookId}/progress", c.updateBookProgress)
			r.Get("/{bookId}/comicinfo.xml", c.exportBookComicInfo)
			r.Get("/{bookId}/bookmarks", c.listReadingBookmarks)
			r.Post("/{bookId}/bookmarks", c.upsertReadingBookmark)
			r.Delete("/{bookId}/bookmarks/{bookmarkId}", c.deleteReadingBookmark)
			r.Get("/{seriesId}", c.getBooksBySeries)
		})

		r.Route("/tags", func(r chi.Router) {
			r.Get("/all", c.getAllTags)
		})

		r.Route("/authors", func(r chi.Router) {
			r.Get("/all", c.getAllAuthors)
		})

		r.Get("/system/config", c.getSystemConfig)
		r.Get("/system/capabilities", c.getSystemCapabilities)
		r.Post("/system/config", c.updateSystemConfig)
		r.Get("/system/logs", c.getSystemLogs)
		r.Get("/system/tasks", c.listTasks)
		r.Delete("/system/tasks", c.clearTasks)
		r.Post("/system/tasks/{taskKey}/retry", c.retryTask)
		r.Get("/system/koreader", c.getKOReaderSettings)
		r.Get("/system/koreader/accounts", c.listKOReaderAccounts)
		r.Get("/system/koreader/unmatched", c.listKOReaderUnmatched)
		r.Post("/system/koreader", c.updateKOReaderSettings)
		r.Post("/system/koreader/accounts", c.createKOReaderAccount)
		r.Post("/system/koreader/accounts/{accountId}/rotate-key", c.rotateKOReaderAccountKey)
		r.Post("/system/koreader/accounts/{accountId}/toggle", c.toggleKOReaderAccount)
		r.Delete("/system/koreader/accounts/{accountId}", c.deleteKOReaderAccount)
		r.Post("/system/koreader/apply-matching", c.applyKOReaderMatching)
		r.Post("/system/koreader/rebuild-hashes", c.rebuildKOReaderHashes)
		r.Post("/system/koreader/reconcile", c.reconcileKOReaderProgress)
		r.Post("/system/rebuild-index", c.rebuildIndex)
		r.Post("/system/rebuild-thumbnails", c.rebuildThumbnails)
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
		r.Post("/series/{seriesId}/relations", c.createSeriesRelation)
		r.Delete("/relations/{relationId}", c.deleteSeriesRelation)

		// 独立路径，避免与 /books/{seriesId} 通配符冲突
		r.Get("/book-info/{bookId}", c.getBookInfo)
		r.Get("/book-next/{bookId}", c.getNextBook)

		r.Route("/pages", func(r chi.Router) {
			r.Get("/{bookId}", c.getPagesByBook)
			r.Get("/{bookId}/{pageNumber}", c.servePageImage)
		})

		r.Route("/covers", func(r chi.Router) {
			r.Get("/{bookId}", c.serveCoverImage)
		})

		// 通用静态直接下发，适配首卷封面作为系列代表图（支持二级哈希子目录）
		r.Get("/thumbnails/*", func(w http.ResponseWriter, r *http.Request) {
			cfg := c.currentConfig()
			thumbDir := filepath.Join(".", "data", "thumbnails")
			if cfg.Cache.Dir != "" {
				thumbDir = cfg.Cache.Dir
			}
			filename := chi.URLParam(r, "*")
			w.Header().Set("Cache-Control", "public, max-age=31536000")
			http.ServeFile(w, r, filepath.Join(thumbDir, filename))
		})
	})
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

	if c.engine == nil {
		jsonError(w, http.StatusServiceUnavailable, "Search engine not initialized")
		return
	}

	res, err := c.engine.Search(query, target, 20)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Search failed")
		return
	}

	jsonResponse(w, http.StatusOK, res)
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

	if createdLib.ScanMode == "watch" && c.watcher != nil {
		_ = c.watcher.WatchLibrary(createdLib.ID, createdLib.Path)
	}

	// 触发异步扫描任务，不阻塞前端 API 响应
	go func() {
		// 使用独立 context 避免跟随请求自动取消，创建库默认全量
		err := c.scanner.ScanLibrary(context.Background(), createdLib.ID, req.Path, false)
		if err != nil {
			// 在生产环境需要接入日志中心打印
			_ = err
		}
	}()

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
	if !c.startTask(taskKey, "scan_library", fmt.Sprintf("开始扫描资源库: %s", lib.Name), 1) {
		return false
	}
	c.setTaskMetadata(taskKey, map[string]string{"force": strconv.FormatBool(force)}, lib.Name)

	go func() {
		err := c.scanner.ScanLibrary(context.Background(), lib.ID, lib.Path, force)
		if err != nil {
			c.failTaskWithError(taskKey, fmt.Sprintf("资源库扫描失败: %v", err), err.Error())
			return
		}
		c.finishTask(taskKey, fmt.Sprintf("资源库扫描完成: %s", lib.Name))
	}()

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
	if !c.startTask(taskKey, "scan_series", fmt.Sprintf("开始扫描系列 #%d", seriesID), 1) {
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
	c.setTaskMetadata(taskKey, map[string]string{"force": strconv.FormatBool(force)}, scopeName)

	go func() {
		err := c.scanner.ScanSeries(context.Background(), seriesID, force)
		if err != nil {
			slog.Error("ScanSeries Failed", "seriesId", seriesID, "error", err)
			c.failTaskWithError(taskKey, fmt.Sprintf("系列扫描失败: %v", err), err.Error())
			return
		}
		c.finishTask(taskKey, fmt.Sprintf("系列扫描完成 #%d", seriesID))
	}()

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

	go func() {
		err := c.scanner.CleanupLibrary(context.Background(), libraryID)
		if err != nil {
			slog.Error("Failed to cleanup library", "library_id", libraryID, "error", err)
			c.failTaskWithError(taskKey, fmt.Sprintf("资源库清理失败: %v", err), err.Error())
			return
		}
		c.finishTask(taskKey, fmt.Sprintf("资源库清理完成 #%d", libraryID))
	}()

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

	series, total, err := c.store.SearchSeriesPaged(ctx, libID, "", letter, status, tags, authors, int32(limit), int32(offset), sortBy)
	if err != nil {
		slog.Error("SearchSeriesPaged Failed", "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to fetch series")
		return
	}

	if series == nil {
		series = []database.SearchSeriesPagedRow{}
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"items": series,
		"total": total,
		"page":  page,
		"limit": limit,
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

	err = c.store.ExecTx(r.Context(), func(q *database.Queries) error {
		_, err := q.UpdateSeriesMetadata(r.Context(), database.UpdateSeriesMetadataParams{
			Title:        sql.NullString{String: req.Title, Valid: req.Title != ""},
			Summary:      sql.NullString{String: req.Summary, Valid: req.Summary != ""},
			Publisher:    sql.NullString{String: req.Publisher, Valid: req.Publisher != ""},
			Status:       sql.NullString{String: req.Status, Valid: req.Status != ""},
			Rating:       sql.NullFloat64{Float64: req.Rating, Valid: req.Rating > 0},
			Language:     sql.NullString{String: req.Language, Valid: req.Language != ""},
			LockedFields: sql.NullString{String: req.LockedFields, Valid: true},
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

		return nil
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
	Series  database.Series       `json:"series"`
	Books   []database.Book       `json:"books"`
	Tags    []database.Tag        `json:"tags"`
	Authors []database.Author     `json:"authors"`
	Links   []database.SeriesLink `json:"links"`
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

	jsonResponse(w, http.StatusOK, SeriesContextResponse{
		Series:  series,
		Books:   books,
		Tags:    tags,
		Authors: authors,
		Links:   links,
	})
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
	for _, id := range req.BookIDs {
		var page int64 = 1
		validPage := false
		var readAt sql.NullTime

		if req.IsRead {
			book, err := c.store.GetBook(ctx, id)
			if err == nil && book.PageCount > 0 {
				page = book.PageCount
			} else {
				page = 99999 // Fallback
			}
			validPage = true
			readAt = sql.NullTime{Time: time.Now(), Valid: true}
		} else {
			// 标记为未读：清空阅读记录
			validPage = false
			readAt = sql.NullTime{Valid: false}
		}

		err := c.store.UpdateBookProgress(ctx, database.UpdateBookProgressParams{
			LastReadPage: sql.NullInt64{Int64: page, Valid: validPage},
			LastReadAt:   readAt,
			ID:           id,
		})
		if err != nil {
			slog.Error("Failed to bulk update book progress", "book_id", id, "error", err)
		}
		// 记录阅读活动
		if req.IsRead && validPage {
			if err := c.store.LogReadingActivity(ctx, id, int(page)); err != nil {
				slog.Error("Failed to log reading activity", "book_id", id, "error", err)
			}
		}
	}

	jsonResponse(w, http.StatusOK, map[string]string{"message": "Bulk progress update completed"})
}

func (c *Controller) getNextBook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bookID, err := parseID(r, "bookId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid book ID")
		return
	}

	nextBook, err := c.store.GetNextBookInSeries(ctx, bookID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "No next book")
		return
	}

	jsonResponse(w, http.StatusOK, nextBook)
}

type UpdateProgressRequest struct {
	Page int64 `json:"page"`
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

	params := database.UpdateBookProgressParams{
		LastReadPage: sql.NullInt64{Int64: validPage, Valid: true},
		LastReadAt:   sql.NullTime{Time: time.Now(), Valid: true},
		ID:           bookID,
	}

	if err := c.store.UpdateBookProgress(ctx, params); err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to update progress")
		return
	}

	// 记录阅读活动到 reading_activity 表
	if err := c.store.LogReadingActivity(ctx, bookID, int(validPage)); err != nil {
		slog.Error("Failed to log reading activity", "book_id", bookID, "error", err)
	}

	jsonResponse(w, http.StatusOK, map[string]string{"status": "Progress updated"})
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

	item, err := c.store.UpsertReadingBookmark(r.Context(), bookID, page, strings.TrimSpace(req.Note))
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
	if err := c.store.DeleteReadingBookmark(r.Context(), bookID, bookmarkID); err != nil {
		if err == sql.ErrNoRows {
			jsonError(w, http.StatusNotFound, "Bookmark not found")
			return
		}
		jsonError(w, http.StatusInternalServerError, "Failed to delete reading bookmark")
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

	// 注册客户端通道
	messageChan := make(chan string, 16)
	c.newClients <- messageChan

	// 监听从客户端意外断开链接
	notify := r.Context().Done()
	go func() {
		<-notify
		c.defunctClients <- messageChan
	}()

	for {
		msg, open := <-messageChan
		if !open {
			break // 连接已从服务端侧切断
		}
		// 按 SSE 格式写入流
		_, err := w.Write([]byte("data: " + msg + "\n\n"))
		if err != nil {
			break
		}

		// 强制刷新缓冲区使客户端即刻收到
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
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

	arc, err := parser.OpenArchive(book.Path)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to open archive")
		return
	}
	defer arc.Close()

	pagesInfo, err := arc.GetPages()
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

func (c *Controller) getSystemConfig(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, http.StatusOK, c.buildSystemConfigResponse(c.currentConfig()))
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

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"message":    "配置已成功保存。大部分设定会立刻生效。",
		"config":     newCfg,
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
			go c.scanner.ScanLibrary(context.Background(), lib.ID, lib.Path, true)
		}
	}
}

func (c *Controller) launchRebuildIndexTask() error {
	if c.engine == nil {
		return fmt.Errorf("search engine not initialized")
	}
	if !c.startTask("rebuild_index", "rebuild_index", "开始重建搜索索引", 1) {
		return fmt.Errorf("task already running")
	}
	c.setTaskMetadata("rebuild_index", nil, "系统")

	cfg := c.currentConfig()
	dataPath := filepath.Dir(cfg.Database.Path)
	if err := c.engine.Rebuild(dataPath); err != nil {
		c.failTaskWithError("rebuild_index", fmt.Sprintf("搜索索引重建失败: %v", err), err.Error())
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
		if strings.Contains(err.Error(), "not initialized") {
			jsonError(w, http.StatusServiceUnavailable, "Search engine not initialized")
			return
		}
		jsonError(w, http.StatusInternalServerError, "Failed to rebuild search index")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"message": "搜索索引已在线重建，并已触发全库重新建立索引。"})
}

func (c *Controller) launchRebuildThumbnailsTask() error {
	if !c.startTask("rebuild_thumbnails", "rebuild_thumbnails", "开始重建缩略图", 1) {
		return fmt.Errorf("task already running")
	}
	c.setTaskMetadata("rebuild_thumbnails", nil, "系统")

	thumbDir := filepath.Join(".", "data", "thumbnails")
	cfg := c.currentConfig()
	if cfg.Cache.Dir != "" {
		thumbDir = cfg.Cache.Dir
	}

	_ = os.RemoveAll(thumbDir)
	_ = os.MkdirAll(thumbDir, 0o755)

	go c.triggerGlobalScan(context.Background())
	c.finishTask("rebuild_thumbnails", "缩略图缓存已清空，后台重建已启动")
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

// getDashboardStats 返回全局统计看板数据
func (c *Controller) getDashboardStats(w http.ResponseWriter, r *http.Request) {
	stats, err := c.store.GetDashboardStats(r.Context())
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

	data, err := c.store.GetActivityHeatmap(r.Context(), weeks)
	if err != nil {
		slog.Error("GetActivityHeatmap failed", "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to get activity heatmap")
		return
	}
	if data == nil {
		data = []database.ActivityDay{}
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
	if items == nil {
		items = []database.RecentReadAllRow{}
	}
	jsonResponse(w, http.StatusOK, items)
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
	ctx := metadata.WithLocale(r.Context(), locale)
	forceRefresh := r.URL.Query().Get("refresh") == "true"

	if !forceRefresh {
		c.recommendationsMutex.RLock()
		cache := c.recommendationsCache[locale]
		cacheTime := c.recommendationsCacheTime[locale]
		if time.Since(cacheTime) < 24*time.Hour && len(cache) > 0 {
			c.recommendationsMutex.RUnlock()
			jsonResponse(w, http.StatusOK, cache)
			return
		}
		c.recommendationsMutex.RUnlock()
	}

	dbCache := c.store.(*database.SqlStore)

	// 1. 获取用户最常看的 10 个标签
	tagRows, err := dbCache.GetTopReadingTags(ctx, 10)
	var userTags []string
	if err == nil {
		for _, tr := range tagRows {
			userTags = append(userTags, tr.Name)
		}
	}

	// 2. 随机获取 20 本可能有兴趣的候选漫画
	candidateRows, err := dbCache.GetCandidateSeriesForAI(ctx, 20)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to fetch candidates from database")
		return
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
		jsonResponse(w, http.StatusOK, []AIRecommendationResponse{}) // 没有候选则不推荐
		return
	}

	// 3. 构建 Provider
	cfg := c.currentConfig()
	provider := metadata.NewAIProvider(cfg.LLM.Provider, cfg.LLM.APIMode, cfg.LLM.BaseURL, cfg.LLM.RequestPath, cfg.LLM.Model, cfg.LLM.APIKey, cfg.LLM.Timeout)

	// 4. 交给 LLM 甄选并产出理
	recList, err := provider.GenerateRecommendations(ctx, userTags, candidates, 3)
	if err != nil {
		slog.Error("LLM failed to generate recommendations", "error", err)
		jsonError(w, http.StatusInternalServerError, "AI inference failed: "+err.Error())
		return
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

	jsonResponse(w, http.StatusOK, finalRecs)
}

// aiGroupingLibrary 扫描资料库中没有集合的系列，利用 LLM 进行智能分组
func (c *Controller) launchAIGroupingTask(libID int64, locale string) bool {
	taskKey := fmt.Sprintf("ai_grouping_library_%d", libID)
	if !c.startTask(taskKey, "ai_grouping", "AI 智能分组开始...", 1) {
		return false
	}
	scopeName := ""
	if lib, err := c.store.GetLibrary(context.Background(), libID); err == nil {
		scopeName = lib.Name
	}
	c.setTaskMetadata(taskKey, nil, scopeName)

	go func(libraryID int64, taskLocale string) {
		ctx := metadata.WithLocale(context.Background(), taskLocale)

		dbCache := c.store.(*database.SqlStore)
		seriesRows, err := dbCache.GetSeriesWithoutCollection(ctx, libraryID)
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
		collections, err := provider.GenerateGrouping(ctx, candidates)
		if err != nil {
			slog.Error("Failed to generate grouping", "error", err)
			c.failTaskWithError(taskKey, fmt.Sprintf("AI 分组失败: %s", err.Error()), err.Error())
			return
		}

		dbObj := dbCache.DB()

		for _, col := range collections {
			if len(col.SeriesIDs) == 0 {
				continue
			}
			res, err := dbObj.ExecContext(ctx, "INSERT INTO collections (name, description) VALUES (?, ?)", col.Name, col.Description)
			if err != nil {
				slog.Error("Insert collection failed", "error", err)
				continue
			}
			newColID, _ := res.LastInsertId()

			for _, sid := range col.SeriesIDs {
				dbObj.ExecContext(ctx, "INSERT OR IGNORE INTO collection_series (collection_id, series_id) VALUES (?, ?)", newColID, sid)
			}
		}

		c.finishTask(taskKey, fmt.Sprintf("AI 智能分组成功完成 (生成了 %d 个合集)", len(collections)))
		c.PublishEvent("refresh")
	}(libID, locale)

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

	jsonResponse(w, http.StatusAccepted, map[string]string{"message": "AI 分组任务已提交至后台"})
}
