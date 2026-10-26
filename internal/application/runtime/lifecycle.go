package runtime

import (
	"context"
	"log/slog"
	"manga-manager/internal/config"
	"manga-manager/internal/runtimecfg"
	"manga-manager/internal/task"
	"time"
)

// SampleInterval normalizes the shared sampling cadence used by engine and background ticker.
func SampleInterval(c config.Config) time.Duration {
	s := c.Tasks.SampleIntervalSeconds
	if s < 1 {
		return task.DefaultSampleInterval
	}
	if s > 86400 {
		s = 86400
	}
	return time.Duration(s) * time.Second
}

// BackoffPolicy translates the current Manager snapshot into domain launch policy.
func BackoffPolicy(c config.Config) task.BackoffPolicy {
	h := c.Tasks.BackoffMaxHours
	if h < 0 {
		h = 0
	}
	if h > 24*36500 {
		h = 24 * 36500
	}
	return task.BackoffPolicy{Factor: c.Tasks.BackoffFactor, MaxDelay: time.Duration(h) * time.Hour, StopAfter: c.Tasks.BackoffStopAfter}
}

// Start recovers consistency and interrupted history before opening watchers and background inputs.
// The HTTP host may expose requests only after this method succeeds.
func (r *Runtime) Start(ctx context.Context) error {
	if err := r.Recover(ctx); err != nil {
		return err
	}
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	if r.closing {
		return task.ErrEngineStopped
	}
	if r.started {
		return nil
	}
	initial := r.cfg.Snapshot()
	if err := runtimecfg.Apply(&initial); err != nil {
		slog.WarnContext(ctx, "Failed to apply runtime config at startup", "error", err)
	}
	if err := r.RecoverRuns(ctx); err != nil {
		return err
	}
	if r.options.ConfigPath != "" {
		watch := r.options.WatchConfig
		if watch == nil {
			watch = func(path string, cfg *config.Manager) (ConfigWatch, error) {
				return config.StartWatcher(path, cfg, runtimecfg.Apply)
			}
		}
		w, err := watch(r.options.ConfigPath, r.cfg)
		if err != nil {
			slog.WarnContext(ctx, "Config hot-reload disabled", "path", r.options.ConfigPath, "error", err)
		} else {
			r.configWatcher = w
		}
	}
	e := r.options.Effects
	if e.RunEvents != nil {
		r.Go(func() { e.RunEvents(r.Done()) })
	}
	r.Go(r.scheduledScans)
	r.Go(func() { r.Runs.RunHistoryJanitor(r.Done(), r.cfg.Snapshot) })
	r.Go(r.sampleRuns)
	r.Go(r.pruneSessions)
	if e.TrimPageCache != nil {
		r.Go(func() { r.periodic(5*time.Minute, e.TrimPageCache) })
	}
	if r.WatcherError != nil {
		slog.WarnContext(ctx, "Failed to create file watcher", "error", r.WatcherError)
	} else {
		r.Watcher.Start(e.Publish)
		r.Go(r.watchLibraries)
	}
	r.started = true
	return nil
}

// Shutdown stops config updates, ends long-lived subscriptions, drains requests, then closes background resources.
func (r *Runtime) Shutdown(ctx context.Context, drain func(context.Context) error) error {
	r.stateMu.Lock()
	watch := r.configWatcher
	r.stateMu.Unlock()
	if watch != nil {
		watch.Stop()
	}
	if r.options.Effects.StopEvents != nil {
		r.options.Effects.StopEvents()
	}
	var err error
	if drain != nil {
		err = drain(ctx)
	}
	r.Close()
	return err
}
func (r *Runtime) periodic(interval time.Duration, work func()) {
	work()
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-r.Done():
			return
		case <-tick.C:
			work()
		}
	}
}
func (r *Runtime) pruneSessions() {
	r.periodic(time.Hour, func() {
		if err := r.store.DeleteExpiredSessions(context.Background(), time.Now()); err != nil {
			slog.Warn("Failed to prune expired sessions", "error", err)
		}
	})
}
func (r *Runtime) sampleRuns() {
	tick := time.NewTicker(SampleInterval(r.cfg.Snapshot()))
	defer tick.Stop()
	for {
		select {
		case <-r.Done():
			return
		case <-tick.C:
			r.Engine.SampleActiveRuns(context.Background())
			tick.Reset(SampleInterval(r.cfg.Snapshot()))
		}
	}
}
func (r *Runtime) scheduledScans() {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	last := make(map[int64]time.Time)
	for {
		select {
		case <-r.Done():
			return
		case now := <-tick.C:
			r.DispatchScheduledScans(context.Background(), now, last)
		}
	}
}

// DispatchScheduledScans evaluates per-library intervals against one scheduling tick.
func (r *Runtime) DispatchScheduledScans(ctx context.Context, now time.Time, last map[int64]time.Time) {
	libs, err := r.store.ListLibraries(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "Daemon failed to fetch libraries", "error", err)
		return
	}
	for _, lib := range libs {
		if lib.ScanMode != "interval" {
			continue
		}
		if before, ok := last[lib.ID]; ok && now.Sub(before) < time.Duration(lib.ScanInterval)*time.Minute {
			continue
		}
		last[lib.ID] = now
		slog.InfoContext(ctx, "Library reached its scan interval", "library_id", lib.ID, "path", lib.Path)
		if _, err := r.Scans.ScanLibrary(lib, false, task.TriggerScheduled); err != nil {
			slog.ErrorContext(ctx, "Auto-scan could not be started", "library_id", lib.ID, "error", err)
		}
	}
}
func (r *Runtime) watchLibraries() {
	libs, err := r.store.ListLibraries(context.Background())
	if err != nil {
		slog.Warn("Failed to list libraries for watcher", "error", err)
		return
	}
	for _, lib := range libs {
		select {
		case <-r.Done():
			return
		default:
		}
		if lib.ScanMode != "watch" {
			continue
		}
		p, err := r.store.LibraryRelocationProtection(context.Background(), lib.ID)
		if err != nil || p != nil {
			continue
		}
		release, err := r.Engine.AcquireLibraryRead(lib.ID)
		if err != nil {
			continue
		}
		_ = r.Watcher.WatchLibrary(lib.ID, lib.Path, lib.ScanFormats)
		release()
	}
}
