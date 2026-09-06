// 这些用例守**退避**的四条规则：曲线按倍率翻并封顶、连败到阈值停发、两条复位路径、
// 以及退避只挡自动**发起**而不碰已经在跑的运行。破了其中任何一条，那个挂在被拔掉的移动硬盘上
// 的库要么继续每小时白转一遍，要么修好之后再也不自己跑了。时钟全程由用例驱动，不等真实时间。

package task

import (
	"context"
	"errors"
	"testing"
	"time"

	"manga-manager/internal/runhandle"
)

// failingBody 是必定失败的任务体，用来把一个任务推向连败。
func failingBody(context.Context, *runhandle.Handle) (Result, error) {
	return Result{}, errors.New("drive is unplugged")
}

// scheduledScanSpec / watchScanSpec 是同一个库级身份的两种自动**发起方**，
// 与 libraryScanSpec（手动）三者共用一个任务。
func scheduledScanSpec(libraryID int64) RunSpec {
	spec := libraryScanSpec(libraryID)
	spec.Trigger = TriggerScheduled
	return spec
}

func watchScanSpec(libraryID int64) RunSpec {
	spec := libraryScanSpec(libraryID)
	spec.Trigger = TriggerWatch
	return spec
}

// attrsOf 取回这个身份此刻的长期属性。
func attrsOf(t *testing.T, h *testEngine, identity Identity) TaskAttributes {
	t.Helper()
	owner, err := h.store.EnsureTask(context.Background(), identity)
	if err != nil {
		t.Fatalf("取任务身份失败: %v", err)
	}
	return owner.TaskAttributes
}

// 曲线本身就是契约：把 ×2 写成 ×1、把 24h 写成 24m 都不会有任何别的用例变红，
// 而用户看到的是那块坏盘继续每小时转一遍。
func TestBackoffCurveDoublesAndCaps(t *testing.T) {
	policy := DefaultBackoff()
	want := []time.Duration{0, 2 * time.Hour, 4 * time.Hour, 8 * time.Hour, 16 * time.Hour, 24 * time.Hour, 24 * time.Hour}
	for streak, expected := range want {
		if got := policy.Delay(streak); got != expected {
			t.Fatalf("连败 %d 次后的退避是 %v, want %v", streak, got, expected)
		}
	}
}

// 倍率与封顶都填得进大到会溢出的值。乘出来的负数或绕回来的小正数会让到期时刻落在过去，
// 退避当场失效——那正是「每小时白转一遍」原封不动地回来。
func TestBackoffCurveNeverOverflows(t *testing.T) {
	policy := BackoffPolicy{Factor: 1 << 20, MaxDelay: 24 * time.Hour, StopAfter: 6}
	for streak := 1; streak <= 128; streak++ {
		got := policy.Delay(streak)
		if got <= 0 || got > policy.MaxDelay {
			t.Fatalf("连败 %d 次后的退避是 %v, 要求落在 (0, %v]", streak, got, policy.MaxDelay)
		}
	}
}

// 三个数任一不合法都退回默认值。停发阈值取 0 尤其致命：`FailStreak >= 0` 恒真，
// 于是**每一个**任务从建出来的那一刻起就停发，一次都还没失败过。
func TestBackoffFallsBackToDefaultsOnNonsenseThresholds(t *testing.T) {
	policy := BackoffPolicy{}
	if got := policy.Stall(TaskAttributes{}, time.Unix(0, 0)); got != StallNone {
		t.Fatalf("全零阈值下一个没失败过的任务被判成 %q, want 不停发", got)
	}
	if got := policy.Delay(1); got != DefaultBackoff().Delay(1) {
		t.Fatalf("全零阈值下的退避是 %v, want 与默认一致 %v", got, DefaultBackoff().Delay(1))
	}
}

