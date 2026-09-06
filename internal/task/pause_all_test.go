// 守「全部暂停 = 逐个按下**暂停闸门**」这一个概念：被按下的运行状态如实写作**已暂停**，
// 不可暂停的运行一动不动，而**暂停原因**只在暂停期间说话。

package task

import (
	"context"
	"testing"
	"time"

	"manga-manager/internal/taskcontrol"
)

// TestPauseAllPressesEveryGate 守全部暂停按下的是每条运行自己的闸门，而不是别处一个开关：
// 只翻一个全局标志的话，任务体照旧读盘，界面却写着已暂停。
func TestPauseAllPressesEveryGate(t *testing.T) {
	h := newTestEngine(t, registerOnly, 0)
	first := h.start(t, libraryScanSpec(1), idleBody)
	second := h.start(t, libraryScanSpec(2), idleBody)
	gates := map[int64]*taskcontrol.PauseGate{
		first.ID:  taskcontrol.FromContext(h.engine.runtimes[first.ID].ctx),
		second.ID: taskcontrol.FromContext(h.engine.runtimes[second.ID].ctx),
	}

	paused, err := h.engine.PauseAll(context.Background())
	if err != nil {
		t.Fatalf("全部暂停失败: %v", err)
	}
	if paused != 2 {
		t.Fatalf("按下的条数为 %d, want 2", paused)
	}

	for id, gate := range gates {
		if !gate.IsPaused() {
			t.Fatalf("运行 %d 的闸门没被按下，任务体会在界面写着已暂停时继续读盘", id)
		}
		run := h.load(t, id)
		if run.Status != StatusPaused || run.PausedAt == nil {
			t.Fatalf("运行 %d 暂停后状态为 %q、暂停起点为 %v", id, run.Status, run.PausedAt)
		}
		if run.PauseReason != PauseReasonPauseAll {
			t.Fatalf("运行 %d 的暂停原因为 %q, want pause_all", id, run.PauseReason)
		}
	}
}

// TestPauseAllLeavesUnpausableRunsAlone 守不可暂停的运行不受全部暂停影响，
// 且它拦不住同一批里其余那些——一条按不下去不是整批失败。
func TestPauseAllLeavesUnpausableRunsAlone(t *testing.T) {
	h := newTestEngine(t, registerOnly, 0)
	unpausable := libraryScanSpec(1)
	unpausable.CanPause = false
	stubborn := h.start(t, unpausable, idleBody)
	pausable := h.start(t, libraryScanSpec(2), idleBody)

	paused, err := h.engine.PauseAll(context.Background())
	if err != nil {
		t.Fatalf("全部暂停失败: %v", err)
	}
	if paused != 1 {
		t.Fatalf("按下的条数为 %d, want 1（只有可暂停的那条）", paused)
	}
	if got := h.load(t, stubborn.ID); got.Status != StatusRunning || got.PauseReason != PauseReasonNone {
		t.Fatalf("不可暂停的运行被改成了 %q（暂停原因 %q）", got.Status, got.PauseReason)
	}
	if got := h.load(t, pausable.ID).Status; got != StatusPaused {
		t.Fatalf("可暂停的运行没被按下，状态为 %q", got)
	}
}

// TestPauseAllSkipsRunsWithoutAHandle 守进程里没有句柄的运行被跳过而不是让整批失败：
// 库里躺着一条上一轮留下的活动运行时，全部暂停仍要把这一轮真在跑的那些按下去。
func TestPauseAllSkipsRunsWithoutAHandle(t *testing.T) {
	h := newTestEngine(t, registerOnly, 0)
	orphan := h.start(t, libraryScanSpec(1), idleBody)
	live := h.start(t, libraryScanSpec(2), idleBody)
	delete(h.engine.runtimes, orphan.ID)

	paused, err := h.engine.PauseAll(context.Background())
	if err != nil {
		t.Fatalf("全部暂停失败: %v", err)
	}
	if paused != 1 {
		t.Fatalf("按下的条数为 %d, want 1", paused)
	}
	if got := h.load(t, live.ID).Status; got != StatusPaused {
		t.Fatalf("有句柄的那条状态为 %q, want paused", got)
	}
}

// TestResumeAllReleasesEveryGateAndSettlesThePause 守全部恢复放行每条闸门、结清这一段暂停，
// 并清掉暂停原因——留着的话，一条正在跑的运行会带着「因全部暂停而停」显示下去。
func TestResumeAllReleasesEveryGateAndSettlesThePause(t *testing.T) {
	h := newTestEngine(t, registerOnly, 0)
	run := h.start(t, libraryScanSpec(1), idleBody)
	gate := taskcontrol.FromContext(h.engine.runtimes[run.ID].ctx)
	if _, err := h.engine.PauseAll(context.Background()); err != nil {
		t.Fatalf("全部暂停失败: %v", err)
	}

	h.clock.advance(45 * time.Second)
	resumed, err := h.engine.ResumeAll(context.Background())
	if err != nil {
		t.Fatalf("全部恢复失败: %v", err)
	}
	if resumed != 1 {
		t.Fatalf("放行的条数为 %d, want 1", resumed)
	}
	if gate.IsPaused() {
		t.Fatal("状态写回了运行中，闸门却还按着")
	}

	back := h.load(t, run.ID)
	if back.Status != StatusRunning || back.PausedAt != nil || back.PauseReason != PauseReasonNone {
		t.Fatalf("恢复后状态为 %q、起点为 %v、暂停原因为 %q", back.Status, back.PausedAt, back.PauseReason)
	}
	if back.ControlPausedMillis != 45_000 {
		t.Fatalf("累计暂停为 %d ms, want 45000", back.ControlPausedMillis)
	}
}

