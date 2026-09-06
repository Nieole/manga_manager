// 守准入的两道闸门：同一个任务只有一次**活动态**运行，以及全局槽位这个单一数字。
// 前者破了同一个资料库会被同时扫两遍，后者破了几个重活会同时开满把机器压垮。

package task

import (
	"context"
	"sync"
	"testing"

	"manga-manager/internal/taskrun"
)

// deferredRunner 是「先登记、按需执行」版的后台能力：用例因此能决定哪一条任务体何时开跑，
// 也能观察到队列放行确实把下一条交了出来。
type deferredRunner struct {
	mu    sync.Mutex
	fns   []func()
	calls int
}

func (r *deferredRunner) run(fn func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fns = append(r.fns, fn)
	r.calls++
}

// drain 反复执行已登记的任务体，直到再没有新的被登记出来——收尾会放行下一条排队运行，
// 而那一条同样经这里登记。
func (r *deferredRunner) drain() {
	for {
		r.mu.Lock()
		batch := r.fns
		r.fns = nil
		r.mu.Unlock()
		if len(batch) == 0 {
			return
		}
		for _, fn := range batch {
			fn()
		}
	}
}

func TestSecondRunOfTheSameTaskIsRejected(t *testing.T) {
	h := newTestEngine(t, registerOnly, 0)
	h.start(t, libraryScanSpec(1), idleBody)

	if _, err := h.engine.Start(context.Background(), libraryScanSpec(1), idleBody); err != ErrRunAlreadyActive {
		t.Fatalf("同一个任务的第二次发起返回 %v, want ErrRunAlreadyActive", err)
	}
}

// TestRetryIsANewRunAndKeepsThePreviousOne 守重试是**新一次运行**：上一次原样留着，
// 同一个任务的历次运行因此可以并排看。
func TestRetryIsANewRunAndKeepsThePreviousOne(t *testing.T) {
	h := newTestEngine(t, runBodySynchronously, 0)

	first := h.start(t, libraryScanSpec(1), idleBody)
	second := h.start(t, libraryScanSpec(1), idleBody)

	if first.ID == second.ID {
		t.Fatal("重试覆盖了上一次运行，历次运行无从对比")
	}
	if first.NthRun != 1 || second.NthRun != 2 {
		t.Fatalf("第几次运行为 %d 与 %d, want 1 与 2", first.NthRun, second.NthRun)
	}
	if first.TaskID != second.TaskID {
		t.Fatalf("两次运行挂在了两个任务上：%d 与 %d", first.TaskID, second.TaskID)
	}
	if got := h.load(t, first.ID); got.Status != StatusCompleted {
		t.Fatalf("上一次运行的记录被改成了 %q", got.Status)
	}
}

// TestNthRunDoesNotRepeatAfterPruning 守「第几次」在保留裁剪之后不会撞号。
// 按行数算的话，裁掉旧运行会让计数缩回去，下一次运行拿到一个已经用过的编号，
// 而用户看到的「第几次」会倒着走。
func TestNthRunDoesNotRepeatAfterPruning(t *testing.T) {
	h := newTestEngine(t, runBodySynchronously, 0)

	var nth []int
	for i := 0; i < 4; i++ {
		nth = append(nth, h.start(t, libraryScanSpec(1), idleBody).NthRun)
	}
	if nth[3] != 4 {
		t.Fatalf("第四次运行编号为 %d, want 4", nth[3])
	}

	// 保留裁剪只留最近两次：库里此后只剩第 3、4 次。
	pruned, err := h.store.PruneRuns(context.Background(), RetentionPolicy{RunsPerTask: 2})
	if err != nil {
		t.Fatalf("裁剪失败: %v", err)
	}
	if pruned.Runs != 2 {
		t.Fatalf("裁掉了 %d 条, want 2", pruned.Runs)
	}

	if got := h.start(t, libraryScanSpec(1), idleBody).NthRun; got != 5 {
		t.Fatalf("裁剪之后的下一次运行编号为 %d, want 5", got)
	}
}

// TestOverTheSlotLimitStaysQueued 守槽位按**单一全局数字**放行，超限的留在**排队中**。
func TestOverTheSlotLimitStaysQueued(t *testing.T) {
	h := newTestEngine(t, registerOnly, 2)

	first := h.start(t, libraryScanSpec(1), idleBody)
	second := h.start(t, libraryScanSpec(2), idleBody)
	third := h.start(t, libraryScanSpec(3), idleBody)

	if first.Status != StatusRunning || second.Status != StatusRunning {
		t.Fatalf("槽位内的两条没开跑：%q 与 %q", first.Status, second.Status)
	}
	if third.Status != StatusQueued {
		t.Fatalf("第三条的状态为 %q, want queued", third.Status)
	}
	// 排队中不占槽位，开始时刻要等真的开跑才写——写进来的话排队那段会被算进速率的分母。
	if !third.StartedAt.IsZero() {
		t.Fatalf("排队中的运行带上了开始时刻 %v", third.StartedAt)
	}
	if h.engine.Slots() != 2 {
		t.Fatalf("槽位上限为 %d, want 2", h.engine.Slots())
	}
}

