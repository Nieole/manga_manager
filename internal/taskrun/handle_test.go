// 本文件守**任务句柄**的四条契约：`hashed_files` 只数真正读过的书、磁盘作业的实况不依赖调用方
// 记得吸收、过闸门同时是取消检查、全部方法可从任意 goroutine 调用。
//
// 四条都没有编译期约束。破了只在慢盘上显形：任务面板的吞吐量把排队挡下的书也算进去、
// 档位与卷键被零值抹掉、暂停按不动、或并发上报把累加器撞坏。
//
// 每条用例各建一个调度器：包级实例会让它们经由按卷计数的限流器互相污染。

package taskrun

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/diskwork"
	"manga-manager/internal/storageio"
	"manga-manager/internal/taskcontrol"
)

// stubConfig 造一个只有全局存储策略的配置函数：custom 档不会被归一化改写，正值原样生效。
func stubConfig(policy config.StorageIOPolicy) func() config.Config {
	return func() config.Config {
		cfg := config.Config{}
		cfg.Library.StorageProfile = config.StorageProfileCustom
		cfg.Library.IOPolicy = policy
		return cfg
	}
}

// recorder 是三个注入函数的记录桩：句柄的上报面全部经它观测。
type recorder struct {
	mu       sync.Mutex
	frames   []Frame
	params   []map[string]string
	metrics  []map[string]int64
	metricPs []map[string]string
}

func (r *recorder) report(f Frame) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.frames = append(r.frames, f)
}

func (r *recorder) mergeParams(p map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.params = append(r.params, p)
}

func (r *recorder) addMetrics(inc map[string]int64, p map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.metrics = append(r.metrics, inc)
	r.metricPs = append(r.metricPs, p)
}

func (r *recorder) lastFrame(t *testing.T) Frame {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.frames) == 0 {
		t.Fatal("no frame was reported")
	}
	return r.frames[len(r.frames)-1]
}

// newHandle 建一个压在构造点上的句柄：三个记录桩 + 一个新建调度器上的 Runner。
func newHandle(policy config.StorageIOPolicy) (*Handle, *recorder, *storageio.Scheduler) {
	rec := &recorder{}
	sched := storageio.NewScheduler()
	runner := diskwork.NewRunner(stubConfig(policy), sched)
	return New(rec.report, rec.mergeParams, rec.addMetrics, runner), rec, sched
}

// pausedContext 造一个闸门处于暂停态的上下文，并返回它的闸门。
func pausedContext(t *testing.T) (context.Context, *taskcontrol.PauseGate, context.CancelFunc) {
	t.Helper()
	gate := taskcontrol.NewPauseGate()
	gate.Pause()
	ctx, cancel := context.WithCancel(taskcontrol.WithPauseGate(context.Background(), gate))
	return ctx, gate, cancel
}

