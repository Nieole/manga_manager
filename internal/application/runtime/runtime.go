// Package runtime assembles application capabilities and owns process recovery and resource lifetime.
package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"manga-manager/internal/application"
	"manga-manager/internal/application/diskjobs"
	"manga-manager/internal/application/intelligence"
	"manga-manager/internal/application/maintenance"
	"manga-manager/internal/application/relocating"
	"manga-manager/internal/application/scanning"
	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/diskwork"
	"manga-manager/internal/external"
	"manga-manager/internal/koreader"
	"manga-manager/internal/metadata"
	"manga-manager/internal/organize"
	"manga-manager/internal/proposal"
	"manga-manager/internal/reading"
	"manga-manager/internal/relocation"
	"manga-manager/internal/runtimecfg"
	"manga-manager/internal/scanner"
	"manga-manager/internal/storageio"
	"manga-manager/internal/task"
	"manga-manager/internal/taskstore"
	"sync"
	"time"
)

// Effects supplies transport and derived-state leaf operations; lifecycle recipes stay here.
type Effects struct {
	Publish           func(string)
	PurgeReadingPaths func()
	InvalidateStats   func(string)
	WarmStats         func(string)
	InvalidateReading func()
	RunEvents         func(<-chan struct{})
	StopEvents        func()
	TrimPageCache     func()
	ObserveIdentity   func(context.Context, task.Identity) error
	Sources           intelligence.Sources
}

// ConfigWatch is the operating-system configuration subscription owned by this process.
type ConfigWatch interface{ Stop() }

// Options supplies process resources and controllable task clocks, publications and scheduling.
// Store, disk work, resume policy and context decoration are assembled from the shared process graph.
type Options struct {
	WatchConfig func(string, *config.Manager) (ConfigWatch, error)
	ConfigPath  string
	Scanner     *scanner.Scanner
	Tasks       task.Config
	Effects     Effects
}

// Runtime holds assembled capabilities. Their modules retain rules, transactions and execution bodies.
type Runtime struct {
	Engine        *task.Engine
	Runs          *application.Runs
	Tasks         task.Store
	Registry      *Registry
	Scans         *scanning.Service
	Maintenance   *maintenance.Service
	Disk          *diskjobs.Service
	Intelligence  *intelligence.Service
	Moves         *relocating.Service
	KOReader      *koreader.Service
	External      *external.Manager
	Proposals     *proposal.Service
	Relocation    *relocation.Service
	Relations     *organize.Relations
	Scanner       *scanner.Scanner
	Watcher       *scanner.FileWatcher
	WatcherError  error
	DiskWork      *diskwork.Runner
	cfg           *config.Manager
	store         database.Store
	options       Options
	group         *background
	stateMu       sync.Mutex
	recovered     bool
	started       bool
	closing       bool
	configWatcher ConfigWatch
	stopOnce      sync.Once
}
type proposalStore struct{ database.Store }

func (d proposalStore) ExecTx(ctx context.Context, fn func(proposal.Queries) error) error {
	return d.Store.ExecTx(ctx, func(q *database.Queries) error { return fn(q) })
}

