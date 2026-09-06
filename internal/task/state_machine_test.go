// 守状态机的每条边与三条判定。**排队中**既不是**活动态**也不是**终态**这一条尤其要守住：
// 算进活动态就等于让排队的运行白占一个槽位，队列因此永远放不出人。

package task

import (
	"context"
	"testing"
	"time"

	"manga-manager/internal/taskcontrol"
)

func TestStatusPredicates(t *testing.T) {
	cases := []struct {
		status   RunStatus
		active   bool
		terminal bool
		live     bool
	}{
		{StatusQueued, false, false, true},
		{StatusRunning, true, false, true},
		{StatusPaused, true, false, true},
		{StatusCancelling, true, false, true},
		{StatusCompleted, false, true, false},
		{StatusCancelled, false, true, false},
		{StatusFailed, false, true, false},
		{StatusInterrupted, false, true, false},
	}

	for _, tc := range cases {
		t.Run(string(tc.status), func(t *testing.T) {
			if got := tc.status.IsActive(); got != tc.active {
				t.Fatalf("IsActive() = %v, want %v", got, tc.active)
			}
			if got := tc.status.IsTerminal(); got != tc.terminal {
				t.Fatalf("IsTerminal() = %v, want %v", got, tc.terminal)
			}
			if got := tc.status.IsLive(); got != tc.live {
				t.Fatalf("IsLive() = %v, want %v", got, tc.live)
			}
		})
	}
}

// TestPauseAndResumeDriveTheGate 守暂停确实按下了**暂停闸门**而不只是改了个字段：
// 只改状态的话，任务体会在界面写着「已暂停」的同时继续读盘。
func TestPauseAndResumeDriveTheGate(t *testing.T) {
	h := newTestEngine(t, registerOnly, 0)
	run := h.start(t, libraryScanSpec(1), idleBody)
	gate := taskcontrol.FromContext(h.engine.runtimes[run.ID].ctx)

	if err := h.engine.Pause(run.ID); err != nil {
		t.Fatalf("暂停失败: %v", err)
	}
	if !gate.IsPaused() {
		t.Fatal("状态写成了已暂停，闸门却没被按下")
	}
	if paused := h.load(t, run.ID); paused.Status != StatusPaused || paused.PausedAt == nil {
		t.Fatalf("暂停后状态为 %q、暂停起点为 %v", paused.Status, paused.PausedAt)
	}

	h.clock.advance(30 * time.Second)
	if err := h.engine.Resume(run.ID); err != nil {
		t.Fatalf("恢复失败: %v", err)
	}
	if gate.IsPaused() {
		t.Fatal("状态写回了运行中，闸门却还按着")
	}

	resumed := h.load(t, run.ID)
	if resumed.Status != StatusRunning || resumed.PausedAt != nil {
		t.Fatalf("恢复后状态为 %q、暂停起点为 %v", resumed.Status, resumed.PausedAt)
	}
	// 这一段暂停必须折进累计：它是速率与 ETA 分母里要扣掉的那一段。
	if resumed.ControlPausedMillis != 30_000 {
		t.Fatalf("累计暂停为 %d ms, want 30000", resumed.ControlPausedMillis)
	}
}

// TestCancelFromPausedReleasesTheGate 守从暂停直接取消时闸门被放行：
// 只取消 ctx 的话，卡在闸门上的任务体永远收不到取消信号。
func TestCancelFromPausedReleasesTheGate(t *testing.T) {
	h := newTestEngine(t, registerOnly, 0)
	run := h.start(t, libraryScanSpec(1), idleBody)
	runtime := h.engine.runtimes[run.ID]

	if err := h.engine.Pause(run.ID); err != nil {
		t.Fatalf("暂停失败: %v", err)
	}
	h.clock.advance(10 * time.Second)
	if err := h.engine.Cancel(run.ID); err != nil {
		t.Fatalf("取消失败: %v", err)
	}

	if runtime.gate.IsPaused() {
		t.Fatal("取消之后闸门仍按着，任务体收不到取消信号")
	}
	if err := runtime.ctx.Err(); err != context.Canceled {
		t.Fatalf("任务体的 ctx 错误为 %v, want context.Canceled", err)
	}
	cancelling := h.load(t, run.ID)
	if cancelling.Status != StatusCancelling {
		t.Fatalf("取消后状态为 %q, want cancelling", cancelling.Status)
	}
	// 闸门已放行，这次暂停到此为止：不结账的话整段暂停会一路留进已取消那一帧的分母里。
	if cancelling.PausedAt != nil || cancelling.ControlPausedMillis != 10_000 {
		t.Fatalf("取消时没有结算暂停：起点 %v、累计 %d ms", cancelling.PausedAt, cancelling.ControlPausedMillis)
	}
}

