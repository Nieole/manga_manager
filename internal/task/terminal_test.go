// 守终态裁决只有一条路径：**任务体返回的错误**决定进哪一条，Result 只能改文案。
// 破了的话，「忘了收尾」与「收了两次」都不会有编译错误，而运行会永远停在活动态占着槽位。

package task

import (
	"context"
	"errors"
	"testing"

	"manga-manager/internal/runhandle"
)

func TestBodyErrorDecidesTerminalState(t *testing.T) {
	cases := []struct {
		name    string
		bodyErr error
		want    RunStatus
		wantMsg string
	}{
		{"正常返回即完成", nil, StatusCompleted, "task.msg.scan.completed"},
		{"取消错误即已取消", context.Canceled, StatusCancelled, "task.msg.scan.cancelled"},
		{"包装过的取消错误仍是已取消", errors.Join(errors.New("收尾"), context.Canceled), StatusCancelled, "task.msg.scan.cancelled"},
		{"其余错误即失败", errors.New("archive is broken"), StatusFailed, "task.msg.scan.failed"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestEngine(t, runBodySynchronously, 0)
			spec := libraryScanSpec(1)
			spec.CompleteCode = "task.msg.scan.completed"
			spec.CancelCode = "task.msg.scan.cancelled"
			spec.FailCode = "task.msg.scan.failed"

			run := h.start(t, spec, func(context.Context, *runhandle.Handle) (Result, error) {
				return Result{}, tc.bodyErr
			})

			settled := h.load(t, run.ID)
			if settled.Status != tc.want {
				t.Fatalf("终态为 %q, want %q", settled.Status, tc.want)
			}
			if settled.MessageCode != tc.wantMsg {
				t.Fatalf("终态文案码为 %q, want %q", settled.MessageCode, tc.wantMsg)
			}
			if settled.FinishedAt == nil {
				t.Fatal("终态没有结束时刻")
			}
		})
	}
}

// TestResultOnlyOverridesTheText 守 Result 只能改文案：它对三条分支一视同仁，
// 无条件带上码的话，用户按下取消看到的会是「归档处理失败」而不是「已取消」。
func TestResultOnlyOverridesTheText(t *testing.T) {
	h := newTestEngine(t, runBodySynchronously, 0)
	spec := libraryScanSpec(1)
	spec.FailCode = "task.msg.scan.failed"

	run := h.start(t, spec, func(context.Context, *runhandle.Handle) (Result, error) {
		return Result{Code: "task.msg.scan.partial"}, errors.New("half of it failed")
	})

	settled := h.load(t, run.ID)
	if settled.Status != StatusFailed {
		t.Fatalf("Result 改动了终态：得到 %q, want failed", settled.Status)
	}
	if settled.MessageCode != "task.msg.scan.partial" {
		t.Fatalf("终态文案码为 %q, want task.msg.scan.partial", settled.MessageCode)
	}
}

// TestOnlyCompletedFillsTheCount 守只有**完成**补齐计数。已取消跟着补的话，
// 那个数就答不出「做完了多少」，用户据此以为活已经干完。
func TestOnlyCompletedFillsTheCount(t *testing.T) {
	cases := []struct {
		name    string
		bodyErr error
		want    int
	}{
		{"完成补齐", nil, 100},
		{"已取消不补", context.Canceled, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestEngine(t, runBodySynchronously, 0)
			run := h.start(t, libraryScanSpec(1), func(context.Context, *runhandle.Handle) (Result, error) {
				return Result{}, tc.bodyErr
			})

			if settled := h.load(t, run.ID); settled.Current != tc.want {
				t.Fatalf("终态计数为 %d, want %d", settled.Current, tc.want)
			}
		})
	}
}

// TestFailedTerminalKeepsTheErrorOthersClearIt 守技术错误串只属于失败态。
func TestFailedTerminalKeepsTheErrorOthersClearIt(t *testing.T) {
	h := newTestEngine(t, runBodySynchronously, 0)

	failed := h.start(t, libraryScanSpec(1), func(context.Context, *runhandle.Handle) (Result, error) {
		return Result{}, errors.New("archive is broken")
	})
	if got := h.load(t, failed.ID).Error; got != "archive is broken" {
		t.Fatalf("失败态的错误串为 %q, want archive is broken", got)
	}

	done := h.start(t, libraryScanSpec(2), idleBody)
	if got := h.load(t, done.ID).Error; got != "" {
		t.Fatalf("完成态带着错误串 %q", got)
	}
}

// TestPanicBecomesAnExplicitFailure 守 panic 兜底：panic 的任务体走不到裁决处，
// 没有兜底的话那条运行永远停在运行中，既占着槽位、又挡着这个任务的下一次发起。
func TestPanicBecomesAnExplicitFailure(t *testing.T) {
	h := newTestEngine(t, runBodySynchronously, 0)

	run, err := h.engine.Start(context.Background(), libraryScanSpec(1), func(context.Context, *runhandle.Handle) (Result, error) {
		panic("scanner blew up")
	})
	if err != nil {
		t.Fatalf("发起运行失败: %v", err)
	}

	settled := h.load(t, run.ID)
	if settled.Status != StatusFailed {
		t.Fatalf("panic 之后运行状态为 %q, want failed", settled.Status)
	}
	if settled.MessageCode != "task.msg.control.panicked" {
		t.Fatalf("panic 的文案码为 %q, want task.msg.control.panicked", settled.MessageCode)
	}
	if settled.Error == "" {
		t.Fatal("panic 值没有落进错误串，用户看不到原因")
	}
}

// TestLateFramesDoNotReviveATerminalRun 守迟到的报文不会把已收尾的运行拽回运行中：
// **扫描观察者**不在任务体的调用栈上，晚一拍很常见。
func TestLateFramesDoNotReviveATerminalRun(t *testing.T) {
	h := newTestEngine(t, runBodySynchronously, 0)

	var late *runhandle.Handle
	run := h.start(t, libraryScanSpec(1), func(_ context.Context, handle *runhandle.Handle) (Result, error) {
		late = handle
		return Result{}, nil
	})

	before := publishedCountFor(h.snapshots(), run.ID)
	late.Advance(7, 100, "task.msg.scan.progress", nil)

	settled := h.load(t, run.ID)
	if settled.Status != StatusCompleted {
		t.Fatalf("迟到的报文把运行拽回了 %q", settled.Status)
	}
	if settled.Current != 100 {
		t.Fatalf("迟到的报文改写了终态计数：%d", settled.Current)
	}
	if after := publishedCountFor(h.snapshots(), run.ID); after != before {
		t.Fatalf("迟到的报文被投递了出去：%d → %d 帧", before, after)
	}
}
