package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"

	"manga-manager/internal/database"
	"manga-manager/internal/scanner"
	"manga-manager/internal/search"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	lru "github.com/hashicorp/golang-lru/v2"
)

type Controller struct {
	store      database.Store
	imageCache *lru.Cache[string, []byte]
	scanner    *scanner.Scanner
	engine     *search.Engine

	// SSE Broker
	clients        map[chan string]bool
	newClients     chan chan string
	defunctClients chan chan string
	messages       chan string
}

func NewController(store database.Store, scan *scanner.Scanner, engine *search.Engine) *Controller {
	cache, _ := lru.New[string, []byte](256)
	c := &Controller{
		store:          store,
		imageCache:     cache,
		scanner:        scan,
		engine:         engine,
		clients:        make(map[chan string]bool),
		newClients:     make(chan chan string),
		defunctClients: make(chan chan string),
		messages:       make(chan string, 10),
	}

	go c.startBroker()

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
		r.Post("/libraries/{libraryId}/scan", c.scanLibrary)

		r.Route("/series", func(r chi.Router) {
			r.Get("/{libraryId}", c.getSeriesByLibrary)
		})

		r.Route("/books", func(r chi.Router) {
			r.Get("/{seriesId}", c.getBooksBySeries)
			r.Post("/{bookId}/progress", c.updateBookProgress)
		})

		r.Route("/pages", func(r chi.Router) {
			r.Get("/{bookId}", c.getPagesByBook)
			r.Get("/{bookId}/{pageNumber}", c.servePageImage)
		})

		r.Route("/covers", func(r chi.Router) {
			r.Get("/{bookId}", c.serveCoverImage)
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
	if query == "" {
		jsonResponse(w, http.StatusOK, map[string]interface{}{"hits": []interface{}{}})
		return
	}

	if c.engine == nil {
		jsonError(w, http.StatusServiceUnavailable, "Search engine not initialized")
		return
	}

	res, err := c.engine.Search(query, 20)
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

type CreateLibraryRequest struct {
	Name string `json:"name"`
	Path string `json:"path"`
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

	ctx := r.Context()
	newLibID := uuid.New().String()
	libParams := database.CreateLibraryParams{
		ID:   newLibID,
		Name: req.Name,
		Path: req.Path,
	}

	createdLib, err := c.store.CreateLibrary(ctx, libParams)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to create library")
		return
	}

	// 触发异步扫描任务，不阻塞前端 API 响应
	go func() {
		// 使用独立 context 避免跟随请求自动取消，创建库默认全量
		err := c.scanner.ScanLibrary(context.Background(), newLibID, req.Path, false)
		if err != nil {
			// 在生产环境需要接入日志中心打印
			_ = err
		}
	}()

	jsonResponse(w, http.StatusCreated, createdLib)
}

func (c *Controller) scanLibrary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	libID := chi.URLParam(r, "libraryId")

	lib, err := c.store.GetLibrary(ctx, libID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "Library not found")
		return
	}

	go func() {
		// 点击重扫由于需要重推书籍属性甚至修复旧元数据，赋予强扫标识进行 Upsert
		err := c.scanner.ScanLibrary(context.Background(), lib.ID, lib.Path, true)
		if err != nil {
			_ = err
		}
	}()

	jsonResponse(w, http.StatusOK, map[string]string{"status": "Scan initiated"})
}

func (c *Controller) getSeriesByLibrary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	libID := chi.URLParam(r, "libraryId")

	series, err := c.store.ListSeriesByLibrary(ctx, libID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to fetch series")
		return
	}

	if series == nil {
		series = []database.Series{}
	}
	jsonResponse(w, http.StatusOK, series)
}

func (c *Controller) getBooksBySeries(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	seriesID := chi.URLParam(r, "seriesId")

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

type UpdateProgressRequest struct {
	Page int64 `json:"page"`
}

func (c *Controller) updateBookProgress(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bookID := chi.URLParam(r, "bookId")

	var req UpdateProgressRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	params := database.UpdateBookProgressParams{
		LastReadPage: sql.NullInt64{Int64: req.Page, Valid: true},
		ID:           bookID,
	}

	if err := c.store.UpdateBookProgress(ctx, params); err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to update progress")
		return
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
	bookID := chi.URLParam(r, "bookId")

	pages, err := c.store.ListBookPages(ctx, bookID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to fetch pages")
		return
	}

	if pages == nil {
		pages = []database.BookPage{}
	}
	jsonResponse(w, http.StatusOK, pages)
}