// TestSlotReleaseStartsTheOldestQueuedRun 守槽位释放后按序放行。不放行的话，
// 排队的运行永远等不到人，而用户看到的是一条「排队中」再也不动。
func TestSlotReleaseStartsTheOldestQueuedRun(t *testing.T) {
	runner := &deferredRunner{}
	h := newTestEngine(t, runner.run, 1)

	active := h.start(t, libraryScanSpec(1), idleBody)
	earlier := h.start(t, libraryScanSpec(2), idleBody)
	later := h.start(t, libraryScanSpec(3), idleBody)

	if earlier.Status != StatusQueued || later.Status != StatusQueued {
		t.Fatalf("超限的两条没进排队：%q 与 %q", earlier.Status, later.Status)
	}

	// 跑完第一条：它腾出唯一那个槽位，先排上的那条应当接上去。
	runner.drain()

	if got := h.load(t, active.ID).Status; got != StatusCompleted {
		t.Fatalf("第一条的状态为 %q, want completed", got)
	}
	if got := h.load(t, earlier.ID).Status; got != StatusCompleted {
		t.Fatalf("先排上的那条没被放行，状态为 %q", got)
	}
	if got := h.load(t, later.ID).Status; got != StatusCompleted {
		t.Fatalf("后排上的那条没被放行，状态为 %q", got)
	}
	if started := h.load(t, earlier.ID).StartedAt; started.IsZero() {
		t.Fatal("放行之后没有补上开始时刻，速率因此没有分母")
	}
}

// TestSlotLimitHoldsWhileAQueuedRunWaits 守放行不越过上限：一条腾出来只放一条进去。
func TestSlotLimitHoldsWhileAQueuedRunWaits(t *testing.T) {
	h := newTestEngine(t, registerOnly, 1)
	active := h.start(t, libraryScanSpec(1), idleBody)
	first := h.start(t, libraryScanSpec(2), idleBody)
	second := h.start(t, libraryScanSpec(3), idleBody)

	h.engine.settle(active.ID, libraryScanSpec(1), Result{}, nil)

	if got := h.load(t, first.ID).Status; got != StatusRunning {
		t.Fatalf("腾出的槽位没给先排上的那条：%q", got)
	}
	if got := h.load(t, second.ID).Status; got != StatusQueued {
		t.Fatalf("一个槽位放行了两条运行，第二条的状态为 %q", got)
	}
}

// TestQueuedRunOfTheSameTaskIsRejectedByTheStore 守「最多一条排队」这条约束由落盘端口兜底，
// 而不是靠内存表判定——判据下沉之后，「什么叫已经在排队」只有一个答案。
func TestQueuedRunOfTheSameTaskIsRejectedByTheStore(t *testing.T) {
	h := newTestEngine(t, registerOnly, 1)
	h.start(t, libraryScanSpec(1), idleBody)
	h.start(t, libraryScanSpec(2), idleBody)

	if _, err := h.engine.Start(context.Background(), libraryScanSpec(2), idleBody); err != ErrRunAlreadyQueued {
		t.Fatalf("同一个任务的第二条排队返回 %v, want ErrRunAlreadyQueued", err)
	}
}

// TestQueuedRunDoesNotOccupyASlot 守**排队中**不占槽位：算进去的话，
// 排队的运行会把自己等的那个槽位占掉，队列从此永远放不出人。
func TestQueuedRunDoesNotOccupyASlot(t *testing.T) {
	h := newTestEngine(t, registerOnly, 1)
	active := h.start(t, libraryScanSpec(1), idleBody)
	h.start(t, libraryScanSpec(2), idleBody)

	count, err := h.store.CountRuns(context.Background(), RunFilter{Statuses: ActiveStatuses()})
	if err != nil {
		t.Fatalf("数活动运行失败: %v", err)
	}
	if count != 1 {
		t.Fatalf("占着槽位的运行有 %d 条, want 1", count)
	}
	if got := h.load(t, active.ID).Status; got != StatusRunning {
		t.Fatalf("唯一那条活动运行的状态为 %q", got)
	}
}

// TestQueuedRunGetsTheHandleOnlyAfterItIsReleased 守排队中的运行拿不到任何写入资格：
// 它还没开跑，此刻能上报就等于一条没开始的运行在推进度条。
func TestQueuedRunGetsTheHandleOnlyAfterItIsReleased(t *testing.T) {
	runner := &deferredRunner{}
	h := newTestEngine(t, runner.run, 1)

	h.start(t, libraryScanSpec(1), idleBody)
	queued := h.start(t, libraryScanSpec(2), func(_ context.Context, handle *taskrun.Handle) (Result, error) {
		handle.Advance(42, 100, "", nil)
		return Result{}, nil
	})

	if h.engine.runtimes[queued.ID] != nil {
		t.Fatal("排队中的运行已经登记了运行时句柄，它还没过槽位闸门")
	}
	if got := h.load(t, queued.ID).Current; got != 0 {
		t.Fatalf("排队中的运行已经有进度 %d", got)
	}

	runner.drain()

	if got := h.load(t, queued.ID).Current; got != 100 {
		t.Fatalf("放行之后任务体没跑：计数为 %d", got)
	}
}
