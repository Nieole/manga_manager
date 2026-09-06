// 守投递水位：**计数推进**可以被吞，展示态的跃迁不可以。吞掉一次阶段切换，
// 运行气泡会长时间停在一个过期的阶段名上；而计数吞多少都无所谓——载荷是全量快照。

package task

import (
	"context"
	"testing"

	"manga-manager/internal/runhandle"
)

// advancingBody 报 count 次同样展示态的**计数推进**，然后正常返回。
func advancingBody(count int) Body {
	return func(_ context.Context, handle *runhandle.Handle) (Result, error) {
		for i := 1; i <= count; i++ {
			handle.Advance(i, count, "task.msg.scan.progress", nil)
		}
		return Result{}, nil
	}
}

// TestFrozenClockSwallowsRepeatedProgress 守窗口内展示态一字未变的推进被吞掉。
// 首帧、第一条推进与终态那三帧一条都不能少：它们各自是用户在等的一次变化。
func TestFrozenClockSwallowsRepeatedProgress(t *testing.T) {
	h := newTestEngine(t, runBodySynchronously, 0)
	run := h.start(t, libraryScanSpec(1), advancingBody(20))

	if got := publishedCountFor(h.snapshots(), run.ID); got != 3 {
		t.Fatalf("投递了 %d 帧, want 3（首帧 + 第一条推进 + 终态）", got)
	}
	// 节流只跳过投递，运行行照常写：被吞掉的那些推进最后一条仍要在库里。
	if got := h.load(t, run.ID).Current; got != 20 {
		t.Fatalf("被吞掉的推进没有写进运行行：计数为 %d", got)
	}
}

// TestSteppingClockLetsEveryFrameThrough 守水位认注入的时钟。任务体里若还留着一层按墙上
// 时钟计时的节流，这个时钟怎么走都撬不动它。
func TestSteppingClockLetsEveryFrameThrough(t *testing.T) {
	h := newSteppingTestEngine(t, runBodySynchronously, 0)
	run := h.start(t, libraryScanSpec(1), advancingBody(5))

	if got := publishedCountFor(h.snapshots(), run.ID); got != 7 {
		t.Fatalf("投递了 %d 帧, want 7（首帧 + 5 条推进 + 终态）", got)
	}
}

// TestPhaseChangeIsNeverSwallowed 守**阶段**跃迁无条件放行：它是用户正在等的语义变化。
func TestPhaseChangeIsNeverSwallowed(t *testing.T) {
	h := newTestEngine(t, runBodySynchronously, 0)
	run := h.start(t, libraryScanSpec(1), func(_ context.Context, handle *runhandle.Handle) (Result, error) {
		handle.Advance(1, 3, "task.msg.scan.progress", nil)
		handle.Advance(2, 3, "task.msg.scan.progress", nil)
		handle.Phase("covers", "task.msg.scan.covers", nil)
		handle.Advance(3, 3, "task.msg.scan.covers", nil)
		return Result{}, nil
	})

	phases := 0
	for _, snapshot := range h.snapshots() {
		if snapshot.Run.ID == run.ID && snapshot.Run.Phase == "covers" {
			phases++
		}
	}
	if phases == 0 {
		t.Fatal("阶段切换被节流吞掉了，界面会停在过期的阶段名上")
	}
}

// TestControlTransitionsAreNeverThrottled 守控制动作一律不走节流：它们是用户按下按钮后
// 在等的那一帧，吞掉一条界面就停在错误的状态上。
func TestControlTransitionsAreNeverThrottled(t *testing.T) {
	h := newTestEngine(t, registerOnly, 0)
	run := h.start(t, libraryScanSpec(1), idleBody)

	if err := h.engine.Pause(run.ID); err != nil {
		t.Fatalf("暂停失败: %v", err)
	}
	if err := h.engine.Resume(run.ID); err != nil {
		t.Fatalf("恢复失败: %v", err)
	}
	if err := h.engine.Cancel(run.ID); err != nil {
		t.Fatalf("取消失败: %v", err)
	}

	want := []RunStatus{StatusRunning, StatusPaused, StatusRunning, StatusCancelling}
	got := publishedStatusesFor(h.snapshots(), run.ID)
	if len(got) != len(want) {
		t.Fatalf("投递出去的状态序列为 %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("投递出去的状态序列为 %v, want %v", got, want)
		}
	}
}

// TestTerminalRunDropsItsGate 守终态丢掉水位。留着只是泄漏——运行 id 一次一号，
// 那条水位此后再也不会被读到。
func TestTerminalRunDropsItsGate(t *testing.T) {
	h := newTestEngine(t, runBodySynchronously, 0)
	run := h.start(t, libraryScanSpec(1), advancingBody(3))

	if _, ok := h.engine.gates[run.ID]; ok {
		t.Fatal("终态之后水位还留着")
	}
}
