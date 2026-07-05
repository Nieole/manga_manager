// 业务说明：本文件补充存储 IO 调度层的边界回归：卷键归一化/派生、非法请求短路、并发上限钳制（Limit=N）、
// 幂等释放不使计数为负，验证多用户/多任务并发下限流与快照统计正确。
package storageio

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestLimiterKeyNormalizesVolume(t *testing.T) {
	// 卷键应 trim + 小写，并以 limit 拼接，保证同一物理卷不同大小写映射到同一限流桶前缀。
	if got := limiterKey("  E: ", 3); got != "e:|3" {
		t.Errorf("limiterKey = %q, want e:|3", got)
	}
	if got := limiterKey("D:", 1); got != "d:|1" {
		t.Errorf("limiterKey = %q, want d:|1", got)
	}
	// volumeFromLimiterKey 应取回卷键（末个 | 之前）。
	if got := volumeFromLimiterKey("e:|3"); got != "e:" {
		t.Errorf("volumeFromLimiterKey = %q, want e:", got)
	}
	// 无分隔符 → 原样返回。
	if got := volumeFromLimiterKey("noseparator"); got != "noseparator" {
		t.Errorf("volumeFromLimiterKey(no sep) = %q", got)
	}
	// 卷键本身含 | 时应按最后一个 | 切分（保留卷键中的 |）。
	if got := volumeFromLimiterKey("a|b|5"); got != "a|b" {
		t.Errorf("volumeFromLimiterKey(multi) = %q, want a|b", got)
	}
}

func TestAcquireNoopOnInvalidRequest(t *testing.T) {
	s := NewScheduler()
	// Limit<=0 → 空 Lease、无错误、不建限流桶。
	lease, err := s.Acquire(context.Background(), Request{VolumeKey: "e:", Limit: 0, Kind: WorkKindCoverBuild})
	if err != nil {
		t.Fatalf("expected nil error for Limit=0, got %v", err)
	}
	lease.Release() // done 为 nil，应安全无操作。

	// 空/空白卷键 → 同样短路。
	lease2, err := s.Acquire(context.Background(), Request{VolumeKey: "   ", Limit: 2, Kind: WorkKindReader})
	if err != nil {
		t.Fatalf("expected nil error for blank volume, got %v", err)
	}
	lease2.Release()

	if snaps := s.Snapshot(); len(snaps) != 0 {
		t.Fatalf("expected no limiter/reader state for invalid requests, got %+v", snaps)
	}
}

func TestAcquireConcurrencyClampAtLimit(t *testing.T) {
	s := NewScheduler()
	ctx := context.Background()

	// Limit=2：同卷同限额共享一个限流桶，前两个并发获取都应成功。
	first, err := s.Acquire(ctx, Request{VolumeKey: "e:", Limit: 2, Kind: WorkKindMetadataScan})
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	second, err := s.Acquire(ctx, Request{VolumeKey: "e:", Limit: 2, Kind: WorkKindMetadataScan})
	if err != nil {
		t.Fatalf("second acquire failed: %v", err)
	}

	snaps := s.Snapshot()
	if len(snaps) != 1 || snaps[0].Active != 2 || snaps[0].Limit != 2 {
		t.Fatalf("expected Active=2 Limit=2, got %+v", snaps)
	}

	// 第三个必须等待，直到有名额释放。
	acquired := make(chan Lease, 1)
	go func() {
		third, err := s.Acquire(ctx, Request{VolumeKey: "e:", Limit: 2, Kind: WorkKindMetadataScan})
		if err == nil {
			acquired <- third
		}
	}()
	select {
	case <-acquired:
		t.Fatal("expected third acquire to block at concurrency limit")
	case <-time.After(60 * time.Millisecond):
	}

	first.Release()
	select {
	case third := <-acquired:
		third.Release()
	case <-time.After(time.Second):
		t.Fatal("expected third acquire after a slot freed")
	}
	second.Release()
}

func TestDifferentLimitsCreateSeparateBuckets(t *testing.T) {
	s := NewScheduler()
	ctx := context.Background()
	// 同卷但不同 limit → 不同限流桶键，因此互不阻塞（都能立即获取）。
	a, err := s.Acquire(ctx, Request{VolumeKey: "e:", Limit: 1, Kind: WorkKindCoverBuild})
	if err != nil {
		t.Fatalf("acquire limit=1 failed: %v", err)
	}
	defer a.Release()

	done := make(chan Lease, 1)
	go func() {
		b, err := s.Acquire(ctx, Request{VolumeKey: "e:", Limit: 2, Kind: WorkKindCoverBuild})
		if err == nil {
			done <- b
		}
	}()
	select {
	case b := <-done:
		b.Release()
	case <-time.After(200 * time.Millisecond):
		t.Fatal("different-limit acquire should not block on a separate bucket")
	}
}

func TestReleaseIsIdempotent(t *testing.T) {
	s := NewScheduler()
	lease, err := s.Acquire(context.Background(), Request{VolumeKey: "e:", Limit: 1, Kind: WorkKindCacheWrite, PauseWhenReading: true})
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}
	// 重复释放不应使内部计数为负或 panic。
	lease.Release()
	lease.Release()

	if snaps := s.Snapshot(); len(snaps) == 1 && snaps[0].Active != 0 {
		t.Fatalf("expected Active=0 after release, got %+v", snaps)
	}

	// 释放后应能再次获取（名额已归还）。
	next, err := s.Acquire(context.Background(), Request{VolumeKey: "e:", Limit: 1, Kind: WorkKindCacheWrite})
	if err != nil {
		t.Fatalf("expected re-acquire after release, got %v", err)
	}
	next.Release()
}

func TestConcurrentReleaseRaceSafe(t *testing.T) {
	s := NewScheduler()
	lease, err := s.Acquire(context.Background(), Request{VolumeKey: "e:", Limit: 1, Kind: WorkKindReader})
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}
	// releaseOnce 保证多 goroutine 并发释放只生效一次（配合 -race 检验）。
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lease.Release()
		}()
	}
	wg.Wait()
	if snaps := s.Snapshot(); len(snaps) == 1 && snaps[0].ReaderActive != 0 {
		t.Fatalf("expected ReaderActive=0 after concurrent releases, got %+v", snaps)
	}
}
