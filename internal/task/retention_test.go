// 这些用例守分层保留在领域这一侧的两条判定：裁剪按调用方给的阈值走（不是引擎自己挑一份），
// 以及**做这次裁剪的那条运行不会被这次裁剪带走**。后一条破了，清理运行会把自己删掉——
// 它此后的每一次上报都落空，用户看到的是一条永远停在开跑那一帧的运行。
// 选中哪些行由 taskstore 对着真 SQLite 回答，这里不重复一遍近似实现。

package task

import (
	"context"
	"testing"
	"time"

	"manga-manager/internal/runhandle"
)

// cleanupRunSpec 是清理运行的声明：系统作用域、**串联**发起。
func cleanupRunSpec() RunSpec {
	return RunSpec{
		Identity: Identity{Type: "cleanup_run_history", Scope: ScopeSystem, Variant: VariantPrimary},
		Trigger:  TriggerChained,
		Total:    1,
	}
}

// 裁剪发生在任务体**之内**，因此那一刻这条运行正在运行中——活动态永不被选中，它因此清不掉自己。
// 同一个任务上更早的那几次清理照常被裁掉：它自己产生的运行也受同一套保留策略约束，没有特例。
func TestPruneHistoryNeverTakesTheRunThatIsDoingIt(t *testing.T) {
	h := newTestEngine(t, runBodySynchronously, 0)
	spec := cleanupRunSpec()

	oldest := h.start(t, spec, idleBody)
	newest := h.start(t, spec, idleBody)

	var pruned PruneResult
	cleaner := h.start(t, spec, func(ctx context.Context, _ *runhandle.Handle) (Result, error) {
		var err error
		pruned, err = h.engine.PruneHistory(ctx, RetentionPolicy{RunsPerTask: 1})
		return Result{}, err
	})

	if pruned.Runs != 1 {
		t.Fatalf("裁掉 %d 条运行, want 1 —— 引擎没有按调用方给的阈值裁", pruned.Runs)
	}
	if got := h.load(t, cleaner.ID).Status; got != StatusCompleted {
		t.Fatalf("清理运行自己的状态是 %q, want completed", got)
	}
	if _, err := h.store.LoadRun(context.Background(), oldest.ID); err == nil {
		t.Fatalf("配额之外的运行 %d 还在，保留裁剪没有生效", oldest.ID)
	}
	if _, err := h.store.LoadRun(context.Background(), newest.ID); err != nil {
		t.Fatalf("配额之内的运行 %d 被裁掉了: %v", newest.ID, err)
	}
}

// 阈值为零是「这一层不裁剪」，不是「一条都不留」——引擎不得在这时替调用方补一份默认策略。
func TestPruneHistoryWithoutThresholdsKeepsEverything(t *testing.T) {
	h := newTestEngine(t, runBodySynchronously, 0)
	spec := cleanupRunSpec()
	for range 3 {
		h.start(t, spec, idleBody)
	}

	pruned, err := h.engine.PruneHistory(context.Background(), RetentionPolicy{})
	if err != nil {
		t.Fatalf("裁剪失败: %v", err)
	}
	if pruned != (PruneResult{}) {
		t.Fatalf("裁剪结果 %+v, want 零值", pruned)
	}
}

// 默认策略是会删数据的那一类，数字本身就是契约：把 90 天写成 9 天不会有任何用例变红，
// 用户却会在下一次清理时丢掉三个月的历史。
func TestDefaultRetentionIsTheShippedPolicy(t *testing.T) {
	want := RetentionPolicy{RunsPerTask: 20, TerminalAge: 90 * 24 * time.Hour, SampleAge: 7 * 24 * time.Hour}
	if got := DefaultRetention(); got != want {
		t.Fatalf("默认保留策略 %+v, want %+v", got, want)
	}
}