// New assembles all capabilities and restart declarations before recovery or launch inputs open.
func New(store database.Store, cfg *config.Manager, options Options) (*Runtime, error) {
	e := options.Effects
	if e.Publish == nil {
		e.Publish = func(string) {}
	}
	if e.PurgeReadingPaths == nil {
		e.PurgeReadingPaths = func() {}
	}
	if e.InvalidateStats == nil {
		e.InvalidateStats = func(string) {}
	}
	if e.WarmStats == nil {
		e.WarmStats = func(string) {}
	}
	options.Effects = e
	r := &Runtime{store: store, cfg: cfg, options: options, group: newBackground()}
	r.Tasks = taskstore.New(store.DB())
	r.DiskWork = diskwork.NewRunner(cfg.Snapshot, storageio.Default)
	tc := options.Tasks
	tc.Store = r.Tasks
	tc.DiskWork = r.DiskWork
	if tc.RunBackground == nil {
		tc.RunBackground = r.Go
	}
	tc.DecorateRunContext = application.DecorateRunContext
	if tc.Slots == nil {
		tc.Slots = func() int { return cfg.Snapshot().Tasks.RunSlots }
	}
	if tc.Backoff == nil {
		tc.Backoff = func() task.BackoffPolicy { return BackoffPolicy(cfg.Snapshot()) }
	}
	if tc.SampleInterval == nil {
		tc.SampleInterval = func() time.Duration { return SampleInterval(cfg.Snapshot()) }
	}
	tc.Resume = func() task.ResumePolicy { return r.Registry.Policy(cfg.Snapshot()) }
	tc.ControlCodes = task.ControlCodes{Paused: "task.msg.control.paused", Resumed: "task.msg.control.resumed", Cancelling: "task.msg.control.cancelling", Panicked: "task.msg.control.panicked", Interrupted: "task.msg.control.interrupted"}
	r.Engine = task.New(tc)
	r.Runs = application.NewRuns(r.Engine, e.ObserveIdentity)
	r.Scanner = options.Scanner
	if r.Scanner == nil {
		r.Scanner = scanner.NewScanner(store, cfg)
	}
	r.External = external.NewManager(store, 30*time.Minute)
	r.Proposals = proposal.NewService(proposalStore{store})
	r.Relocation = relocation.NewService(store, cfg)
	r.Relocation.ConfigPath = options.ConfigPath
	r.Relocation.ApplyConfig = runtimecfg.Apply
	r.KOReader = koreader.NewService(store, cfg, koreader.WithReadingPositions(reading.New(store, e.InvalidateReading)))
	r.Relations = organize.NewRelations(store, r.Go)
	r.Scans = scanning.New(r.Runs, store, r.Scanner, cfg.Snapshot, scanning.Effects{PurgeReadingPaths: e.PurgeReadingPaths, InvalidateStats: e.InvalidateStats, WarmStats: e.WarmStats, Publish: e.Publish, Backfill: func(ctx context.Context, reason string) { r.Maintenance.ChainBookHashBackfill(ctx, reason) }})
	r.Maintenance = maintenance.New(r.Runs, store, r.Scans, r.KOReader, cfg.Snapshot, maintenance.Effects{Publish: e.Publish, WarmStats: e.WarmStats})
	r.Scanner.SetBatchCallback(func(action string) { r.Scans.BatchEvent(action) })
	r.Scanner.SetCoverRunLauncher(func(ctx context.Context, batch *scanner.CoverBatch) { r.Scans.DispatchCoverBatch(ctx, batch) })
	r.Disk = diskjobs.New(r.Runs, store, r.External, diskjobs.Effects{Publish: e.Publish})
	sources := e.Sources
	if sources.Metadata == nil {
		sources.Metadata = func(name string) metadata.Provider { return metadata.NewConfiguredProvider(cfg.Snapshot(), name) }
	}
	if sources.Grouping == nil {
		sources.Grouping = func() intelligence.GroupingProvider {
			c := cfg.Snapshot()
			return metadata.NewAIProvider(c.LLM.Provider, c.LLM.APIMode, c.LLM.BaseURL, c.LLM.RequestPath, c.LLM.Model, c.LLM.APIKey, c.LLM.Timeout)
		}
	}
	r.Intelligence = intelligence.New(r.Runs, store, r.Proposals, organize.NewAIGrouping(store, nil), sources, e.Publish)
	r.Watcher, r.WatcherError = scanner.NewFileWatcher(scanner.WatcherHooks{ScanLibrary: func(ctx context.Context, id int64) error { return r.Scans.WatchLibrary(ctx, id) }, CleanupLibrary: func(ctx context.Context, id int64) error { return r.Scans.WatchCleanup(ctx, id) }, LibraryScanRunning: r.Scanner.LibraryScanActive})
	r.Moves = relocating.New(r.Relocation, r.Runs, r.Engine, r.Tasks, store, relocating.Effects{PurgeReadingPaths: e.PurgeReadingPaths, InvalidateStats: e.InvalidateStats, InvalidateExternal: r.External.InvalidateLibrary, Watcher: func() (relocating.Watcher, error) {
		if r.Watcher == nil {
			return nil, r.WatcherError
		}
		return r.Watcher, r.WatcherError
	}})
	r.Registry = newRegistry(r)
	cfg.SetAfterUpdate(r.Engine.ReleaseQueued)
	return r, nil
}