// TestCancellingIsStillActive 守**取消中**属于活动态：任务体尚未收尾，
// 此刻放同一个任务的下一次发起进来，两条运行会同时动同一批文件。
//
// 「不放进来」如今写作**排队中**而不是一个错误：那次发起不再被丢弃，它等着上一条收完尾。
func TestCancellingIsStillActive(t *testing.T) {
	h := newTestEngine(t, registerOnly, 0)
	run := h.start(t, libraryScanSpec(1), idleBody)
	if err := h.engine.Cancel(run.ID); err != nil {
		t.Fatalf("取消失败: %v", err)
	}

	next := h.start(t, libraryScanSpec(1), idleBody)
	if next.Status != StatusQueued {
		t.Fatalf("取消中的任务放行了第二次发起：第二条的状态为 %q, want queued", next.Status)
	}
}

// TestQueuedRunCancelsStraightToCancelled 守**排队中**被取消直接进已取消：
// 它从未开跑，没有任务体需要收尾，停在取消中就再也不会有人来收它。
func TestQueuedRunCancelsStraightToCancelled(t *testing.T) {
	h := newTestEngine(t, registerOnly, 1)
	h.start(t, libraryScanSpec(1), idleBody)
	queued := h.start(t, libraryScanSpec(2), idleBody)

	if queued.Status != StatusQueued {
		t.Fatalf("第二条运行的状态为 %q, want queued", queued.Status)
	}
	if err := h.engine.Cancel(queued.ID); err != nil {
		t.Fatalf("取消排队中的运行失败: %v", err)
	}

	cancelled := h.load(t, queued.ID)
	if cancelled.Status != StatusCancelled {
		t.Fatalf("排队中被取消后状态为 %q, want cancelled", cancelled.Status)
	}
	if cancelled.ControlPausedMillis != 0 {
		t.Fatalf("排队中的运行不该有暂停累计，却是 %d ms", cancelled.ControlPausedMillis)
	}
}

// TestRestartMovesLiveRunsToInterrupted 守重启把**活动态与排队中**一起转入**中断**，
// 且结束时刻取的是原来的心跳而不是重启时刻——盖成重启时刻的话，整段停机时长都会被算成在干活。
func TestRestartMovesLiveRunsToInterrupted(t *testing.T) {
	h := newTestEngine(t, registerOnly, 1)
	active := h.start(t, libraryScanSpec(1), idleBody)
	queued := h.start(t, libraryScanSpec(2), idleBody)
	done := h.start(t, libraryScanSpec(3), idleBody)
	h.engine.settle(done.ID, libraryScanSpec(3), Result{}, nil)

	heartbeat := h.load(t, active.ID).UpdatedAt
	h.clock.advance(8 * time.Hour)

	outcome, err := h.engine.MarkInterrupted(context.Background())
	if err != nil {
		t.Fatalf("批量转中断失败: %v", err)
	}
	if outcome.Marked != 2 {
		t.Fatalf("转中断的条数为 %d, want 2（活动态与排队中各一条）", outcome.Marked)
	}

	for _, id := range []int64{active.ID, queued.ID} {
		if got := h.load(t, id).Status; got != StatusInterrupted {
			t.Fatalf("运行 %d 的状态为 %q, want interrupted", id, got)
		}
	}
	if got := h.load(t, done.ID).Status; got != StatusCompleted {
		t.Fatalf("已经收尾的运行被转成了 %q", got)
	}
	interrupted := h.load(t, active.ID)
	if interrupted.FinishedAt == nil || !interrupted.FinishedAt.Equal(heartbeat) {
		t.Fatalf("中断的结束时刻为 %v, want 原来的心跳 %v", interrupted.FinishedAt, heartbeat)
	}
}

// TestTerminalRunRejectsControl 守**终态**不接受控制动作。
func TestTerminalRunRejectsControl(t *testing.T) {
	h := newTestEngine(t, runBodySynchronously, 0)
	run := h.start(t, libraryScanSpec(1), idleBody)

	if err := h.engine.Pause(run.ID); err != ErrRunNotRunning {
		t.Fatalf("暂停一条完成的运行返回 %v, want ErrRunNotRunning", err)
	}
	if err := h.engine.Resume(run.ID); err != ErrRunNotPaused {
		t.Fatalf("恢复一条完成的运行返回 %v, want ErrRunNotPaused", err)
	}
	if err := h.engine.Cancel(run.ID); err != ErrRunNotRunning {
		t.Fatalf("取消一条完成的运行返回 %v, want ErrRunNotRunning", err)
	}
}

// TestUnpausableRunIsNotAffected 守不可暂停的运行不受暂停影响——「全部暂停」是把每条活动运行
// 逐个按下，不是第二套机制。
func TestUnpausableRunIsNotAffected(t *testing.T) {
	h := newTestEngine(t, registerOnly, 0)
	spec := libraryScanSpec(1)
	spec.CanPause = false
	run := h.start(t, spec, idleBody)

	if err := h.engine.Pause(run.ID); err != ErrRunNotPausable {
		t.Fatalf("暂停一条不可暂停的运行返回 %v, want ErrRunNotPausable", err)
	}
	if got := h.load(t, run.ID).Status; got != StatusRunning {
		t.Fatalf("不可暂停的运行状态被改成了 %q", got)
	}
	if caps := lastPublishedFor(t, h.snapshots(), run.ID).Capabilities; caps.CanPause {
		t.Fatal("载荷把不可暂停的运行报成了可暂停，界面会让用户以为暂停失灵了")
	}
}
