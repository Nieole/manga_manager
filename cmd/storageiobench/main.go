// 业务说明：本文件是业务实现，属于存储 IO 性能压测工具，用于评估归档读取、缓存和并发调度的吞吐表现。
// 它服务于扫描、封面提取和阅读器页面加载链路的容量判断。
// 维护时应保证压测参数和真实业务访问模式足够接近，避免误导性能结论。

package main

import (
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/storageio"
)

type benchConfig struct {
	libraryPath       string
	cacheDir          string
	outputPath        string
	label             string
	storageProfile    string
	notes             string
	maxFiles          int
	readSampleMB      int64
	writeFiles        int
	writeSizeKB       int
	coverSamples      int
	coverReadKB       int
	coverWriteKB      int
	coverConcurrency  int
	compareConcurrent bool
	readerProbes      int
	readerProbeKB     int
	backgroundReaders int
}

type archiveFile struct {
	path string
	size int64
}

type scanResult struct {
	files        []archiveFile
	totalFiles   int
	archiveCount int
	totalBytes   int64
	duration     time.Duration
}

type readResult struct {
	name        string
	bytesRead   int64
	fileReads   int
	duration    time.Duration
	concurrency int
	err         error
}

type writeResult struct {
	files    int
	bytes    int64
	duration time.Duration
	dir      string
	err      error
}

type coverResult struct {
	name          string
	items         int
	bytesRead     int64
	bytesWritten  int64
	duration      time.Duration
	concurrency   int
	readPerItem   int
	writePerItem  int
	tempOutputDir string
	err           error
}

type contentionResult struct {
	name            string
	probes          int
	readerBytes     int64
	backgroundBytes int64
	duration        time.Duration
	p50             time.Duration
	p95             time.Duration
	max             time.Duration
	err             error
}

func main() {
	cfg := benchConfig{}
	flag.StringVar(&cfg.libraryPath, "library", "", "library root path to benchmark")
	flag.StringVar(&cfg.cacheDir, "cache", filepath.Join(".", "data", "storage-io-bench"), "cache directory for temporary write benchmark")
	flag.StringVar(&cfg.outputPath, "out", "", "markdown output path; defaults to docs/performance-baselines/<timestamp>-storage-io.md")
	flag.StringVar(&cfg.label, "label", "", "human-readable run label, for example external-hdd or internal-ssd")
	flag.StringVar(&cfg.storageProfile, "profile", "hdd_external", "storage profile under test: auto, ssd, hdd_external, network, or custom")
	flag.StringVar(&cfg.notes, "notes", "", "free-form notes captured in the markdown report")
	flag.IntVar(&cfg.maxFiles, "max-files", 200, "maximum archive files sampled for read benchmarks")
	flag.Int64Var(&cfg.readSampleMB, "read-mb", 256, "maximum MiB to read per read benchmark")
	flag.IntVar(&cfg.writeFiles, "write-files", 256, "number of temporary small files to write")
	flag.IntVar(&cfg.writeSizeKB, "write-kb", 64, "temporary file size in KiB")
	flag.IntVar(&cfg.coverSamples, "cover-samples", 128, "number of archive entries sampled by the cover rebuild simulation")
	flag.IntVar(&cfg.coverReadKB, "cover-read-kb", 512, "archive bytes read per simulated cover build in KiB")
	flag.IntVar(&cfg.coverWriteKB, "cover-write-kb", 96, "thumbnail bytes written per simulated cover build in KiB")
	flag.IntVar(&cfg.coverConcurrency, "cover-concurrency", 1, "concurrency used by the cover rebuild simulation")
	flag.BoolVar(&cfg.compareConcurrent, "compare-concurrency", true, "also measure concurrent reads at 2x and 4x")
	flag.IntVar(&cfg.readerProbes, "reader-probes", 20, "reader latency probes under background read contention")
	flag.IntVar(&cfg.readerProbeKB, "reader-kb", 256, "bytes read by each reader latency probe in KiB")
	flag.IntVar(&cfg.backgroundReaders, "background-readers", 2, "background readers used by contention benchmarks")
	flag.Parse()

	if err := validateBenchConfig(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "storageiobench:", err)
		os.Exit(2)
	}
	if cfg.outputPath == "" {
		slug := sanitizeSlug(cfg.label)
		if slug == "" {
			slug = "storage-io"
		}
		cfg.outputPath = filepath.Join("docs", "performance-baselines", time.Now().Format("2006-01-02T150405")+"-"+slug+".md")
	}

	scan, err := scanArchives(cfg.libraryPath, cfg.maxFiles)
	if err != nil {
		fmt.Fprintln(os.Stderr, "scan:", err)
		os.Exit(1)
	}
	readBudget := cfg.readSampleMB * 1024 * 1024
	readResults := []readResult{readFiles("sequential-read-c1", scan.files, readBudget, 1)}
	if cfg.compareConcurrent {
		readResults = append(readResults,
			readFiles("concurrent-read-c2", scan.files, readBudget, 2),
			readFiles("concurrent-read-c4", scan.files, readBudget, 4),
		)
	}
	write := writeSmallFiles(cfg.cacheDir, cfg.writeFiles, cfg.writeSizeKB*1024)
	cover := simulateCoverRebuild(cfg.cacheDir, scan.files, cfg.coverSamples, cfg.coverReadKB*1024, cfg.coverWriteKB*1024, cfg.coverConcurrency)
	contentionResults := []contentionResult{
		measureReaderContention("reader-latency-unthrottled", scan.files, cfg, false),
		measureReaderContention("reader-latency-low-impact", scan.files, cfg, true),
	}

	report := renderMarkdown(cfg, scan, readResults, write, cover, contentionResults, time.Now())
	if err := os.MkdirAll(filepath.Dir(cfg.outputPath), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "create output dir:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(cfg.outputPath, []byte(report), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write report:", err)
		os.Exit(1)
	}
	fmt.Println(cfg.outputPath)
}