// 连败一次之后自动发起就要等退避走完；退避期内再到点一次是**一条运行都不建**，
// 而不是建一条然后失败——后者同样会刷屏，也同样白转一遍盘。
func TestBackoffSuppressesTheNextScheduledLaunch(t *testing.T) {
	h := newTestEngine(t, runBodySynchronously, 0)
	spec := scheduledScanSpec(7)

	first := h.start(t, spec, failingBody)
	if got := h.load(t, first.ID).Status; got != StatusFailed {
		t.Fatalf("首次运行的状态是 %q, want failed", got)
	}
	attrs := attrsOf(t, h, spec.Identity)
	if attrs.FailStreak != 1 {
		t.Fatalf("连败次数是 %d, want 1", attrs.FailStreak)
	}
	if attrs.BackoffUntil == nil || !attrs.BackoffUntil.Equal(h.clock.Now().Add(2*time.Hour)) {
		t.Fatalf("退避到期时刻是 %v, want %v", attrs.BackoffUntil, h.clock.Now().Add(2*time.Hour))
	}

	launched, err := h.engine.Start(context.Background(), spec, failingBody)
	if err != nil {
		t.Fatalf("第二次守护发起返回错误: %v", err)
	}
	if launched.Stalled != StallBackoff {
		t.Fatalf("退避期内的发起被判成 %q, want backoff", launched.Stalled)
	}
	if launched.Run.ID != 0 {
		t.Fatalf("退避期内还是建出了运行 %d", launched.Run.ID)
	}

	// 退避走完，同一个发起方照常放行。
	h.clock.advance(2 * time.Hour)
	second := h.start(t, spec, failingBody)
	if second.ID == first.ID {
		t.Fatalf("退避到期后没有建出新运行")
	}
	if got := attrsOf(t, h, spec.Identity).FailStreak; got != 2 {
		t.Fatalf("第二次失败后的连败次数是 %d, want 2", got)
	}
}

// 连败到阈值就停发：此后无论退避到没到期，自动发起一次都不再发生。
func TestFailStreakAtThresholdStopsAutomaticLaunches(t *testing.T) {
	h := newTestEngine(t, runBodySynchronously, 0)
	spec := scheduledScanSpec(7)

	for range DefaultBackoff().StopAfter {
		h.start(t, spec, failingBody)
		h.clock.advance(48 * time.Hour)
	}
	if got := attrsOf(t, h, spec.Identity).FailStreak; got != DefaultBackoff().StopAfter {
		t.Fatalf("连败次数是 %d, want %d", got, DefaultBackoff().StopAfter)
	}

	launched, err := h.engine.Start(context.Background(), spec, failingBody)
	if err != nil {
		t.Fatalf("停发之后的守护发起返回错误: %v", err)
	}
	if launched.Stalled != StallFailLimit {
		t.Fatalf("停发之后的发起被判成 %q, want fail_limit", launched.Stalled)
	}
	// 监听派生的那条同样被挡住：停发挡的是「自动发起」，不是某一个发起方。
	watched, err := h.engine.Start(context.Background(), watchScanSpec(7), failingBody)
	if err != nil {
		t.Fatalf("停发之后的监听发起返回错误: %v", err)
	}
	if watched.Stalled != StallFailLimit {
		t.Fatalf("停发之后的监听发起被判成 %q, want fail_limit", watched.Stalled)
	}
}

// 复位路径之一：手动发起。它在**发起的那一刻**复位而不是等这次跑完——用户按下重试的意思
// 就是「我修好了」，让他先等一次成功才解除退避，等于修好之后还要再空等一轮。
func TestManualLaunchResetsTheStreakEvenWhenItAlsoFails(t *testing.T) {
	h := newTestEngine(t, runBodySynchronously, 0)
	auto := scheduledScanSpec(7)
	for range 3 {
		h.start(t, auto, failingBody)
		h.clock.advance(48 * time.Hour)
	}
	if got := attrsOf(t, h, auto.Identity).FailStreak; got != 3 {
		t.Fatalf("连败次数是 %d, want 3", got)
	}

	// 手动发起的这一次同样失败，因此收尾时连败从 0 重新数起，而不是接着数到 4。
	h.start(t, libraryScanSpec(7), failingBody)
	if got := attrsOf(t, h, auto.Identity).FailStreak; got != 1 {
		t.Fatalf("手动发起并失败之后的连败次数是 %d, want 1（复位后重新数起）", got)
	}
}

