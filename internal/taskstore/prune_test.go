// 这些用例守的是分层保留的 DELETE 到底选中了哪些行。破了意味着历史被多删一批（用户丢记录）
// 或少删一批（一年之后数据库把自己压垮），而**活动态与排队中永不被选中**是它的前提，不是策略。

package taskstore

import (
	"context"
	"testing"
	"time"

	"manga-manager/internal/task"
)

func TestPruneKeepsLatestTerminalRunsPerTask(t *testing.T) {
	ctx := context.Background()
	store := newStoreForTest(t)
	first := ensureTask(t, store, 1)
	second := ensureTask(t, store, 2)

	var sequence int64
	kept := map[int64]bool{}
	for _, taskID := range []int64{first, second} {
		for i := 0; i < 4; i++ {
			sequence++
			run := createRun(t, store, taskID, task.StatusRunning, sequence)
			run = finishRun(t, store, run, task.StatusCompleted, time.Now())
			if i >= 2 {
				kept[run.ID] = true
			}
		}
	}

	result, err := store.PruneRuns(ctx, task.RetentionPolicy{RunsPerTask: 2})
	if err != nil {
		t.Fatalf("prune failed: %v", err)
	}
	if result.Runs != 4 {
		t.Fatalf("pruned runs=%d want 4", result.Runs)
	}
	for _, id := range runIDs(t, store, task.RunFilter{}) {
		if !kept[id] {
			t.Fatalf("run %d survived but should have been pruned", id)
		}
	}
	if got := countRows(t, store, tableRuns); got != len(kept) {
		t.Fatalf("surviving runs=%d want %d", got, len(kept))
	}
}

func TestPruneDropsTerminalRunsOlderThanTheAgeLimit(t *testing.T) {
	ctx := context.Background()
	store := newStoreForTest(t)
	taskID := ensureTask(t, store, 1)

	old := finishRun(t, store, createRun(t, store, taskID, task.StatusRunning, 1),
		task.StatusCompleted, time.Now().Add(-100*24*time.Hour))
	recent := finishRun(t, store, createRun(t, store, taskID, task.StatusRunning, 2),
		task.StatusFailed, time.Now().Add(-24*time.Hour))

	result, err := store.PruneRuns(ctx, task.RetentionPolicy{TerminalAge: 90 * 24 * time.Hour})
	if err != nil {
		t.Fatalf("prune failed: %v", err)
	}
	if result.Runs != 1 {
		t.Fatalf("pruned runs=%d want 1", result.Runs)
	}
	if _, err := store.LoadRun(ctx, recent.ID); err != nil {
		t.Fatalf("recent run should survive: %v", err)
	}
	if _, err := store.LoadRun(ctx, old.ID); err == nil {
		t.Fatalf("old run %d survived the age limit", old.ID)
	}
}

