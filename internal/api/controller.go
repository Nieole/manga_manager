package api

import (
	"context"
	"crypto/subtle"
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
	"strconv"
	"strings"
	"sync"
	"time"

	"manga-manager/internal/application/configuration"
	"manga-manager/internal/application/diskjobs"
	"manga-manager/internal/application/intelligence"
	"manga-manager/internal/application/libraries"
	"manga-manager/internal/application/maintenance"
	"manga-manager/internal/application/relocating"
	appruntime "manga-manager/internal/application/runtime"
	"manga-manager/internal/application/scanning"
	"manga-manager/internal/bookcontent"
	"manga-manager/internal/config"
	"manga-manager/internal/dashboard"
	"manga-manager/internal/database"
	"manga-manager/internal/diskwork"
	"manga-manager/internal/external"
	"manga-manager/internal/koreader"
	"manga-manager/internal/metadata"
	"manga-manager/internal/organize"
	"manga-manager/internal/proposal"
	"manga-manager/internal/recommendation"
	"manga-manager/internal/relocation"
	"manga-manager/internal/scanner"
	"manga-manager/internal/taskcontrol"

	"github.com/go-chi/chi/v5"
)

type Controller struct {
	*organizationHTTP
	reader *ReaderHTTP
	store  database.Store
	// imageCache 必须按**总字节**限流（见 image_memory_cache.go），不能按条数——
	// 页图大小悬殊，按条数给不出稳定的内存上界，而它只是个加速缓存。
	imageCache *imageMemoryCache
	content    *bookcontent.Service
	// 仪表盘统计缓存已抽成独立组件（stats_cache.go）；Controller 仅持引用，失效经薄委托方法转发。
	stats   *dashboard.Service
	scanner *scanner.Scanner
	config  *config.Manager
	// diskWork 引用应用运行时为唯一任务引擎装配的共享磁盘执行器。
	diskWork       *diskwork.Runner
	koreader       *koreader.Service
	external       *external.Manager
	diskJobs       *diskjobs.Service
	intelligence   *intelligence.Service
	proposals      *proposal.Service
	relocation     *relocation.Service
	configPath     string
	scans          *scanning.Service
	maintenance    *maintenance.Service
	watcher        *scanner.FileWatcher
	watcherInitErr error
	moves          *relocating.Service

	// SSE 事件推送已抽成独立组件（sse_broker.go）；Controller 仅持引用做编排。
	sse *sseBroker

	// AI 阅读推荐缓存已抽成独立组件（recommendation_cache.go，按 locale + TTL + singleflight）；Controller 仅持引用。
	recommendations *recommendation.Service

	// taskEngine 是任务子域的适配层：把启动入口、控制端点与**重启函数**注册表接到
	// internal/task 的领域引擎与 internal/taskstore 的落盘上（它自己不留任务表，理由见它的符号 doc）。
	// HTTP 控制与快照经适配层访问同一任务引擎；实际任务体归应用用例。
	taskEngine *taskEngine

	openPath        func(string) error
	fetcherFactory  func(string) metadata.Fetcher
	searcherFactory func(string) metadata.Searcher

	// 鉴权链路的进程内状态（账户存在性、Basic 凭据缓存、两个失败限流器）
	// 已抽成独立组件（auth_state.go）。
	auth *authState
	// untrustedProtoWarnOnce 保证「忽略了来自不可信对端的 X-Forwarded-Proto」这条告警每进程只打一次：
	// 该头由客户端可控，逐次打日志等于给了一个刷日志的口子。
	untrustedProtoWarnOnce sync.Once

	runtime       *appruntime.Runtime
	process       Lifecycle
	configuration *configuration.Service

	relations         *organize.Relations
	libraryManagement *libraries.Service
}

