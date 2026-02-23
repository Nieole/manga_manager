package scanner

import (
	"context"
	"database/sql"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/images"
	"manga-manager/internal/parser"
	"manga-manager/internal/search"

	"github.com/google/uuid"
)

type Scanner struct {
	store  database.Store
	engine *search.Engine
	// 批量插入结束后的回调播送机制
	onBatchIngested func(action string)
}

func NewScanner(store database.Store, engine *search.Engine) *Scanner {
	return &Scanner{
		store:  store,
		engine: engine,
	}
}

// SetBatchCallback 允许外部注册事件通知钩子
func (s *Scanner) SetBatchCallback(cb func(string)) {
	s.onBatchIngested = cb
}

type scanJob struct {
	path string
	info os.FileInfo
}

type scanResult struct {
	seriesName string
	seriesPath string
	book       database.CreateBookParams
	pages      []database.CreateBookPageParams
}

// 递归扫描库目录查找漫画包，支持万级归档的跨三阶段流水线极速并发模式
func (s *Scanner) ScanLibrary(ctx context.Context, libraryID string, rootPath string, force bool) error {
	log.Printf("Starting ultra-fast concurrent scan for library [%s] at: %s (force=%v)", libraryID, rootPath, force)

	// Step 0: Pre-load cache for increment scanning
	bookCache := make(map[string]time.Time)

	if !force {
		existingBooks, err := s.store.ListBooksByLibrary(ctx, libraryID)
		if err != nil {
			log.Printf("Failed to load existing books cache: %v", err)
			return err
		}

		for _, b := range existingBooks {
			bookCache[b.Path] = b.FileModifiedAt
		}
	}

	jobs := make(chan scanJob, 1000)
	results := make(chan scanResult, 1000)

	var wg sync.WaitGroup

	// 第 2 阶段：解析工作池 (The Worker Pool)
	// 在此处限制最大同时榨取的数量，保证文件描述符 FD 不枯竭且避免 CPU 长时间锁顿
	numWorkers := runtime.NumCPU() * 2
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				s.workerProcess(ctx, libraryID, rootPath, job, results)
			}
		}()
	}

	// 第 3 阶段：数据库写入器 (The Database Ingester)
	// 利用独占协程开启包含 100+ INSERTs 的大事务以化解 SQLite 在并发模式下的 database is locked
	ingestWg := sync.WaitGroup{}
	ingestWg.Add(1)
	go func() {
		defer ingestWg.Done()
		s.ingestResults(ctx, libraryID, results)
	}()

	// 第 1 阶段：发现者 (The Discoverer)
	// 使用极速的 filepath.WalkDir 替代 filepath.Walk 减少系统软中断
	var walkErr error
	walkErr = filepath.WalkDir(rootPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			log.Printf("Error accessing path %q: %v\n", path, err)
			return nil
		}

		if d.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".cbz" || ext == ".zip" || ext == ".cbr" || ext == ".rar" {
			info, err := d.Info()
			if err != nil {
				return nil
			}

			// 增量拦截：非强制扫描下检查修改时间
			if !force {
				if lastMod, exists := bookCache[path]; exists {
					// 若存在同名记录且时间精确吻合，跳过这本卷的解析派发
					if lastMod.Equal(info.ModTime()) {
						return nil
					}
				}
			}

			select {
			case jobs <- scanJob{path: path, info: info}:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	})

	close(jobs)     // 通知 Workers 没活儿了
	wg.Wait()       // 等待所有 Worker 的解析收尾
	close(results)  // 通知 Ingester 没数据投递了
	ingestWg.Wait() // 等待 Ingester 将批次强刷入磁盘

	log.Printf("Scan completely flushed for library: %s", libraryID)
	return walkErr
}

