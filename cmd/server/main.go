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
	"manga-manager/internal/images"
	"manga-manager/internal/logger"
	"manga-manager/internal/parser"
	"manga-manager/internal/scanner"
	"manga-manager/web"

	"github.com/fsnotify/fsnotify"
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

	// 初始化归档句柄重用池与 AI 并发控制参数
	parser.InitPool(cfg.Scanner.ArchivePoolSize)
	images.InitProcessor(cfg.Scanner.MaxAiConcurrency)
	cfgManager := config.NewManager(cfg)

	// 启动配置热重载监听
	go watchConfig(resolvedConfigPath, cfgManager)

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

	// 启动期安全姿态告警：提示无鉴权裸奔 / 鉴权配置不完整。
	switch {
	case cfg.Server.Auth.Enabled && cfg.Server.Auth.Token == "":
		slog.Warn("server.auth.enabled=true 但 token 为空，管理 API 鉴权未生效（视为关闭）。请设置 server.auth.token。")
	case cfg.Server.Auth.Enabled:
		slog.Info("管理 API 令牌鉴权已启用")
	case cfg.Server.Host == "0.0.0.0":
		slog.Warn("管理 API 无鉴权且监听 0.0.0.0，仅应用于受信内网或前置反向代理。可设置 server.auth 开启令牌鉴权。")
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
		IdleTimeout:       60 * time.Second,
	}

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

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("Graceful HTTP shutdown failed", "error", err)
	}

	// 收尾后台服务：停配置监听、恢复暂停闸、取消后台任务并等待其退出。
	apiController.Close()
	slog.Info("Shutdown complete")
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
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

func watchConfig(path string, cfgManager *config.Manager) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		slog.Error("Failed to create config watcher", "error", err)
		return
	}
	defer watcher.Close()

	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}
	// 监听所在目录而非文件本身：编辑器保存与本项目的原子写都会以 rename 替换文件，
	// 直接 watch 文件在 Linux 上会因 inode 变化而永久失效（收不到后续事件）。监听目录后
	// 按文件名过滤，即可稳定捕获替换后的新文件。
	dir := filepath.Dir(absPath)
	if err := watcher.Add(dir); err != nil {
		slog.Error("Failed to add config directory to watcher", "dir", dir, "error", err)
		return
	}

	slog.Info("Config hot-reload watcher started", "path", absPath, "watching_dir", dir)

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if filepath.Clean(event.Name) != absPath {
				continue // 只关心目标 config 文件，忽略同目录下的临时文件/数据文件事件
			}
			// 原子替换/编辑器保存表现为 Write、Create 或 Rename-到位，任一都触发重载。
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Rename) {
				slog.Info("Config file changed, re-applying settings...", "event", event.Name)
				newCfg, err := config.LoadConfig(path)
				if err != nil {
					slog.Error("Failed to reload config during hot-swap", "error", err)
					continue
				}

				// 1. 同步更新全局单例/传递的引用
				// 注意：这里更新的是 *newCfg，如果 apiController 持有的是 *cfg 的引用，
				// 我们需要手动将 *newCfg 的值刷入 *currentCfg 指向的内存，
				// 或者确保后端组件统一订阅配置变更。
				// 简单的做法是把新值 Copy 过去 (深拷贝结构体)
				cfgManager.Replace(newCfg)
				currentCfg := cfgManager.Snapshot()

				// 2. 刷新具有受限状态的底层资源池
				parser.InitPool(currentCfg.Scanner.ArchivePoolSize)
				images.InitProcessor(currentCfg.Scanner.MaxAiConcurrency)
				if err := logger.SetLevel(currentCfg.Logging.Level); err != nil {
					slog.Error("Failed to apply logger level during hot-swap", "level", currentCfg.Logging.Level, "error", err)
				}

				slog.Info("Config hot-reload applied successfully",
					"log_level", currentCfg.Logging.Level,
					"pool_size", currentCfg.Scanner.ArchivePoolSize,
					"ai_concurrency", currentCfg.Scanner.MaxAiConcurrency)
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			slog.Error("Config watcher error", "error", err)
		}
	}
}
