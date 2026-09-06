// 守**运行事件**的四条：种类是封闭枚举、四类各自真的发得出去、条目失败的 500 条上限与它的
// 溢出计数，以及事件不进推送通道（详情页按需拉）。

package task

import (
	"context"
	"strconv"
	"testing"

	"manga-manager/internal/runhandle"
)

// eventsOf 取这条运行至今落下的全部事件。
func (h *testEngine) eventsOf(t *testing.T, runID int64) []Event {
	t.Helper()
	events, err := h.engine.RunEvents(context.Background(), runID, 0)
	if err != nil {
		t.Fatalf("运行 %d 的事件读不回来: %v", runID, err)
	}
	return events
}

// eventsByKind 按种类挑出事件。
func eventsByKind(events []Event, kind EventKind) []Event {
	picked := make([]Event, 0, len(events))
	for _, event := range events {
		if event.Kind == kind {
			picked = append(picked, event)
		}
	}
	return picked
}

// TestPhaseEventsLandOnEveryPhaseChangeOnly 守阶段时间线的原料：**每一次**阶段切换落一条，
// 同一个阶段反复上报只落一条。
//
// 扫描器每 250ms 报一次、报的多数是同一个阶段名，照单全收会让事件流长成第二个日志，
// 而时间线上会挤满零长的段；只落首次的话，回到前一个阶段就再也看不见了。
func TestPhaseEventsLandOnEveryPhaseChangeOnly(t *testing.T) {
	harness := newTestEngine(t, registerOnly, 0)
	run := harness.start(t, libraryScanSpec(1), idleBody)

	handle, ok := harness.engine.Handle(run.ID)
	if !ok {
		t.Fatal("刚发起的运行拿不到运行句柄")
	}
	for _, phase := range []string{"discovering", "discovering", "reading_metadata", "discovering"} {
		handle.Phase(phase, "", nil)
	}

	phases := eventsByKind(harness.eventsOf(t, run.ID), EventPhase)
	got := make([]string, 0, len(phases))
	for _, event := range phases {
		got = append(got, event.Phase)
	}
	want := []string{"discovering", "reading_metadata", "discovering"}
	if len(got) != len(want) {
		t.Fatalf("阶段事件 %v，想要 %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("阶段事件 %v，想要 %v", got, want)
		}
	}
}

// TestItemFailureEventsCarryFileAndReason 守详情页那个「哪些文件失败了、为什么」：
// 每条失败都带着文件与原因，而不只是一个计数。
func TestItemFailureEventsCarryFileAndReason(t *testing.T) {
	harness := newTestEngine(t, registerOnly, 0)
	run := harness.start(t, libraryScanSpec(1), idleBody)

	handle, ok := harness.engine.Handle(run.ID)
	if !ok {
		t.Fatal("刚发起的运行拿不到运行句柄")
	}
	handle.ItemFailed("/library/vol01.cbz", "zip: not a valid archive")

	failures := eventsByKind(harness.eventsOf(t, run.ID), EventItem)
	if len(failures) != 1 {
		t.Fatalf("条目失败事件 %d 条，想要 1 条", len(failures))
	}
	if failures[0].Item != "/library/vol01.cbz" || failures[0].Reason != "zip: not a valid archive" {
		t.Fatalf("失败明细是 %+v，想要文件与原因各就各位", failures[0])
	}
	if failures[0].At != harness.clock.Now() {
		t.Errorf("事件时刻取的不是引擎的时钟：%v", failures[0].At)
	}
}

// TestItemFailuresStopAtTheCapAndCountTheRest 守 500 条上限：写满就不再落事件，
// 超出的只累加计数，收尾时落一条「还有 N 条未列出」。
//
// 一次大库扫描可能有几千个条目失败，全写进去既撑爆表也撑爆界面（用户故事 11）。
func TestItemFailuresStopAtTheCapAndCountTheRest(t *testing.T) {
	const overflow = 37
	harness := newTestEngine(t, registerOnly, 0)
	run := harness.start(t, libraryScanSpec(1), idleBody)

	handle, ok := harness.engine.Handle(run.ID)
	if !ok {
		t.Fatal("刚发起的运行拿不到运行句柄")
	}
	for i := 0; i < MaxItemFailureEvents+overflow; i++ {
		handle.ItemFailed("/library/vol"+strconv.Itoa(i)+".cbz", "corrupted")
	}

	failures := eventsByKind(harness.eventsOf(t, run.ID), EventItem)
	if len(failures) != MaxItemFailureEvents {
		t.Fatalf("留下 %d 条失败明细，想要恰好 %d 条", len(failures), MaxItemFailureEvents)
	}
	// 上限之内那些还没收尾，因此「还有 N 条未列出」此刻不该已经落下。
	if omitted := eventsByKind(harness.eventsOf(t, run.ID), EventWarn); len(omitted) != 0 {
		t.Fatalf("运行还没收尾就落了 %d 条告警", len(omitted))
	}

	harness.engine.finalize(run.ID, StatusCompleted, Result{}, "")

	warns := eventsByKind(harness.eventsOf(t, run.ID), EventWarn)
	if len(warns) != 1 {
		t.Fatalf("收尾时落了 %d 条告警，想要恰好 1 条「还有 N 条未列出」", len(warns))
	}
	if warns[0].Code != EventCodeItemFailuresOmitted || warns[0].Count != overflow {
		t.Fatalf("溢出计数事件是 %+v，想要 code=%s count=%d", warns[0], EventCodeItemFailuresOmitted, overflow)
	}
}

