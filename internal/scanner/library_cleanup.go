package scanner

import (
	"context"
	"fmt"
	"log/slog"
	"manga-manager/internal/database"
	"os"
)

type cleanupStore interface {
	LibraryRelocationProtection(context.Context, int64) (*database.LibraryRelocationProtection, error)
	GetLibrary(context.Context, int64) (database.Library, error)
	ListSeriesByLibraryLite(context.Context, int64) ([]database.Series, error)
	ListBooksBySeries(context.Context, int64) ([]database.Book, error)
	DeleteMissingSeries(context.Context, int64) error
	DeleteMissingBook(context.Context, int64) error
	UpdateSeriesStatistics(context.Context, database.UpdateSeriesStatisticsParams) error
}

// libraryCleanup owns reachability, surviving-book and ratio guards before deletion.
// It never infers loss from a format filter or an uncertain filesystem error.
type libraryCleanup struct{ store cleanupStore }

// cleanupDeleteRatioGuard 熔断阈值：当一次清理判定为“缺失”的系列占全库比例超过该值时，
// 极可能是存储离线、盘符漂移或 UNC 断连造成的整库误判，而非用户真的删了这么多文件。
// 此时中止清理，保护系列、书籍及其阅读进度不被级联删除。
const cleanupDeleteRatioGuard = 0.5