func TestHandleCountsHashedFileOnlyWhenClosureRan(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book.cbz")

	t.Run("整文件读取且闭包执行", func(t *testing.T) {
		h, _, _ := newHandle(config.StorageIOPolicy{HashConcurrency: 1})
		ran := false
		err := h.Disk(context.Background(), diskwork.Work{Kind: storageio.WorkKindIdentityHash, Path: path}, func() error {
			ran = true
			return nil
		})
		if err != nil {
			t.Fatalf("Disk returned %v, want nil", err)
		}
		if !ran {
			t.Fatal("closure did not run")
		}
		if got := h.IOMetrics().HashedFiles; got != 1 {
			t.Fatalf("hashed files = %d, want 1", got)
		}
	})

	t.Run("闭包自己出错也算读过", func(t *testing.T) {
		h, _, _ := newHandle(config.StorageIOPolicy{HashConcurrency: 1})
		wantErr := errors.New("hash failed")
		err := h.Disk(context.Background(), diskwork.Work{Kind: storageio.WorkKindIdentityHash, Path: path}, func() error {
			return wantErr
		})
		if !errors.Is(err, wantErr) {
			t.Fatalf("Disk returned %v, want %v", err, wantErr)
		}
		if got := h.IOMetrics().HashedFiles; got != 1 {
			t.Fatalf("hashed files = %d, want 1", got)
		}
	})

	t.Run("上下文已取消", func(t *testing.T) {
		h, _, _ := newHandle(config.StorageIOPolicy{HashConcurrency: 1})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		ran := false
		err := h.Disk(ctx, diskwork.Work{Kind: storageio.WorkKindIdentityHash, Path: path}, func() error {
			ran = true
			return nil
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Disk returned %v, want context.Canceled", err)
		}
		if ran {
			t.Fatal("closure ran on a cancelled context")
		}
		if got := h.IOMetrics().HashedFiles; got != 0 {
			t.Fatalf("hashed files = %d, want 0", got)
		}
	})

	t.Run("闸门暂停期间被取消", func(t *testing.T) {
		h, _, _ := newHandle(config.StorageIOPolicy{HashConcurrency: 1})
		ctx, _, cancel := pausedContext(t)
		cancel()

		ran := false
		err := h.Disk(ctx, diskwork.Work{Kind: storageio.WorkKindIdentityHash, Path: path}, func() error {
			ran = true
			return nil
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Disk returned %v, want context.Canceled", err)
		}
		if ran {
			t.Fatal("closure ran while the gate was paused")
		}
		if got := h.IOMetrics().HashedFiles; got != 0 {
			t.Fatalf("hashed files = %d, want 0", got)
		}
	})

	t.Run("其余工种执行了也不计", func(t *testing.T) {
		h, _, _ := newHandle(config.StorageIOPolicy{ArchiveOpenConcurrency: 1, CoverConcurrency: 1})
		ran := false
		err := h.Disk(context.Background(), diskwork.Work{Kind: storageio.WorkKindCoverBuild, Path: path}, func() error {
			ran = true
			return nil
		})
		if err != nil {
			t.Fatalf("Disk returned %v, want nil", err)
		}
		if !ran {
			t.Fatal("closure did not run")
		}
		if got := h.IOMetrics().HashedFiles; got != 0 {
			t.Fatalf("hashed files = %d, want 0", got)
		}
	})
}

func TestHandleAbsorbsDiskWorkStats(t *testing.T) {
	h, _, sched := newHandle(config.StorageIOPolicy{HashConcurrency: 1})
	path := filepath.Join(t.TempDir(), "book.cbz")
	work := diskwork.Work{Kind: storageio.WorkKindIdentityHash, Path: path}

	// 让后台整体暂停逼出一段可测量的等待，两次作业的时长必须累加而不是覆盖。
	for i := 0; i < 2; i++ {
		sched.PauseBackground()
		done := make(chan error, 1)
		go func() { done <- h.Disk(context.Background(), work, func() error { return nil }) }()
		time.Sleep(60 * time.Millisecond)
		sched.ResumeBackground()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Disk returned %v, want nil", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Disk did not return after background IO resumed")
		}
	}

	got := h.IOMetrics()
	if got.IOWaitMillis < 100 {
		t.Fatalf("io wait = %dms, want at least 100ms (two waits accumulated)", got.IOWaitMillis)
	}
	if got.PausedMillis < 100 {
		t.Fatalf("paused = %dms, want at least 100ms (two waits accumulated)", got.PausedMillis)
	}
	if got.StorageProfile != config.StorageProfileCustom {
		t.Fatalf("storage profile = %q, want %q", got.StorageProfile, config.StorageProfileCustom)
	}
	if want := config.VolumeKey(path); got.VolumeKey != want {
		t.Fatalf("volume key = %q, want %q", got.VolumeKey, want)
	}

	// 闸门挡下的作业根本没解析策略，交回零值实况；照抄会把「没有这回事」写成「实况为空」。
	ctx, _, cancel := pausedContext(t)
	cancel()
	if err := h.Disk(ctx, work, func() error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("Disk returned %v, want context.Canceled", err)
	}
	after := h.IOMetrics()
	if after.StorageProfile != got.StorageProfile || after.VolumeKey != got.VolumeKey {
		t.Fatalf("gated work overwrote the last real stats: %+v, want %+v", after, got)
	}
}