// TestNoOmittedEventWhenNothingWasDropped 守「一条也没被挡掉时什么都不落」：
// 每次运行的事件流末尾多一句「还有 0 条未列出」是纯废话。
func TestNoOmittedEventWhenNothingWasDropped(t *testing.T) {
	harness := newTestEngine(t, runBodySynchronously, 0)
	run := harness.start(t, libraryScanSpec(1), func(_ context.Context, handle *runhandle.Handle) (Result, error) {
		handle.ItemFailed("/library/vol01.cbz", "corrupted")
		return Result{}, nil
	})

	if warns := eventsByKind(harness.eventsOf(t, run.ID), EventWarn); len(warns) != 0 {
		t.Fatalf("没有任何失败被挡掉，却落了 %d 条告警", len(warns))
	}
}

// TestControlActionsLandAsEvents 守控制那一类：暂停、恢复、取消都落进事件流，
// 用户因此答得出「这条运行中间被谁按过什么」。
func TestControlActionsLandAsEvents(t *testing.T) {
	harness := newTestEngine(t, registerOnly, 0)
	run := harness.start(t, libraryScanSpec(1), idleBody)

	if err := harness.engine.Pause(run.ID); err != nil {
		t.Fatalf("暂停失败: %v", err)
	}
	if err := harness.engine.Resume(run.ID); err != nil {
		t.Fatalf("恢复失败: %v", err)
	}
	if err := harness.engine.Cancel(run.ID); err != nil {
		t.Fatalf("取消失败: %v", err)
	}

	controls := eventsByKind(harness.eventsOf(t, run.ID), EventControl)
	want := []ControlAction{ControlPaused, ControlResumed, ControlCancelled}
	if len(controls) != len(want) {
		t.Fatalf("控制事件 %d 条，想要 %d 条", len(controls), len(want))
	}
	for i, action := range want {
		if controls[i].Action != action {
			t.Fatalf("第 %d 条控制事件是 %q，想要 %q", i+1, controls[i].Action, action)
		}
	}
}

// TestCoalescedLaunchLandsOnTheQueuedRun 守**合并**记在被合并进的那一条上：
// 发起方那一次没有自己的运行行，不记在这里那次发起就无处可查（规格关键决定 4）。
func TestCoalescedLaunchLandsOnTheQueuedRun(t *testing.T) {
	harness := newTestEngine(t, registerOnly, 1)
	spec := libraryScanSpec(1)
	first := harness.start(t, spec, idleBody)
	queued := harness.start(t, spec, idleBody)
	harness.start(t, spec, idleBody)

	if queued.Status != StatusQueued {
		t.Fatalf("第二次发起落在 %s，想要排队中", queued.Status)
	}
	if controls := eventsByKind(harness.eventsOf(t, first.ID), EventControl); len(controls) != 0 {
		t.Fatalf("在跑的那条上落了 %d 条控制事件，合并该记在排队那条上", len(controls))
	}
	controls := eventsByKind(harness.eventsOf(t, queued.ID), EventControl)
	if len(controls) != 1 || controls[0].Action != ControlCoalesced {
		t.Fatalf("排队那条上的控制事件是 %+v，想要一条被合并", controls)
	}
}

// TestRepeatedCoalescingLandsOneEvent 守合并事件不随发起次数增长：守护与监听扫描会在一次长扫
// 期间反复撞上同一条排队运行，每次落一条就能在一条还没起跑的运行上堆出几千行。
//
// 「一共被合并了几次」由运行行上的计数回答，事件只回答「它被合并过」。
func TestRepeatedCoalescingLandsOneEvent(t *testing.T) {
	harness := newTestEngine(t, registerOnly, 1)
	spec := libraryScanSpec(1)
	harness.start(t, spec, idleBody)
	queued := harness.start(t, spec, idleBody)
	for i := 0; i < 50; i++ {
		harness.start(t, spec, idleBody)
	}

	if controls := eventsByKind(harness.eventsOf(t, queued.ID), EventControl); len(controls) != 1 {
		t.Fatalf("合并了 50 次，落了 %d 条控制事件，想要恰好 1 条", len(controls))
	}
	if got := harness.load(t, queued.ID).CoalescedCount; got != 50 {
		t.Fatalf("合并计数是 %d，想要 50 —— 次数由运行行上的计数回答", got)
	}
}

