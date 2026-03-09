package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
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
	config     *config.Config
	configPath string
	watcher    *scanner.FileWatcher

	// SSE Broker
	clients        map[chan string]bool
	newClients     chan chan string
	defunctClients chan chan string
	messages       chan string
}

func NewController(store database.Store, scan *scanner.Scanner, engine *search.Engine, cfg *config.Config, cfgPath string) *Controller {
	cache, _ := lru.New[string, []byte](256)
	c := &Controller{
		store:          store,
		imageCache:     cache,
		scanner:        scan,
		engine:         engine,
		config:         cfg,
		configPath:     cfgPath,
		clients:        make(map[chan string]bool),
		newClients:     make(chan chan string),
		defunctClients: make(chan chan string),
		messages:       make(chan string, 10),
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
				_ = fw.WatchLibrary(lib.ID, lib.Path)
			}
		}()
	}

	return c
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
			if !lib.AutoScan {
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
	c.messages <- event
}

func (c *Controller) SetupRoutes(r chi.Router) {
	r.Route("/api", func(r chi.Router) {
		r.Get("/events", c.sseHandler)
		r.Get("/search", c.searchBooks)
		r.Get("/libraries", c.getLibraries)
		r.Post("/libraries", c.createLibrary)
		r.Put("/libraries/{libraryId}", c.updateLibrary)
		r.Post("/libraries/{libraryId}/scan", c.scanLibrary)
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
			r.Post("/{seriesId}/rescan", c.scanSeries)
			r.Post("/{seriesId}/scrape", c.scrapeSeriesMetadata)
			r.Get("/{seriesId}/scrape-search", c.scrapeSearchMetadata)
			r.Post("/{seriesId}/scrape-apply", c.applyScrapedMetadata)
			r.Get("/{seriesId}/tags", c.getSeriesTags)
			r.Get("/{seriesId}/authors", c.getSeriesAuthors)
			r.Get("/{seriesId}/links", c.getSeriesLinks)
			r.Get("/{seriesId}/context", c.getSeriesContext)
		})

		r.Route("/books", func(r chi.Router) {
			r.Post("/bulk-progress", c.bulkUpdateBookProgress)
			r.Post("/{bookId}/progress", c.updateBookProgress)
			r.Get("/{seriesId}", c.getBooksBySeries)
		})

		r.Route("/tags", func(r chi.Router) {
			r.Get("/all", c.getAllTags)
		})

		r.Route("/authors", func(r chi.Router) {
			r.Get("/all", c.getAllAuthors)
		})

		r.Get("/system/config", c.getSystemConfig)
		r.Post("/system/config", c.updateSystemConfig)
		r.Get("/system/logs", c.getSystemLogs)
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
			thumbDir := filepath.Join(".", "data", "thumbnails")
			if c.config != nil && c.config.Cache.Dir != "" {
				thumbDir = c.config.Cache.Dir
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

	err = c.store.DeleteLibrary(ctx, libraryID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to delete library")
		return
	}

	jsonResponse(w, http.StatusOK, map[string]string{"status": "deleted"})
}

type CreateLibraryRequest struct {
	Name         string `json:"name"`
	Path         string `json:"path"`
	AutoScan     bool   `json:"auto_scan"`
	ScanInterval int64  `json:"scan_interval"`
	ScanFormats  string `json:"scan_formats"`
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
		req.ScanInterval = 60
	}
	if req.ScanFormats == "" {
		req.ScanFormats = "zip,cbz,rar,cbr,pdf"
	}

	ctx := r.Context()
	libParams := database.CreateLibraryParams{
		Name:         req.Name,
		Path:         req.Path,
		AutoScan:     req.AutoScan,
		ScanInterval: req.ScanInterval,
		ScanFormats:  req.ScanFormats,
	}

	createdLib, err := c.store.CreateLibrary(ctx, libParams)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to create library")
		return
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
	Name         string `json:"name"`
	Path         string `json:"path"`
	AutoScan     bool   `json:"auto_scan"`
	ScanInterval int64  `json:"scan_interval"`
	ScanFormats  string `json:"scan_formats"`
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

	if req.ScanInterval <= 0 {
		req.ScanInterval = 60
	}
	if req.ScanFormats == "" {
		req.ScanFormats = "zip,cbz,rar,cbr,pdf"
	}

	libParams := database.UpdateLibraryParams{
		ID:           libraryID,
		Name:         req.Name,
		Path:         req.Path,
		AutoScan:     req.AutoScan,
		ScanInterval: req.ScanInterval,
		ScanFormats:  req.ScanFormats,
	}

	updatedLib, err := c.store.UpdateLibrary(ctx, libParams)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to update library")
		return
	}

	jsonResponse(w, http.StatusOK, updatedLib)
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

	go func() {
		err := c.scanner.ScanLibrary(context.Background(), lib.ID, lib.Path, isForce)
		if err != nil {
			_ = err
		}
	}()

	jsonResponse(w, http.StatusOK, map[string]string{"status": "Scan initiated"})
}

