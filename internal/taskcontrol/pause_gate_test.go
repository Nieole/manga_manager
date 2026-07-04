// 业务说明：本文件是并发原语回归测试，验证 PauseGate 的暂停/恢复/等待唤醒/取消传播与重复调用幂等，
// 保障扫描等长任务的暂停控制在并发下行为正确、不丢唤醒、不误阻塞。
package taskcontrol

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestPauseGateWaitReturnsWhenNotPaused(t *testing.T) {
	g := NewPauseGate()

	// 未暂停时 Wait 应立即返回 ctx.Err()（此处 nil）。
	if err := g.Wait(context.Background()); err != nil {
		t.Fatalf("expected nil error when not paused, got %v", err)
	}
	// nil ctx 亦应立即返回 nil。
	if err := g.Wait(context.TODO()); err != nil {
		t.Fatalf("expected nil error for background ctx, got %v", err)
	}
}

func TestPauseGateWaitBlocksUntilResume(t *testing.T) {
	g := NewPauseGate()
	g.Pause()
	if !g.IsPaused() {
		t.Fatal("expected gate to report paused")
	}
	if g.PausedAt().IsZero() {
		t.Fatal("expected PausedAt to be set while paused")
	}

	done := make(chan error, 1)
	go func() { done <- g.Wait(context.Background()) }()

	// 暂停期间 Wait 必须阻塞。
	select {
	case err := <-done:
		t.Fatalf("Wait returned %v while still paused; expected it to block", err)
	case <-time.After(50 * time.Millisecond):
	}

	g.Resume()
	if g.IsPaused() {
		t.Fatal("expected gate to report resumed")
	}
	if !g.PausedAt().IsZero() {
		t.Fatal("expected PausedAt to reset after resume")
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected nil error after resume, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not return after Resume")
	}
}

func TestPauseGateWaitHonorsContextCancellation(t *testing.T) {
	g := NewPauseGate()
	g.Pause()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- g.Wait(ctx) }()

	select {
	case err := <-done:
		t.Fatalf("Wait returned %v before cancel; expected it to block", err)
	case <-time.After(50 * time.Millisecond):
	}

	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not observe context cancellation while paused")
	}
}

func TestPauseGateReleasesAllConcurrentWaiters(t *testing.T) {
	g := NewPauseGate()
	g.Pause()

	const waiters = 16
	var wg sync.WaitGroup
	wg.Add(waiters)
	for i := 0; i < waiters; i++ {
		go func() {
			defer wg.Done()
			_ = g.Wait(context.Background())
		}()
	}

	// 给所有 waiter 时间进入阻塞。
	time.Sleep(30 * time.Millisecond)
	g.Resume()

	released := make(chan struct{})
	go func() { wg.Wait(); close(released) }()
	select {
	case <-released:
	case <-time.After(2 * time.Second):
		t.Fatal("not all waiters were released after Resume")
	}
}

func TestPauseGateIdempotentTransitions(t *testing.T) {
	g := NewPauseGate()

	// 未暂停时 Resume 应无操作、不 panic（不会 close 未创建/已 close 的 channel）。
	g.Resume()

	g.Pause()
	first := g.PausedAt()
	// 重复 Pause 不应刷新 pausedAt 或重建 channel。
	g.Pause()
	if !g.PausedAt().Equal(first) {
		t.Fatal("repeated Pause should not change PausedAt")
	}

	g.Resume()
	// 重复 Resume 应幂等、不 panic（避免 double close）。
	g.Resume()
	if g.IsPaused() {
		t.Fatal("expected gate to remain resumed after repeated Resume")
	}
}

func TestPauseGateNilReceiverAndContextHelpers(t *testing.T) {
	var g *PauseGate // nil 接收者

	g.Pause()
	g.Resume()
	if g.IsPaused() {
		t.Fatal("nil gate should report not paused")
	}
	if !g.PausedAt().IsZero() {
		t.Fatal("nil gate PausedAt should be zero")
	}
	if err := g.Wait(context.Background()); err != nil {
		t.Fatalf("nil gate Wait should return ctx.Err (nil), got %v", err)
	}

	// WithPauseGate(nil) 应原样返回 ctx；FromContext 应能取回已注入的 gate。
	base := context.Background()
	if WithPauseGate(base, nil) != base {
		t.Fatal("WithPauseGate(ctx, nil) should return the original ctx")
	}
	real := NewPauseGate()
	ctx := WithPauseGate(base, real)
	if FromContext(ctx) != real {
		t.Fatal("FromContext should retrieve the injected gate")
	}
	if FromContext(base) != nil {
		t.Fatal("FromContext on a gate-less ctx should be nil")
	}
}

func TestPackageWaitUsesGateFromContext(t *testing.T) {
	// 无 gate 的 ctx：包级 Wait 直接返回 ctx.Err()。
	if err := Wait(context.Background()); err != nil {
		t.Fatalf("package Wait without gate should return nil, got %v", err)
	}

	g := NewPauseGate()
	g.Pause()
	ctx, cancel := context.WithCancel(WithPauseGate(context.Background(), g))
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- Wait(ctx) }()
	select {
	case err := <-done:
		t.Fatalf("package Wait returned %v while gate paused; expected block", err)
	case <-time.After(50 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("expected context.Canceled from package Wait, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("package Wait did not observe cancellation")
	}
}
