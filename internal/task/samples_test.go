// 守**采样**的四条：取点节奏（一个间隔一个点，催得再密也一样）、只对**活动态**取、
// 曲线上的吞吐量的是那一段窗口（因此停滞段如实是零），以及本票那条硬约束——
// **没有任何判断依赖采样表**。
//
// 节奏一律由注入的时钟驱动，不睡真实的十秒。

package task

import (
	"context"
	"testing"
	"time"

	"manga-manager/internal/runhandle"
)

// samplesOf 取这条运行至今落下的全部采样点。
//
// 它经落盘端口直接读，不走 Engine.RunSamples：那条读取面自己会把读次数记进计账，
// 而 TestNoJudgementReadsTheSampleTable 数的正是那个计数。
func (h *testEngine) samplesOf(runID int64) []Sample {
	h.store.mu.Lock()
	defer h.store.mu.Unlock()
	return append([]Sample(nil), h.store.samples[runID]...)
}

// advanceTo 把这条运行的计数推到 current，供曲线的用例造出「这一段处理了多少条」。
func advanceTo(t *testing.T, h *testEngine, runID int64, current int) {
	t.Helper()
	handle, ok := h.engine.Handle(runID)
	if !ok {
		t.Fatalf("运行 %d 拿不到运行句柄", runID)
	}
	handle.Report(runhandle.Frame{Current: &current})
}

// TestSamplingTakesOnePointPerIntervalHowEverOftenItIsAsked 守取点节奏：一个间隔一个点。
//
// 催的次数不该改变落点的个数——节拍在 api 那一侧，而写入量的上限必须在这里。这条守的正是
// 「采样写入要节流成本可控：10s 一次、每条运行一行，**不要跟着进度帧走**」：
// 哪怕将来有人把取点挂到每一帧上，一个间隔里也只落得下一个点。
func TestSamplingTakesOnePointPerIntervalHowEverOftenItIsAsked(t *testing.T) {
	h := newTestEngine(t, registerOnly, 0)
	run := h.start(t, libraryScanSpec(1), idleBody)
	ctx := context.Background()

	// 还差一秒到第一个间隔：催多少次都不该落点。
	h.clock.advance(DefaultSampleInterval - time.Second)
	h.engine.SampleActiveRuns(ctx)
	h.engine.SampleActiveRuns(ctx)
	if got := len(h.samplesOf(run.ID)); got != 0 {
		t.Fatalf("不满一个间隔就落了 %d 个点，采样跟着调用次数走了", got)
	}

	// 跨过第一个间隔：落一个点，同一拍里再催也还是一个。
	h.clock.advance(time.Second)
	h.engine.SampleActiveRuns(ctx)
	h.engine.SampleActiveRuns(ctx)
	h.engine.SampleActiveRuns(ctx)
	if got := len(h.samplesOf(run.ID)); got != 1 {
		t.Fatalf("一个间隔里落了 %d 个点, want 1", got)
	}

	// 再跨一个间隔：第二个点。
	h.clock.advance(DefaultSampleInterval)
	h.engine.SampleActiveRuns(ctx)
	samples := h.samplesOf(run.ID)
	if len(samples) != 2 {
		t.Fatalf("两个间隔之后落了 %d 个点, want 2", len(samples))
	}
	if gap := samples[1].At.Sub(samples[0].At); gap != DefaultSampleInterval {
		t.Fatalf("相邻两点隔了 %v, want %v", gap, DefaultSampleInterval)
	}
}

// TestSamplingIntervalIsConfigurable 守间隔可配，且改了当场生效——引擎收的是一个读间隔的
// 闭包而不是一个数，因此不必重启进程。
func TestSamplingIntervalIsConfigurable(t *testing.T) {
	h := newTestEngine(t, registerOnly, 0)
	h.sampleInterval = time.Minute
	run := h.start(t, libraryScanSpec(1), idleBody)
	ctx := context.Background()

	h.clock.advance(DefaultSampleInterval)
	h.engine.SampleActiveRuns(ctx)
	if got := len(h.samplesOf(run.ID)); got != 0 {
		t.Fatalf("间隔配成一分钟，十秒就落了 %d 个点", got)
	}

	h.clock.advance(time.Minute - DefaultSampleInterval)
	h.engine.SampleActiveRuns(ctx)
	if got := len(h.samplesOf(run.ID)); got != 1 {
		t.Fatalf("满一分钟后落了 %d 个点, want 1", got)
	}
}

// TestSamplingSkipsQueuedAndTerminalRuns 守采样的边界：**排队中**与**终态**一个点都不取。
//
// 排队中的运行连开跑时刻都没有，窗口无从谈起；终态的不会再动，给它续上一串零吞吐的点
// 等于在曲线尾巴上画一段没发生过的停滞。
func TestSamplingSkipsQueuedAndTerminalRuns(t *testing.T) {
	h := newTestEngine(t, registerOnly, 1)
	running := h.start(t, libraryScanSpec(1), idleBody)
	queued := h.start(t, libraryScanSpec(2), idleBody)
	if got := h.load(t, queued.ID).Status; got != StatusQueued {
		t.Fatalf("第二条运行的状态为 %q, want queued（槽位只有一个）", got)
	}

	settled := h.start(t, libraryScanSpec(3), idleBody)
	h.engine.finalize(settled.ID, StatusCompleted, Result{}, "")

	h.clock.advance(DefaultSampleInterval)
	h.engine.SampleActiveRuns(context.Background())

	if got := len(h.samplesOf(running.ID)); got != 1 {
		t.Fatalf("活动态的运行落了 %d 个点, want 1", got)
	}
	if got := len(h.samplesOf(queued.ID)); got != 0 {
		t.Fatalf("排队中的运行落了 %d 个点——它还没开跑，那条曲线是编的", got)
	}
	if got := len(h.samplesOf(settled.ID)); got != 0 {
		t.Fatalf("终态的运行落了 %d 个点——它不会再动了", got)
	}
}