// **活动态与排队中的运行永不被选中**：配额与时长都算不到它们头上，哪怕它们是最老的那一批。
func TestPruneNeverSelectsLiveRuns(t *testing.T) {
	ctx := context.Background()
	store := newStoreForTest(t)
	ancient := time.Now().Add(-365 * 24 * time.Hour)

	live := map[int64]bool{}
	for i, status := range task.LiveStatuses() {
		taskID := ensureTask(t, store, int64(i+1))
		// 活动与排队中的那条是全场最老的一条：它同时落在配额之外与时长阈值之外，
		// 只要有一条谓词忘了排除活着的运行，它就会第一个被删掉。
		run, err := store.CreateRun(ctx, task.Run{
			TaskID: taskID, Trigger: task.TriggerScheduled, NthRun: 1, Status: status,
			UpdatedAt: ancient, StartedAt: ancient, Sequence: int64(i + 1),
		})
		if err != nil {
			t.Fatalf("create %s run failed: %v", status, err)
		}
		live[run.ID] = true
		// 同一个任务上另加两条刚跑完的终态运行，让配额那一层确实有得可裁。
		for nth := 0; nth < 2; nth++ {
			finishRun(t, store, createRun(t, store, taskID, task.StatusCompleted, int64(100+i*10+nth)),
				task.StatusCompleted, time.Now())
		}
	}

	result, err := store.PruneRuns(ctx, task.RetentionPolicy{
		RunsPerTask: 1,
		TerminalAge: 90 * 24 * time.Hour,
		SampleAge:   7 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("prune failed: %v", err)
	}
	if want := int64(len(live)); result.Runs != want {
		t.Fatalf("pruned runs=%d want %d (one surplus terminal run per task)", result.Runs, want)
	}
	if got, want := countRows(t, store, tableRuns), 2*len(live); got != want {
		t.Fatalf("surviving runs=%d want %d", got, want)
	}
	for id := range live {
		if _, err := store.LoadRun(ctx, id); err != nil {
			t.Fatalf("live run %d was pruned: %v", id, err)
		}
	}
}

// 裁剪要报出「清了多少」，而级联删除不报行数——子行必须在删之前数。
func TestPruneCountsCascadedEventsAndSamples(t *testing.T) {
	ctx := context.Background()
	store := newStoreForTest(t)
	taskID := ensureTask(t, store, 1)

	doomed := finishRun(t, store, createRun(t, store, taskID, task.StatusRunning, 1),
		task.StatusCompleted, time.Now())
	kept := finishRun(t, store, createRun(t, store, taskID, task.StatusRunning, 2),
		task.StatusCompleted, time.Now())
	for _, run := range []task.Run{doomed, kept} {
		if err := store.AppendRunEvents(ctx, run.ID, []task.Event{
			{At: time.Now(), Kind: task.EventPhase, Payload: "scan"},
			{At: time.Now(), Kind: task.EventItem, Payload: "broken.cbz"},
		}); err != nil {
			t.Fatalf("append events failed: %v", err)
		}
		if err := store.AppendRunSamples(ctx, run.ID, []task.Sample{
			{At: time.Now(), Current: 1, RatePerMinute: 60},
		}); err != nil {
			t.Fatalf("append samples failed: %v", err)
		}
	}

	result, err := store.PruneRuns(ctx, task.RetentionPolicy{RunsPerTask: 1})
	if err != nil {
		t.Fatalf("prune failed: %v", err)
	}
	if result.Runs != 1 || result.Events != 2 || result.Samples != 1 {
		t.Fatalf("result=%+v want {Runs:1 Events:2 Samples:1}", result)
	}
	if got := countRows(t, store, "run_events"); got != 2 {
		t.Fatalf("surviving events=%d want 2", got)
	}
}

// 既没收尾时刻也没心跳的终态运行算无穷老：NULL 比不出大小，不兜底就等于让它永远躺在库里。
func TestPruneAgeSelectsTerminalRunsWithoutAnyTimestamp(t *testing.T) {
	ctx := context.Background()
	store := newStoreForTest(t)

	undatable, err := store.CreateRun(ctx, task.Run{
		TaskID: ensureTask(t, store, 1), Trigger: task.TriggerChained, NthRun: 1,
		Status: task.StatusInterrupted, Sequence: 1,
	})
	if err != nil {
		t.Fatalf("create undatable run failed: %v", err)
	}

	result, err := store.PruneRuns(ctx, task.RetentionPolicy{TerminalAge: 90 * 24 * time.Hour})
	if err != nil {
		t.Fatalf("prune failed: %v", err)
	}
	if result.Runs != 1 {
		t.Fatalf("pruned runs=%d want 1", result.Runs)
	}
	if _, err := store.LoadRun(ctx, undatable.ID); err == nil {
		t.Fatalf("undatable terminal run %d survived", undatable.ID)
	}
}

// **采样**比运行留得短：运行还在，曲线没了。
func TestPruneSampleAgeLeavesTheRunInPlace(t *testing.T) {
	ctx := context.Background()
	store := newStoreForTest(t)
	run := createRun(t, store, ensureTask(t, store, 1), task.StatusRunning, 1)

	if err := store.AppendRunSamples(ctx, run.ID, []task.Sample{
		{At: time.Now().Add(-8 * 24 * time.Hour), Current: 1, RatePerMinute: 10},
		{At: time.Now(), Current: 9, RatePerMinute: 90},
	}); err != nil {
		t.Fatalf("append samples failed: %v", err)
	}

	result, err := store.PruneRuns(ctx, task.RetentionPolicy{SampleAge: 7 * 24 * time.Hour})
	if err != nil {
		t.Fatalf("prune failed: %v", err)
	}
	if result.Samples != 1 || result.Runs != 0 {
		t.Fatalf("result=%+v want {Runs:0 Samples:1}", result)
	}
	if got := countRows(t, store, "run_samples"); got != 1 {
		t.Fatalf("surviving samples=%d want 1", got)
	}
	if _, err := store.LoadRun(ctx, run.ID); err != nil {
		t.Fatalf("run should survive its samples: %v", err)
	}
}

// 零与负数的阈值都表示这一层不裁剪，别把它当成「一条都不留」。
//
// 负数那几档是这里最要紧的一格：照面值算的话，「留 -1 条」让排名谓词选中全部终态运行，
// 负的时长把截止时刻推到未来，同样一条不剩——而阈值一路来自手写的配置文件。
func TestPruneWithNonPositivePolicyRemovesNothing(t *testing.T) {
	cases := []struct {
		name   string
		policy task.RetentionPolicy
	}{
		{name: "三个阈值全为零", policy: task.RetentionPolicy{}},
		{name: "条数为负", policy: task.RetentionPolicy{RunsPerTask: -1}},
		{name: "终态时长为负", policy: task.RetentionPolicy{TerminalAge: -24 * time.Hour}},
		{name: "采样时长为负", policy: task.RetentionPolicy{SampleAge: -24 * time.Hour}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			store := newStoreForTest(t)
			taskID := ensureTask(t, store, 1)
			ancient := time.Now().Add(-365 * 24 * time.Hour)
			for i := 1; i <= 3; i++ {
				run := finishRun(t, store, createRun(t, store, taskID, task.StatusRunning, int64(i)),
					task.StatusCompleted, ancient)
				if err := store.AppendRunSamples(ctx, run.ID, []task.Sample{
					{At: ancient, Current: 1, RatePerMinute: 60},
				}); err != nil {
					t.Fatalf("append samples failed: %v", err)
				}
			}

			result, err := store.PruneRuns(ctx, tc.policy)
			if err != nil {
				t.Fatalf("prune failed: %v", err)
			}
			if result != (task.PruneResult{}) {
				t.Fatalf("result=%+v want zero", result)
			}
			if got := countRows(t, store, tableRuns); got != 3 {
				t.Fatalf("runs=%d want 3", got)
			}
			if got := countRows(t, store, tableRunSamples); got != 3 {
				t.Fatalf("samples=%d want 3", got)
			}
		})
	}
}
