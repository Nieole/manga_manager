// 业务说明：本文件是业务回归测试，属于存储 IO 调度层，负责协调扫描、封面提取和阅读器页面读取时的并发访问。
// 它通过自动化断言保护对应业务场景在扫描、读取、展示或配置变更后仍保持兼容。
// 维护时应让用例名称、测试数据和断言结果直接反映真实用户流程，而不是只覆盖实现细节。

package storageio

import (
	"context"
	"testing"
	"time"
)

func TestSchedulerSerializesSameVolume(t *testing.T) {
	s := NewScheduler()
	ctx := context.Background()

	first, err := s.Acquire(ctx, Request{VolumeKey: "e:", Limit: 1, Kind: WorkKindMetadataScan})
	if err != nil {
		t.Fatalf("acquire first lease failed: %v", err)
	}

	acquired := make(chan struct{})
	go func() {
		second, err := s.Acquire(ctx, Request{VolumeKey: "e:", Limit: 1, Kind: WorkKindCoverBuild})
		if err == nil {
			second.Release()
			close(acquired)
		}
	}()

	select {
	case <-acquired:
		t.Fatal("expected second same-volume lease to wait")
	case <-time.After(60 * time.Millisecond):
	}

	first.Release()
	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("expected second lease after release")
	}
}

func TestSchedulerPausesBackgroundWhileReaderActive(t *testing.T) {
	s := NewScheduler()
	ctx := context.Background()

	reader, err := s.Acquire(ctx, Request{VolumeKey: "e:", Limit: 1, Kind: WorkKindReader})
	if err != nil {
		t.Fatalf("acquire reader lease failed: %v", err)
	}

	acquired := make(chan struct{})
	go func() {
		background, err := s.Acquire(ctx, Request{
			VolumeKey:          "e:",
			Limit:              1,
			Kind:               WorkKindCoverBuild,
			PauseWhenReading:   true,
			ReaderIdleDuration: 10 * time.Millisecond,
		})
		if err == nil {
			background.Release()
			close(acquired)
		}
	}()

	select {
	case <-acquired:
		t.Fatal("expected background lease to wait while reader is active")
	case <-time.After(60 * time.Millisecond):
	}

	reader.Release()
	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("expected background lease after reader idle window")
	}
}

func TestSchedulerReportsPausedWait(t *testing.T) {
	s := NewScheduler()
	reader, err := s.Acquire(context.Background(), Request{VolumeKey: "e:", Limit: 1, Kind: WorkKindReader})
	if err != nil {
		t.Fatalf("acquire reader lease failed: %v", err)
	}

	done := make(chan Lease, 1)
	go func() {
		background, err := s.Acquire(context.Background(), Request{
			VolumeKey:          "e:",
			Limit:              1,
			Kind:               WorkKindCacheWrite,
			PauseWhenReading:   true,
			ReaderIdleDuration: 10 * time.Millisecond,
		})
		if err == nil {
			done <- background
		}
	}()

	time.Sleep(60 * time.Millisecond)
	reader.Release()
	var background Lease
	select {
	case background = <-done:
	case <-time.After(time.Second):
		t.Fatal("expected background lease after reader release")
	}
	defer background.Release()
	if background.PausedWait <= 0 {
		t.Fatalf("expected paused wait to be recorded, got %+v", background)
	}
	if background.Wait < background.PausedWait {
		t.Fatalf("expected total wait to include paused wait, got %+v", background)
	}
}

func TestSchedulerLetsWaitingReaderPrecedeBackground(t *testing.T) {
	s := NewScheduler()
	ctx := context.Background()

	firstBackground, err := s.Acquire(ctx, Request{VolumeKey: "e:", Limit: 1, Kind: WorkKindCoverBuild})
	if err != nil {
		t.Fatalf("acquire first background lease failed: %v", err)
	}

	readerAcquired := make(chan Lease, 1)
	go func() {
		reader, err := s.Acquire(ctx, Request{VolumeKey: "e:", Limit: 1, Kind: WorkKindReader})
		if err == nil {
			readerAcquired <- reader
		}
	}()
	time.Sleep(40 * time.Millisecond)

	backgroundAcquired := make(chan struct{})
	go func() {
		background, err := s.Acquire(ctx, Request{
			VolumeKey:          "e:",
			Limit:              1,
			Kind:               WorkKindIdentityHash,
			PauseWhenReading:   true,
			ReaderIdleDuration: 10 * time.Millisecond,
		})
		if err == nil {
			background.Release()
			close(backgroundAcquired)
		}
	}()

	firstBackground.Release()
	reader := <-readerAcquired
	select {
	case <-backgroundAcquired:
		t.Fatal("expected waiting reader to acquire before background")
	case <-time.After(60 * time.Millisecond):
	}

	reader.Release()
	select {
	case <-backgroundAcquired:
	case <-time.After(time.Second):
		t.Fatal("expected background after reader release")
	}
}