func validateBenchConfig(cfg benchConfig) error {
	if strings.TrimSpace(cfg.libraryPath) == "" {
		return fmt.Errorf("-library is required")
	}
	cfg.storageProfile = config.NormalizeStorageProfile(cfg.storageProfile)
	info, err := os.Stat(cfg.libraryPath)
	if err != nil {
		return fmt.Errorf("library path is not accessible: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("library path must be a directory")
	}
	if cfg.maxFiles <= 0 || cfg.readSampleMB <= 0 || cfg.writeFiles < 0 || cfg.writeSizeKB <= 0 || cfg.coverSamples < 0 || cfg.coverReadKB <= 0 || cfg.coverWriteKB <= 0 || cfg.coverConcurrency <= 0 || cfg.readerProbes < 0 || cfg.readerProbeKB <= 0 || cfg.backgroundReaders <= 0 {
		return fmt.Errorf("max-files, read-mb, write-kb, cover-read-kb, cover-write-kb, cover-concurrency, reader-kb, and background-readers must be positive; write-files, cover-samples, and reader-probes must be zero or positive")
	}
	return nil
}

func scanArchives(root string, maxFiles int) (scanResult, error) {
	started := time.Now()
	result := scanResult{files: make([]archiveFile, 0, maxFiles)}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		result.totalFiles++
		info, err := d.Info()
		if err != nil {
			return err
		}
		result.totalBytes += info.Size()
		if !isArchive(path) {
			return nil
		}
		result.archiveCount++
		if len(result.files) < maxFiles {
			result.files = append(result.files, archiveFile{path: path, size: info.Size()})
		}
		return nil
	})
	result.duration = time.Since(started)
	sort.Slice(result.files, func(i, j int) bool {
		if result.files[i].size == result.files[j].size {
			return result.files[i].path < result.files[j].path
		}
		return result.files[i].size > result.files[j].size
	})
	return result, err
}

func isArchive(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".zip", ".cbz", ".rar", ".cbr", ".7z", ".pdf":
		return true
	default:
		return false
	}
}

func readFiles(name string, files []archiveFile, budget int64, concurrency int) readResult {
	started := time.Now()
	if len(files) == 0 {
		return readResult{name: name, concurrency: concurrency}
	}
	if concurrency < 1 {
		concurrency = 1
	}
	perWorkerBudget := budget / int64(concurrency)
	if perWorkerBudget <= 0 {
		perWorkerBudget = budget
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	result := readResult{name: name, concurrency: concurrency}
	jobs := make(chan archiveFile)
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			localBytes, localFiles, err := readFileSample(jobs, perWorkerBudget)
			mu.Lock()
			defer mu.Unlock()
			result.bytesRead += localBytes
			result.fileReads += localFiles
			if result.err == nil && err != nil {
				result.err = err
			}
		}()
	}
	for _, file := range files {
		jobs <- file
	}
	close(jobs)
	wg.Wait()
	result.duration = time.Since(started)
	return result
}

