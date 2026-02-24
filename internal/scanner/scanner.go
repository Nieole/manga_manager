package scanner

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"fmt"
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
	book       database.UpsertBookByPathParams
	comicInfo  *parser.ComicInfo
}

// 递归扫描库目录查找漫画包，支持万级归档的跨三阶段流水线极速并发模式
func (s *Scanner) ScanLibrary(ctx context.Context, libraryID string, rootPath string, force bool) error {
	libIDInt, err := strconv.ParseInt(libraryID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid library ID: %v", err)
	}
	log.Printf("Starting ultra-fast concurrent scan for library [%d] at: %s (force=%v)", libIDInt, rootPath, force)

	// Step 0: Pre-load cache for increment scanning
	bookCache := make(map[string]time.Time)

	if !force {
		existingBooks, err := s.store.ListBooksByLibrary(ctx, libIDInt)
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
				s.workerProcess(ctx, libIDInt, rootPath, job, results)
			}
		}()
	}

	// 第 3 阶段：数据库写入器 (The Database Ingester)
	// 利用独占协程开启包含 100+ INSERTs 的大事务以化解 SQLite 在并发模式下的 database is locked
	ingestWg := sync.WaitGroup{}
	ingestWg.Add(1)
	go func() {
		defer ingestWg.Done()
		s.ingestResults(ctx, libIDInt, results)
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

func (s *Scanner) workerProcess(ctx context.Context, libIDInt int64, rootPath string, job scanJob, results chan<- scanResult) {
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

	bookHash := fmt.Sprintf("%x", sha1.Sum([]byte(job.path)))
	baseName := filepath.Base(job.path)
	bookTitle := sql.NullString{
		String: strings.TrimSuffix(baseName, filepath.Ext(baseName)),
		Valid:  true,
	}

	var seriesName, seriesPath string
	var volumeName string
	relPath, err := filepath.Rel(rootPath, job.path)
	if err == nil {
		parts := strings.Split(relPath, string(filepath.Separator))
		if len(parts) > 2 {
			// 第一级目录作为 Series，第二级目录作为 Volume
			seriesName = parts[0]
			seriesPath = filepath.Join(rootPath, seriesName)
			volumeName = parts[1]
		} else if len(parts) > 1 {
			// 第一级目录作为 Series，无 Volume
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
		err = os.MkdirAll(thumbDir, 0755)
		if err != nil {
			log.Printf("Failed to mkdir thumbDir %s: %v", thumbDir, err)
		}

		webpPath := filepath.Join(thumbDir, bookHash+".webp")
		jpgPath := filepath.Join(thumbDir, bookHash+".jpg")

		if _, err := os.Stat(webpPath); err == nil {
			coverPath = sql.NullString{String: bookHash + ".webp", Valid: true}
		} else if _, err := os.Stat(jpgPath); err == nil {
			coverPath = sql.NullString{String: bookHash + ".jpg", Valid: true}
		} else {
			pageData, err := arc.ReadPage(pages[0].Name)
			if err == nil {
				// 优先尝试 WebP（体积小画质高），失败则降级到 JPEG（纯 Go 实现，跨平台无忧）
				var processed []byte
				var fileName string
				webpData, _, webpErr := images.ProcessImage(pageData, pages[0].MediaType, images.ProcessOptions{
					Width: 400, Quality: 82, Format: "webp",
				})
				if webpErr == nil && len(webpData) > 0 {
					processed = webpData
					fileName = bookHash + ".webp"
				} else {
					if webpErr != nil {
						log.Printf("WebP generation failed for %s: %v", job.path, webpErr)
					}
					jpegData, _, jpegErr := images.ProcessImage(pageData, pages[0].MediaType, images.ProcessOptions{
						Width: 400, Quality: 82, Format: "jpeg",
					})
					if jpegErr == nil {
						processed = jpegData
						fileName = bookHash + ".jpg"
					} else {
						log.Printf("JPEG generation failed for %s: %v", job.path, jpegErr)
					}
				}

				if len(processed) > 0 && fileName != "" {
					fullPath := filepath.Join(thumbDir, fileName)
					if err := os.WriteFile(fullPath, processed, 0644); err == nil {
						coverPath = sql.NullString{String: fileName, Valid: true}
					} else {
						log.Printf("Failed to write thumbnail file %s: %v", fullPath, err)
					}
				} else {
					log.Printf("No processed thumbnail data for %s", job.path)
				}
			} else {
				log.Printf("Failed to ReadPage %s from %s: %v", pages[0].Name, job.path, err)
			}
		}
	} else {
		log.Printf("No pages found in %s to extract cover", job.path)
	}

	book := database.UpsertBookByPathParams{
		LibraryID:      libIDInt,
		Name:           baseName,
		Path:           job.path,
		Size:           job.info.Size(),
		FileModifiedAt: job.info.ModTime(),
		Volume:         volumeName,
		Title:          bookTitle,
		PageCount:      int64(len(pages)),
		SortNumber:     sql.NullFloat64{Float64: sortNumber, Valid: true},
		CoverPath:      coverPath,
	}

	// 尝试提取 ComicInfo.xml
	var cInfo *parser.ComicInfo
	if xmlData, err := arc.ReadMetadataFile("ComicInfo.xml"); err == nil {
		if parsed, err := parser.ParseComicInfo(xmlData); err == nil {
			cInfo = parsed
		}
	}

	res := scanResult{
		seriesName: seriesName,
		seriesPath: seriesPath,
		book:       book,
		comicInfo:  cInfo,
	}

	select {
	case results <- res:
	case <-ctx.Done():
	}
}

func (s *Scanner) ingestResults(ctx context.Context, libIDInt int64, results <-chan scanResult) {
	// 系列缓存：路径 -> 原系列对象 (保留原属性能防止 Upsert 被 NULL 覆盖)
	seriesCache := make(map[string]database.ListSeriesByLibraryRow)
	// 锁定字段缓存：ID -> 锁定字段列表 (用 map 提高查找速度)
	lockedFieldsCache := make(map[int64]map[string]bool)

	// 预加载已有的 Series
	existingSeries, _ := s.store.ListSeriesByLibrary(ctx, libIDInt)
	for _, series := range existingSeries {
		seriesCache[series.Path] = series

		lfMap := make(map[string]bool)
		if series.LockedFields.Valid && series.LockedFields.String != "" {
			for _, f := range strings.Split(series.LockedFields.String, ",") {
				lfMap[strings.TrimSpace(f)] = true
			}
		}
		lockedFieldsCache[series.ID] = lfMap
	}

	var batch []scanResult
	const batchSize = 100 // 每蓄满 100 卷漫画就开启一次写事务

	flush := func() {
		if len(batch) == 0 {
			return
		}

		type indexTask struct {
			book       database.Book
			seriesName string
		}
		var tasks []indexTask

		err := s.store.ExecTx(ctx, func(q *database.Queries) error {
			for _, res := range batch {
				// 获取或创建/更新归属系列
				var seriesID int64
				existingS, ok := seriesCache[res.seriesPath]
				if ok {
					seriesID = existingS.ID
				}

				// 提取元数据准备
				var rSummary, rPublisher, rStatus, rLang string
				if res.comicInfo != nil {
					rSummary = res.comicInfo.Summary
					rPublisher = res.comicInfo.Publisher
					rLang = res.comicInfo.LanguageISO
				}
				var rating float64
				if res.comicInfo != nil && res.comicInfo.CommunityRating > 0 {
					rating = float64(res.comicInfo.CommunityRating)
				}

				if !ok {
					// 初次创建
					createdSeries, err := q.UpsertSeriesByPath(ctx, database.UpsertSeriesByPathParams{
						LibraryID: libIDInt,
						Name:      res.seriesName,
						Path:      res.seriesPath,
						Title:     sql.NullString{String: res.seriesName, Valid: true},
						Summary:   sql.NullString{String: rSummary, Valid: rSummary != ""},
						Publisher: sql.NullString{String: rPublisher, Valid: rPublisher != ""},
						Status:    sql.NullString{String: rStatus, Valid: rStatus != ""},
						Rating:    sql.NullFloat64{Float64: rating, Valid: rating > 0},
						Language:  sql.NullString{String: rLang, Valid: rLang != ""},
					})
					if err != nil {
						log.Printf("Failed to create/upsert series %q: %v", res.seriesName, err)
						continue
					}
					seriesID = createdSeries.ID
					// 为了保持下文逻辑，我们塞一个临时的进去
					seriesCache[res.seriesPath] = database.ListSeriesByLibraryRow{ID: seriesID, Path: res.seriesPath}
				} else {
					// 已存在的系列，利用 UpsertSeriesByPath 去更新其累积统计和元数据（仅当有新元数据时增补）
					if res.comicInfo != nil {
						// 检查字段锁定机制
						locks := lockedFieldsCache[seriesID]
						if locks == nil {
							locks = make(map[string]bool)
						}

						// 若被锁定则沿用旧有库中的数据，不被更新的 NULL 覆盖掉
						getStr := func(field string, newVal string) sql.NullString {
							if locks[field] {
								// 从缓存的老对象中读
								switch field {
								case "summary":
									return existingS.Summary
								case "publisher":
									return existingS.Publisher
								case "status":
									return existingS.Status
								case "language":
									return existingS.Language
								}
							}
							return sql.NullString{String: newVal, Valid: newVal != ""}
						}

						getRating := func() sql.NullFloat64 {
							if locks["rating"] {
								return existingS.Rating
							}
							return sql.NullFloat64{Float64: rating, Valid: rating > 0}
						}

						_, _ = q.UpsertSeriesByPath(ctx, database.UpsertSeriesByPathParams{
							LibraryID: libIDInt,
							Name:      res.seriesName,
							Path:      res.seriesPath,
							Title:     sql.NullString{String: res.seriesName, Valid: true},
							Summary:   getStr("summary", rSummary),
							Publisher: getStr("publisher", rPublisher),
							Status:    getStr("status", rStatus),
							Rating:    getRating(),
							Language:  getStr("language", rLang),
							// LockedFields 这里应该保持原样，所以 Valid 设为 false 让 Upsert 判定或传旧值
							// 因为我们的 Upsert 里会用 excluded.locked_fields 覆盖，为了不丢掉我们传回现有的锁。
							LockedFields: sql.NullString{String: getKeys(locks), Valid: true},
						})
					}
				}
				res.book.SeriesID = seriesID

				// 维护系列与标签、作者的多对多关系 (在单卷有新元数据时重刷)
				if res.comicInfo != nil {
					// 为每个卷提取补充，由于事务中，且中间表用 INSERT OR IGNORE, 不会报错。
					tags := res.comicInfo.GetTags()
					for _, t := range tags {
						if inserted, err := q.UpsertTag(ctx, t); err == nil {
							_ = q.LinkSeriesTag(ctx, database.LinkSeriesTagParams{SeriesID: seriesID, TagID: inserted.ID})
						}
					}

					authors := res.comicInfo.GetAuthors()
					for _, a := range authors {
						if inserted, err := q.UpsertAuthor(ctx, database.UpsertAuthorParams{Name: a.Name, Role: a.Role}); err == nil {
							_ = q.LinkSeriesAuthor(ctx, database.LinkSeriesAuthorParams{SeriesID: seriesID, AuthorID: inserted.ID})
						}
					}
				}

				// 使用 Upsert 模式：同路径书籍只更新元数据，保留 last_read_page / last_read_at，返回带主键的对象
				actualBook, err := q.UpsertBookByPath(ctx, res.book)
				if err != nil {
					log.Printf("Failed to upsert book %q: %v", res.book.Path, err)
					continue
				}

				if s.engine != nil {
					tasks = append(tasks, indexTask{book: actualBook, seriesName: res.seriesName})
				}
			}
			return nil
		})

		if err != nil {
			log.Printf("Batch ingest transaction failed: %v", err)
		} else {
			// 在事务外并发建立检索，释放数据库写锁，解救由于更新阅读进度卡死的连接
			for _, t := range tasks {
				_ = s.engine.IndexBook(t.book, t.seriesName)
			}
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

// 提取 locks 字典的所有 key 重组成字符串
func getKeys(m map[string]bool) string {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	return strings.Join(keys, ",")
}