// Recover restores relocation consistency before any automatic inputs start.
func (r *Runtime) Recover(ctx context.Context) error {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	if r.closing {
		return task.ErrEngineStopped
	}
	if r.recovered {
		return nil
	}
	if err := r.Moves.Recover(ctx); err != nil {
		return fmt.Errorf("initialize library relocation: %w", err)
	}
	r.recovered = true
	return nil
}

// RecoverRuns preserves interrupted history and replays supported identities through their real application use cases.
func (r *Runtime) RecoverRuns(ctx context.Context) error {
	live, err := r.Tasks.ListRuns(ctx, task.RunFilter{Statuses: task.LiveStatuses()})
	if err != nil {
		return err
	}
	if observe := r.options.Effects.ObserveIdentity; observe != nil {
		ids := make([]int64, 0, len(live))
		for _, run := range live {
			ids = append(ids, run.TaskID)
		}
		owners, err := r.Tasks.LoadTasks(ctx, ids)
		if err != nil {
			return err
		}
		for _, owner := range owners {
			if err = observe(ctx, owner.Identity); err != nil {
				return err
			}
		}
	}
	outcome, err := r.Engine.MarkInterrupted(ctx)
	if err != nil {
		return err
	}
	if outcome.Marked > 0 {
		slog.InfoContext(ctx, "Recovered interrupted runs", "count", outcome.Marked)
	}
	ids := make([]int64, 0, len(outcome.Resume))
	for _, s := range outcome.Resume {
		ids = append(ids, s.Run.TaskID)
	}
	owners, err := r.Tasks.LoadTasks(ctx, ids)
	if err != nil {
		return err
	}
	for _, s := range outcome.Resume {
		owner, ok := owners[s.Run.TaskID]
		if !ok {
			continue
		}
		if err := r.Registry.Restart(ctx, owner.Identity, s.Side.Args, task.TriggerResumed); err != nil {
			slog.WarnContext(ctx, "Failed to resume interrupted run", "run_id", s.Run.ID, "task_id", s.Run.TaskID, "error", err)
		}
	}
	return nil
}

// Go registers owned background work. Task admission is stopped before registration is sealed.
func (r *Runtime) Go(fn func()) { r.group.goRun(fn) }

// Done closes when the process starts stopping its background inputs.
func (r *Runtime) Done() <-chan struct{} { return r.group.done }

// WaitBackground waits for registered work without stopping the process, for controlled embedded callers.
func (r *Runtime) WaitBackground() { r.group.wg.Wait() }

// Close stops background inputs, cancels admitted tasks, and waits before releasing scanner workers.
func (r *Runtime) Close() {
	r.stopOnce.Do(func() {
		r.stateMu.Lock()
		r.closing = true
		watch := r.configWatcher
		r.stateMu.Unlock()
		if watch != nil {
			watch.Stop()
		}
		if r.options.Effects.StopEvents != nil {
			r.options.Effects.StopEvents()
		}
		r.group.signal()
		if r.Watcher != nil {
			r.Watcher.Stop()
		}
		r.Engine.Shutdown()
		r.group.sealAndWait()
		r.Scanner.Close()
		runtimecfg.Close()
	})
}

// Config returns a snapshot of the same Manager used by every assembled capability.
func (r *Runtime) Config() config.Config { return r.cfg.Snapshot() }
