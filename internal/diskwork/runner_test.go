// 本文件守磁盘作业的三条顺序不变量与一张上限表：闸门必然先于令牌、令牌在每条出口（含闭包
// panic）都被归还、每个工种的并发上限只由工种决定。
//
// 三条都没有编译期约束，破了也不报错，只会在慢盘上显形：被暂停的作业攥着令牌堵死阅读、
// 那块盘的并发额度永久少一格、或某个工种被一个与它不相干的字段压着。
//
// 每条用例各建一个调度器：包级实例会让它们经由按卷计数的限流器互相污染。

package diskwork

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"manga-manager/internal/config"
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

// captureLogs 把默认 logger 换成写进缓冲的 JSON handler，返回读出已记录条目的函数。
// 只在被观测的 goroutine 结束后调用它，缓冲才有 happens-before 保证。
func captureLogs(t *testing.T) func() []map[string]any {
	t.Helper()
	buffer := &bytes.Buffer{}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buffer, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	return func() []map[string]any {
		t.Helper()
		var entries []map[string]any
		for _, line := range strings.Split(strings.TrimSpace(buffer.String()), "\n") {
			if line == "" {
				continue
			}
			entry := map[string]any{}
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				t.Fatalf("log line %q is not JSON: %v", line, err)
			}
			entries = append(entries, entry)
		}
		return entries
	}
}

// volumeUsage 从调度器快照里读某块卷当前的并发上限与在用数。
func volumeUsage(t *testing.T, sched *storageio.Scheduler, volumeKey string) (limit, active int) {
	t.Helper()
	for _, snapshot := range sched.Snapshot() {
		if strings.EqualFold(snapshot.VolumeKey, volumeKey) {
			return snapshot.Limit, snapshot.Active
		}
	}
	return 0, 0
}

func TestRunnerAppliesPerKindConcurrencyLimit(t *testing.T) {
	base := config.StorageIOPolicy{
		ScanConcurrency:        5,
		ArchiveOpenConcurrency: 2,
		CoverConcurrency:       3,
		HashConcurrency:        4,
	}
	cases := []struct {
		name   string
		kind   storageio.WorkKind
		policy config.StorageIOPolicy
		limit  int
	}{
		{"读归档元数据按归档打开并发", storageio.WorkKindMetadataScan, base, 2},
		{"算文件指纹按哈希并发", storageio.WorkKindIdentityHash, base, 4},
		{"构建封面按归档打开与封面并发取小", storageio.WorkKindCoverBuild, base, 2},
		{
			"构建封面取小时封面并发更小也生效",
			storageio.WorkKindCoverBuild,
			config.StorageIOPolicy{ScanConcurrency: 5, ArchiveOpenConcurrency: 6, CoverConcurrency: 2, HashConcurrency: 4},
			2,
		},
		{"写缓存按封面并发", storageio.WorkKindCacheWrite, base, 3},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sched := storageio.NewScheduler()
			runner := NewRunner(stubConfig(tc.policy), sched)
			path := filepath.Join(t.TempDir(), "book.cbz")
			volumeKey := config.VolumeKey(path)

			var gotLimit, gotActive int
			_, err := runner.Do(context.Background(), Work{Kind: tc.kind, Path: path}, func() error {
				gotLimit, gotActive = volumeUsage(t, sched, volumeKey)
				return nil
			})
			if err != nil {
				t.Fatalf("Do returned %v, want nil", err)
			}
			if gotLimit != tc.limit {
				t.Fatalf("limit while running = %d, want %d", gotLimit, tc.limit)
			}
			if gotActive != 1 {
				t.Fatalf("active while running = %d, want 1", gotActive)
			}
			if _, active := volumeUsage(t, sched, volumeKey); active != 0 {
				t.Fatalf("active after Do = %d, want 0", active)
			}
		})
	}
}