// TestOmittedItemFailuresAreVisibleWhileTheRunIsAlive 守用户最想问「还有多少条」的那一刻
// 答得出：运行还在跑时溢出计数就读得到，不必等它收尾（用户故事 11）。
func TestOmittedItemFailuresAreVisibleWhileTheRunIsAlive(t *testing.T) {
	const overflow = 12
	harness := newTestEngine(t, registerOnly, 0)
	run := harness.start(t, libraryScanSpec(1), idleBody)

	handle, ok := harness.engine.Handle(run.ID)
	if !ok {
		t.Fatal("刚发起的运行拿不到运行句柄")
	}
	for i := 0; i < MaxItemFailureEvents+overflow; i++ {
		handle.ItemFailed("/library/vol"+strconv.Itoa(i)+".cbz", "corrupted")
	}

	if got := harness.engine.OmittedItemFailures(run.ID); got != overflow {
		t.Fatalf("跑着的运行报出 %d 条溢出，想要 %d", got, overflow)
	}
	// 收尾把计账结算成事件并清掉：两个来源恰好互斥，相加才不会把同一批数两遍。
	harness.engine.finalize(run.ID, StatusCompleted, Result{}, "")
	if got := harness.engine.OmittedItemFailures(run.ID); got != 0 {
		t.Fatalf("收尾之后内存里还留着 %d 条溢出计数，它已经落成事件了", got)
	}
}

// TestOverCapItemFailuresNeverTouchTheStore 守上限之外那条路**一次库都不查**：
// 五万个坏文件就是五万次串行 SELECT，全压在序列化了所有运行上报与投递的那把锁上。
func TestOverCapItemFailuresNeverTouchTheStore(t *testing.T) {
	harness := newTestEngine(t, registerOnly, 0)
	run := harness.start(t, libraryScanSpec(1), idleBody)

	handle, ok := harness.engine.Handle(run.ID)
	if !ok {
		t.Fatal("刚发起的运行拿不到运行句柄")
	}
	for i := 0; i < MaxItemFailureEvents; i++ {
		handle.ItemFailed("/library/vol"+strconv.Itoa(i)+".cbz", "corrupted")
	}

	before := harness.store.loadRunCalls()
	for i := 0; i < 100; i++ {
		handle.ItemFailed("/library/extra"+strconv.Itoa(i)+".cbz", "corrupted")
	}
	if after := harness.store.loadRunCalls(); after != before {
		t.Fatalf("上限之外的 100 条失败查了 %d 次库，想要一次都不查", after-before)
	}
}

// TestEventsNeverRideThePushChannel 守规格的边界：事件**不推送**，详情页打开时按需拉。
//
// 一条运行落下几百条失败明细的同时，推送通道上不该多出哪怕一帧——推送的载荷只有全量运行快照
// 与**实况汇总**，把事件挤进去等于让每个开着页面的浏览器替所有人收下整条事件流。
func TestEventsNeverRideThePushChannel(t *testing.T) {
	harness := newTestEngine(t, registerOnly, 0)
	run := harness.start(t, libraryScanSpec(1), idleBody)

	before := publishedCountFor(harness.snapshots(), run.ID)
	handle, ok := harness.engine.Handle(run.ID)
	if !ok {
		t.Fatal("刚发起的运行拿不到运行句柄")
	}
	for i := 0; i < 50; i++ {
		handle.ItemFailed("/library/vol"+strconv.Itoa(i)+".cbz", "corrupted")
		handle.Warn("degraded", "fell back to the slow path", 0)
	}

	if after := publishedCountFor(harness.snapshots(), run.ID); after != before {
		t.Fatalf("落事件的过程投递了 %d 帧，想要一帧都不投", after-before)
	}
	if got := len(harness.eventsOf(t, run.ID)); got < 100 {
		t.Fatalf("事件只落下 %d 条：不推送不等于不落盘", got)
	}
}

// TestEventsAreRefusedOnceTheRunIsTerminal 守迟到的报文不再长出新的明细：
// 一条已经收尾的运行不该在用户眼皮底下继续多出失败行。
func TestEventsAreRefusedOnceTheRunIsTerminal(t *testing.T) {
	var late *runhandle.Handle
	harness := newTestEngine(t, runBodySynchronously, 0)
	run := harness.start(t, libraryScanSpec(1), func(_ context.Context, handle *runhandle.Handle) (Result, error) {
		late = handle
		return Result{}, nil
	})
	if got := harness.load(t, run.ID).Status; got != StatusCompleted {
		t.Fatalf("运行停在 %s，想要完成", got)
	}

	late.ItemFailed("/library/vol01.cbz", "corrupted")
	late.Warn("degraded", "too late", 0)
	if got := len(harness.eventsOf(t, run.ID)); got != 0 {
		t.Fatalf("终态之后又落下 %d 条事件", got)
	}
}