type TaskRuntime struct {
	Context   context.Context
	Cancel    context.CancelFunc
	PauseGate *taskcontrol.PauseGate
	StartedAt time.Time
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
	FieldEffects []config.FieldEffect       `json:"field_effects"`
	Publication  configuration.Publication  `json:"publication"`
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

// controllerCacheSizes 是各内存 LRU 的容量。抽成参数是为了让白盒测试用极小容量构造，
// 从而能在几条数据内触发淘汰路径，而不必为此复制一份装配逻辑。
type controllerCacheSizes struct {
	// imageBytes 是页图内存缓存的**字节**预算（不是条数）。
	imageBytes     int64
	page           int
	bookPageSource int
}

func defaultControllerCacheSizes() controllerCacheSizes {
	return controllerCacheSizes{imageBytes: defaultImageCacheBytes, page: 128, bookPageSource: 512}
}

// newControllerCore binds HTTP-owned caches and projections to the application runtime graph.
// Consistency recovery finishes before this constructor exposes any handlers.
//
// 白盒测试构造 Controller 必须调用这里，不得手工拼装字段：手工拼装在新增组件字段时容易漏掉，
// 两条路径共用同一份装配后，这类遗漏在结构上不可能出现。
func newControllerCore(store database.Store, scan *scanner.Scanner, cfg *config.Manager, cfgPath string, sizes controllerCacheSizes) *Controller {
	cache := newImageMemoryCache(sizes.imageBytes)
	c := NewTransport(store, cfg, cfgPath).legacy
	c.stats = dashboard.New()
	c.imageCache = cache
	c.scanner = scan
	c.recommendations = recommendation.New(24 * time.Hour)

	c.content = bookcontent.New(func(ctx context.Context, id int64) (bookcontent.Source, error) {
		book, err := c.store.GetBook(ctx, id)
		return bookPageSourceFromBook(book), err
	}, c.currentConfig, cache, sizes.bookPageSource, sizes.page)
	if err := c.assembleRuntime(scan); err != nil {
		panic(err)
	}

	return c
}

// NewController retains embedded callers until the HTTP family migrations remove Controller.
// The production executable uses bootstrap.Open to own construction and startup.
func NewController(store database.Store, scan *scanner.Scanner, cfg *config.Manager, cfgPath string) *Controller {
	c := newControllerCore(store, scan, cfg, cfgPath, defaultControllerCacheSizes())
	if err := c.runtime.Start(context.Background()); err != nil {
		c.Close()
		panic(fmt.Errorf("start application: %w", err))
	}
	return c
}
func (c *Controller) lifecycleDone() <-chan struct{} { return c.process.Done() }
func (c *Controller) runBackground(fn func())        { c.process.Go(fn) }
func (c *Controller) Close() {
	if c.process != nil {
		c.process.Close()
	}
}

// Shutdown drains the HTTP host through the application-owned shutdown sequence.
func (c *Controller) Shutdown(ctx context.Context, drain func(context.Context) error) error {
	return c.process.Shutdown(ctx, drain)
}

func (c *Controller) currentConfig() config.Config {
	if c.config == nil {
		return config.Config{}
	}
	return c.config.Snapshot()
}

// taskSlots 读此刻生效的**运行槽位**上限。
//
// 每次判定现读一遍配置快照，不在装配期取一个数收进引擎：上限在设置里可改，而改完要对
// **新的放行**生效——收成一个数的话，调大上限要重启进程才算数。已经在跑的不受影响：
// 引擎只在准入与放行时读它。
func (c *Controller) taskSlots() int {
	return c.currentConfig().Tasks.RunSlots
}

// taskResumeEnabled 读**可续跑**的全局开关，理由同 taskSlots：设置里可改，现读一遍。
//
// 配置里没写（nil）按**开着**算：归一化本该已经把它补上，这里再收一道是因为算错方向的代价
// 不对称——多恢复一条白名单内的工作只是多跑一次扫描，少恢复一条就是无人值守的机器重启后
// 什么也不做，而那正是这一票要修的。
func (c *Controller) taskResumeEnabled() bool {
	resume := c.currentConfig().Tasks.ResumeAfterRestart
	return resume == nil || *resume
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
	// 也刻意不走 runBackground：那会把这个 goroutine 计入应用后台，于是优雅停机要等
	// 用户关闭文件管理器才能完成。这个 goroutine 的唯一职责就是收尸，进程退出时随之消失即可。
	//
	// （对应地也不用 exec.CommandContext + 超时兜底：那会在超时后杀掉用户正开着的文件管理器。）
	go func() {
		_ = cmd.Wait() // explorer.exe 惯例返回非零退出码，与成功与否无关，丢弃即可
	}()
	return nil
}

// dispatchScheduledScans is the retained scheduling adapter for embedded callers.
func (c *Controller) dispatchScheduledScans(ctx context.Context, now time.Time, last map[int64]time.Time) {
	c.runtime.DispatchScheduledScans(ctx, now, last)
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
	c.setupRoutes(r, c.httpFamilies(), c.administration())
}

func (c *Controller) setupRoutes(r chi.Router, f httpFamilies, admin *administrationHTTP) {
	r.Route("/api", func(r chi.Router) {
		// 多用户会话鉴权（Cookie session + CSRF + 角色）。必须在挂载任何子路由之前 Use，
		// 中间件内部会放行公开鉴权端点（/api/auth/status|setup|login）与阅读协议前缀 /api/mihon/，
		// 首次建管理员前，其余管理端点也必须拒绝访问。
		r.Use(admin.authGate)
		admin.mount(r)
		c.readerHTTP().setupRootRoutes(r)
		c.readerHTTP().setupMihonRoutes(r)
		c.organizationHTTP.setupRootRoutes(r)

		r.Get("/events", c.serveEvents)
		r.Get("/libraries", f.library.getLibraries)
		r.Post("/libraries", f.library.createLibrary)
		r.Put("/libraries/{libraryId}", f.library.updateLibrary)
		r.Post("/libraries/{libraryId}/relocation-plans", f.relocation.createRelocationPlan)
		r.Get("/libraries/{libraryId}/relocation-restoration", f.relocation.getRelocationRestoration)
		r.Post("/libraries/{libraryId}/relocation-restoration/checks", f.relocation.createRestorationCheck)
		r.Post("/libraries/{libraryId}/relocation-restoration/release", f.relocation.releaseRelocationProtection)
		r.Get("/libraries/{libraryId}/relocation-source-roots", f.relocation.relocationSourceRoots)
		r.Get("/libraries/{libraryId}/relocation-plans/latest", f.relocation.latestRelocationPlan)
		r.Get("/relocation-plans/{planId}", f.relocation.getRelocationPlan)
		r.Get("/relocation-plans/{planId}/details", f.relocation.relocationDetails)
		r.Post("/relocation-plans/{planId}/commit", f.relocation.commitRelocationPlan)
		r.Get("/relocation-operations/{operationId}", f.relocation.getRelocationOperation)
		r.Post("/libraries/{libraryId}/scan", f.library.scanLibrary)
		r.Post("/libraries/{libraryId}/external-libraries/session", f.disk.createExternalLibrarySession)
		r.Get("/libraries/{libraryId}/external-libraries/session/{sessionId}", f.disk.getExternalLibrarySession)
		r.Get("/libraries/{libraryId}/external-libraries/session/{sessionId}/series", f.disk.getExternalLibrarySeries)
		r.Post("/libraries/{libraryId}/external-libraries/session/{sessionId}/transfer", f.disk.transferToExternalLibrary)
		r.Post("/libraries/{libraryId}/scrape", f.maintenance.scrapeLibrary)
		r.Post("/libraries/{libraryId}/ai-grouping", c.aiGroupingLibrary)
		r.Post("/libraries/{libraryId}/cleanup", f.library.cleanupLibrary)
		r.Delete("/libraries/{libraryId}", f.library.deleteLibrary)
		r.Get("/metadata/search", c.searchMetadata)
		r.Get("/metadata/providers", c.listProviders)

		r.Route("/series", func(r chi.Router) {
			c.readerHTTP().setupSeriesRoutes(r)
			r.Post("/bulk-update", c.bulkUpdateSeries)
			c.organizationHTTP.setupSeriesRoutes(r)
			r.Post("/{seriesId}/open-dir", c.openSeriesDirectory)
			r.Post("/{seriesId}/rescan", f.library.scanSeries)
			r.Post("/{seriesId}/scrape", c.scrapeSeriesMetadata)
			r.Get("/{seriesId}/scrape-search", c.scrapeSearchMetadata)
			r.Post("/{seriesId}/scrape-apply", c.applyScrapedMetadata)

			r.Put("/{seriesId}/custom-fields", c.replaceSeriesCustomFields)

			r.Get("/{seriesId}/comicinfo.zip", f.disk.exportSeriesComicInfoArchive)
			r.Post("/{seriesId}/comicinfo", f.disk.writeSeriesComicInfo)
		})

		r.Route("/books", func(r chi.Router) {
			c.readerHTTP().setupBooksRoutes(r)
			r.Get("/duplicates", f.removal.getDuplicateBooks)
			r.Post("/remove", f.removal.removeBooks)

			r.Get("/{bookId}/comicinfo.xml", f.disk.exportBookComicInfo)
			r.Post("/{bookId}/comicinfo", f.disk.writeBookComicInfo)
			r.Post("/{bookId}/cover", c.setBookCoverFromPage)
			r.Post("/{bookId}/cover/upload", c.uploadBookCover)
			r.Get("/{bookId}/file", c.serveBookFile)

		})

		r.Route("/tags", func(r chi.Router) {
			c.readerHTTP().setupTagsRoutes(r)

			r.Patch("/{tagId}", c.renameTag)
			r.Post("/{tagId}/merge", c.mergeTag)
			r.Delete("/{tagId}", c.deleteTag)
		})

		r.Route("/authors", func(r chi.Router) {
			c.readerHTTP().setupAuthorsRoutes(r)

		})

		r.Get("/system/storage-io", f.storage.getStorageIODiagnostics)
		r.Get("/system/page-cache", c.getPageCacheStats)
		r.Delete("/system/page-cache", c.clearPageCache)
		r.Get("/system/tasks", f.runs.listTasks)
		r.Delete("/system/tasks", f.runs.clearTasks)
		r.Get("/system/tasks/live", f.runs.getTaskCenterLive)
		r.Get("/system/tasks/summary", f.runs.listTaskSummaries)
		r.Post("/system/tasks/pause-all", f.runs.pauseAllTasks)
		r.Post("/system/tasks/resume-all", f.runs.resumeAllTasks)
		// 重试与禁用作用在**任务**上（前者再发起一次同一件事，后者关掉它的自动发起），
		// 三条一律按**任务 id** 寻址（ADR 0007：**任务键**退出寻址）——禁用是身份上的长期属性，
		// 而外部库那两类的键带着会话 id，同一身份的两次运行键并不相同。
		// 暂停 / 恢复 / 取消作用在**运行**上，按运行 id 寻址——同一个任务此刻可以有两条仍会变化的
		// 运行（一条在跑、一条排队），按任务寻址答不出用户按的是哪一条。
		r.Post("/system/tasks/{taskID}/retry", f.runs.retryTask)
		r.Post("/system/tasks/{taskID}/disable", f.runs.disableTask)
		r.Post("/system/tasks/{taskID}/enable", f.runs.enableTask)
		r.Post("/system/runs/{runID}/pause", f.runs.pauseRun)
		r.Post("/system/runs/{runID}/resume", f.runs.resumeRun)
		r.Post("/system/runs/{runID}/cancel", f.runs.cancelRun)
		// 事件流与**采样**都按**运行**寻址，且**不进推送通道**：详情页打开时才来问这两条。
		r.Get("/system/runs/{runID}/events", f.runs.getRunEvents)
		r.Get("/system/runs/{runID}/samples", f.runs.getRunSamples)
		r.Post("/system/koreader/apply-matching", f.maintenance.applyKOReaderMatching)
		r.Post("/system/koreader/rebuild-hashes", f.maintenance.rebuildKOReaderHashes)
		r.Post("/system/koreader/reconcile", f.maintenance.reconcileKOReaderProgress)
		r.Post("/system/rebuild-index", f.maintenance.rebuildIndex)
		r.Post("/system/rebuild-initials", f.maintenance.rebuildInitials)
		r.Post("/system/rebuild-thumbnails", f.maintenance.rebuildThumbnails)
		r.Post("/system/cleanup-thumbnails", f.maintenance.cleanupThumbnails)
		r.Post("/system/rebuild-file-identities", f.maintenance.rebuildFileIdentities)
		r.Post("/system/batch-scrape", f.maintenance.batchScrapeAllSeries)

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
func (c *libraryHTTP) getLibraries(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	libs, err := c.store.ListLibraries(ctx)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to fetch libraries")
		return
	}

	type libraryStatus struct {
		database.Library
		RelocationProtected bool `json:"relocation_protected"`
	}
	result := make([]libraryStatus, 0, len(libs))
	for _, lib := range libs {
		protection, protectionErr := c.store.LibraryRelocationProtection(ctx, lib.ID)
		if protectionErr != nil {
			jsonError(w, http.StatusInternalServerError, "Unable to read relocation protection")
			return
		}
		result = append(result, libraryStatus{Library: lib, RelocationProtected: protection != nil})
	}
	jsonResponse(w, http.StatusOK, result)
}

func parseID(r *http.Request, param string) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, param), 10, 64)
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

func (c *Controller) getPagesByBook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bookID, err := parseID(r, "bookId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid Book ID")
		return
	}

	release, ok := c.guardBookRead(w, r, bookID)
	if !ok {
		return
	}
	defer release()
	manifest, err := c.content.List(ctx, bookID)
	if err != nil {
		if errors.Is(err, bookcontent.ErrBusy) {
			jsonError(w, http.StatusServiceUnavailable, "Storage is busy")
			return
		}
		if errors.Is(err, bookcontent.ErrSourceMissing) {
			jsonError(w, http.StatusNotFound, "Book not found")
			return
		}
		writeStorageFailure(w, c.diagnoseStorageFailure(ctx, storageTargetFromSource(manifest.Source), err), "list_pages")
		return
	}
	pagesInfo := manifest.Pages
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