func TestHandleAbsorbsTheLatestProfileAndVolume(t *testing.T) {
	// 一批书可以跨资料库跨卷：这两项报的是最近那一次作业落在哪一档、哪块盘上。
	// 两条路径都不必真的存在——磁盘作业只解析策略与申领令牌，读文件是闭包自己的事。
	const first, second = "/srv/one.cbz", "/mnt/two.cbz"
	cfg := func() config.Config {
		c := config.Config{}
		c.Library.StorageProfile = config.StorageProfileCustom
		c.Library.StoragePolicies = []config.LibraryStoragePolicy{
			{Path: "/srv", StorageProfile: config.StorageProfileCustom, IOPolicy: config.StorageIOPolicy{HashConcurrency: 1}},
			{Path: "/mnt", StorageProfile: config.StorageProfileHDDExternal},
		}
		return c
	}
	rec := &recorder{}
	h := New(rec.report, rec.mergeParams, rec.addMetrics, diskwork.NewRunner(cfg, storageio.NewScheduler()))

	for _, path := range []string{first, second} {
		if err := h.Disk(context.Background(), diskwork.Work{Kind: storageio.WorkKindIdentityHash, Path: path}, func() error { return nil }); err != nil {
			t.Fatalf("Disk(%q) returned %v, want nil", path, err)
		}
	}

	got := h.IOMetrics()
	if got.StorageProfile != config.StorageProfileHDDExternal {
		t.Fatalf("storage profile = %q, want the last job's %q", got.StorageProfile, config.StorageProfileHDDExternal)
	}
	if want := config.VolumeKey(second); got.VolumeKey != want {
		t.Fatalf("volume key = %q, want the last job's %q", got.VolumeKey, want)
	}
	if got.HashedFiles != 2 {
		t.Fatalf("hashed files = %d, want 2", got.HashedFiles)
	}
}

func TestHandleDiskGateBlocksBeforeToken(t *testing.T) {
	h, _, sched := newHandle(config.StorageIOPolicy{HashConcurrency: 1})
	path := filepath.Join(t.TempDir(), "book.cbz")
	ctx, gate, cancel := pausedContext(t)
	defer cancel()

	ran := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- h.Disk(ctx, diskwork.Work{Kind: storageio.WorkKindIdentityHash, Path: path}, func() error {
			close(ran)
			return nil
		})
	}()

	select {
	case <-done:
		t.Fatal("expected Disk to block on the paused gate")
	case <-ran:
		t.Fatal("closure ran while the task was paused")
	case <-time.After(80 * time.Millisecond):
	}
	if snapshot := sched.Snapshot(); len(snapshot) != 0 {
		t.Fatalf("paused work must not hold a storage token, snapshot = %+v", snapshot)
	}

	gate.Resume()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Disk returned %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected Disk to run once the gate resumed")
	}
	if got := h.IOMetrics().HashedFiles; got != 1 {
		t.Fatalf("hashed files = %d, want 1", got)
	}
}

func TestHandleCheckpoint(t *testing.T) {
	t.Run("暂停态阻塞、恢复后放行", func(t *testing.T) {
		h, _, _ := newHandle(config.StorageIOPolicy{})
		ctx, gate, cancel := pausedContext(t)
		defer cancel()

		done := make(chan error, 1)
		go func() { done <- h.Checkpoint(ctx) }()
		select {
		case err := <-done:
			t.Fatalf("Checkpoint returned %v while paused; expected it to block", err)
		case <-time.After(60 * time.Millisecond):
		}

		gate.Resume()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Checkpoint returned %v after resume, want nil", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Checkpoint did not return after Resume")
		}
	})

	t.Run("上下文已取消时返回取消错误", func(t *testing.T) {
		h, _, _ := newHandle(config.StorageIOPolicy{})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := h.Checkpoint(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("Checkpoint returned %v, want context.Canceled", err)
		}
	})
}

