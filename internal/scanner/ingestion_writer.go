package scanner

import (
	"context"
	"log/slog"
	"manga-manager/internal/database"
	"manga-manager/internal/seriesmeta"
	"manga-manager/internal/taskcontrol"
	"time"
)

type ingestionStore interface {
	WithScanIngestion(context.Context, func(database.ScanIngestionWrite) error) error
	ListSeriesByLibraryLite(context.Context, int64) ([]database.Series, error)
	RefreshSeriesDerivedData(context.Context, int64) error
}

// ingestionWriter owns one scan's claims, transactional caches and committed follow-up work.
// Only this single consumer can claim old identities; interpreters have no access to it.
type ingestionWriter struct {
	store             ingestionStore
	libraryID         int64
	seriesCache       map[string]database.Series
	lockedFieldsCache map[int64]seriesmeta.ScanLocks
	metrics           *scanMetrics
	progress          *scanProgressReporter
	renames           *renameIndex
	refreshInterval   time.Duration
	onBatch           func(string)
	enqueueCovers     func(context.Context, []coverJob)
}

// claimedArchive adds the writer's optional old-record claim to immutable source facts.
type claimedArchive struct {
	interpretedArchive
	rehome *bookRehome
}

type ingestedBatch struct {
	seriesIDs map[int64]bool
	covers    []coverJob
	rehomes   []claimedArchive
}

// persist publishes cache changes only after the batch transaction commits.
func (w *ingestionWriter) persist(ctx context.Context, batch []claimedArchive) (ingestedBatch, error) {
	libIDInt := w.libraryID
	seriesCache, lockedFieldsCache := w.seriesCache, w.lockedFieldsCache
	var coverJobs []coverJob
	var committedRehomes []claimedArchive
	updatedSeriesIDs := make(map[int64]bool)
	pendingSeries := make(map[string]database.Series)
	pendingLocks := make(map[int64]seriesmeta.ScanLocks)
	err := w.store.WithScanIngestion(ctx, func(q database.ScanIngestionWrite) error {
		for _, res := range batch {
			// 获取或创建/更新归属系列
			var seriesID int64
			existingS, ok := pendingSeries[res.seriesPath]
			if !ok {
				existingS, ok = seriesCache[res.seriesPath]
			}
			if ok {
				seriesID = existingS.ID
			}

			if !ok {
				metadata := seriesmeta.PrepareScanInitial(res.seriesName, res.comicInfo)
				createdSeries, err := q.UpsertSeriesByPath(ctx, database.UpsertSeriesByPathParams{
					LibraryID:    libIDInt,
					Name:         res.seriesName,
					Path:         res.seriesPath,
					Title:        metadata.Title,
					Summary:      metadata.Summary,
					Publisher:    metadata.Publisher,
					Status:       metadata.Status,
					Rating:       metadata.Rating,
					Language:     metadata.Language,
					LockedFields: metadata.LockedFields,
					VolumeCount:  0,
					BookCount:    0,
					TotalPages:   0,
					NameInitial:  metadata.NameInitial,
				})
				if err != nil {
					slog.ErrorContext(ctx, "Failed to create/upsert series", "series_name", res.seriesName, "error", err)
					continue
				}
				seriesID = createdSeries.ID
				// 缓存整行而不是只留 ID：同一次扫描里该系列的后续卷会走增补分支，
				// 要在那里读到刚写下的默认标题与默认锁，而不是把它们冲成空。
				pendingSeries[res.seriesPath] = createdSeries
				pendingLocks[seriesID] = seriesmeta.ParseScanLocks(createdSeries.LockedFields)
			} else {
				if res.comicInfo != nil {
					locks := lockedFieldsCache[seriesID]
					if staged, ok := pendingLocks[seriesID]; ok {
						locks = staged
					}
					metadata := seriesmeta.PrepareScanUpdate(existingS, locks, res.seriesName, res.comicInfo)
					// Identity and counts stay in the scanner's single batch upsert.
					_, _ = q.UpsertSeriesByPath(ctx, database.UpsertSeriesByPathParams{
						LibraryID:    libIDInt,
						Name:         res.seriesName,
						Path:         res.seriesPath,
						Title:        metadata.Title,
						Summary:      metadata.Summary,
						Publisher:    metadata.Publisher,
						Status:       metadata.Status,
						Rating:       metadata.Rating,
						Language:     metadata.Language,
						LockedFields: metadata.LockedFields,
						VolumeCount:  existingS.VolumeCount,
						BookCount:    existingS.BookCount,
						TotalPages:   existingS.TotalPages,
						NameInitial:  metadata.NameInitial,
					})
				}
			}
			input := res.book.forSeries(libIDInt, seriesID)
			updatedSeriesIDs[seriesID] = true

			seriesmeta.AppendScanAssociations(ctx, q, seriesID, res.comicInfo)

			// 改名重连：先把那条「文件已消失」的旧记录改挂到新路径上，紧接着的 UpsertBookByPath
			// 就会经 ON CONFLICT(path) 命中同一行——id 不变，user_book_progress / 书签 / 合集归属
			// 因而全部保留。若更新落空（并发扫描已把该行改走），退回按新书插入即可。
			if res.rehome != nil {
				affected, err := q.RehomeBookPath(ctx, database.RehomeBookPathParams{
					Path:   res.book.Path,
					ID:     res.rehome.bookID,
					Path_2: res.rehome.oldPath,
				})
				switch {
				case err != nil:
					slog.WarnContext(ctx, "Failed to rehome renamed book", "book_id", res.rehome.bookID,
						"old_path", res.rehome.oldPath, "new_path", res.book.Path, "error", err)
				case affected == 0:
					slog.InfoContext(ctx, "Skipped rehoming renamed book because the row moved concurrently",
						"book_id", res.rehome.bookID, "old_path", res.rehome.oldPath)
				default:
					committedRehomes = append(committedRehomes, res)
				}
			}

			// 同路径的书只更新元数据，保留 last_read_page / last_read_at，返回带主键的对象。
			// 两个 upsert 的差别只在 page_count 与 cover_path 该不该被这一趟改写，语义见 sql/query.sql。
			var actualBook database.Book
			var err error
			if res.openedArchive {
				actualBook, err = q.UpsertBookByPath(ctx, input)
			} else {
				actualBook, err = q.UpsertBookByPathKeepingPagesAndCover(ctx,
					database.UpsertBookByPathKeepingPagesAndCoverParams(input))
			}
			if err != nil {
				slog.ErrorContext(ctx, "Failed to upsert book", "path", res.book.Path, "error", err)
				continue
			}
			if err := q.UpdateBookIdentity(ctx, database.UpdateBookIdentityParams{
				ID:                   actualBook.ID,
				FileHash:             string(res.fileHash),
				QuickHash:            string(res.quickHash),
				PathFingerprint:      string(res.pathFingerprint),
				PathFingerprintNoExt: string(res.pathFingerprintNoExt),
			}); err != nil {
				slog.WarnContext(ctx, "Failed to update book identity", "book_id", actualBook.ID, "path", actualBook.Path, "error", err)
			}
			if res.coverCandidate != nil && (!actualBook.CoverPath.Valid || actualBook.CoverPath.String == "") {
				coverJobs = append(coverJobs, coverJob{
					bookID:    actualBook.ID,
					seriesID:  actualBook.SeriesID,
					libraryID: libIDInt,
					candidate: *res.coverCandidate,
				})
			}

		}
		// 读模型刷新不放在批事务内：touched 系列交由 derivedSeries.refresh 节流
		// 与 derivedSeries.drain 在扫描末尾刷新，避免逐批重算大系列。
		return nil
	})
	if err != nil {
		return ingestedBatch{}, err
	}
	for path, series := range pendingSeries {
		seriesCache[path] = series
	}
	for id, locks := range pendingLocks {
		lockedFieldsCache[id] = locks
	}
	return ingestedBatch{seriesIDs: updatedSeriesIDs, covers: coverJobs, rehomes: committedRehomes}, nil
}