// 复位路径之二：成功一次。上次成功的时刻同时前移——任务清单那一栏「上次成功是什么时候」读的就是它。
func TestSuccessResetsTheStreakAndRecordsLastSuccess(t *testing.T) {
	h := newTestEngine(t, runBodySynchronously, 0)
	spec := scheduledScanSpec(7)
	for range 3 {
		h.start(t, spec, failingBody)
		h.clock.advance(48 * time.Hour)
	}

	succeededAt := h.clock.Now()
	h.start(t, spec, idleBody)

	attrs := attrsOf(t, h, spec.Identity)
	if attrs.FailStreak != 0 || attrs.BackoffUntil != nil {
		t.Fatalf("成功之后的连败与退避是 %d / %v, want 0 / nil", attrs.FailStreak, attrs.BackoffUntil)
	}
	if attrs.LastSuccessAt == nil || !attrs.LastSuccessAt.Equal(succeededAt) {
		t.Fatalf("上次成功的时刻是 %v, want %v", attrs.LastSuccessAt, succeededAt)
	}
}

// **已取消**与**中断**都不是失败：把它们算进连败，一次关服、一次点错的取消就能把一个
// 健康的任务推向停发，而它对着的那块盘一点问题都没有。
func TestCancelAndInterruptDoNotCountAsFailures(t *testing.T) {
	spec := scheduledScanSpec(7)

	cancelling := newTestEngine(t, runBodySynchronously, 0)
	cancelled := cancelling.start(t, spec, func(context.Context, *runhandle.Handle) (Result, error) {
		return Result{}, context.Canceled
	})
	if got := cancelling.load(t, cancelled.ID).Status; got != StatusCancelled {
		t.Fatalf("运行的状态是 %q, want cancelled", got)
	}
	if got := attrsOf(t, cancelling, spec.Identity).FailStreak; got != 0 {
		t.Fatalf("取消之后的连败次数是 %d, want 0", got)
	}

	// 中断只发生在重启那一刻，因此任务体必须还没跑完：换成「只登记不执行」的后台能力。
	restarting := newTestEngine(t, registerOnly, 0)
	restarting.start(t, spec, idleBody)
	if _, err := restarting.engine.MarkInterrupted(context.Background()); err != nil {
		t.Fatalf("批量转中断失败: %v", err)
	}
	if got := attrsOf(t, restarting, spec.Identity).FailStreak; got != 0 {
		t.Fatalf("中断之后的连败次数是 %d, want 0", got)
	}
}

// 人工禁用只挡自动发起，手动照发；它也不动连败与退避那一组值。
func TestDisabledTaskStillAcceptsManualLaunches(t *testing.T) {
	h := newTestEngine(t, runBodySynchronously, 0)
	spec := scheduledScanSpec(7)
	owner, err := h.store.EnsureTask(context.Background(), spec.Identity)
	if err != nil {
		t.Fatalf("取任务身份失败: %v", err)
	}
	if _, err := h.engine.SetTaskDisabled(context.Background(), owner.ID, true); err != nil {
		t.Fatalf("禁用失败: %v", err)
	}

	// 定时与监听两条都不再为它发起。
	for _, blocked := range []RunSpec{spec, watchScanSpec(7)} {
		launched, err := h.engine.Start(context.Background(), blocked, idleBody)
		if err != nil {
			t.Fatalf("禁用后发起方 %q 的发起返回错误: %v", blocked.Trigger, err)
		}
		if launched.Stalled != StallDisabled {
			t.Fatalf("禁用后发起方 %q 的发起被判成 %q, want disabled", blocked.Trigger, launched.Stalled)
		}
	}

	manual := h.start(t, libraryScanSpec(7), idleBody)
	if got := h.load(t, manual.ID).Status; got != StatusCompleted {
		t.Fatalf("禁用期间手动发起的运行状态是 %q, want completed", got)
	}
	if !attrsOf(t, h, spec.Identity).Disabled {
		t.Fatalf("一次手动发起把人工禁用也关掉了")
	}
}

