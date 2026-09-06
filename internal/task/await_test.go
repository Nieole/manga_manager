// 守 Await 的判据是**运行收尾了没有**，而不是「任务体返回了没有」。
// 两者在**排队中**被取消时会分家：那条运行的任务体一次都不会执行，只等任务体就是永远等下去。

package task

import (
	"context"
	"testing"
	"time"

	"manga-manager/internal/runhandle"
)

// TestAwaitReturnsImmediatelyForATerminalRun 守已经收尾的运行不必等：登记与「已经是终态了吗」
// 在同一个临界区里做，否则两者之间收尾的那条运行谁也不会来叫醒等待方。
func TestAwaitReturnsImmediatelyForATerminalRun(t *testing.T) {
	h := newTestEngine(t, runBodySynchronously, 0)
	run := h.start(t, libraryScanSpec(1), idleBody)

	settled, err := h.engine.Await(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("等一条已经收尾的运行返回了错误: %v", err)
	}
	if settled.Status != StatusCompleted {
		t.Fatalf("交回的状态为 %q, want completed", settled.Status)
	}
}

// TestAwaitWakesWhenTheBodyFinishes 是主路径：任务体跑完、终态落盘之后等待方才醒，
// 醒来读回的必须已经是终态那一行。
func TestAwaitWakesWhenTheBodyFinishes(t *testing.T) {
	release := make(chan struct{})
	h := newTestEngine(t, func(fn func()) { go fn() }, 0)

	launched, err := h.engine.Start(context.Background(), libraryScanSpec(1), func(context.Context, *runhandle.Handle) (Result, error) {
		<-release
		return Result{}, nil
	})
	if err != nil {
		t.Fatalf("发起运行失败: %v", err)
	}

	awaited := make(chan Run, 1)
	go func() {
		settled, _ := h.engine.Await(context.Background(), launched.Run.ID)
		awaited <- settled
	}()

	select {
	case settled := <-awaited:
		t.Fatalf("任务体还没返回，等待方就醒了：状态 %q", settled.Status)
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	select {
	case settled := <-awaited:
		if settled.Status != StatusCompleted {
			t.Fatalf("醒来读回的状态为 %q, want completed —— 叫醒发生在落盘之前", settled.Status)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("任务体跑完了，等待方没醒")
	}
}

// TestAwaitWakesWhenAQueuedRunIsCancelled 是那条会分家的路：**排队中**被取消的运行任务体
// 一次都不会执行，而等待方必须照样醒过来——不然发起它的那条 goroutine 会一直挂到停机。
func TestAwaitWakesWhenAQueuedRunIsCancelled(t *testing.T) {
	h := newTestEngine(t, func(func()) {}, 1)

	first := h.start(t, libraryScanSpec(1), idleBody)
	queued := h.start(t, libraryScanSpec(2), idleBody)
	if got := h.load(t, queued.ID).Status; got != StatusQueued {
		t.Fatalf("第二条运行状态为 %q, want queued（上限为 1）", got)
	}
	_ = first

	awaited := make(chan Run, 1)
	go func() {
		settled, _ := h.engine.Await(context.Background(), queued.ID)
		awaited <- settled
	}()

	if err := h.engine.Cancel(queued.ID); err != nil {
		t.Fatalf("取消排队中的运行失败: %v", err)
	}
	select {
	case settled := <-awaited:
		if settled.Status != StatusCancelled {
			t.Fatalf("交回的状态为 %q, want cancelled", settled.Status)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("排队中的运行被取消了，等待方还挂着 —— 它的任务体永远不会来叫醒它")
	}
}

// TestAwaitReleasesOnContextCancel 守等待方半路走开：ctx 一取消就返回，
// 且不在引擎里留下一个永远等不到的通道。
func TestAwaitReleasesOnContextCancel(t *testing.T) {
	h := newTestEngine(t, func(func()) {}, 0)
	run := h.start(t, libraryScanSpec(1), idleBody)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := h.engine.Await(ctx, run.ID); err == nil {
		t.Fatal("ctx 已取消，Await 却正常返回了")
	}

	h.engine.mu.Lock()
	remaining := len(h.engine.settled)
	h.engine.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("走开之后还留着 %d 条运行的等待通道", remaining)
	}
}