func TestRunnerSkipsTokenWhenUnlimited(t *testing.T) {
	cases := []struct {
		name   string
		policy config.StorageIOPolicy
		path   string
	}{
		{"四项并发全为零", config.StorageIOPolicy{}, filepath.Join(t.TempDir(), "book.cbz")},
		{"卷键为空", config.StorageIOPolicy{HashConcurrency: 1}, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sched := storageio.NewScheduler()
			// 后台整体暂停也拦不住短路的作业：默认档与 SSD 档从来不排这个队。
			sched.PauseBackground()
			runner := NewRunner(stubConfig(tc.policy), sched)

			ran := false
			stats, err := runner.Do(context.Background(), Work{Kind: storageio.WorkKindIdentityHash, Path: tc.path}, func() error {
				ran = true
				return nil
			})
			if err != nil {
				t.Fatalf("Do returned %v, want nil", err)
			}
			if !ran {
				t.Fatal("closure did not run")
			}
			if snapshot := sched.Snapshot(); len(snapshot) != 0 {
				t.Fatalf("expected no storage token, snapshot = %+v", snapshot)
			}
			if stats.Wait != 0 || stats.PausedWait != 0 {
				t.Fatalf("stats = %+v, want zero waits", stats)
			}
		})
	}
}

func TestRunnerPauseGateBlocksBeforeToken(t *testing.T) {
	gate := taskcontrol.NewPauseGate()
	gate.Pause()
	ctx, cancel := context.WithCancel(taskcontrol.WithPauseGate(context.Background(), gate))
	defer cancel()

	sched := storageio.NewScheduler()
	runner := NewRunner(stubConfig(config.StorageIOPolicy{HashConcurrency: 1}), sched)
	path := filepath.Join(t.TempDir(), "book.cbz")

	var ran atomic.Bool
	done := make(chan error, 1)
	go func() {
		_, err := runner.Do(ctx, Work{Kind: storageio.WorkKindIdentityHash, Path: path}, func() error {
			ran.Store(true)
			return nil
		})
		done <- err
	}()

	select {
	case <-done:
		t.Fatal("expected Do to block on the paused gate")
	case <-time.After(80 * time.Millisecond):
	}
	if ran.Load() {
		t.Fatal("closure ran while the task was paused")
	}
	if snapshot := sched.Snapshot(); len(snapshot) != 0 {
		t.Fatalf("paused work must not hold a storage token, snapshot = %+v", snapshot)
	}

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Do returned %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("expected Do to return once the context is cancelled")
	}
	if ran.Load() {
		t.Fatal("closure ran after cancellation")
	}
}

func TestRunnerSkipsWorkWhenContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	sched := storageio.NewScheduler()
	runner := NewRunner(stubConfig(config.StorageIOPolicy{HashConcurrency: 1}), sched)

	ran := false
	_, err := runner.Do(ctx, Work{Kind: storageio.WorkKindIdentityHash, Path: filepath.Join(t.TempDir(), "book.cbz")}, func() error {
		ran = true
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Do returned %v, want context.Canceled", err)
	}
	if ran {
		t.Fatal("closure ran on a cancelled context")
	}
	if snapshot := sched.Snapshot(); len(snapshot) != 0 {
		t.Fatalf("cancelled work must not take a storage token, snapshot = %+v", snapshot)
	}
}

func TestRunnerSkipsWorkWhenCancelledWhileQueued(t *testing.T) {
	sched := storageio.NewScheduler()
	runner := NewRunner(stubConfig(config.StorageIOPolicy{HashConcurrency: 1}), sched)
	path := filepath.Join(t.TempDir(), "book.cbz")
	volumeKey := config.VolumeKey(path)

	// 占满该卷的唯一额度，让 Do 停在排队上——取消要在这条路径上也生效。
	held, err := sched.Acquire(context.Background(), storageio.Request{
		VolumeKey: volumeKey,
		Limit:     1,
		Kind:      storageio.WorkKindIdentityHash,
	})
	if err != nil {
		t.Fatalf("holding the only slot failed: %v", err)
	}
	defer held.Release()

	ctx, cancel := context.WithCancel(context.Background())
	var ran atomic.Bool
	done := make(chan error, 1)
	go func() {
		_, err := runner.Do(ctx, Work{Kind: storageio.WorkKindIdentityHash, Path: path}, func() error {
			ran.Store(true)
			return nil
		})
		done <- err
	}()

	select {
	case <-done:
		t.Fatal("expected Do to queue for the busy volume")
	case <-time.After(80 * time.Millisecond):
	}
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Do returned %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("expected Do to return once the context is cancelled")
	}
	if ran.Load() {
		t.Fatal("closure ran after cancellation")
	}
	if _, active := volumeUsage(t, sched, volumeKey); active != 1 {
		t.Fatalf("active = %d, want 1 (only the held slot)", active)
	}
}