// TestResumeAllAlsoReleasesIndividuallyPausedRuns 守全部恢复认状态而不认暂停原因：
// 按原因分拣的话，用户按完「全部恢复」界面上还剩着几条已暂停，而那个按钮已经灰掉了。
func TestResumeAllAlsoReleasesIndividuallyPausedRuns(t *testing.T) {
	h := newTestEngine(t, registerOnly, 0)
	run := h.start(t, libraryScanSpec(1), idleBody)
	if err := h.engine.Pause(run.ID); err != nil {
		t.Fatalf("暂停失败: %v", err)
	}
	if got := h.load(t, run.ID).PauseReason; got != PauseReasonManual {
		t.Fatalf("单条暂停原因为 %q, want manual", got)
	}

	if _, err := h.engine.ResumeAll(context.Background()); err != nil {
		t.Fatalf("全部恢复失败: %v", err)
	}
	if got := h.load(t, run.ID).Status; got != StatusRunning {
		t.Fatalf("单独暂停的运行没被全部恢复放行，状态为 %q", got)
	}
}

// TestPauseReasonDoesNotSurviveTheRun 守暂停原因不跟进**终态**：一条早已跑完的运行
// 带着「因全部暂停而停」只会误导——它此刻既没暂停，也不会再动。
func TestPauseReasonDoesNotSurviveTheRun(t *testing.T) {
	h := newTestEngine(t, registerOnly, 0)
	spec := libraryScanSpec(1)
	run := h.start(t, spec, idleBody)
	if _, err := h.engine.PauseAll(context.Background()); err != nil {
		t.Fatalf("全部暂停失败: %v", err)
	}

	h.engine.settle(run.ID, spec, Result{}, nil)
	settled := h.load(t, run.ID)
	if !settled.Status.IsTerminal() {
		t.Fatalf("收尾后状态为 %q，不是终态", settled.Status)
	}
	if settled.PauseReason != PauseReasonNone {
		t.Fatalf("终态仍带着暂停原因 %q", settled.PauseReason)
	}
}

// TestPauseAllHoldsTheQueue 守全部暂停也**按得住排队中的运行**。
//
// 排队中的运行没有开跑、没有闸门可按，逐个按下那一半够不着它们；不拦住放行的话，用户按下
// 全部暂停之后队列里的东西照样一条条接上去跑——与「让盘安静下来」正好相反。
func TestPauseAllHoldsTheQueue(t *testing.T) {
	runner := &deferredRunner{}
	h := newTestEngine(t, runner.run, 1)

	active := h.start(t, libraryScanSpec(1), idleBody)
	queued := h.start(t, libraryScanSpec(2), idleBody)
	if queued.Status != StatusQueued {
		t.Fatalf("第二条的状态为 %q, want queued", queued.Status)
	}

	if _, err := h.engine.PauseAll(context.Background()); err != nil {
		t.Fatalf("全部暂停失败: %v", err)
	}
	if !h.engine.PausedAll() {
		t.Fatal("全部暂停之后闸门没关上 —— 界面因此看不出队列被谁拦着")
	}
	// 唯一那条活动运行收尾，槽位空出来：闸门关着，队列一条都不该放行。
	h.engine.finalize(active.ID, StatusCancelled, Result{}, "")
	runner.drain()

	if got := h.load(t, queued.ID).Status; got != StatusQueued {
		t.Fatalf("全部暂停期间队列放行了，排队那条的状态为 %q", got)
	}

	if _, err := h.engine.ResumeAll(context.Background()); err != nil {
		t.Fatalf("全部恢复失败: %v", err)
	}
	runner.drain()

	if h.engine.PausedAll() {
		t.Fatal("全部恢复之后闸门还关着")
	}
	if got := h.load(t, queued.ID).Status; got != StatusCompleted {
		t.Fatalf("全部恢复没把排队那条放开，状态为 %q", got)
	}
}

// TestStartWhilePausedAllIsQueued 守全部暂停期间新发起的运行进**排队中**：
// 闸门只拦已经排上的那些的话，一次新发起就能绕过它，盘照样转起来。
func TestStartWhilePausedAllIsQueued(t *testing.T) {
	h := newTestEngine(t, registerOnly, 0)
	if _, err := h.engine.PauseAll(context.Background()); err != nil {
		t.Fatalf("全部暂停失败: %v", err)
	}

	run := h.start(t, libraryScanSpec(1), idleBody)
	if run.Status != StatusQueued {
		t.Fatalf("全部暂停期间发起的运行状态为 %q, want queued", run.Status)
	}
	if !run.StartedAt.IsZero() {
		t.Fatalf("还没开跑的运行带上了开始时刻 %v", run.StartedAt)
	}
}

// TestPauseAllReportsOnlyPressedRuns 守返回的条数只数真正被按下的运行：闸门不是一条运行，
// 报进去会让用户以为有一条他没见过的东西被暂停了。
func TestPauseAllReportsOnlyPressedRuns(t *testing.T) {
	h := newTestEngine(t, registerOnly, 0)

	pressed, err := h.engine.PauseAll(context.Background())
	if err != nil {
		t.Fatalf("全部暂停失败: %v", err)
	}
	if pressed != 0 {
		t.Fatalf("一条运行都没有时按下了 %d 条", pressed)
	}
	if !h.engine.PausedAll() {
		t.Fatal("没有运行可按就不关闸门 —— 此后发起的运行会直接开跑")
	}
}