func readFileSample(jobs <-chan archiveFile, budget int64) (int64, int, error) {
	buf := make([]byte, 1024*1024)
	var total int64
	var fileReads int
	for file := range jobs {
		if total >= budget {
			continue
		}
		n, err := readOneFile(file.path, buf, budget-total)
		total += n
		fileReads++
		if err != nil {
			return total, fileReads, err
		}
	}
	return total, fileReads, nil
}

func readOneFile(path string, buf []byte, maxBytes int64) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	var total int64
	for total < maxBytes {
		limit := int64(len(buf))
		if remaining := maxBytes - total; remaining < limit {
			limit = remaining
		}
		n, err := f.Read(buf[:limit])
		total += int64(n)
		if err == io.EOF {
			return total, nil
		}
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

func writeSmallFiles(cacheDir string, files int, size int) writeResult {
	started := time.Now()
	result := writeResult{files: files, bytes: int64(files * size)}
	if files == 0 {
		return result
	}
	result.dir = filepath.Join(cacheDir, "storage-io-bench-"+time.Now().Format("20060102-150405"))
	if err := os.MkdirAll(result.dir, 0o755); err != nil {
		result.err = err
		return result
	}
	defer os.RemoveAll(result.dir)

	payload := make([]byte, size)
	if _, err := rand.Read(payload); err != nil {
		result.err = err
		return result
	}
	for i := 0; i < files; i++ {
		path := filepath.Join(result.dir, fmt.Sprintf("thumb-%05d.bin", i))
		if err := os.WriteFile(path, payload, 0o644); err != nil {
			result.err = err
			return result
		}
	}
	result.duration = time.Since(started)
	return result
}

func simulateCoverRebuild(cacheDir string, files []archiveFile, samples int, readSize int, writeSize int, concurrency int) coverResult {
	started := time.Now()
	result := coverResult{
		name:         "cover-rebuild-sim",
		concurrency:  concurrency,
		readPerItem:  readSize,
		writePerItem: writeSize,
	}
	if samples == 0 || len(files) == 0 {
		return result
	}
	if concurrency < 1 {
		concurrency = 1
	}
	result.concurrency = concurrency
	result.tempOutputDir = filepath.Join(cacheDir, "storage-io-cover-bench-"+time.Now().Format("20060102-150405"))
	if err := os.MkdirAll(result.tempOutputDir, 0o755); err != nil {
		result.err = err
		return result
	}
	defer os.RemoveAll(result.tempOutputDir)

	payload := make([]byte, writeSize)
	if _, err := rand.Read(payload); err != nil {
		result.err = err
		return result
	}

	jobs := make(chan archiveFile)
	var wg sync.WaitGroup
	var bytesRead atomic.Int64
	var bytesWritten atomic.Int64
	var items atomic.Int64
	var errMu sync.Mutex
	var firstErr error
	setErr := func(err error) {
		errMu.Lock()
		defer errMu.Unlock()
		if firstErr == nil {
			firstErr = err
		}
	}
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, readSize)
			for file := range jobs {
				n, err := readOneFile(file.path, buf, int64(readSize))
				bytesRead.Add(n)
				if err != nil {
					setErr(err)
					continue
				}
				id := items.Add(1)
				path := filepath.Join(result.tempOutputDir, fmt.Sprintf("cover-%05d.bin", id))
				if err := os.WriteFile(path, payload, 0o644); err != nil {
					setErr(err)
					continue
				}
				bytesWritten.Add(int64(len(payload)))
			}
		}()
	}
	for i := 0; i < samples; i++ {
		jobs <- files[i%len(files)]
	}
	close(jobs)
	wg.Wait()
	result.duration = time.Since(started)
	result.items = int(items.Load())
	result.bytesRead = bytesRead.Load()
	result.bytesWritten = bytesWritten.Load()
	result.err = firstErr
	return result
}