// TestSampleRateMeasuresTheWindowSoStallsReadAsZero 守曲线的诚实度：那个速率量的是
// **这一段窗口**的吞吐，不是这次运行至今的平均值。
//
// 平均值在停滞段只会缓慢下滑，一条卡死的运行照旧显示着几百条每分钟——而这条曲线存在的
// 全部理由就是「它是不是卡住了」不用靠猜。
func TestSampleRateMeasuresTheWindowSoStallsReadAsZero(t *testing.T) {
	h := newTestEngine(t, registerOnly, 0)
	run := h.start(t, libraryScanSpec(1), idleBody)
	ctx := context.Background()

	// 第一个间隔处理了 100 条：窗口从开跑那一刻起算，600 条/分钟。
	advanceTo(t, h, run.ID, 100)
	h.clock.advance(DefaultSampleInterval)
	h.engine.SampleActiveRuns(ctx)

	// 第二个间隔一条都没动：这才是停滞段该有的样子。
	h.clock.advance(DefaultSampleInterval)
	h.engine.SampleActiveRuns(ctx)

	// 第三个间隔又处理了 50 条：曲线重新抬起来，而不是被前两段拉平的平均值。
	advanceTo(t, h, run.ID, 150)
	h.clock.advance(DefaultSampleInterval)
	h.engine.SampleActiveRuns(ctx)

	samples := h.samplesOf(run.ID)
	if len(samples) != 3 {
		t.Fatalf("落了 %d 个点, want 3", len(samples))
	}
	wantCurrent := []int{100, 100, 150}
	wantRate := []float64{600, 0, 300}
	for i, sample := range samples {
		if sample.Current != wantCurrent[i] {
			t.Fatalf("第 %d 个点的计数为 %d, want %d", i, sample.Current, wantCurrent[i])
		}
		if sample.ThroughputPerMinute != wantRate[i] {
			t.Fatalf("第 %d 个点的吞吐为 %.2f/min, want %.2f/min", i, sample.ThroughputPerMinute, wantRate[i])
		}
	}
}

// TestPausedRunsKeepBeingSampledAtZero 守**已暂停**照样取点：它是**活动态**，而它的吞吐
// 真的是零。
//
// 曲线因此在暂停那几段是**平的**而不是断的：断口留给「这一段我们一个观测都没有」
// （丢点、过保留期），而暂停期间我们观测得清清楚楚——它什么都没产出。
func TestPausedRunsKeepBeingSampledAtZero(t *testing.T) {
	h := newTestEngine(t, registerOnly, 0)
	run := h.start(t, libraryScanSpec(1), idleBody)
	advanceTo(t, h, run.ID, 40)
	if err := h.engine.Pause(run.ID); err != nil {
		t.Fatalf("暂停失败: %v", err)
	}

	h.clock.advance(DefaultSampleInterval)
	h.engine.SampleActiveRuns(context.Background())

	samples := h.samplesOf(run.ID)
	if len(samples) != 1 {
		t.Fatalf("已暂停的运行落了 %d 个点, want 1（它是活动态）", len(samples))
	}
	if samples[0].Current != 40 {
		t.Fatalf("点上的计数为 %d, want 40", samples[0].Current)
	}
}

// TestNoJudgementReadsTheSampleTable 守本票那条硬约束：**计数的事实来源是运行行本身，
// 不是采样点**。
//
// 一整轮下来——发起、上报、取点、读快照、列表、收尾——采样表一次都不该被读。读了它，
// 丢一个点就不再只是「曲线少一段」，而会变成进度、终态或速率跟着错。
func TestNoJudgementReadsTheSampleTable(t *testing.T) {
	h := newTestEngine(t, registerOnly, 0)
	ctx := context.Background()
	run := h.start(t, libraryScanSpec(1), idleBody)

	advanceTo(t, h, run.ID, 30)
	h.clock.advance(DefaultSampleInterval)
	h.engine.SampleActiveRuns(ctx)

	if _, err := h.engine.RunSnapshot(ctx, run.ID); err != nil {
		t.Fatalf("取运行快照失败: %v", err)
	}
	if _, err := h.engine.ListSnapshots(ctx, RunFilter{}); err != nil {
		t.Fatalf("列运行失败: %v", err)
	}
	h.engine.finalize(run.ID, StatusCompleted, Result{}, "")

	if got := h.store.sampleReadCalls(); got != 0 {
		t.Fatalf("这一轮读了 %d 次采样表——判断依赖上采样了，而采样是允许丢点的", got)
	}

	// 反过来也要成立：详情页那条按需拉的读取面读得到它，否则上面那个零只是因为一个点都没落下。
	samples, err := h.engine.RunSamples(ctx, run.ID, 0)
	if err != nil {
		t.Fatalf("按需拉采样失败: %v", err)
	}
	if len(samples) != 1 {
		t.Fatalf("按需拉回 %d 个点, want 1", len(samples))
	}
}