func TestHandleReportsToInjectedFunctions(t *testing.T) {
	t.Run("整帧", func(t *testing.T) {
		h, rec, _ := newHandle(config.StorageIOPolicy{})
		current, total := 3, 9
		want := Frame{
			Current: &current,
			Total:   &total,
			Phase:   "hashing",
			Item:    "volume_03.cbz",
			Code:    "task.msg.progress",
			Params:  map[string]string{"item": "volume_03.cbz"},
			Metrics: map[string]int64{"hashed_files": 3},
			Labels:  map[string]string{"current_library": "alpha"},
		}
		h.Report(want)

		got := rec.lastFrame(t)
		if got.Phase != want.Phase || got.Item != want.Item || got.Code != want.Code {
			t.Fatalf("frame = %+v, want %+v", got, want)
		}
		if got.Current == nil || *got.Current != current || got.Total == nil || *got.Total != total {
			t.Fatalf("frame counts = %+v, want %d/%d", got, current, total)
		}
		if got.Metrics["hashed_files"] != 3 || got.Labels["current_library"] != "alpha" || got.Params["item"] != "volume_03.cbz" {
			t.Fatalf("frame maps = %+v, want %+v", got, want)
		}
	})

	t.Run("计数推进只动计数与总数", func(t *testing.T) {
		h, rec, _ := newHandle(config.StorageIOPolicy{})
		h.Advance(2, 7, "task.msg.progress", map[string]string{"item": "b"})

		got := rec.lastFrame(t)
		if got.Current == nil || *got.Current != 2 || got.Total == nil || *got.Total != 7 {
			t.Fatalf("frame counts = %+v, want 2/7", got)
		}
		if got.Phase != "" {
			t.Fatalf("Advance must not set a phase, got %q", got.Phase)
		}
		if got.Code != "task.msg.progress" || got.Params["item"] != "b" {
			t.Fatalf("frame message = %+v, want the code and params passed in", got)
		}
	})

	t.Run("阶段只动阶段与文案", func(t *testing.T) {
		h, rec, _ := newHandle(config.StorageIOPolicy{})
		h.Phase("clearing_cache", "task.msg.clearing", map[string]string{"dir": "/tmp"})

		got := rec.lastFrame(t)
		if got.Phase != "clearing_cache" || got.Code != "task.msg.clearing" || got.Params["dir"] != "/tmp" {
			t.Fatalf("frame = %+v, want the phase, code and params passed in", got)
		}
		if got.Current != nil || got.Total != nil {
			t.Fatalf("Phase must not touch the counts, got %+v", got)
		}
	})

	t.Run("合并参数与累加指标各走各的函数", func(t *testing.T) {
		h, rec, _ := newHandle(config.StorageIOPolicy{})
		h.MergeParams(map[string]string{"storage_profile": "hdd_external"})
		h.AddMetrics(map[string]int64{"hashed_files": 4}, map[string]string{"volume_key": "/srv"})

		rec.mu.Lock()
		defer rec.mu.Unlock()
		if len(rec.frames) != 0 {
			t.Fatalf("neither MergeParams nor AddMetrics may report a frame, got %+v", rec.frames)
		}
		if len(rec.params) != 1 || rec.params[0]["storage_profile"] != "hdd_external" {
			t.Fatalf("merged params = %+v, want one entry carrying storage_profile", rec.params)
		}
		if len(rec.metrics) != 1 || rec.metrics[0]["hashed_files"] != 4 {
			t.Fatalf("metric increments = %+v, want one entry carrying hashed_files", rec.metrics)
		}
		if len(rec.metricPs) != 1 || rec.metricPs[0]["volume_key"] != "/srv" {
			t.Fatalf("metric params = %+v, want one entry carrying volume_key", rec.metricPs)
		}
	})
}

func TestHandleIsSafeForConcurrentUse(t *testing.T) {
	h, _, _ := newHandle(config.StorageIOPolicy{HashConcurrency: 2})
	path := filepath.Join(t.TempDir(), "book.cbz")
	work := diskwork.Work{Kind: storageio.WorkKindIdentityHash, Path: path}

	const rounds = 50
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			if err := h.Disk(context.Background(), work, func() error { return nil }); err != nil {
				t.Errorf("Disk returned %v, want nil", err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			io := h.IOMetrics()
			h.Report(Frame{Phase: "hashing", Metrics: map[string]int64{"hashed_files": io.HashedFiles}})
		}
	}()
	wg.Wait()

	if got := h.IOMetrics().HashedFiles; got != rounds {
		t.Fatalf("hashed files = %d, want %d", got, rounds)
	}
}