func (c *libraryCleanup) run(ctx context.Context, libraryID int64) error {
	protection, protectionErr := c.store.LibraryRelocationProtection(ctx, libraryID)
	if protectionErr != nil {
		return protectionErr
	}
	if protection != nil {
		return database.ErrLibraryProtected
	}
	// 先探测资料库根目录：存储离线时库内所有系列路径都会“看起来不存在”，
	// 若继续清理会把整库系列连同书籍、阅读进度一并级联删除。任何 Stat 错误
	// （不存在 / 权限 / 超时 / 网络）或非目录都视为不可达，直接中止。
	library, err := c.store.GetLibrary(ctx, libraryID)
	if err != nil {
		return fmt.Errorf("failed to load library %d: %w", libraryID, err)
	}
	if info, statErr := os.Stat(library.Path); statErr != nil || !info.IsDir() {
		return fmt.Errorf("library root %q unreachable, aborting cleanup to avoid mass deletion: %w", library.Path, statErr)
	}

	seriesList, err := c.store.ListSeriesByLibraryLite(ctx, libraryID)
	if err != nil {
		return fmt.Errorf("failed to list series: %w", err)
	}

	// 第一遍：仅收集“确证缺失”（os.IsNotExist）的系列；权限、超时、网络等不确定错误
	// 一律跳过而非删除，避免瞬时 IO 故障被误判为文件消失。
	//
	// 注意 series.Path 并不总对应磁盘上真实存在的目录：库根目录直放的散装归档
	// （<root>/OnePiece.cbz）会被 archiveInterpreter 归到一个合成路径 <root>/OnePiece 下，
	// 该目录从来就不存在。只按目录是否存在判定，会把这类“虚拟系列”连同其书籍与
	// 每用户阅读进度一起 CASCADE 删掉，且每次扫描重建、下次清理再删，进度反复丢失。
	// 删除前须二次确认：目录不存在时再看它的书还在不在磁盘上，只要还有一本在就不删。
	//
	// ctx 早退（本循环 / 删系列 / 删书）是安全的：取消只会造成**少删**。
	// hasSurvivingBook 出错时返回 true（保守留下），missingSeries 只会被低估，
	// DeleteMissingSeries/DeleteMissingBook 拿到已取消的 ctx 直接失败，所以半途停下不会级联删除、
	// 更不会丢阅读进度；剩下的幽灵记录留到下一次清理。
	var missingSeries []database.Series
	for _, series := range seriesList {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, statErr := os.Stat(series.Path); statErr == nil {
			continue
		} else if !os.IsNotExist(statErr) {
			slog.WarnContext(ctx, "Skipping series with ambiguous stat error during cleanup",
				"series_id", series.ID, "path", series.Path, "error", statErr)
			continue
		}
		if c.hasSurvivingBook(ctx, series.ID) {
			slog.DebugContext(ctx, "Series directory missing but books still on disk; treating as virtual series",
				"series_id", series.ID, "path", series.Path)
			continue
		}
		missingSeries = append(missingSeries, series)
	}

	// 熔断：待删系列占比过高，极可能是存储异常而非真实删除。
	if total := len(seriesList); total > 0 && float64(len(missingSeries))/float64(total) > cleanupDeleteRatioGuard {
		return fmt.Errorf("cleanup aborted: %d/%d series appear missing (> %.0f%%), likely a storage issue rather than real deletions",
			len(missingSeries), total, cleanupDeleteRatioGuard*100)
	}

	removedSeries := make(map[int64]bool, len(missingSeries))
	for _, series := range missingSeries {
		if err := ctx.Err(); err != nil {
			return err
		}
		slog.InfoContext(ctx, "Removing missing series", "series_id", series.ID, "path", series.Path)
		if err := c.store.DeleteMissingSeries(ctx, series.ID); err != nil {
			slog.ErrorContext(ctx, "Failed to delete series", "series_id", series.ID, "error", err)
			continue
		}
		removedSeries[series.ID] = true
	}

	// 存活的系列再逐卷清理缺失书籍（同样只在确证缺失时删除）。
	for _, series := range seriesList {
		if err := ctx.Err(); err != nil {
			return err
		}
		if removedSeries[series.ID] {
			continue
		}
		books, err := c.store.ListBooksBySeries(ctx, series.ID)
		if err != nil {
			continue
		}
		booksChanged := false
		for _, book := range books {
			if _, statErr := os.Stat(book.Path); statErr != nil {
				if os.IsNotExist(statErr) {
					slog.InfoContext(ctx, "Removing missing book", "book_id", book.ID, "path", book.Path)
					if err := c.store.DeleteMissingBook(ctx, book.ID); err != nil {
						slog.ErrorContext(ctx, "Failed to delete book", "book_id", book.ID, "error", err)
					}
					booksChanged = true
				} else {
					slog.WarnContext(ctx, "Skipping book with ambiguous stat error during cleanup",
						"book_id", book.ID, "path", book.Path, "error", statErr)
				}
			}
		}
		// 如果有卷被删除，更新系列的统计信息
		if booksChanged {
			_ = c.store.UpdateSeriesStatistics(ctx, database.UpdateSeriesStatisticsParams{
				SeriesID:   series.ID,
				SeriesID_2: series.ID,
				SeriesID_3: series.ID,
				ID:         series.ID,
			})
		}
	}

	slog.InfoContext(ctx, "Library cleanup completed", "library_id", libraryID, "removed_series", len(removedSeries))
	return nil
}

// hasSurvivingBook 报告该系列名下是否还有至少一本书的文件真实存在于磁盘。
// 用于在删除“目录已不存在”的系列之前做二次确认：库根散装文件构成的虚拟系列没有对应目录，
// 但它们的书是真实存在的，不能因为目录探测失败就整串删掉。
// 查询失败时返回 true（fail-safe：宁可留下一个幽灵记录，也不要误删阅读进度）。
func (c *libraryCleanup) hasSurvivingBook(ctx context.Context, seriesID int64) bool {
	books, err := c.store.ListBooksBySeries(ctx, seriesID)
	if err != nil {
		slog.WarnContext(ctx, "Failed to list books while confirming series removal; keeping series",
			"series_id", seriesID, "error", err)
		return true
	}
	for _, book := range books {
		if _, statErr := os.Stat(book.Path); statErr == nil {
			return true
		} else if !os.IsNotExist(statErr) {
			// 权限/超时等不确定错误同样按“可能还在”处理，与上方系列级判定口径一致。
			return true
		}
	}
	return false
}