func (s *Scanner) workerProcess(ctx context.Context, libraryID string, rootPath string, job scanJob, results chan<- scanResult) {
	arc, err := parser.OpenArchive(job.path)
	if err != nil {
		log.Printf("Failed to open archive %s (may be corrupted): %v", job.path, err)
		return
	}
	defer arc.Close()

	pages, err := arc.GetPages()
	if err != nil {
		log.Printf("Failed to scan pages in %s: %v", job.path, err)
		return
	}

	bookID := uuid.New().String()
	baseName := filepath.Base(job.path)
	bookTitle := sql.NullString{
		String: strings.TrimSuffix(baseName, filepath.Ext(baseName)),
		Valid:  true,
	}

	var seriesName, seriesPath string
	relPath, err := filepath.Rel(rootPath, job.path)
	if err == nil {
		parts := strings.Split(relPath, string(filepath.Separator))
		if len(parts) > 1 {
			// 第一级目录作为 Series
			seriesName = parts[0]
			seriesPath = filepath.Join(rootPath, seriesName)
		} else {
			// 如果直接放在资源库根目录，则以去后缀的文件名作为 Series
			seriesName = strings.TrimSuffix(parts[0], filepath.Ext(parts[0]))
			seriesPath = filepath.Join(rootPath, seriesName)
		}
	} else {
		// Fallback
		seriesPath = filepath.Dir(job.path)
		seriesName = filepath.Base(seriesPath)
	}

	// 尝试解析文件名中的第一个可能代表卷号的数字作为自然排序依据 (Komga 默认策略之一)
	var sortNumber float64 = 0
	if matches := regexp.MustCompile(`\d+(\.\d+)?`).FindString(bookTitle.String); matches != "" {
		if val, err := strconv.ParseFloat(matches, 64); err == nil {
			sortNumber = val
		}
	}

	// 尝试生成冷热分离封面缓存图
	var coverPath sql.NullString
	if len(pages) > 0 {
		thumbDir := filepath.Join(".", "data", "thumbnails")
		_ = os.MkdirAll(thumbDir, 0755)

		if pageData, err := arc.ReadPage(pages[0].Name); err == nil {
			if processed, _, err := images.ProcessImage(pageData, pages[0].MediaType, images.ProcessOptions{
				Width:   400, // 提供极佳的海量图片显示分辨率，但不拖慢首次加载
				Quality: 82,  // 质量折中
				Format:  "webp",
			}); err == nil {
				fileName := bookID + ".webp"
				fullPath := filepath.Join(thumbDir, fileName)
				if err := os.WriteFile(fullPath, processed, 0644); err == nil {
					coverPath = sql.NullString{String: fileName, Valid: true}
				}
			}
		}
	}

	book := database.CreateBookParams{
		ID:             bookID,
		LibraryID:      libraryID,
		Name:           baseName,
		Path:           job.path,
		Size:           job.info.Size(),
		FileModifiedAt: job.info.ModTime(),
		Title:          bookTitle,
		PageCount:      int64(len(pages)),
		SortNumber:     sql.NullFloat64{Float64: sortNumber, Valid: true},
		CoverPath:      coverPath,
	}

	var pageParams []database.CreateBookPageParams
	for i, page := range pages {
		pageParams = append(pageParams, database.CreateBookPageParams{
			ID:        uuid.New().String(),
			BookID:    bookID,
			FileName:  page.Name,
			MediaType: page.MediaType,
			Number:    int64(i + 1),
			Size:      page.Size,
		})
	}

	res := scanResult{
		seriesName: seriesName,
		seriesPath: seriesPath,
		book:       book,
		pages:      pageParams,
	}

	select {
	case results <- res:
	case <-ctx.Done():
	}
}

func (s *Scanner) ingestResults(ctx context.Context, libraryID string, results <-chan scanResult) {
	// 系列关系缓存，避免每个文件频繁发起 SELECT 检测
	seriesCache := make(map[string]string)

	// 预加载已有的 Series，使得写入过程中不发生主键重复
	existingSeries, _ := s.store.ListSeriesByLibrary(ctx, libraryID)
	for _, series := range existingSeries {
		seriesCache[series.Path] = series.ID
	}

	var batch []scanResult
	const batchSize = 100 // 每蓄满 100 卷漫画就开启一次写事务

	flush := func() {
		if len(batch) == 0 {
			return
		}

		err := s.store.ExecTx(ctx, func(q *database.Queries) error {
			for _, res := range batch {
				// 获取或创建归属系列
				seriesID, ok := seriesCache[res.seriesPath]
				if !ok {
					seriesID = uuid.New().String()
					_, err := q.CreateSeries(ctx, database.CreateSeriesParams{
						ID:        seriesID,
						LibraryID: libraryID,
						Name:      res.seriesName,
						Path:      res.seriesPath,
						Title:     sql.NullString{String: res.seriesName, Valid: true},
					})
					if err != nil {
						log.Printf("Failed to create series %q: %v", res.seriesName, err)
						continue
					}
					seriesCache[res.seriesPath] = seriesID
				}
				res.book.SeriesID = seriesID

				// 清理老旧同 Path 书籍记录从而安全地覆盖（避免重写异常）
				_ = q.DeleteBookByPath(ctx, res.book.Path)

				_, err := q.CreateBook(ctx, res.book)
				if err != nil {
					log.Printf("Failed to insert book %q: %v", res.book.Path, err)
					continue
				}
				for _, p := range res.pages {
					_, _ = q.CreateBookPage(ctx, p) // 忽略外键单次报错避免事务塌方
				}

				if s.engine != nil {
					_ = s.engine.IndexBook(res.book, res.seriesName)
				}
			}
			return nil
		})

		if err != nil {
			log.Printf("Batch ingest transaction failed: %v", err)
		} else {
			log.Printf("Successfully ingested batch of %d books.", len(batch))
			if s.onBatchIngested != nil {
				s.onBatchIngested("batch_inserted")
			}
		}

		batch = batch[:0]
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case res, ok := <-results:
			if !ok {
				flush() // 通道被收尾，最后一次刷盘
				if s.onBatchIngested != nil {
					s.onBatchIngested("scan_completed")
				}
				return
			}
			batch = append(batch, res)
			if len(batch) >= batchSize {
				flush()
			}
		case <-ticker.C:
			flush() // 按时间自然聚合，避免低频挂起锁
		}
	}
}