func (w *ingestionWriter) consume(ctx context.Context, results <-chan interpretedArchive) {
	w.seriesCache = make(map[string]database.Series)
	w.lockedFieldsCache = make(map[int64]seriesmeta.ScanLocks)
	existing, _ := w.store.ListSeriesByLibraryLite(ctx, w.libraryID)
	for _, series := range existing {
		w.seriesCache[series.Path] = series
		w.lockedFieldsCache[series.ID] = seriesmeta.ParseScanLocks(series.LockedFields)
	}
	dirty := newDerivedSeries(w.store)
	var batch []claimedArchive
	const batchSize = 100
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := taskcontrol.Wait(ctx); err != nil {
			return
		}
		w.progress.publish("writing_database", "", true)
		committed, err := w.persist(ctx, batch)
		if err != nil {
			slog.ErrorContext(ctx, "Batch ingest transaction failed, dropping batch", "book_count", len(batch), "error", err)
			w.metrics.failedArchives.Add(int64(len(batch)))
			w.progress.warn(ScanWarning{Code: WarnBatchIngestDropped, Detail: err.Error(), Count: int64(len(batch))})
		} else {
			w.metrics.rehomedBooks.Add(int64(len(committed.rehomes)))
			for _, res := range committed.rehomes {
				slog.InfoContext(ctx, "Rehomed renamed book", "book_id", res.rehome.bookID, "old_path", res.rehome.oldPath, "new_path", res.book.Path, "match", res.rehome.reason)
			}
			slog.InfoContext(ctx, "Successfully ingested batch", "book_count", len(batch))
			dirty.touch(committed.seriesIDs)
			if w.onBatch != nil {
				w.onBatch("batch_inserted")
			}
			w.progress.publish("queueing_covers", "", true)
			w.enqueueCovers(ctx, committed.covers)
		}
		batch = batch[:0]
	}
	interval := w.refreshInterval
	if interval <= 0 {
		interval = defaultDirtyRefreshInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case res, ok := <-results:
			if !ok {
				flush()
				dirty.drain(ctx, w.metrics)
				if w.onBatch != nil {
					w.onBatch("scan_completed")
				}
				return
			}
			// Filesystem absence checks happen before taking the SQLite write transaction.
			claimed := claimedArchive{interpretedArchive: res, rehome: w.renames.claim(rehomeRequest{path: res.book.Path, size: res.book.Size, modTime: res.book.FileModifiedAt, quickHash: string(res.quickHash), fileHash: string(res.fileHash)})}
			batch = append(batch, claimed)
			if len(batch) >= batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
			dirty.refresh(ctx)
		}
	}
}

func (b interpretedBook) forSeries(libraryID, seriesID int64) database.UpsertBookByPathParams {
	return database.UpsertBookByPathParams{SeriesID: seriesID, LibraryID: libraryID, Name: b.Name, Path: b.Path, Size: b.Size, FileModifiedAt: b.FileModifiedAt, Volume: b.Volume, Title: b.Title, PageCount: b.PageCount, SortNumber: b.SortNumber, CoverPath: b.CoverPath}
}