func TestSchedulerLetsReaderBypassPauseableBackground(t *testing.T) {
	s := NewScheduler()
	ctx := context.Background()

	background, err := s.Acquire(ctx, Request{
		VolumeKey:        "e:",
		Limit:            1,
		Kind:             WorkKindMetadataScan,
		PauseWhenReading: true,
	})
	if err != nil {
		t.Fatalf("acquire background lease failed: %v", err)
	}
	defer background.Release()

	reader, err := s.Acquire(ctx, Request{VolumeKey: "e:", Limit: 1, Kind: WorkKindReader})
	if err != nil {
		t.Fatalf("expected reader to bypass active pauseable background: %v", err)
	}
	defer reader.Release()

	snapshots := s.Snapshot()
	if len(snapshots) != 1 || snapshots[0].ReaderActive != 1 || snapshots[0].Active != 2 {
		t.Fatalf("unexpected snapshot after reader bypass: %+v", snapshots)
	}
}

func TestSchedulerCapsReaderBypassToReaderLimit(t *testing.T) {
	s := NewScheduler()
	ctx := context.Background()

	background, err := s.Acquire(ctx, Request{
		VolumeKey:        "e:",
		Limit:            1,
		Kind:             WorkKindCoverBuild,
		PauseWhenReading: true,
	})
	if err != nil {
		t.Fatalf("acquire background lease failed: %v", err)
	}
	defer background.Release()

	firstReader, err := s.Acquire(ctx, Request{VolumeKey: "e:", Limit: 1, Kind: WorkKindReader})
	if err != nil {
		t.Fatalf("acquire first reader failed: %v", err)
	}
	defer firstReader.Release()

	acquired := make(chan Lease, 1)
	go func() {
		secondReader, err := s.Acquire(ctx, Request{VolumeKey: "e:", Limit: 1, Kind: WorkKindReader})
		if err == nil {
			acquired <- secondReader
		}
	}()

	select {
	case secondReader := <-acquired:
		secondReader.Release()
		t.Fatal("expected second reader to wait for the active reader bypass slot")
	case <-time.After(60 * time.Millisecond):
	}

	firstReader.Release()
	select {
	case secondReader := <-acquired:
		secondReader.Release()
	case <-time.After(time.Second):
		t.Fatal("expected second reader after first reader releases")
	}
}

func TestSchedulerPauseBackgroundKeepsReaderAvailable(t *testing.T) {
	s := NewScheduler()
	s.PauseBackground()

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	if _, err := s.Acquire(ctx, Request{VolumeKey: "e:", Limit: 1, Kind: WorkKindCoverBuild}); err == nil {
		t.Fatal("expected paused background acquisition to time out")
	}

	reader, err := s.Acquire(context.Background(), Request{VolumeKey: "e:", Limit: 1, Kind: WorkKindReader})
	if err != nil {
		t.Fatalf("expected reader to bypass background pause: %v", err)
	}
	reader.Release()

	s.ResumeBackground()
	background, err := s.Acquire(context.Background(), Request{VolumeKey: "e:", Limit: 1, Kind: WorkKindCoverBuild})
	if err != nil {
		t.Fatalf("expected background after resume: %v", err)
	}
	background.Release()
}

func TestSchedulerIdleOnlyWaitsForSameVolumeActivity(t *testing.T) {
	s := NewScheduler()
	ctx := context.Background()

	first, err := s.Acquire(ctx, Request{VolumeKey: "e:", Limit: 2, Kind: WorkKindMetadataScan})
	if err != nil {
		t.Fatalf("acquire active lease failed: %v", err)
	}

	acquired := make(chan struct{})
	go func() {
		second, err := s.Acquire(ctx, Request{
			VolumeKey: "e:",
			Limit:     2,
			Kind:      WorkKindIdentityHash,
			IdleOnly:  true,
		})
		if err == nil {
			second.Release()
			close(acquired)
		}
	}()

	select {
	case <-acquired:
		t.Fatal("expected idle-only background work to wait for same-volume activity")
	case <-time.After(60 * time.Millisecond):
	}

	snapshots := s.Snapshot()
	if len(snapshots) != 1 || snapshots[0].BackgroundWaiting != 1 || snapshots[0].PauseReason != "volume_busy" {
		t.Fatalf("unexpected idle-only snapshot: %+v", snapshots)
	}

	first.Release()
	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("expected idle-only lease after volume became idle")
	}
}

func TestSchedulerSnapshot(t *testing.T) {
	s := NewScheduler()
	s.PauseBackground()
	lease, err := s.Acquire(context.Background(), Request{VolumeKey: "e:", Limit: 2, Kind: WorkKindReader})
	if err != nil {
		t.Fatalf("acquire reader failed: %v", err)
	}
	defer lease.Release()

	snapshots := s.Snapshot()
	if len(snapshots) != 1 {
		t.Fatalf("expected one snapshot, got %+v", snapshots)
	}
	if snapshots[0].VolumeKey != "e:" || snapshots[0].Active != 1 || snapshots[0].Limit != 2 || snapshots[0].ReaderActive != 1 || !snapshots[0].BackgroundPaused {
		t.Fatalf("unexpected snapshot: %+v", snapshots[0])
	}
}
