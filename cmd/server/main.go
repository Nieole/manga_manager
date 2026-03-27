package main

import (
	_ "embed"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

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
	"manga-manager/internal/search"
	"manga-manager/web"

	"github.com/fsnotify/fsnotify"
)

func main() {
	// 在最前面初始化记录系统：这里先输出到命令行与 data 文件夹
	if err := logger.Init("data"); err != nil {
		fmt.Printf("Fatal: Logger init failed: %v\n", err)
		os.Exit(1)
	}
	slog.Info("Starting Manga Manager...")

	cfg, err := config.LoadConfig("config.yaml")
	if err != nil {
		slog.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	// 初始化归档句柄重用池与 AI 并发控制参数
	parser.InitPool(cfg.Scanner.ArchivePoolSize)
	images.InitProcessor(cfg.Scanner.MaxAiConcurrency)
	cfgManager := config.NewManager(cfg)

	// 启动配置热重载监听
	go watchConfig("config.yaml", cfgManager)

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
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"https://*", "http://*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	dataPath := filepath.Dir(cfg.Database.Path)
	engine, err := search.NewEngine(dataPath)
	if err != nil {
		slog.Warn("Failed to initialize search engine, continuing without search", "error", err)
	} else {
		defer engine.Close()
	}

	// API 端点挂载
	scan := scanner.NewScanner(store, engine, cfgManager)
	apiController := api.NewController(store, scan, engine, cfgManager, "config.yaml")

	// 连接扫描器的完成回调以向 SSE Broker 抛出刷新消息
	scan.SetBatchCallback(func(action string) {
		apiController.PublishEvent("refresh")
	})

	apiController.SetupRoutes(r)
	apiController.SetupOPDSRoutes(r)

	r.Get("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status": "ok"}`))
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
			w.Header().Set("Content-Type", "text/html")
			w.Write(index)
			return
		}

		if strings.HasSuffix(path, ".css") {
			w.Header().Set("Content-Type", "text/css")
		} else if strings.HasSuffix(path, ".js") {
			w.Header().Set("Content-Type", "application/javascript")
		} else if strings.HasSuffix(path, ".html") {
			w.Header().Set("Content-Type", "text/html")
		} else if strings.HasSuffix(path, ".svg") {
			w.Header().Set("Content-Type", "image/svg+xml")
		}

		w.Write(content)
	})

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	slog.Info("Server listening", "address", addr)
	if err := http.ListenAndServe(addr, r); err != nil {
		slog.Error("Server stopped", "error", err)
		os.Exit(1)
	}
}

func watchConfig(path string, cfgManager *config.Manager) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		slog.Error("Failed to create config watcher", "error", err)
		return
	}
	defer watcher.Close()

	err = watcher.Add(path)
	if err != nil {
		slog.Error("Failed to add config path to watcher", "path", path, "error", err)
		return
	}

	slog.Info("Config hot-reload watcher started", "path", path)

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			// 某些编辑器（如 vim）保存时会触发 Rename 或 Remove 后再 Create，
			// 所以监听 Write 和 Create 两个事件更稳健。
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
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

				slog.Info("Config hot-reload applied successfully",
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
