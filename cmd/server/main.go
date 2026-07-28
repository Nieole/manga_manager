// 业务说明：本文件是业务实现，属于后端服务启动入口，负责装配配置、数据库、扫描器、HTTP 路由和静态资源服务。
// 它把内部各领域服务连接成可运行进程，是部署、初始化和运行时诊断的入口。
// 维护时应保持启动顺序、资源释放、错误日志和前端资源挂载逻辑清晰可追踪。

package main

import (
	"context"
	"crypto/sha1"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"manga-manager/internal/api"
	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/logger"
	"manga-manager/internal/runtimecfg"
	"manga-manager/internal/scanner"
	"manga-manager/web"
)

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

func main() {
	configPath := flag.String("config", envOrDefault("MANGA_MANAGER_CONFIG", "config.yaml"), "path to the config file (env MANGA_MANAGER_CONFIG)")
	dataDir := flag.String("data-dir", envOrDefault("MANGA_MANAGER_DATA_DIR", "data"), "directory for log files (env MANGA_MANAGER_DATA_DIR)")
	showVersion := flag.Bool("version", false, "print build version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("Manga Manager %s\ncommit: %s\nbuilt: %s\n", Version, Commit, BuildTime)
		return
	}

	// 把配置文件与日志目录解析为绝对路径，使其位置与进程工作目录(cwd)解耦：二者均可经
	// -config/-data-dir 命令行参数或 MANGA_MANAGER_CONFIG/MANGA_MANAGER_DATA_DIR 环境变量覆盖。
	// 数据库与缓存目录本就可经 config 的 database.path / cache.dir 指定绝对路径。
	resolvedConfigPath := absOrSelf(*configPath)
	resolvedDataDir := absOrSelf(*dataDir)

	cfg, err := config.LoadConfig(resolvedConfigPath)
	if err != nil {
		fmt.Printf("Fatal: Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// 在最前面初始化记录系统：这里先输出到命令行与日志目录（默认 ./data，可经 -data-dir 覆盖）
	if err := logger.Init(resolvedDataDir, cfg.Logging.Level); err != nil {
		fmt.Printf("Fatal: Logger init failed: %v\n", err)
		os.Exit(1)
	}
	slog.Info("Starting Manga Manager...", "version", Version, "commit", Commit, "build_time", BuildTime,
		"config", resolvedConfigPath, "data_dir", resolvedDataDir)

	// 初始化配置派生的运行时资源（归档句柄池、AI 并发、日志级别）。与热重载 / API 保存走同一 runtimecfg.Apply，
	// 保证三条生效路径的副作用一致。日志级别此前已由 logger.Init 设过，这里再设一次是幂等的。
	if err := runtimecfg.Apply(cfg); err != nil {
		slog.Warn("Failed to apply runtime config at startup", "error", err)
	}
	cfgManager := config.NewManager(cfg)

	// 启动配置热重载监听
	// 配置热重载监听：拿住句柄以便停机时停掉。此前是裸 `go watchConfig(...)`，
	// 进程活着就永远停不掉——停机排空的 20 秒里仍可能落地一次重载，改写日志级别、
	// 归档句柄池与 AI 并发。
	// 启动时也跑一次值域校验，理由是「热重载会拒绝不合法的配置」这条规则必须对用户可见：
	// 否则一个配置本就非法的实例，表现是「改了文件却怎么都不生效」，而日志里只有一行
	// rejected，用户无从知道问题出在哪、也不知道热重载已经变成哑巴了。
	// 这里刻意不 fatal——保持既有的可启动性，坏值的实际影响由各使用点自行承担。
	if result := config.ValidateConfigValues(cfg); !result.Valid {
		slog.Warn("Configuration has invalid values",
			"path", resolvedConfigPath,
			"issues", config.FormatValidationIssues(result.Issues),
			"hint", "hot-reload will refuse to apply this file until these are fixed")
	}

	cfgWatcher, watchErr := config.StartWatcher(resolvedConfigPath, cfgManager, runtimecfg.Apply)
	if watchErr != nil {
		// 监听建不起来只影响「改文件自动生效」，服务本身照常跑（经设置页保存仍立即生效）。
		slog.Warn("Config hot-reload disabled", "path", resolvedConfigPath, "error", watchErr)
	}

	if err := database.Migrate(cfg.Database.Path); err != nil {
		slog.Error("Failed to migrate database schema", "error", err)
		os.Exit(1)
	}

	store, err := database.NewStore(cfg.Database.Path)
	if err != nil {
		slog.Error("Failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(api.RequestMetrics)
	r.Use(middleware.Recoverer)
	// 请求体闸门要早于任何会读 body 的处理：未鉴权的 /api/auth/login 与 KOReader 协议端点
	// 此前没有任何上限，单个巨型 JSON 即可撑爆内存。
	r.Use(api.RequestBodyLimit)
	r.Use(securityHeaders)
	r.Use(middleware.Compress(5,
		"text/html",
		"text/css",
		"application/javascript",
		"application/json",
		"image/svg+xml",
		"text/plain",
		"application/xml",
	))

	// 通配 Origin 与 AllowCredentials=true 是规范禁止且危险的组合：任意站点都能携带凭据跨域读取
	// 管理接口。存在通配来源时强制关闭凭据；本服务令牌走 X-API-Token/Authorization 头而非 cookie，
	// 关闭凭据不影响其功能。
	allowCredentials := !containsWildcardOrigin(cfg.Server.AllowedOrigins)
	if !allowCredentials {
		slog.Warn("CORS allowed_origins 含通配符，已禁用 AllowCredentials（通配+凭据为危险组合）。生产环境建议改为精确来源白名单。",
			"allowed_origins", cfg.Server.AllowedOrigins)
	}
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.Server.AllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token", "X-API-Token"},
		AllowCredentials: allowCredentials,
		MaxAge:           300,
	}))

	// 启动期安全姿态告警：提示无鉴权裸奔。
	// 注意这里不再有「令牌鉴权已启用」这一档——历史上的共享令牌鉴权已随多用户改造退役
	// （见 internal/api/controller.go 的说明），配置项也已删除。此前 main 仍会在
	// server.auth.enabled=true 时打印「管理 API 令牌鉴权已启用」，而实际上没有任何代码
	// 校验该令牌，管理员会误以为已经加固。
	if cfg.Server.Host == "0.0.0.0" {
		slog.Info("管理 API 监听 0.0.0.0，鉴权由站点账户体系（会话 Cookie + CSRF）提供；" +
			"若前置反向代理，请配置 server.trusted_proxies 以便限流拿到真实客户端 IP。")
	}

	// API 端点挂载
	scan := scanner.NewScanner(store, cfgManager)
	apiController := api.NewController(store, scan, cfgManager, resolvedConfigPath)

	// 注意：扫描完成回调由 NewController 内部注册（handleScannerBatchEvent），它会失效并预热
	// dashboard 统计缓存、并以真实 action 名发 SSE。此处不要再 SetBatchCallback，否则单字段覆盖
	// 语义会把富回调替换成只发 "refresh" 的闭包，导致手动/watch 扫描后统计缓存不刷新且事件名丢失语义。

	apiController.SetupRoutes(r)
	apiController.SetupOPDSRoutes(r)
	apiController.SetupKOReaderRoutes(r)

	r.Get("/api/health", func(w http.ResponseWriter, r *http.Request) {
		// 存活/就绪探测：探测数据库连接，DB 不可达时返回 503，供反向代理/编排器判断实例健康。
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		w.Header().Set("Content-Type", "application/json")
		if err := store.PingContext(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"status": "unavailable", "database": "down"}`))
			return
		}
		w.Write([]byte(`{"status": "ok", "database": "up"}`))
	})

	// Serve the embedded static files
	r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}

		content, err := web.FS.ReadFile("dist" + path)
		if err != nil {
			// Fallback to index.html for SPA routing
			index, err := web.FS.ReadFile("dist/index.html")
			if err != nil {
				w.Write([]byte("Manga Manager API is running. Web builds are not yet embedded. Please run UI building task."))
				return
			}
			writeStaticContent(w, r, "/index.html", index)
			return
		}

		writeStaticContent(w, r, path, content)
	})

	addr := net.JoinHostPort(cfg.Server.Host, strconv.Itoa(cfg.Server.Port))
	srv := &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 15 * time.Second,
		// ReadTimeout 覆盖整个请求（含 body）的读取：没有它，一个慢速客户端可以一直
		// 挂着连接慢慢喂 body 占用 goroutine。取值需容得下最大的封面上传走慢速链路。
		// 不设 WriteTimeout：SSE 与整卷下载都是长写，设了会被中途掐断。
		ReadTimeout: 120 * time.Second,
		IdleTimeout: 60 * time.Second,
	}

	// SSE 是长连接，Shutdown 的「排空在途请求」对它永远不会自然完成——不先切断的话，
	// 只要有一个浏览器标签开着，每次停机都必然跑满 20 秒超时。RegisterOnShutdown 的
	// 回调会在 Shutdown 一开始被同步调用，正好用于此。
	srv.RegisterOnShutdown(apiController.ShutdownNotify)

	// 优雅停机：捕获 SIGINT/SIGTERM，先停止接收新连接并排空在途请求，再收尾后台任务与资源。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("Server listening", "address", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("Server stopped unexpectedly", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	stop() // 恢复默认信号处理：停机过程中再次 Ctrl-C 可强制退出
	slog.Info("Shutdown signal received, draining in-flight requests...")

	// 先停配置监听：排空期间不该再有热重载去改写日志级别 / 归档句柄池 / AI 并发。
	// （此前下方那条「停配置监听」的注释是空话——apiController.Close() 停的是
	// 库目录的 scanner.FileWatcher，与配置监听毫无关系。）
	cfgWatcher.Stop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("Graceful HTTP shutdown failed", "error", err)
	}

	// 收尾后台服务：恢复暂停闸、取消后台任务并等待其退出。
	apiController.Close()
	slog.Info("Shutdown complete")
}

// contentSecurityPolicy 是全站静态 CSP。每一条放宽都对应 web/ 里实地存在的用法，不是模板抄来的；
// 想收紧任何一条之前，请先确认对应用法确实已消失。
//
//   - script-src 'self'：前提是 index.html 里没有内联 <script>。SW 注册脚本已抽到 /register-sw.js；
//     若有人把它挪回内联，CSP 会静默拦掉它 —— SW 永不注册、离线阅读与 PWA 安装随之失效，
//     而且浏览器控制台之外没有任何迹象。TestIndexHTMLHasNoInlineScript 是这条的兜底。
//   - style-src 'unsafe-inline'：@yui540/comimi 在运行时 document.createElement("style") 注入整套
//     阅读器样式，ComimiTheme.tsx 自己也渲染了一段 <style>。前者随库版本变化，用 hash 固定不可维护；
//     去掉这一项，Comimi 阅读主题会彻底掉样式。
//   - img-src blob:：阅读器把取到的页图字节转成 URL.createObjectURL 再喂给 <img>（usePageImageCache.ts）。
//   - img-src data:：ComimiTheme 用 1x1 透明 GIF 的 data URI 占位，真实页图随后异步换上。
//   - img-src https:：刮削搜索结果直接渲染各元数据源（AniList / MangaDex / MyAnimeList / Comic Vine /
//     Bangumi）返回的远端封面 URL，这些主机随 provider 配置变化、无法枚举。
//     只放行 https：provider 若返回 http:// 封面会裂图，但这是刻意的 —— https 部署下混合内容本就被
//     浏览器拦截，加 http: 也无效，正解是由后端代理远端封面（另一个改动）。
//   - connect-src 'self'：axios 不设 baseURL，EventSource 指向 /api/events，全部同源。
//
// 刻意不写的：'unsafe-eval'（构建产物里无 eval / new Function / wasm）、font-src / media-src /
// frame-src 的放宽（无自带字体文件、无音视频、无 iframe）。
//
// 刻意不加 upgrade-insecure-requests：本服务的典型部署是局域网 http:// 直连，
// 升级后所有同源请求都会指向一个并不存在的 https 端口，整站瞬间不可用。
//
// 注意这条头也会随 /sw.js 的响应下发，从而成为 Service Worker 自身的 CSP。当前 sw.js 对跨域请求
// 在 fetch 事件里直接放行不拦截，网络访问全是同源，所以 'self' 不影响现状；但今后若在 sw.js 里
// 加跨域 fetch，会被这条 CSP 拦下，且报错只出现在 SW 的独立控制台里。
const contentSecurityPolicy = "default-src 'self'; " +
	"base-uri 'none'; " +
	"object-src 'none'; " +
	"frame-ancestors 'none'; " +
	"form-action 'self'; " +
	"script-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob: https:; " +
	"connect-src 'self'; " +
	"worker-src 'self'; " +
	"manifest-src 'self'"

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		// frame-ancestors 与上面的 X-Frame-Options: DENY 等价，两者并存是为了兼容只认旧头的客户端。
		w.Header().Set("Content-Security-Policy", contentSecurityPolicy)
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

// containsWildcardOrigin 判断 CORS 来源白名单中是否含通配符（如 http://*、*）。
func containsWildcardOrigin(origins []string) bool {
	for _, o := range origins {
		if strings.Contains(o, "*") {
			return true
		}
	}
	return false
}

func setStaticResponseHeaders(w http.ResponseWriter, path string) {
	if contentType := staticContentType(path); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.Header().Set("Cache-Control", staticCacheControl(path))
}

func writeStaticContent(w http.ResponseWriter, r *http.Request, path string, content []byte) {
	setStaticResponseHeaders(w, path)
	etag := staticETag(path, content)
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Write(content)
}

func staticETag(path string, content []byte) string {
	sum := sha1.Sum(append([]byte(path+"\x00"), content...))
	return `W/"` + fmt.Sprintf("%x", sum) + `"`
}

func staticContentType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" {
		return ""
	}

	// Always fallback to built-in overrides first to prevent Windows registry issues
	switch ext {
	case ".js", ".mjs":
		return "application/javascript"
	case ".css":
		return "text/css; charset=utf-8"
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".json":
		return "application/json"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	case ".wasm":
		return "application/wasm"
	}

	return mime.TypeByExtension(ext)
}

func staticCacheControl(path string) string {
	normalized := strings.TrimPrefix(path, "/")
	if normalized == "" || normalized == "index.html" {
		return "no-cache"
	}

	if strings.HasPrefix(normalized, "assets/") {
		return "public, max-age=31536000, immutable"
	}

	return "no-cache"
}

// envOrDefault 返回环境变量 key 的非空(去空白后)值，否则返回 def。
// 用于让命令行参数的默认值可被对应环境变量覆盖（flag 显式指定时仍优先于 env）。
func envOrDefault(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// absOrSelf 把路径解析为绝对路径；解析失败时原样返回，保证不因此中断启动。
func absOrSelf(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}
