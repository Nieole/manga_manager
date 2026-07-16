// 业务说明：本文件补充 PauseGate 的边界回归：暂停→恢复→再暂停应重建 resume channel 并重新阻塞新等待者，
// 以及 nil ctx 在暂停期间的阻塞唤醒路径，保障长任务多轮暂停控制的正确性。
package taskcontrol

import (
	"context"
	"testing"
	"time"
)

func TestPauseGateRePauseBlocksNewWaiter(t *testing.T) {
	g := NewPauseGate()

	// 第一轮：暂停后恢复，Wait 应返回。
	g.Pause()
	g.Resume()
	if err := g.Wait(context.Background()); err != nil {
		t.Fatalf("Wait after first resume = %v, want nil", err)
	}

	// 第二轮：再次暂停必须重建 channel，让新的 Wait 重新阻塞。
	g.Pause()
	firstPausedAt := g.PausedAt()
	if firstPausedAt.IsZero() {
		t.Fatal("expected PausedAt set after re-pause")
	}

	done := make(chan error, 1)
	go func() { done <- g.Wait(context.Background()) }()
	select {
	case err := <-done:
		t.Fatalf("Wait returned %v during re-pause; expected block", err)
	case <-time.After(50 * time.Millisecond):
	}

	g.Resume()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Wait after second resume = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not return after second resume")
	}
}

func TestPauseGateWaitNilContextBlocksUntilResume(t *testing.T) {
	g := NewPauseGate()
	g.Pause()

	done := make(chan struct{})
	go func() {
		// nil ctx 走 <-resumeCh 分支（无取消能力），应阻塞至 Resume。
		_ = g.Wait(nil) //nolint:staticcheck // intentionally passing nil to exercise the no-cancellation resumeCh branch
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("Wait(nil) returned while paused; expected it to block")
	case <-time.After(50 * time.Millisecond):
	}

	g.Resume()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Wait(nil) did not return after Resume")
	}
}

func TestPauseGatePausedAtAdvancesOnRePause(t *testing.T) {
	g := NewPauseGate()
	g.Pause()
	first := g.PausedAt()
	g.Resume()

	time.Sleep(5 * time.Millisecond)
	g.Pause()
	second := g.PausedAt()
	g.Resume()

	// 每次全新的 Pause（非重复 Pause）都应刷新 pausedAt 时间戳。
	if !second.After(first) {
		t.Fatalf("expected pausedAt to advance across pause cycles: first=%v second=%v", first, second)
	}
}