func measureReaderContention(name string, files []archiveFile, cfg benchConfig, lowImpact bool) contentionResult {
	result := contentionResult{name: name, probes: cfg.readerProbes, readerBytes: int64(cfg.readerProbeKB * 1024)}
	if cfg.readerProbes <= 0 || len(files) == 0 {
		return result
	}
	started := time.Now()
	ctxDone := make(chan struct{})
	var backgroundBytes atomic.Int64
	var firstErr atomic.Value
	scheduler := storageio.NewScheduler()
	volumeKey := config.VolumeKey(cfg.libraryPath)
	probeSize := cfg.readerProbeKB * 1024
	latencies := make([]time.Duration, 0, cfg.readerProbes)

	var wg sync.WaitGroup
	for i := 0; i < cfg.backgroundReaders; i++ {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			buf := make([]byte, probeSize)
			index := offset
			for {
				select {
				case <-ctxDone:
					return
				default:
				}
				file := files[index%len(files)]
				index++
				release := func() {}
				if lowImpact && volumeKey != "" {
					lease, err := scheduler.Acquire(context.TODO(), storageio.Request{
						VolumeKey:          volumeKey,
						Limit:              1,
						Kind:               storageio.WorkKindMetadataScan,
						PauseWhenReading:   true,
						IdleOnly:           true,
						ReaderIdleDuration: 25 * time.Millisecond,
					})
					if err != nil {
						firstErr.Store(err)
						return
					}
					release = lease.Release
				}
				n, err := readOneFile(file.path, buf, int64(probeSize))
				release()
				backgroundBytes.Add(n)
				if err != nil {
					firstErr.Store(err)
					return
				}
			}
		}(i)
	}

	readerFile := files[0]
	readerBuf := make([]byte, probeSize)
	for i := 0; i < cfg.readerProbes; i++ {
		time.Sleep(25 * time.Millisecond)
		release := func() {}
		startProbe := time.Now()
		if lowImpact && volumeKey != "" {
			lease, err := scheduler.Acquire(context.TODO(), storageio.Request{
				VolumeKey: volumeKey,
				Limit:     1,
				Kind:      storageio.WorkKindReader,
			})
			if err != nil {
				result.err = err
				break
			}
			release = lease.Release
		}
		if _, err := readOneFile(readerFile.path, readerBuf, int64(probeSize)); err != nil {
			release()
			result.err = err
			break
		}
		release()
		latencies = append(latencies, time.Since(startProbe))
	}
	close(ctxDone)
	wg.Wait()
	result.duration = time.Since(started)
	result.backgroundBytes = backgroundBytes.Load()
	if err, ok := firstErr.Load().(error); ok && result.err == nil {
		result.err = err
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	result.probes = len(latencies)
	if len(latencies) > 0 {
		result.p50 = percentile(latencies, 0.50)
		result.p95 = percentile(latencies, 0.95)
		result.max = latencies[len(latencies)-1]
	}
	return result
}

func percentile(values []time.Duration, p float64) time.Duration {
	if len(values) == 0 {
		return 0
	}
	index := int(float64(len(values)-1) * p)
	if index < 0 {
		index = 0
	}
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index]
}

