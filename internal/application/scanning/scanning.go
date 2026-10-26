// Package scanning orchestrates scan runs and safe filesystem-watcher completion.
package scanning

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"manga-manager/internal/application"
	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/logger"
	"manga-manager/internal/runhandle"
	"manga-manager/internal/scanner"
	"manga-manager/internal/task"
	"strconv"
	"strings"
)

// Store is the scan planning and automatic-processing protection read boundary.
type Store interface {
	GetLibrary(context.Context, int64) (database.Library, error)
	GetSeries(context.Context, int64) (database.Series, error)
	LibraryRelocationProtection(context.Context, int64) (*database.LibraryRelocationProtection, error)
}

// Scanner owns ingestion, rename reconnection and missing-record removal.
type Scanner interface {
	ScanLibraryWithOptions(context.Context, int64, string, scanner.LibraryScanOptions, scanner.ScanObserver) error
	CleanupThumbnails(context.Context, func(int, int)) error
	ScanLibrary(context.Context, int64, string, bool, scanner.ScanObserver) error
	ScanSeries(context.Context, int64, bool, scanner.ScanObserver) error
	CleanupLibrary(context.Context, int64) error
	QueueMissingCovers(context.Context, int64) (*scanner.CoverBatch, error)
}

// Effects are leaf capabilities; orchestration decides their timing and composition.
type Effects struct {
	PurgeReadingPaths func()
	InvalidateStats   func(string)
	WarmStats         func(string)
	Backfill          func(context.Context, string)
	Publish           func(string)
}

// Service shares the application run entry and leaves filesystem event scheduling to scanner.
type Service struct {
	runs      *application.Runs
	store     Store
	scanner   Scanner
	config    func() config.Config
	effects   Effects
	coverRuns *coverRunQueues
}

// New assembles the scan use cases before any run is admitted.
func New(runs *application.Runs, store Store, scan Scanner, cfg func() config.Config, effects Effects) *Service {
	return &Service{runs: runs, store: store, scanner: scan, config: cfg, effects: effects, coverRuns: newCoverRunQueues()}
}

func (c *Service) requireAutomaticProcessing(ctx context.Context, libraryID int64) error {
	protection, err := c.store.LibraryRelocationProtection(ctx, libraryID)
	if err != nil {
		return err
	}
	if protection != nil {
		return database.ErrLibraryProtected
	}
	return nil
}

// BatchEvent preserves statistics invalidation, completed-scan warming and semantic publication.
func (c *Service) BatchEvent(action string) {
	c.effects.InvalidateStats("scanner_" + action)
	if action == "scan_completed" {
		c.effects.WarmStats("scanner_" + action)
	}
	c.effects.Publish(action)
}

// ScanLibrary starts a library scan with one identity across manual, scheduled and watcher inputs.
func (c *Service) ScanLibrary(lib database.Library, force bool, trigger task.Trigger) (task.Launched, error) {
	if trigger != task.TriggerManual {
		if err := c.requireAutomaticProcessing(context.Background(), lib.ID); err != nil {
			return task.Launched{}, err
		}
	}
	cfg := c.config()
	storagePolicy := config.ResolveStoragePolicy(cfg, lib.Path)

	spec := task.RunSpec{
		ValidateAccess: func() error {
			if trigger != task.TriggerManual {
				if err := c.requireAutomaticProcessing(context.Background(), lib.ID); err != nil {
					return err
				}
			}
			current, err := c.store.GetLibrary(context.Background(), lib.ID)
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
			if current.Path != lib.Path {
				return task.ErrLibraryUnavailable
			}
			return nil
		},
		Key:         fmt.Sprintf("scan_library_%d", lib.ID),
		StartCode:   "task.msg.scan_library.start",
		StartParams: map[string]string{"name": lib.Name},
		CanCancel:   true,
		CanPause:    true,
		ScopeName:   lib.Name,
		Args: map[string]string{
			"force":                    strconv.FormatBool(force),
			"scan_profile":             cfg.Scanner.ScanProfile,
			"storage_profile":          storagePolicy.StorageProfile,
			"volume_key":               storagePolicy.VolumeKey,
			"archive_open_concurrency": strconv.Itoa(storagePolicy.IOPolicy.ArchiveOpenConcurrency),
			"cover_concurrency":        strconv.Itoa(storagePolicy.IOPolicy.CoverConcurrency),
		},
		Limits:       LimitsForPath(c.config(), lib.Path),
		CompleteCode: "task.msg.scan_library.complete",
		CancelCode:   "task.msg.scan_library.cancelled",
		FailCode:     "task.msg.scan_library.failed",
	}

	spec.Identity = task.Identity{Type: "scan_library", Scope: task.ScopeLibrary, ScopeID: lib.ID}
	spec.Trigger = trigger
	spec.LibraryIDs = []int64{lib.ID}
	return c.runs.Start(context.Background(), spec, func(ctx context.Context, handle *runhandle.Handle) (task.Result, error) {
		defer c.effects.PurgeReadingPaths()
		// 把**运行句柄**包成**扫描观察者**一起交出去：扫描器的报文不带身份，
		// 「这次扫描的进度写到哪」由这次交出的是谁回答。
		if err := c.scanner.ScanLibrary(ctx, lib.ID, lib.Path, force, NewObserver(handle)); err != nil {
			if errors.Is(err, context.Canceled) {
				c.effects.InvalidateStats("scan_library_cancelled")
				return task.Result{Params: map[string]string{"name": lib.Name}}, err
			}
			c.effects.InvalidateStats("scan_library_failed")
			return task.Result{}, err
		}
		c.effects.WarmStats("scan_library_completed")
		c.effects.Backfill(ctx, "scan_library")
		return task.Result{Params: map[string]string{"name": lib.Name}}, nil
	})
}

