// 本文件是业务回归测试，属于存储 IO 调度层，负责协调扫描、封面提取和阅读器页面读取时的并发访问。
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

func TestSchedulerSnapshotMatchesRequestedVolumeKey(t *testing.T) {
	// 面板一行代表一块盘：快照必须按调用方传入的卷键归拢，否则读者数、后台排队数与暂停原因会落到
	// 另一条行上，用户排障时读到的是假数据。
	cases := []struct {
		name      string
		volumeKey string
	}{
		{name: "含大写字母的卷键只出一行且计数正确", volumeKey: "/Volumes"},
		{name: "全小写卷键不退化", volumeKey: "/var"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewScheduler()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			reader, err := s.Acquire(ctx, Request{VolumeKey: tc.volumeKey, Limit: 1, Kind: WorkKindReader})
			if err != nil {
				t.Fatalf("acquire reader lease failed: %v", err)
			}
			defer reader.Release()

			backgroundDone := make(chan struct{})
			go func() {
				defer close(backgroundDone)
				background, err := s.Acquire(ctx, Request{
					VolumeKey:        tc.volumeKey,
					Limit:            1,
					Kind:             WorkKindCoverBuild,
					PauseWhenReading: true,
				})
				if err == nil {
					background.Release()
				}
			}()

			snapshots := waitForBackgroundWaiting(t, s, 1)
			if len(snapshots) != 1 {
				t.Fatalf("expected one snapshot row per volume, got %+v", snapshots)
			}
			got := snapshots[0]
			want := VolumeSnapshot{
				VolumeKey:         tc.volumeKey,
				Active:            1,
				Limit:             1,
				ReaderActive:      1,
				BackgroundWaiting: 1,
				PauseReason:       "reader_active",
			}
			if got != want {
				t.Fatalf("snapshot = %+v, want %+v", got, want)
			}

			cancel()
			<-backgroundDone
		})
	}

	t.Run("大写卷键的并发上限仍按卷生效", func(t *testing.T) {
		s := NewScheduler()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		first, err := s.Acquire(ctx, Request{VolumeKey: "/Volumes", Limit: 1, Kind: WorkKindMetadataScan})
		if err != nil {
			t.Fatalf("acquire first lease failed: %v", err)
		}

		acquired := make(chan Lease, 1)
		go func() {
			second, err := s.Acquire(ctx, Request{VolumeKey: "/Volumes", Limit: 1, Kind: WorkKindCoverBuild})
			if err == nil {
				acquired <- second
			}
		}()

		snapshots := waitForBackgroundWaiting(t, s, 1)
		select {
		case second := <-acquired:
			second.Release()
			t.Fatal("expected second same-volume lease to wait at the concurrency limit")
		case <-time.After(60 * time.Millisecond):
		}
		if len(snapshots) != 1 {
			t.Fatalf("expected one snapshot row per volume, got %+v", snapshots)
		}
		got := snapshots[0]
		want := VolumeSnapshot{
			VolumeKey:         "/Volumes",
			Active:            1,
			Limit:             1,
			BackgroundWaiting: 1,
			PauseReason:       "volume_busy",
		}
		if got != want {
			t.Fatalf("snapshot = %+v, want %+v", got, want)
		}

		first.Release()
		select {
		case second := <-acquired:
			second.Release()
		case <-time.After(time.Second):
			t.Fatal("expected second lease after the first releases")
		}
	})

	t.Run("大小写不同的卷键是不同的卷", func(t *testing.T) {
		// 卷键在本包里是不透明身份，不折叠大小写：Linux 上 /Data 与 /data 本就是两个目录，
		// 该不该按同一块盘限流由推导卷键的 config 决定。
		s := NewScheduler()
		ctx := context.Background()

		upper, err := s.Acquire(ctx, Request{VolumeKey: "/Data", Limit: 1, Kind: WorkKindMetadataScan})
		if err != nil {
			t.Fatalf("acquire upper-case volume lease failed: %v", err)
		}
		defer upper.Release()

		acquired := make(chan Lease, 1)
		go func() {
			lower, err := s.Acquire(ctx, Request{VolumeKey: "/data", Limit: 1, Kind: WorkKindMetadataScan})
			if err == nil {
				acquired <- lower
			}
		}()
		select {
		case lower := <-acquired:
			lower.Release()
		case <-time.After(time.Second):
			t.Fatal("expected a different-case volume key to use its own limiter")
		}

		if snapshots := s.Snapshot(); len(snapshots) != 2 {
			t.Fatalf("expected one snapshot row per volume key, got %+v", snapshots)
		}
	})
}

// waitForBackgroundWaiting 等到快照里排队的后台作业总数达到 want，返回当时的快照。它按总数而非按行
// 等待，好让本文件的断言留给「归到哪一行」本身。
func waitForBackgroundWaiting(t *testing.T, s *Scheduler, want int) []VolumeSnapshot {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		snapshots := s.Snapshot()
		total := 0
		for _, snapshot := range snapshots {
			total += snapshot.BackgroundWaiting
		}
		if total == want {
			return snapshots
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d queued background work, got %+v", want, snapshots)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