func renderMarkdown(cfg benchConfig, scan scanResult, reads []readResult, write writeResult, cover coverResult, contentionResults []contentionResult, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Storage IO Baseline\n\n")
	fmt.Fprintf(&b, "- Captured at: `%s`\n", now.Format(time.RFC3339))
	fmt.Fprintf(&b, "- Run label: `%s`\n", emptyDash(cfg.label))
	fmt.Fprintf(&b, "- Storage profile under test: `%s`\n", config.NormalizeStorageProfile(cfg.storageProfile))
	fmt.Fprintf(&b, "- Library path: `%s`\n", cfg.libraryPath)
	fmt.Fprintf(&b, "- Library volume: `%s`\n", config.VolumeKey(cfg.libraryPath))
	fmt.Fprintf(&b, "- Cache dir: `%s`\n", cfg.cacheDir)
	fmt.Fprintf(&b, "- Cache volume: `%s`\n", config.VolumeKey(cfg.cacheDir))
	fmt.Fprintf(&b, "- Same volume: `%t`\n\n", config.SameVolume(cfg.libraryPath, cfg.cacheDir))
	if strings.TrimSpace(cfg.notes) != "" {
		fmt.Fprintf(&b, "- Notes: %s\n\n", strings.TrimSpace(cfg.notes))
	}

	fmt.Fprintf(&b, "## Results\n\n")
	fmt.Fprintf(&b, "| Test | Files | Bytes | Duration | Throughput |\n")
	fmt.Fprintf(&b, "| --- | ---: | ---: | ---: | ---: |\n")
	fmt.Fprintf(&b, "| walk+stat | %d | %s | %s | %s/s |\n", scan.totalFiles, formatBytes(scan.totalBytes), scan.duration.Round(time.Millisecond), formatBytes(rate(scan.totalBytes, scan.duration)))
	for _, r := range reads {
		status := ""
		if r.err != nil {
			status = " (" + r.err.Error() + ")"
		}
		fmt.Fprintf(&b, "| %s%s | %d | %s | %s | %s/s |\n", r.name, status, r.fileReads, formatBytes(r.bytesRead), r.duration.Round(time.Millisecond), formatBytes(rate(r.bytesRead, r.duration)))
	}
	writeName := "small-file-write"
	if write.err != nil {
		writeName += " (" + write.err.Error() + ")"
	}
	fmt.Fprintf(&b, "| %s | %d | %s | %s | %s/s |\n", writeName, write.files, formatBytes(write.bytes), write.duration.Round(time.Millisecond), formatBytes(rate(write.bytes, write.duration)))
	coverName := cover.name
	if coverName == "" {
		coverName = "cover-rebuild-sim"
	}
	if cover.err != nil {
		coverName += " (" + cover.err.Error() + ")"
	}
	coverBytes := cover.bytesRead + cover.bytesWritten
	fmt.Fprintf(&b, "| %s | %d | %s | %s | %s/s |\n\n", coverName, cover.items, formatBytes(coverBytes), cover.duration.Round(time.Millisecond), formatBytes(rate(coverBytes, cover.duration)))

	fmt.Fprintf(&b, "## Cover Rebuild Simulation\n\n")
	fmt.Fprintf(&b, "| Item | Value |\n")
	fmt.Fprintf(&b, "| --- | ---: |\n")
	fmt.Fprintf(&b, "| Samples completed | `%d` |\n", cover.items)
	fmt.Fprintf(&b, "| Concurrency | `%d` |\n", cover.concurrency)
	fmt.Fprintf(&b, "| Archive bytes read | `%s` |\n", formatBytes(cover.bytesRead))
	fmt.Fprintf(&b, "| Thumbnail bytes written | `%s` |\n", formatBytes(cover.bytesWritten))
	fmt.Fprintf(&b, "| Read per item | `%s` |\n", formatBytes(int64(cover.readPerItem)))
	fmt.Fprintf(&b, "| Write per item | `%s` |\n", formatBytes(int64(cover.writePerItem)))
	fmt.Fprintf(&b, "| Items per second | `%.2f` |\n\n", rateFloat(int64(cover.items), cover.duration))

	fmt.Fprintf(&b, "## Reader Latency Under Background Reads\n\n")
	fmt.Fprintf(&b, "| Test | Probes | Reader bytes/probe | Background bytes | Duration | P50 | P95 | Max |\n")
	fmt.Fprintf(&b, "| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for _, r := range contentionResults {
		status := ""
		if r.err != nil {
			status = " (" + r.err.Error() + ")"
		}
		fmt.Fprintf(&b, "| %s%s | %d | %s | %s | %s | %s | %s | %s |\n",
			r.name,
			status,
			r.probes,
			formatBytes(r.readerBytes),
			formatBytes(r.backgroundBytes),
			r.duration.Round(time.Millisecond),
			r.p50.Round(time.Millisecond),
			r.p95.Round(time.Millisecond),
			r.max.Round(time.Millisecond),
		)
	}
	fmt.Fprintf(&b, "\n")

	summary := buildRecommendationSummary(scan, reads, write, contentionResults, cfg)
	fmt.Fprintf(&b, "## Decision Summary\n\n")
	fmt.Fprintf(&b, "| Item | Value |\n")
	fmt.Fprintf(&b, "| --- | --- |\n")
	fmt.Fprintf(&b, "| Best read concurrency | `%s` |\n", summary.bestReadConcurrency)
	fmt.Fprintf(&b, "| c2 throughput gain vs c1 | `%s` |\n", formatPercent(summary.c2Gain))
	fmt.Fprintf(&b, "| c4 throughput gain vs c1 | `%s` |\n", formatPercent(summary.c4Gain))
	fmt.Fprintf(&b, "| Low-impact reader P95 change | `%s` |\n", formatPercent(summary.readerP95Change))
	fmt.Fprintf(&b, "| Recommended archive_open_concurrency | `%d` |\n", summary.recommendedArchiveConcurrency)
	fmt.Fprintf(&b, "| Recommended cover_concurrency | `%d` |\n", summary.recommendedCoverConcurrency)
	fmt.Fprintf(&b, "| Recommended hash_concurrency | `%d` |\n", summary.recommendedHashConcurrency)
	fmt.Fprintf(&b, "| Move cache to SSD | `%t` |\n\n", summary.moveCache)

	fmt.Fprintf(&b, "## Interpretation\n\n")
	fmt.Fprintf(&b, "- Archive files discovered: `%d`.\n", scan.archiveCount)
	fmt.Fprintf(&b, "- If concurrent read throughput does not improve over `sequential-read-c1`, keep `archive_open_concurrency = 1` for this volume.\n")
	fmt.Fprintf(&b, "- If `small-file-write` is slow and cache/library are on the same volume, move `cache.dir` to an SSD or keep same-disk page cache disabled.\n")
	fmt.Fprintf(&b, "- If `reader-latency-low-impact` P95 is lower than `reader-latency-unthrottled`, the low-impact scheduler is protecting page turns under background load.\n")
	fmt.Fprintf(&b, "- Record Windows Task Manager active time during this run when testing an actual external HDD.\n")
	return b.String()
}