// ScanSeries starts a series scan with its library policy and admission protection.
func (c *Service) ScanSeries(seriesID int64, force bool, trigger task.Trigger) error {
	var automaticLibraryID int64
	if trigger != task.TriggerManual {
		series, err := c.store.GetSeries(context.Background(), seriesID)
		if err != nil {
			return err
		}
		automaticLibraryID = series.LibraryID
		if err = c.requireAutomaticProcessing(context.Background(), series.LibraryID); err != nil {
			return err
		}
	}
	idParams := map[string]string{"id": strconv.FormatInt(seriesID, 10)}
	scopeName := ""
	storagePolicy := config.ResolvedStoragePolicy{}
	// 并发上限只在真找到了所属资料库时才有意义：查不到就让任务声明里的 Limits 留零值，
	// 引擎不会为它凭空造一份全零的上限（那会在任务面板上显示成「0 并发」的假数据）。
	limits := task.Limits{}
	if series, err := c.store.GetSeries(context.Background(), seriesID); err == nil {
		scopeName = series.Name
		if series.Title.Valid && strings.TrimSpace(series.Title.String) != "" {
			scopeName = series.Title.String
		}
		if lib, libErr := c.store.GetLibrary(context.Background(), series.LibraryID); libErr == nil {
			storagePolicy = config.ResolveStoragePolicy(c.config(), lib.Path)
			limits = LimitsForPath(c.config(), lib.Path)
		}
	}

	spec := task.RunSpec{
		ValidateAccess: func() error {
			if trigger == task.TriggerManual {
				return nil
			}
			current, err := c.store.GetSeries(context.Background(), seriesID)
			if err != nil {
				return err
			}
			if current.LibraryID != automaticLibraryID {
				return task.ErrLibraryUnavailable
			}
			return c.requireAutomaticProcessing(context.Background(), current.LibraryID)
		},
		Key:         fmt.Sprintf("scan_series_%d", seriesID),
		StartCode:   "task.msg.scan_series.start",
		StartParams: idParams,
		CanCancel:   true,
		CanPause:    true,
		ScopeName:   scopeName,
		Args: map[string]string{
			"force":                    strconv.FormatBool(force),
			"scan_profile":             c.config().Scanner.ScanProfile,
			"storage_profile":          storagePolicy.StorageProfile,
			"volume_key":               storagePolicy.VolumeKey,
			"archive_open_concurrency": strconv.Itoa(storagePolicy.IOPolicy.ArchiveOpenConcurrency),
			"cover_concurrency":        strconv.Itoa(storagePolicy.IOPolicy.CoverConcurrency),
		},
		Limits:       limits,
		CompleteCode: "task.msg.scan_series.complete",
		CancelCode:   "task.msg.scan_series.cancelled",
		FailCode:     "task.msg.scan_series.failed",
	}

	spec.Identity = task.Identity{Type: "scan_series", Scope: task.ScopeSeries, ScopeID: seriesID}
	spec.Trigger = trigger
	spec.LibraryIDs = []int64{task.AllLibraries}
	if series, err := c.store.GetSeries(context.Background(), seriesID); err == nil {
		spec.LibraryIDs = []int64{series.LibraryID}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err := c.runs.Start(context.Background(), spec, func(ctx context.Context, handle *runhandle.Handle) (task.Result, error) {
		defer c.effects.PurgeReadingPaths()
		if err := c.scanner.ScanSeries(ctx, seriesID, force, NewObserver(handle)); err != nil {
			if errors.Is(err, context.Canceled) {
				c.effects.InvalidateStats("scan_series_cancelled")
				return task.Result{Params: idParams}, err
			}
			slog.ErrorContext(ctx, "ScanSeries Failed", "seriesId", seriesID, "error", err)
			c.effects.InvalidateStats("scan_series_failed")
			return task.Result{}, err
		}
		c.effects.WarmStats("scan_series_completed")
		c.effects.Backfill(ctx, "scan_series")
		return task.Result{Params: idParams}, nil
	})
	return err
}

// CleanupLibrary starts protected missing-record removal; shutdown does not interrupt its transaction.
func (c *Service) CleanupLibrary(libraryID int64, trigger task.Trigger) (task.Launched, error) {
	if err := c.requireAutomaticProcessing(context.Background(), libraryID); err != nil {
		return task.Launched{}, err
	}
	idParams := map[string]string{"id": strconv.FormatInt(libraryID, 10)}
	scopeName := ""
	if lib, err := c.store.GetLibrary(context.Background(), libraryID); err == nil {
		scopeName = lib.Name
	}

	spec := task.RunSpec{
		Key:          fmt.Sprintf("cleanup_library_%d", libraryID),
		StartCode:    "task.msg.cleanup_library.start",
		StartParams:  idParams,
		Total:        1,
		ScopeName:    scopeName,
		CompleteCode: "task.msg.cleanup_library.complete",
		FailCode:     "task.msg.cleanup_library.failed",
	}

	spec.Identity = task.Identity{Type: "cleanup_library", Scope: task.ScopeLibrary, ScopeID: libraryID}
	spec.Trigger = trigger
	spec.LibraryIDs = []int64{libraryID}
	return c.runs.Start(context.Background(), spec, func(taskCtx context.Context, handle *runhandle.Handle) (task.Result, error) {
		handle.Phase("scanning_records", "task.msg.cleanup_library.scanning_records", idParams)
		// 刻意不用任务体 ctx 的**取消**能力：本任务不可取消，而停机会取消所有任务 ctx——用了它，
		// 一次关服就会把这个没人取消过的任务写成**已取消**。改动前先读本函数的 doc。
		// 只把它携带的**任务键**转移到这条不可取消的 ctx 上，否则这个任务跑出的日志按任务键一条也过滤不到。
		cleanupCtx := logger.WithRunID(logger.WithTaskKey(context.Background(), logger.TaskKeyFrom(taskCtx)), logger.RunIDFrom(taskCtx))
		if err := c.scanner.CleanupLibrary(cleanupCtx, libraryID); err != nil {
			slog.ErrorContext(cleanupCtx, "Failed to cleanup library", "library_id", libraryID, "error", err)
			return task.Result{}, err
		}
		return task.Result{Params: idParams}, nil
	})
}

// WatchLibrary waits for a real successful scan before the watcher may clean missing records.
func (c *Service) WatchLibrary(ctx context.Context, libraryID int64) error {
	lib, err := c.store.GetLibrary(ctx, libraryID)
	if err != nil {
		return err
	}
	// 监听触发的是一次**增量**扫描：它要的只是「把这批新文件收进来」。
	launched, err := c.ScanLibrary(lib, false, task.TriggerWatch)
	return c.awaitWatchedRun(ctx, launched, err)
}

// WatchCleanup waits for the cleanup run so watcher shutdown includes its work.
func (c *Service) WatchCleanup(ctx context.Context, libraryID int64) error {
	launched, err := c.CleanupLibrary(libraryID, task.TriggerWatch)
	return c.awaitWatchedRun(ctx, launched, err)
}

func (c *Service) awaitWatchedRun(ctx context.Context, launched task.Launched, launchErr error) error {
	err := c.runs.AwaitWatched(ctx, launched, launchErr)
	if errors.Is(err, application.ErrRunCoalesced) {
		return scanner.ErrScanCoalesced
	}
	if errors.Is(err, application.ErrRunSuppressed) {
		return scanner.ErrScanSuppressed
	}
	return err
}
