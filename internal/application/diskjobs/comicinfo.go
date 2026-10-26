package diskjobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"manga-manager/internal/database"
	"manga-manager/internal/diskwork"
	"manga-manager/internal/parser"
	"manga-manager/internal/runhandle"
	"manga-manager/internal/seriesmeta"
	"manga-manager/internal/storageio"
	"manga-manager/internal/task"
	"strconv"
)

// ComicInfoLaunch describes the accepted series write without retaining request state.
type ComicInfoLaunch struct {
	Run   task.Launched
	Total int
}

// ComicInfoKey is the series write diagnostic key.
func ComicInfoKey(seriesID int64) string { return fmt.Sprintf("write_comicinfo_series_%d", seriesID) }

// WriteSeriesComicInfo reads the accepted metadata snapshot and owns its background write.
func (c *Service) WriteSeriesComicInfo(ctx context.Context, seriesID int64) (ComicInfoLaunch, error) {
	series, err := c.store.GetSeries(ctx, seriesID)
	if err != nil {
		return ComicInfoLaunch{}, &PreparationError{Stage: "series", Err: err}
	}
	books, err := c.store.ListBooksBySeries(ctx, seriesID)
	if err != nil {
		return ComicInfoLaunch{}, &PreparationError{Stage: "books", Err: err}
	}
	tags, err := c.store.GetTagsForSeries(ctx, seriesID)
	if err != nil {
		return ComicInfoLaunch{}, &PreparationError{Stage: "tags", Err: err}
	}
	authors, err := c.store.GetAuthorsForSeries(ctx, seriesID)
	if err != nil {
		return ComicInfoLaunch{}, &PreparationError{Stage: "authors", Err: err}
	}
	run, err := c.WriteComicInfoPrepared(ctx, series, books, tags, authors)
	if err != nil {
		return ComicInfoLaunch{}, &LaunchError{err}
	}
	return ComicInfoLaunch{Run: run, Total: len(books)}, nil
}

// WriteComicInfoPrepared writes an accepted metadata snapshot using archive disk permits.
func (c *Service) WriteComicInfoPrepared(ctx context.Context, series database.Series, books []database.Book, tags []database.Tag, authors []database.Author) (task.Launched, error) {
	spec := task.RunSpec{
		Key:          ComicInfoKey(series.ID),
		StartCode:    "task.msg.write_comicinfo.start",
		Total:        len(books),
		CanCancel:    true,
		Args:         map[string]string{"series_id": strconv.FormatInt(series.ID, 10)},
		ScopeName:    series.Name,
		CompleteCode: "task.msg.write_comicinfo.complete",
		CancelCode:   "task.msg.write_comicinfo.cancelled",
		FailCode:     "task.msg.write_comicinfo.failed",
	}

	spec.Identity = task.Identity{Type: "write_comicinfo", Scope: task.ScopeSeries, ScopeID: series.ID}
	spec.Trigger = task.TriggerManual
	spec.LibraryIDs = []int64{series.LibraryID}
	paths := make([]string, 0, len(books))
	for _, book := range books {
		paths = append(paths, book.Path)
	}
	if err := c.cover(ctx, &spec, series.LibraryID, paths); err != nil {
		return task.Launched{}, err
	}

	if c.store != nil {
		previous := spec.ValidateAccess
		spec.ValidateAccess = func() error {
			if previous != nil {
				if err := previous(); err != nil {
					return err
				}
			}
			current, err := c.store.GetSeries(context.WithoutCancel(ctx), series.ID)
			if err != nil {
				return err
			}
			if current.LibraryID != series.LibraryID || current.Path != series.Path {
				return task.ErrLibraryUnavailable
			}
			currentBooks, err := c.store.ListBooksBySeries(context.WithoutCancel(ctx), series.ID)
			if err != nil {
				return err
			}
			paths := make(map[int64]string, len(currentBooks))
			for _, book := range currentBooks {
				paths[book.ID] = book.Path
			}
			for _, book := range books {
				if path, ok := paths[book.ID]; !ok || path != book.Path {
					return task.ErrLibraryUnavailable
				}
			}
			return nil
		}
	}

	return c.runs.Start(ctx, spec, func(ctx context.Context, handle *runhandle.Handle) (task.Result, error) {
		written, skipped, failed := 0, 0, 0
		for i, book := range books {
			// 聚合是纯 CPU，留在**磁盘作业**之外：把它夹进令牌的持有区间只会虚占这块盘的归档打开额度。
			info := seriesmeta.BuildComicInfoForBook(book, series, tags, authors)

			// 以这个任务的名义发起**磁盘作业**：归档回写是重 IO（逐本解压重压），不受调度会让
			// 阅读器取页明显卡顿。用 MetadataScan 这一类：它与扫描器读归档同属「后台读写归档」，
			// 共用同一档并发上限。闭包只捕获自己的错误——回写失败是这一本书的结局，中止只由句柄
			// 返回的闸门错误决定。
			// 实况由句柄吸收，但这个任务不报 IO 指标：上报是任务体的选择，这一处没有选它。
			var writeErr error
			if err := handle.Disk(ctx, diskwork.Work{Kind: storageio.WorkKindMetadataScan, Path: book.Path}, func() error {
				writeErr = parser.WriteComicInfoIntoArchive(book.Path, info)
				return nil
			}); err != nil {
				return task.Result{}, err
			}

			switch {
			case writeErr == nil:
				written++
			case errors.Is(writeErr, parser.ErrArchiveNotWritable), errors.Is(writeErr, parser.ErrComicInfoNotMergeable):
				// 无法安全合并的原文档与不可写的格式同属「这本书写不了」，都按跳过计，不算失败。
				skipped++
			default:
				slog.ErrorContext(ctx, "write ComicInfo into archive failed", "book_id", book.ID, "path", book.Path, "error", writeErr)
				failed++
			}

			// 计数、书名与三个结局计数同属这一本书，必须整帧报出：拆开报会被投递水位撕断，
			// 撕开之后是什么样见 runhandle.Handle.Report。
			current, total := i+1, len(books)
			handle.Report(runhandle.Frame{
				Current: &current,
				Total:   &total,
				Phase:   "writing",
				Item:    book.Name,
				Code:    "task.msg.write_comicinfo.progress",
				Params:  map[string]string{"current": strconv.Itoa(current), "total": strconv.Itoa(total)},
				Metrics: map[string]int64{
					"written": int64(written), "skipped": int64(skipped), "failed": int64(failed),
				},
			})
		}
		return task.Result{Params: map[string]string{
			"written": strconv.Itoa(written),
			"skipped": strconv.Itoa(skipped),
			"failed":  strconv.Itoa(failed),
		}}, nil
	})
}

// PreparationError identifies the failed metadata read for compatible adapter responses.
type PreparationError struct {
	Stage string
	Err   error
}

func (e *PreparationError) Error() string { return e.Err.Error() }
func (e *PreparationError) Unwrap() error { return e.Err }
