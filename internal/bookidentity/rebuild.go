package bookidentity

import (
	"context"
	"log/slog"
	"manga-manager/internal/database"
	"manga-manager/internal/diskwork"
	"manga-manager/internal/storageio"
	"strings"
	"time"
)

const ContentMode = "binary_hash"
const PathMode = "file_path"

// Store exposes only candidates missing evidence and the evidence write.
type Store interface {
	CountBooksMissingIdentity(context.Context, string) (int64, error)
	ListBooksMissingIdentityBatch(context.Context, string, int64, int) ([]database.BookIdentityCandidate, error)
	UpdateBookIdentity(context.Context, database.UpdateBookIdentityParams) error
}

// TaskHandle supplies caller-owned cancellation, pausing and disk admission.
type TaskHandle interface {
	Advance(int, int)
	Checkpoint(context.Context) error
	Disk(context.Context, diskwork.Work, func() error) error
}

type Service struct{ store Store }

func New(store Store) *Service { return &Service{store: store} }

// RebuildOptions fixes the evidence mode and batching for one rebuild.
type RebuildOptions struct {
	// BatchSize 是一批取几本；非正时取 500。
	BatchSize int
	// BatchGap 是每批之后的停顿，用来把后台回填压低到不与别的活抢盘；非正时不停。
	// 停顿先过**暂停闸门**，且本身可被取消。
	BatchGap time.Duration
	// MatchMode is ContentMode or PathMode; empty uses ContentMode. Callers retain
	// the mode selected when admitting a run, independently of later configuration.
	MatchMode string
}

// Rebuild 回填缺**指纹**的书。二进制哈希模式下每本要读完整个文件，那是一次
// **磁盘作业**；路径模式只拼字符串，一次盘都不读。
//
// 指纹算不出来只跳过这一本、推进分页游标；中止只由磁盘作业入口返回的错误——闸门与令牌的错误
// ——决定。游标停住即死循环：批次按 id > afterID 切，坏书排在末尾时同一本会被永远切回来。
//
// task 不得为 nil：过闸门与发起磁盘作业都经它，没有兜底的空实现可用。
func (s *Service) Rebuild(ctx context.Context, opts RebuildOptions, task TaskHandle) (int, int, error) {
	limit := opts.BatchSize
	if limit <= 0 {
		limit = 500
	}
	matchMode := strings.TrimSpace(opts.MatchMode)
	if matchMode == "" {
		matchMode = ContentMode
	}
	missingCount, err := s.store.CountBooksMissingIdentity(ctx, matchMode)
	if err != nil {
		return 0, 0, err
	}

	total := int(missingCount)
	updated := 0
	var afterID int64
	for {
		if err := task.Checkpoint(ctx); err != nil {
			return updated, total, err
		}
		books, err := s.store.ListBooksMissingIdentityBatch(ctx, matchMode, afterID, limit)
		if err != nil {
			return updated, total, err
		}
		if len(books) == 0 {
			break
		}

		for _, book := range books {
			if err := task.Checkpoint(ctx); err != nil {
				return updated, total, err
			}
			params := database.UpdateBookIdentityParams{ID: book.ID}
			if matchMode == ContentMode {
				var fileHash ContentHash
				var hashErr error
				// 闭包只捕获自己的错误：算不出指纹不该中止整个回填。
				err := task.Disk(ctx, diskwork.Work{Kind: storageio.WorkKindIdentityHash, Path: book.Path}, func() error {
					fileHash, hashErr = FingerprintFileContext(ctx, book.Path)
					return nil
				})
				if err != nil {
					return updated, total, err
				}
				if hashErr != nil {
					slog.WarnContext(ctx, "Failed to fingerprint book", "book_id", book.ID, "path", book.Path, "error", hashErr)
					afterID = book.ID
					continue
				}
				params.FileHash = string(fileHash)
			} else {
				params.PathFingerprint = string(FingerprintRelativePath(book.LibraryPath, book.Path, false))
				params.PathFingerprintNoExt = string(FingerprintRelativePath(book.LibraryPath, book.Path, true))
			}

			if err := s.store.UpdateBookIdentity(ctx, params); err != nil {
				return updated, total, err
			}

			updated++
			afterID = book.ID
			task.Advance(updated, total)
		}

		if opts.BatchGap > 0 {
			if err := task.Checkpoint(ctx); err != nil {
				return updated, total, err
			}
			timer := time.NewTimer(opts.BatchGap)
			select {
			case <-timer.C:
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return updated, total, ctx.Err()
			}
		}
	}
	return updated, total, nil
}
