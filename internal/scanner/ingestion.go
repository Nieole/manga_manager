package scanner

import (
	"context"
	"io/fs"
	"log/slog"
	"manga-manager/internal/config"
	"manga-manager/internal/taskcontrol"
	"path/filepath"
	"strings"
	"sync"
)

// ingestionPlan keeps discovery scope distinct from the library root used to interpret sources.
type ingestionPlan struct {
	libraryID   int64
	libraryRoot string
	scanPath    string
	options     ScanOptions
	formats     config.ScanFormatSet
	existing    map[string]bookScanSnapshot
	renames     *renameIndex
	queueSize   int
}

// ingest owns discovery, bounded interpretation and the sole writer's drain.
// Entry adapters supply a scope snapshot; they never schedule workers or flush SQL.
func (s *Scanner) ingest(ctx context.Context, plan ingestionPlan, metrics *scanMetrics, progress *scanProgressReporter, covers *CoverBatch) error {
	jobs := make(chan archiveCandidate, plan.queueSize)
	results := make(chan interpretedArchive, plan.queueSize)
	interpreter := &archiveInterpreter{rootPath: plan.libraryRoot, options: plan.options, metrics: metrics, progress: progress, currentConfig: s.currentConfig, diskWork: s.diskWork, openArchive: s.openArchive}
	writer := &ingestionWriter{store: s.store, libraryID: plan.libraryID, metrics: metrics, progress: progress, renames: plan.renames, refreshInterval: s.dirtyRefreshInterval, onBatch: s.onBatchIngested, enqueueCovers: func(ctx context.Context, jobs []coverJob) { s.enqueueCoverJobs(ctx, covers, metrics, jobs) }}
	var workers sync.WaitGroup
	workerCount := WorkerCount(s.currentConfig(), plan.libraryRoot, plan.options)
	for n := 0; n < workerCount; n++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for candidate := range jobs {
				interpreted, ok := interpreter.interpret(ctx, candidate)
				if !ok {
					continue
				}
				select {
				case results <- interpreted:
				case <-ctx.Done():
				}
			}
		}()
	}
	writerDone := make(chan struct{})
	go func() { defer close(writerDone); writer.consume(ctx, results) }()
	discovery := archiveDiscovery{rootPath: plan.scanPath, options: plan.options, formats: plan.formats, existing: plan.existing, metrics: metrics, progress: progress}
	progress.publish("discovering", plan.scanPath, true)
	err := discovery.discover(ctx, jobs)
	close(jobs)
	progress.publish("reading_metadata", "", true)
	workers.Wait()
	close(results)
	<-writerDone
	if err == nil {
		err = ctx.Err()
	}
	return err
}

// archiveDiscovery owns format admission and the profile-aware incremental decision.
// Its outputs contain only a path and stat snapshot; archives stay closed here.
type archiveDiscovery struct {
	rootPath string
	options  ScanOptions
	formats  config.ScanFormatSet
	existing map[string]bookScanSnapshot
	metrics  *scanMetrics
	progress *scanProgressReporter
}

func (d *archiveDiscovery) discover(ctx context.Context, jobs chan<- archiveCandidate) error {
	rootPath, opts, formats, metrics, progress, bookCache := d.rootPath, d.options, d.formats, d.metrics, d.progress, d.existing
	return walkDirFollowingSymlinks(ctx, rootPath, func(path string, d fs.DirEntry, err error) error {
		if err := taskcontrol.Wait(ctx); err != nil {
			return err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			metrics.uncertainEntries.Add(1)
			slog.WarnContext(ctx, "Error accessing path", "path", path, "error", err)
			return nil
		}

		if d.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if config.IsSupportedArchiveExtension(ext) && !formats.Matches(path) {
			// 本可入库、但被用户的 scan_formats 排除。只统计这一类，
			// 别把 .txt/.jpg 之类的噪音也算进去，否则这个数字没有诊断价值。
			metrics.formatFilteredArchives.Add(1)
			return nil
		}
		if config.IsSupportedArchiveExtension(ext) {
			metrics.discoveredArchives.Add(1)
			progress.publish("discovering", path, false)
			info, err := d.Info()
			if err != nil {
				metrics.uncertainEntries.Add(1)
				return nil
			}

			// 增量拦截：非强制扫描下，文件没变**且**本档位补不出这本书缺的数据，才跳过解析派发。
			if !opts.Force {
				if existing, exists := bookCache[path]; exists {
					if existing.unchanged(info) && !opts.Profile.backfills(existing) {
						metrics.skippedArchives.Add(1)
						progress.publish("comparing", path, false)
						return nil
					}
				}
			}

			select {
			case jobs <- archiveCandidate{path: path, info: info}:
				metrics.processedArchives.Add(1)
				progress.publish("reading_metadata", path, false)
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	})

}
