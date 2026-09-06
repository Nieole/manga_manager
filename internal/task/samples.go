// **采样**这一侧：每隔一个间隔对活动运行的计数与吞吐取一个点，连起来就是详情页那条吞吐曲线。
// 采样**只用来看趋势**——计数的事实来源始终是运行行本身，本包不让任何判断读采样。

package task

import (
	"context"
	"log/slog"
	"time"
)

// DefaultSampleInterval 是**采样**的默认取点间隔。
//
// 十秒是「看得出停滞」与「写得起」之间的取点：一次跑一夜的扫描落下几千行，
// 而更密的取点并不会让「它是不是卡住了」这个问题答得更早——用户本来也要盯上十几秒才起疑。
const DefaultSampleInterval = 10 * time.Second

// sampleGate 是单条运行的取点水位：上一个点落在什么时刻、当时的计数是多少。
//
// 一个结构体记两样，因为它同时是两件事的依据——「这一次该不该取点」看时刻，
// 「这一段的吞吐是多少」看计数差。分成两处存会让「丢了一个点之后窗口从哪算」有两个答案。
//
// 它活在进程里而不是落盘：重启之后一条活动运行都不剩（全部转**中断**），没有哪条曲线要接上。
type sampleGate struct {
	at      time.Time
	current int
}

// SampleInterval 返回此刻生效的取点间隔；读回非正数按 DefaultSampleInterval 处理。
//
// 每次取点都现读一遍，因此改设置对**下一个点**生效，不必重启进程（理由同 Slots）。
//
// **它不加锁，也不得加锁**：取点在临界区里读它，加了锁当场死锁。它读的 sampleInterval 属于
// 「装配期注入、之后只读」的那组，因此本来也不需要。
func (e *Engine) SampleInterval() time.Duration {
	if e.sampleInterval == nil {
		return DefaultSampleInterval
	}
	if interval := e.sampleInterval(); interval > 0 {
		return interval
	}
	return DefaultSampleInterval
}

// SampleActiveRuns 对每条**活动态**运行取一个**采样**点：这一刻的计数，与自上一个点以来的吞吐。
//
// **排队中与终态一个点都不取**：排队中的运行还没开跑，它连开始时刻都没有；终态的不会再动，
// 给它续上一串零吞吐的点等于在曲线尾巴上画一段没发生过的停滞。
//
// 取点的节奏由**本方法自己的水位**说了算，不由调用方的节拍说了算：调用得再密，
// 一条运行在一个间隔里也只落一个点。这条是「采样写入要节流成本可控」的落点——
// 它让采样**不跟着进度帧走**，哪怕将来有人把它挂到每一帧上。
//
// 它**不改运行行、不取序号、不投递**：采样是一次观察，不是一次变化。落一个点就把运行顶到
// 任务中心最前，或者把它推上推送通道（票 19 明写事件与采样不推），都会让「观察」变成「事件」。
//
// 出错只告警：列不出运行就这一轮不取点，写不进去就少一个点。**采样丢一个点没有后果**，
// 界面上曲线少一段而已——让它把任务体或停机流程带崩要糟得多。
func (e *Engine) SampleActiveRuns(ctx context.Context) {
	e.mu.Lock()
	defer e.mu.Unlock()

	runs, err := e.store.ListRuns(ctx, RunFilter{Statuses: activeStatuses})
	if err != nil {
		slog.Warn("Failed to list active runs for sampling", "error", err)
		return
	}
	now := e.clock()
	interval := e.SampleInterval()
	for _, run := range runs {
		e.sampleRunLocked(ctx, run, now, interval)
	}
}

// sampleRunLocked 给一条运行取一个点，水位未到就跳过。调用方持锁。
//
// 首个点的**前一点**取开跑那一刻、计数为零：不这样的话首点没有窗口，吞吐无从算起，
// 而「开跑就先取一个点」会在每条曲线头上钉一个零吞吐的假点。活动态的运行一定有开始时刻；
// 万一没有就整条跳过——宁可少一段曲线，也不为它编一个窗口。
//
// 水位无论落盘成不成都往前挪：写失败重试等于让一条写不进去的运行每一轮都再试一次，
// 而它丢的本来就只是一个点。
func (e *Engine) sampleRunLocked(ctx context.Context, run Run, now time.Time, interval time.Duration) {
	previous, seen := e.samples[run.ID]
	if !seen {
		if run.StartedAt.IsZero() {
			return
		}
		previous = sampleGate{at: run.StartedAt}
	}
	window := now.Sub(previous.at)
	if window < interval {
		return
	}
	e.samples[run.ID] = sampleGate{at: now, current: run.Current}
	sample := Sample{
		At:            now,
		Current:       run.Current,
		RatePerMinute: throughput(run.Current-previous.current, window),
	}
	if err := e.store.AppendRunSamples(ctx, run.ID, []Sample{sample}); err != nil {
		slog.Warn("Failed to persist a run sample", "run_id", run.ID, "error", err)
	}
}

// throughput 是一段窗口里的吞吐：这一段处理了多少条，折成每分钟多少条。
//
// **它量的是这一段，不是这次运行至今的平均值**，两者答的不是同一个问题。运行卡片上那个速率
// 答「这次运行整体跑多快」，因此分母里要扣掉**暂停**；曲线上这个数答「它此刻还在不在产出」，
// 因此分母就是墙上时间——暂停那几段里一条都没处理，算成零正是要看的那件事。
// 把这里也改成扣暂停，暂停期间的窗口会缩成零，曲线在最该说话的地方反而断掉。
//
// 倒退的计数与非正的窗口一律算零：计数本该单调，真出现倒退（上报方重置了它）也不该长出一个
// 负的吞吐——那在界面上没有任何念法。
func throughput(delta int, window time.Duration) float64 {
	if delta <= 0 || window <= 0 {
		return 0
	}
	return float64(delta) * 60 / window.Seconds()
}

// RunSamples 取一条运行的**采样**点，按时刻升序交回；limit 大于 0 时只交回**最近的**那几个。
//
// 采样**不进推送通道**（那里只有全量运行快照与**实况汇总**），详情页打开时按这一条按需拉。
func (e *Engine) RunSamples(ctx context.Context, runID int64, limit int) ([]Sample, error) {
	return e.store.ListRunSamples(ctx, runID, limit)
}