func TestRunnerReleasesTokenOnEveryExit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book.cbz")
	volumeKey := config.VolumeKey(path)
	work := Work{Kind: storageio.WorkKindIdentityHash, Path: path}

	t.Run("闭包返回错误", func(t *testing.T) {
		sched := storageio.NewScheduler()
		runner := NewRunner(stubConfig(config.StorageIOPolicy{HashConcurrency: 1}), sched)

		wantErr := errors.New("work failed")
		_, err := runner.Do(context.Background(), work, func() error { return wantErr })
		if !errors.Is(err, wantErr) {
			t.Fatalf("Do returned %v, want %v", err, wantErr)
		}
		if _, active := volumeUsage(t, sched, volumeKey); active != 0 {
			t.Fatalf("active after a failing closure = %d, want 0", active)
		}
	})

	t.Run("闭包 panic", func(t *testing.T) {
		sched := storageio.NewScheduler()
		runner := NewRunner(stubConfig(config.StorageIOPolicy{HashConcurrency: 1}), sched)

		recovered := func() (value any) {
			defer func() { value = recover() }()
			_, _ = runner.Do(context.Background(), work, func() error { panic("work exploded") })
			return nil
		}()
		if recovered == nil {
			t.Fatal("expected the panic to propagate to the caller")
		}
		if _, active := volumeUsage(t, sched, volumeKey); active != 0 {
			t.Fatalf("active after a panicking closure = %d, want 0", active)
		}
	})
}

func TestRunnerLogsSlowTokenAcquisition(t *testing.T) {
	cases := []struct {
		name              string
		cancelWhileQueued bool
	}{
		{"等到了令牌", false},
		// 等了很久最终被取消的申领同样不该是无声的——它恰是慢盘上最该看见的一种。
		{"排队中被取消", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureLogs(t)
			sched := storageio.NewScheduler()
			runner := NewRunner(stubConfig(config.StorageIOPolicy{HashConcurrency: 1}), sched)
			path := filepath.Join(t.TempDir(), "book.cbz")

			// 占满该卷的唯一额度，逼出一段超过阈值的等待。
			held, err := sched.Acquire(context.Background(), storageio.Request{
				VolumeKey: config.VolumeKey(path),
				Limit:     1,
				Kind:      storageio.WorkKindIdentityHash,
			})
			if err != nil {
				t.Fatalf("holding the only slot failed: %v", err)
			}
			defer held.Release()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan struct{})
			go func() {
				defer close(done)
				_, _ = runner.Do(ctx, Work{Kind: storageio.WorkKindIdentityHash, Path: path}, func() error { return nil })
			}()

			time.Sleep(slowAcquireThreshold + 80*time.Millisecond)
			if tc.cancelWhileQueued {
				cancel()
			} else {
				held.Release()
			}
			<-done

			var slow map[string]any
			entries := logs()
			for _, entry := range entries {
				if entry["msg"] == "Disk work waited for storage IO token" {
					slow = entry
				}
			}
			if slow == nil {
				t.Fatalf("expected a slow-acquire log, got %v", entries)
			}
			for _, key := range []string{"work_kind", "path", "storage_profile", "volume_key", "io_wait_ms"} {
				if _, ok := slow[key]; !ok {
					t.Fatalf("slow-acquire log is missing %q: %v", key, slow)
				}
			}
			if slow["work_kind"] != string(storageio.WorkKindIdentityHash) {
				t.Fatalf("work_kind = %v, want %q", slow["work_kind"], storageio.WorkKindIdentityHash)
			}
			if slow["path"] != path {
				t.Fatalf("path = %v, want %q", slow["path"], path)
			}
		})
	}
}

