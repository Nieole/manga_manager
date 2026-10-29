package scanner

import (
	"context"
	"database/sql"
	"log/slog"
	"manga-manager/internal/bookidentity"
	"manga-manager/internal/booksort"
	"manga-manager/internal/config"
	"manga-manager/internal/diskwork"
	"manga-manager/internal/parser"
	"manga-manager/internal/storageio"
	"manga-manager/internal/taskcontrol"
	"path/filepath"
	"strings"
	"time"
)

// interpretedBook contains observed source facts, without ownership IDs or SQL intent.
type interpretedBook struct {
	Name           string
	Path           string
	Size           int64
	FileModifiedAt time.Time
	Volume         string
	Title          sql.NullString
	PageCount      int64
	SortNumber     sql.NullFloat64
	CoverPath      sql.NullString
}

// archiveInterpreter materializes source, metadata, cover hints and typed evidence.
// It cannot claim an old identity or write any database rows.
type archiveInterpreter struct {
	rootPath      string
	options       ScanOptions
	metrics       *scanMetrics
	progress      *scanProgressReporter
	currentConfig func() config.Config
	diskWork      *diskwork.Runner
	openArchive   func(string) (parser.Archive, error)
}

func (i *archiveInterpreter) interpret(ctx context.Context, job archiveCandidate) (interpretedArchive, bool) {
	rootPath, opts, metrics, progress := i.rootPath, i.options, i.metrics, i.progress

	select {
	case <-ctx.Done():
		return interpretedArchive{}, false
	default:
	}
	if err := taskcontrol.Wait(ctx); err != nil {
		return interpretedArchive{}, false
	}

	cfg := i.currentConfig()
	var pages []parser.PageMetadata
	var cInfo *parser.ComicInfo
	// 令牌覆盖归档 IO 的全程，又不越出它：闭包外那些活不碰这个归档，圈进来只会虚占归档打开的
	// 并发额度；而令牌若拖到它们之后才还，同卷的指纹申领就要自我等待。
	if opts.Profile.opensArchive() {
		progress.publish("reading_metadata", job.path, false)
		archiveWork := diskwork.Work{Kind: storageio.WorkKindMetadataScan, Path: job.path}
		stats, err := i.diskWork.Do(ctx, archiveWork, func() error {
			arc, err := i.openArchive(job.path)
			if err != nil {
				if metrics != nil {
					metrics.failedArchives.Add(1)
				}
				slog.WarnContext(ctx, "Failed to open archive (may be corrupted)", "path", job.path, "error", err)
				progress.itemFailed(job.path, err.Error())
				return err
			}
			defer arc.Close()
			if metrics != nil {
				metrics.openedArchives.Add(1)
			}
			progress.publish("reading_metadata", job.path, false)

			pages, err = arc.GetPages()
			if err != nil {
				if metrics != nil {
					metrics.failedArchives.Add(1)
				}
				slog.WarnContext(ctx, "Failed to scan pages inside archive", "path", job.path, "error", err)
				progress.itemFailed(job.path, err.Error())
				return err
			}

			if opts.Profile.extractsMetadata() {
				if xmlData, err := arc.ReadMetadataFile("ComicInfo.xml"); err == nil {
					if parsed, err := parser.ParseComicInfo(xmlData); err == nil {
						cInfo = parsed
					}
				}
			}
			return nil
		})
		metrics.absorbDiskWork(stats)
		if err != nil {
			return interpretedArchive{}, false
		}
	}

	bookHash := bookCoverHash(job.path, job.info.ModTime(), job.info.Size())
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

	// 尝试解析文件名中的第一个可能代表话数的数字作为自然排序依据，支持 01、第十话 等格式。
	var sortNumber float64 = 0
	if val, ok := booksort.ExtractSortNumber(bookTitle.String); ok {
		sortNumber = val
	}

	// 封面缓存只在扫描 worker 内做轻量命中检查；缺失时交给后台封面队列生成。
	var coverPath sql.NullString
	var coverHint *coverCandidate
	if opts.Profile.extractsMetadata() && len(pages) > 0 {
		if existing := existingThumbnailPath(cfg, bookHash); existing.Valid {
			coverPath = existing
		} else {
			coverHint = &coverCandidate{
				path:      job.path,
				pageName:  pages[0].Name,
				mediaType: pages[0].MediaType,
				bookHash:  bookHash,
			}
		}
	} else if opts.Profile.extractsMetadata() {
		slog.WarnContext(ctx, "No pages found in archive to extract cover", "path", job.path)
	}

	book := interpretedBook{
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
	// 两处指纹都是最干净的**磁盘作业**形状：取令牌 → 读一遍文件 → 立刻归还。闭包只捕获自己的
	// 错误，因为算不出指纹不该中止这本书；中止只由 Do 返回的闸门与令牌错误决定。
	hashWork := diskwork.Work{Kind: storageio.WorkKindIdentityHash, Path: job.path}

	var fileHash bookidentity.ContentHash
	if opts.Profile.computesFullHash(cfg) {
		progress.publish("hashing", job.path, false)
		var hashErr error
		stats, err := i.diskWork.Do(ctx, hashWork, func() error {
			fileHash, hashErr = bookidentity.FingerprintFileContext(ctx, job.path)
			return nil
		})
		metrics.absorbDiskWork(stats)
		if err != nil {
			return interpretedArchive{}, false
		}
		if metrics != nil {
			metrics.hashedFiles.Add(1)
		}
		progress.publish("hashing", job.path, false)
		if hashErr != nil {
			slog.WarnContext(ctx, "Failed to compute book binary fingerprint", "path", job.path, "error", hashErr, "scan_profile", opts.Profile)
		}
	}

	var quickHash bookidentity.QuickHash
	if opts.Profile.computesQuickHash() {
		progress.publish("hashing", job.path, false)
		var hashErr error
		stats, err := i.diskWork.Do(ctx, hashWork, func() error {
			quickHash, hashErr = bookidentity.FingerprintQuickFile(job.path)
			return nil
		})
		metrics.absorbDiskWork(stats)
		if err != nil {
			return interpretedArchive{}, false
		}
		if metrics != nil {
			metrics.hashedFiles.Add(1)
		}
		progress.publish("hashing", job.path, false)
		if hashErr != nil {
			slog.WarnContext(ctx, "Failed to compute quick book fingerprint", "path", job.path, "error", hashErr, "scan_profile", opts.Profile)
		}
	}

	res := interpretedArchive{
		seriesName:           seriesName,
		seriesPath:           seriesPath,
		book:                 book,
		openedArchive:        opts.Profile.opensArchive(),
		coverCandidate:       coverHint,
		comicInfo:            cInfo,
		fileHash:             fileHash,
		quickHash:            quickHash,
		pathFingerprint:      bookidentity.FingerprintRelativePath(rootPath, job.path, false),
		pathFingerprintNoExt: bookidentity.FingerprintRelativePath(rootPath, job.path, true),
	}

	return res, true
}

type interpretedArchive struct {
	seriesName string
	seriesPath string
	book       interpretedBook
	// openedArchive 记下这一趟到底开没开归档，决定入库时用哪个 upsert：只有开过的那趟
	// 才有权改写 page_count 与 cover_path，fast 档位读不出它们，库里那份就是最好的一份。
	// 判据必须跟着这份结果走，不能在入库侧回头问档位——同一次扫描里两者本就一致，
	// 但把它挂在结果上，「这个值是读出来的还是空的」才在一处说清。
	openedArchive        bool
	coverCandidate       *coverCandidate
	comicInfo            *parser.ComicInfo
	fileHash             bookidentity.ContentHash
	quickHash            bookidentity.QuickHash
	pathFingerprint      bookidentity.PathHash
	pathFingerprintNoExt bookidentity.PathHash
}