// 停发挡的是**发起**。已经在跑的那条运行不因为退避被打断，也不因为禁用被打断——
// 规格把这条单独写成一句，因为它最容易被实现成「顺手把它停了」。
func TestStallDoesNotTouchTheRunAlreadyInFlight(t *testing.T) {
	h := newTestEngine(t, registerOnly, 0)
	spec := scheduledScanSpec(7)

	// 先让它连败一次进**退避**：任务体只登记不执行，因此这条运行得手工落成失败态。
	failing := h.start(t, spec, idleBody)
	h.engine.finalize(failing.ID, StatusFailed, Result{}, "drive is unplugged")
	if attrsOf(t, h, spec.Identity).BackoffUntil == nil {
		t.Fatalf("第一次失败之后没有进退避")
	}

	// 退避期内发起一条**串联**运行（串联不受停发约束），它此刻正在跑。
	chained := spec
	chained.Trigger = TriggerChained
	running := h.start(t, chained, idleBody)
	owner, err := h.store.EnsureTask(context.Background(), spec.Identity)
	if err != nil {
		t.Fatalf("取任务身份失败: %v", err)
	}
	if _, err := h.engine.SetTaskDisabled(context.Background(), owner.ID, true); err != nil {
		t.Fatalf("禁用失败: %v", err)
	}

	// 退避与禁用都只挡**发起**：那条正在跑的运行状态没变，运行句柄也还在。
	if got := h.load(t, running.ID).Status; got != StatusRunning {
		t.Fatalf("停发之后那条正在跑的运行状态是 %q, want running", got)
	}
	if _, ok := h.engine.Handle(running.ID); !ok {
		t.Fatalf("停发之后那条运行的运行句柄没了")
	}
}

// **串联**与**恢复**不受停发约束：它们形不成「每小时白转一遍盘」那个循环，
// 而清理运行这类收尾工作被一个不相干的连败挡住，历史就再也不清了。
func TestChainedAndResumedLaunchesIgnoreTheStall(t *testing.T) {
	h := newTestEngine(t, runBodySynchronously, 0)
	spec := scheduledScanSpec(7)
	for range DefaultBackoff().StopAfter {
		h.start(t, spec, failingBody)
		h.clock.advance(48 * time.Hour)
	}

	for _, trigger := range []Trigger{TriggerChained, TriggerResumed} {
		chained := scheduledScanSpec(7)
		chained.Trigger = trigger
		launched, err := h.engine.Start(context.Background(), chained, idleBody)
		if err != nil {
			t.Fatalf("发起方 %q 的发起返回错误: %v", trigger, err)
		}
		if launched.Stalled != StallNone {
			t.Fatalf("发起方 %q 被停发挡下（%q），它不该受约束", trigger, launched.Stalled)
		}
	}
}

// 界面标红读的判据与那道闸门必须是同一处：两处各判一遍的话，用户会看到一个标红却仍在
// 每小时白转的任务，或者反过来——一个不再自动跑、界面上却毫无提示的任务。
func TestStallOfAgreesWithTheGate(t *testing.T) {
	h := newTestEngine(t, runBodySynchronously, 0)
	spec := scheduledScanSpec(7)
	h.start(t, spec, failingBody)

	if got := h.engine.StallOf(attrsOf(t, h, spec.Identity)); got != StallBackoff {
		t.Fatalf("界面读到的停发原因是 %q, want backoff", got)
	}
	h.clock.advance(2 * time.Hour)
	if got := h.engine.StallOf(attrsOf(t, h, spec.Identity)); got != StallNone {
		t.Fatalf("退避到期后界面仍读到停发原因 %q", got)
	}
}