func TestRunnerStatsReportsResolvedPolicy(t *testing.T) {
	sched := storageio.NewScheduler()
	runner := NewRunner(stubConfig(config.StorageIOPolicy{HashConcurrency: 1}), sched)
	path := filepath.Join(t.TempDir(), "book.cbz")

	stats, err := runner.Do(context.Background(), Work{Kind: storageio.WorkKindIdentityHash, Path: path}, func() error { return nil })
	if err != nil {
		t.Fatalf("Do returned %v, want nil", err)
	}
	if stats.StorageProfile != config.StorageProfileCustom {
		t.Fatalf("storage profile = %q, want %q", stats.StorageProfile, config.StorageProfileCustom)
	}
	if want := config.VolumeKey(path); stats.VolumeKey != want {
		t.Fatalf("volume key = %q, want %q", stats.VolumeKey, want)
	}
}

func TestRunnerStatsReportsWaitAndPausedWait(t *testing.T) {
	sched := storageio.NewScheduler()
	sched.PauseBackground()
	runner := NewRunner(stubConfig(config.StorageIOPolicy{HashConcurrency: 1}), sched)
	path := filepath.Join(t.TempDir(), "book.cbz")

	type outcome struct {
		stats Stats
		err   error
	}
	done := make(chan outcome, 1)
	go func() {
		stats, err := runner.Do(context.Background(), Work{Kind: storageio.WorkKindIdentityHash, Path: path}, func() error { return nil })
		done <- outcome{stats, err}
	}()

	select {
	case <-done:
		t.Fatal("expected Do to wait while background IO is paused")
	case <-time.After(100 * time.Millisecond):
	}
	sched.ResumeBackground()

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("Do returned %v, want nil", got.err)
		}
		if got.stats.Wait < 50*time.Millisecond {
			t.Fatalf("wait = %v, want at least 50ms", got.stats.Wait)
		}
		if got.stats.PausedWait < 50*time.Millisecond {
			t.Fatalf("paused wait = %v, want at least 50ms", got.stats.PausedWait)
		}
	case <-time.After(time.Second):
		t.Fatal("expected Do to return once background IO resumes")
	}
}

func TestRunnerPolicyFromDecidesPolicyOnSameVolume(t *testing.T) {
	root := t.TempDir()
	libraryPath := filepath.Join(root, "library")
	cacheDir := filepath.Join(root, "cache")
	// 缓存目录落不进资料库的路径前缀，自己只能拿到不限流的全局默认档。
	cfg := func() config.Config {
		c := config.Config{}
		c.Library.StorageProfile = config.StorageProfileCustom
		c.Library.StoragePolicies = []config.LibraryStoragePolicy{{
			Path:           libraryPath,
			StorageProfile: config.StorageProfileCustom,
			IOPolicy:       config.StorageIOPolicy{CoverConcurrency: 3},
		}}
		return c
	}

	t.Run("留空时按 Path 解析", func(t *testing.T) {
		sched := storageio.NewScheduler()
		runner := NewRunner(cfg, sched)
		_, err := runner.Do(context.Background(), Work{Kind: storageio.WorkKindCacheWrite, Path: cacheDir}, func() error { return nil })
		if err != nil {
			t.Fatalf("Do returned %v, want nil", err)
		}
		if snapshot := sched.Snapshot(); len(snapshot) != 0 {
			t.Fatalf("expected no storage token, snapshot = %+v", snapshot)
		}
	})

	t.Run("同卷时服从藏书那一侧的策略", func(t *testing.T) {
		sched := storageio.NewScheduler()
		runner := NewRunner(cfg, sched)

		var gotLimit int
		stats, err := runner.Do(context.Background(), Work{
			Kind:       storageio.WorkKindCacheWrite,
			Path:       cacheDir,
			PolicyFrom: filepath.Join(libraryPath, "book.cbz"),
		}, func() error {
			gotLimit, _ = volumeUsage(t, sched, config.VolumeKey(cacheDir))
			return nil
		})
		if err != nil {
			t.Fatalf("Do returned %v, want nil", err)
		}
		if gotLimit != 3 {
			t.Fatalf("limit while running = %d, want 3", gotLimit)
		}
		if want := config.VolumeKey(cacheDir); stats.VolumeKey != want {
			t.Fatalf("volume key = %q, want %q", stats.VolumeKey, want)
		}
	})
}