func (c *Controller) scanSeries(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}

	forceParam := r.URL.Query().Get("force")
	isForce := forceParam == "true"

	go func() {
		err := c.scanner.ScanSeries(context.Background(), seriesID, isForce)
		if err != nil {
			slog.Error("ScanSeries Failed", "seriesId", seriesID, "error", err)
		}
	}()

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
func (c *Controller) cleanupLibrary(w http.ResponseWriter, r *http.Request) {
	libraryID, err := parseID(r, "libraryId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid library ID")
		return
	}

	go func() {
		err := c.scanner.CleanupLibrary(context.Background(), libraryID)
		if err != nil {
			slog.Error("Failed to cleanup library", "library_id", libraryID, "error", err)
		}
	}()

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

	series, total, err := c.store.SearchSeriesPaged(ctx, libID, letter, status, tags, authors, int32(limit), int32(offset), sortBy)
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

func (c *Controller) sseHandler(w http.ResponseWriter, r *http.Request) {
	// 设置 SSE 需要响应头
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// 允许跨域及凭证支持长链接
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// 注册客户端通道
	messageChan := make(chan string)
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
	jsonResponse(w, http.StatusOK, c.config)
}

func (c *Controller) updateSystemConfig(w http.ResponseWriter, r *http.Request) {
	var newCfg config.Config
	if err := json.NewDecoder(r.Body).Decode(&newCfg); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid configuration format")
		return
	}

	data, err := yaml.Marshal(&newCfg)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to marshal configuration")
		return
	}

	if err := os.WriteFile(c.configPath, data, 0644); err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to write configuration file")
		return
	}

	// Update in-memory config
	*c.config = newCfg

	jsonResponse(w, http.StatusOK, map[string]string{"message": "配置已成功保存。得益于热重载技术，大部分核心设定（如 AI 引擎路径、并发进程数）已立等生效，无需重启应用。"})
}

func (c *Controller) testLLMConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string `json:"provider"`
		Endpoint string `json:"endpoint"`
		Model    string `json:"model"`
		APIKey   string `json:"api_key"`
		Prompt   string `json:"prompt"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if req.Prompt == "" {
		req.Prompt = "Hello, this is a test from Manga Manager."
	}

	provider := metadata.NewAIProvider(req.Provider, req.Endpoint, req.Model, req.APIKey)
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

func (c *Controller) rebuildIndex(w http.ResponseWriter, r *http.Request) {
	if c.engine != nil {
		c.engine.Close()
	}

	indexPath := "data/search.bleve"
	_ = os.RemoveAll(indexPath)

	// Since we can't easily recreate the engine here due to the mapping logic being in search package,
	// we will ask the user to restart the application to trigger a fresh engine creation and scan.
	jsonResponse(w, http.StatusOK, map[string]string{"message": "搜索引擎归档已被安全擦除。由于底层引擎句柄已被重置，请您重新启动应用以触发全新索引构建。"})
}

func (c *Controller) rebuildThumbnails(w http.ResponseWriter, r *http.Request) {
	thumbDir := filepath.Join(".", "data", "thumbnails")
	if c.config != nil && c.config.Cache.Dir != "" {
		thumbDir = c.config.Cache.Dir
	}

	// 尽力删除缓存目录
	_ = os.RemoveAll(thumbDir)
	_ = os.MkdirAll(thumbDir, 0755)

	// 发起无视跳过的全局缓存重铸
	go c.triggerGlobalScan(context.Background())

	c.PublishEvent("refresh_thumbnails")
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
	ctx := r.Context()
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
	provider := metadata.NewAIProvider(c.config.LLM.Provider, c.config.LLM.Endpoint, c.config.LLM.Model, c.config.LLM.APIKey)

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

	jsonResponse(w, http.StatusOK, finalRecs)
}

// aiGroupingLibrary 扫描资料库中没有集合的系列，利用 LLM 进行智能分组
func (c *Controller) aiGroupingLibrary(w http.ResponseWriter, r *http.Request) {
	libID, err := parseID(r, "libraryId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid library ID")
		return
	}

	jsonResponse(w, http.StatusAccepted, map[string]string{"message": "AI 分组任务已提交至后台"})

	go func(libraryID int64) {
		ctx := context.Background()
		c.PublishEvent(fmt.Sprintf("library_message:%d:AI 智能分组开始", libraryID))

		dbCache := c.store.(*database.SqlStore)
		seriesRows, err := dbCache.GetSeriesWithoutCollection(ctx, libraryID)
		if err != nil {
			slog.Error("Failed to fetch series for grouping", "error", err)
			c.PublishEvent(fmt.Sprintf("library_error:%d:AI 分组失败 (数据库获取异常)", libraryID))
			return
		}

		if len(seriesRows) == 0 {
			c.PublishEvent(fmt.Sprintf("library_message:%d:目前此库中所有带简介的作品已分组完成", libraryID))
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

		provider := metadata.NewAIProvider(c.config.LLM.Provider, c.config.LLM.Endpoint, c.config.LLM.Model, c.config.LLM.APIKey)
		collections, err := provider.GenerateGrouping(ctx, candidates)
		if err != nil {
			slog.Error("Failed to generate grouping", "error", err)
			c.PublishEvent(fmt.Sprintf("library_error:%d:AI 摘要生成失败: %v", libraryID, err))
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

		c.PublishEvent(fmt.Sprintf("library_message:%d:AI 智能分组成功完成 (生成了 %d 个合集)", libraryID, len(collections)))
	}(libID)
}
