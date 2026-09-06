// **运行事件**的发出这一侧：四类各自的落点，以及条目失败的上限与它的溢出计账。
// 事件一律**显式**发出，本包不从日志里推导任何一条。

package task

import (
	"context"
	"log/slog"
)

// itemFailureTally 是一条运行的条目失败计账：写下去了几条，以及被上限挡掉了几条。
//
// 它活在进程里而不是落盘：溢出计数只在收尾时用一次，而收尾就发生在同一个进程里。
// 落盘的话，每挡掉一条就要写一次库——那正是上限要省掉的那些写。
type itemFailureTally struct {
	recorded int
	omitted  int64
}

// emitEventLocked 落一条**运行事件**。调用方持锁。
//
// 发生时刻由引擎的时钟盖，不由调用方给：事件的先后要与运行行上的时刻可比，
// 让每个上报方各自取一次 time.Now 就等于在可控时钟的测试里放进一个真实时钟。
//
// 写失败只告警：一条事件没落下去，代价是详情页少一行，而让它把任务体带崩要糟得多。
func (e *Engine) emitEventLocked(runID int64, event Event) {
	event.At = e.clock()
	if err := e.store.AppendRunEvents(context.Background(), runID, []Event{event}); err != nil {
		slog.Warn("Failed to persist a run event", "run_id", runID, "kind", event.Kind, "error", err)
	}
}

// emitControlLocked 落一条控制动作事件。调用方持锁。
//
// 控制那一类由引擎发而不是经**运行句柄**发，因为句柄属于任务体，而这四个动作没有一个是任务体
// 做的：**合并**发生在一条连任务体都还没起的排队运行上，暂停 / 恢复 / 取消是用户按下的。
// 它仍是**显式**的一次调用，与「日志自动转事件」不是一回事。
func (e *Engine) emitControlLocked(runID int64, action ControlAction) {
	e.emitEventLocked(runID, Event{Kind: EventControl, Action: action})
}

// acceptsEventsLocked 判这条运行此刻还收不收事件：判据与整帧上报一致（**活动态**）。
// 一条已经收尾的运行不该再长出新的失败明细或告警。调用方持锁。
func (e *Engine) acceptsEventsLocked(runID int64) bool {
	_, ok := e.loadActiveLocked(context.Background(), runID)
	return ok
}

// failItem 落一条条目失败事件：哪个文件、为什么。它是**运行句柄**上 ItemFailed 那条通道的落点。
//
// 上限之内落事件，之外**只累加计数**——收尾时由 flushOmittedItemFailuresLocked 落一条
// 「还有 N 条未列出」。一次大库扫描可能有几千个条目失败，全写进去既撑爆表也撑爆界面。
//
// 上限先判、活动态后判，顺序不能反过来：**上限之外的那条路必须一次库都不查**。反过来的话，
// 五万个坏文件就是五万次串行 SELECT，全压在这把序列化了所有运行上报与投递的锁上——
// 而这道上限本来就是为了省掉超出部分的那些库往返。
//
// 反过来这个顺序也是安全的：计账只在上限之内那条路上建（见下），而收尾会把它删掉，
// 因此一条已收尾的运行拿到的是零值计账，照样走到活动态那一判上被挡回去。
func (e *Engine) failItem(runID int64, item, reason string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if tally, ok := e.itemFailures[runID]; ok && tally.recorded >= MaxItemFailureEvents {
		tally.omitted++
		e.itemFailures[runID] = tally
		return
	}
	if !e.acceptsEventsLocked(runID) {
		return
	}
	tally := e.itemFailures[runID]
	tally.recorded++
	e.itemFailures[runID] = tally
	e.emitEventLocked(runID, Event{Kind: EventItem, Item: item, Reason: reason})
}

// warn 落一条运行级告警事件：降级、批量跳过、护栏触发。
// 它是**运行句柄**上 Warn 那条通道的落点，准入判据同 failItem。
//
// count 是「这一条涉及多少个条目」（一整批 N 本被丢掉、还有 N 条未列出），零表示不带计数。
func (e *Engine) warn(runID int64, code, detail string, count int64) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.acceptsEventsLocked(runID) {
		return
	}
	e.emitEventLocked(runID, Event{Kind: EventWarn, Code: code, Detail: detail, Count: count})
}

// OmittedItemFailures 返回这条运行至今被上限挡掉、还没落成事件的条目失败条数。
//
// 它读的是**进程内存**里的计账，因此只在这条运行还活着时非零：收尾那一刻计账被结算成一条
// 「还有 N 条未列出」的告警事件并删掉。运行详情页两处都要看——跑着的时候看这里，
// 跑完了看那条事件——否则用户在最想问「还有多少条」的那一刻恰好什么也看不到。
func (e *Engine) OmittedItemFailures(runID int64) int64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.itemFailures[runID].omitted
}

// flushOmittedItemFailuresLocked 在一条运行离场时结算它的溢出计数：落一条「还有 N 条未列出」，
// 并把计账丢掉。调用方持锁。
//
// 一条也没被挡掉时什么都不落：写一条「还有 0 条未列出」只会让每次运行的事件流末尾多一行废话。
// 计账无论如何都要丢掉，否则运行 id 只增不减地在那张表里堆下去。
func (e *Engine) flushOmittedItemFailuresLocked(runID int64) {
	tally, ok := e.itemFailures[runID]
	if !ok {
		return
	}
	delete(e.itemFailures, runID)
	if tally.omitted <= 0 {
		return
	}
	e.emitEventLocked(runID, Event{
		Kind:  EventWarn,
		Code:  EventCodeItemFailuresOmitted,
		Count: tally.omitted,
	})
}

// RunEvents 取一次运行的**运行事件**，按发生顺序交回；limit 小于等于 0 表示不限条数。
//
// 事件**不进推送通道**（那里只有全量运行快照与**实况汇总**），详情页打开时按这一条按需拉。
func (e *Engine) RunEvents(ctx context.Context, runID int64, limit int) ([]Event, error) {
	return e.store.ListRunEvents(ctx, runID, limit)
}