type recommendationSummary struct {
	bestReadConcurrency           string
	c2Gain                        float64
	c4Gain                        float64
	readerP95Change               float64
	recommendedArchiveConcurrency int
	recommendedCoverConcurrency   int
	recommendedHashConcurrency    int
	moveCache                     bool
}

func buildRecommendationSummary(scan scanResult, reads []readResult, write writeResult, contention []contentionResult, cfg benchConfig) recommendationSummary {
	summary := recommendationSummary{
		bestReadConcurrency:           "c1",
		recommendedArchiveConcurrency: 1,
		recommendedCoverConcurrency:   1,
		recommendedHashConcurrency:    1,
	}
	c1 := readThroughput(reads, "sequential-read-c1")
	c2 := readThroughput(reads, "concurrent-read-c2")
	c4 := readThroughput(reads, "concurrent-read-c4")
	if c1 > 0 {
		summary.c2Gain = (float64(c2) - float64(c1)) / float64(c1)
		summary.c4Gain = (float64(c4) - float64(c1)) / float64(c1)
	}
	if summary.c2Gain > 0.25 && c2 > c1 {
		summary.bestReadConcurrency = "c2"
		summary.recommendedArchiveConcurrency = 2
	}
	if summary.c4Gain > summary.c2Gain && summary.c4Gain > 0.40 {
		summary.bestReadConcurrency = "c4"
		summary.recommendedArchiveConcurrency = 4
	}
	if config.NormalizeStorageProfile(cfg.storageProfile) == config.StorageProfileSSD && summary.recommendedArchiveConcurrency < 2 && c1 > 0 {
		summary.recommendedArchiveConcurrency = 2
	}
	summary.recommendedCoverConcurrency = minPositive(summary.recommendedArchiveConcurrency, cfg.backgroundReaders)
	summary.recommendedHashConcurrency = summary.recommendedArchiveConcurrency

	unthrottled := contentionP95(contention, "reader-latency-unthrottled")
	lowImpact := contentionP95(contention, "reader-latency-low-impact")
	if unthrottled > 0 {
		summary.readerP95Change = (float64(lowImpact) - float64(unthrottled)) / float64(unthrottled)
	}
	summary.moveCache = config.SameVolume(cfg.libraryPath, cfg.cacheDir) && write.duration > 0 && rate(write.bytes, write.duration) < 10*1024*1024
	_ = scan
	return summary
}

func readThroughput(reads []readResult, name string) int64 {
	for _, r := range reads {
		if r.name == name {
			return rate(r.bytesRead, r.duration)
		}
	}
	return 0
}

func contentionP95(results []contentionResult, name string) time.Duration {
	for _, r := range results {
		if r.name == name {
			return r.p95
		}
	}
	return 0
}

func minPositive(a, b int) int {
	if a <= 0 {
		return b
	}
	if b <= 0 || a < b {
		return a
	}
	return b
}

func formatPercent(value float64) string {
	return fmt.Sprintf("%.1f%%", value*100)
}

func sanitizeSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune('-')
		case r == ' ' || r == '.':
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func emptyDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return strings.TrimSpace(value)
}

func rate(bytes int64, duration time.Duration) int64 {
	if bytes <= 0 || duration <= 0 {
		return 0
	}
	return int64(float64(bytes) / duration.Seconds())
}

func rateFloat(value int64, duration time.Duration) float64 {
	if value <= 0 || duration <= 0 {
		return 0
	}
	return float64(value) / duration.Seconds()
}

func formatBytes(value int64) string {
	if value <= 0 {
		return "0 B"
	}
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	size := float64(value)
	unit := 0
	for size >= 1024 && unit < len(units)-1 {
		size /= 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%d %s", value, units[unit])
	}
	return fmt.Sprintf("%.2f %s", size, units[unit])
}
