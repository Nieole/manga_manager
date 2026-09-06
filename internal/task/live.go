// **实况汇总**这一帧：它数的是什么、什么时候投。逐条运行的快照走 progress.go 与 engine.go，
// 与本文件互不相干——那一路答「这一次跑到哪了」，这一路答「盘上此刻总共有几件事」。

package task

import (
	"context"
	"log/slog"
)

// Live 是一帧**实况汇总**：仍会变化的运行有多少、占了几个槽位、闸门关没关着。它一条运行都不带。
//
// 界面上那格「槽位 2/2、排队 1」要的正是这几个数，而它们不由浏览器从运行列表里数出来：
// 那份列表是推来的帧攒出来的，丢过一帧就少一条，而少一条数出来的占用不会报错，只会静静少 1。
type Live struct {
	// Sequence 是这一帧在推送通道上的序号，与运行快照取自同一个计数器（见 Engine.nextSequenceLocked）。
	// 有了它，丢掉一帧汇总与丢掉一帧快照一样能被前端发现。
	Sequence int64
	// Active 是**活动态**运行数，也就是占着**运行槽位**的那些；Queued 是**排队中**的条数。
	Active int
	Queued int
	// Slots 是此刻的运行槽位上限。
	Slots int
	// Paused 是「有没有运行被暂停」，PausedAll 是「全部暂停的闸门还关着吗」。
	// 两个都要发，它们答的不是同一个问题（理由见 Engine.PausedAll）。
	Paused    bool
	PausedAll bool
}

// Summarize 把一批仍会变化的运行数成一帧**实况汇总**。
//
// 它是导出的，因为实况区那条路（一次查询同时要运行列表与汇总）与推送那条路必须数得一模一样：
// 各写一遍的话，两边只要错开一次，界面上的「槽位 2/2」就会配着三张运行卡片。
func (e *Engine) Summarize(runs []Run) Live {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.summarizeLocked(runs)
}

// summarizeLocked 是 Summarize 的持锁版本。调用方持锁。
func (e *Engine) summarizeLocked(runs []Run) Live {
	live := Live{Slots: e.Slots(), PausedAll: e.pausedAll}
	for _, run := range runs {
		if run.Status.IsActive() {
			live.Active++
		} else {
			live.Queued++
		}
		if run.Status == StatusPaused {
			live.Paused = true
		}
	}
	return live
}

// refreshLiveLocked 在**实况汇总**变了的时候投一帧，没变则一帧不投。调用方持锁。
//
// 只在**状态跃迁**之后调，不跟着**计数推进**走：纯计数推进动不了这几个数，跟着算一遍等于
// 每个投递窗口白查一次库。汇总没变时也不投——一条运行从 3 报到 4 不改变盘上有几件事。
func (e *Engine) refreshLiveLocked() {
	if e.publishLive == nil {
		return
	}
	runs, err := e.store.ListRuns(context.Background(), RunFilter{Statuses: liveStatuses})
	if err != nil {
		slog.Warn("Failed to list live runs for the live summary", "error", err)
		return
	}
	live := e.summarizeLocked(runs)
	if live == e.lastLive {
		return
	}
	// 先记下不带序号的那一份再编号：记成带序号的，下一次比较会因为序号必然不同而永远判「变了」。
	e.lastLive = live
	live.Sequence = e.nextSequenceLocked()
	e.publishLive(live)
}
