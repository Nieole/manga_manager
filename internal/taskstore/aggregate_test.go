// 这些用例守的是「这个任务最近十次跑成什么样」确实由一句 SQL 答出。
// 破了意味着指标又退回成一段把行读回内存再循环的 Go，而那正是把指标压进 JSON 堆的老路。

package taskstore

import (
	"context"
	"testing"
	"time"

	"manga-manager/internal/task"
)

// recordRun 落一条已收尾的运行，指定它跑了多久。
func recordRun(t *testing.T, store *Store, taskID int64, sequence int64, took time.Duration) task.Run {
	t.Helper()

	startedAt := time.Now().Add(-time.Hour)
	finishedAt := startedAt.Add(took)
	run, err := store.CreateRun(context.Background(), task.Run{
		TaskID: taskID, Trigger: task.TriggerScheduled, NthRun: int(sequence),
		Status: task.StatusCompleted, StartedAt: startedAt, UpdatedAt: finishedAt,
		FinishedAt: &finishedAt, Sequence: sequence,
	})
	if err != nil {
		t.Fatalf("record run failed: %v", err)
	}
	return run
}

func TestAverageRunDurationCoversOnlyTheRecentRuns(t *testing.T) {
	ctx := context.Background()
	store := newStoreForTest(t)
	taskID := ensureTask(t, store, 1)

	// 最老的那条慢得离谱：只要窗口取错，平均值立刻被它拽走。
	recordRun(t, store, taskID, 1, time.Hour)
	recordRun(t, store, taskID, 2, 10*time.Second)
	recordRun(t, store, taskID, 3, 20*time.Second)

	average, counted, err := store.AverageRunDuration(ctx, taskID, 2)
	if err != nil {
		t.Fatalf("average run duration failed: %v", err)
	}
	if counted != 2 {
		t.Fatalf("counted=%d want 2", counted)
	}
	if average != 15*time.Second {
		t.Fatalf("average=%v want 15s", average)
	}
}

// **排队中**的运行没有开跑时刻，把它算进来等于用一个排队时长冒充耗时。
func TestAverageRunDurationSkipsRunsWithoutBothTimestamps(t *testing.T) {
	ctx := context.Background()
	store := newStoreForTest(t)
	taskID := ensureTask(t, store, 1)

	recordRun(t, store, taskID, 1, 30*time.Second)
	createRun(t, store, taskID, task.StatusQueued, 2)

	average, counted, err := store.AverageRunDuration(ctx, taskID, 10)
	if err != nil {
		t.Fatalf("average run duration failed: %v", err)
	}
	if counted != 1 || average != 30*time.Second {
		t.Fatalf("counted=%d average=%v want 1 and 30s", counted, average)
	}
}

// 窗口先取后筛：还没收尾的那几条占掉「最近十次」里的名额，平均值不得因此够到更老的运行。
func TestAverageRunDurationWindowDoesNotReachPastUnfinishedRuns(t *testing.T) {
	ctx := context.Background()
	store := newStoreForTest(t)
	taskID := ensureTask(t, store, 1)

	recordRun(t, store, taskID, 1, time.Hour) // 更老的那条，不该进最近两次的窗口
	recordRun(t, store, taskID, 2, 20*time.Second)
	createRun(t, store, taskID, task.StatusQueued, 3)

	average, counted, err := store.AverageRunDuration(ctx, taskID, 2)
	if err != nil {
		t.Fatalf("average run duration failed: %v", err)
	}
	if counted != 1 || average != 20*time.Second {
		t.Fatalf("counted=%d average=%v want 1 and 20s", counted, average)
	}
}

func TestAverageRunDurationReportsNoRuns(t *testing.T) {
	ctx := context.Background()
	store := newStoreForTest(t)

	average, counted, err := store.AverageRunDuration(ctx, ensureTask(t, store, 1), 10)
	if err != nil {
		t.Fatalf("average run duration failed: %v", err)
	}
	if counted != 0 || average != 0 {
		t.Fatalf("counted=%d average=%v want 0 and 0", counted, average)
	}
}

// 指标按键加总，且只加这个任务窗口内那几条运行的——别的任务的同名键不得混进来。
func TestSumRunMetricsAggregatesAcrossRecentRunsOfOneTask(t *testing.T) {
	ctx := context.Background()
	store := newStoreForTest(t)
	mine := ensureTask(t, store, 1)
	other := ensureTask(t, store, 2)

	for i, scanned := range []int64{3, 5, 7} {
		run := recordRun(t, store, mine, int64(i+1), time.Second)
		if err := store.SetRunMetrics(ctx, run.ID, map[string]int64{"scanned": scanned}); err != nil {
			t.Fatalf("set metrics failed: %v", err)
		}
	}
	stranger := recordRun(t, store, other, 99, time.Second)
	if err := store.SetRunMetrics(ctx, stranger.ID, map[string]int64{"scanned": 1000}); err != nil {
		t.Fatalf("set metrics failed: %v", err)
	}

	totals, err := store.SumRunMetrics(ctx, mine, 2)
	if err != nil {
		t.Fatalf("sum run metrics failed: %v", err)
	}
	if totals["scanned"] != 12 {
		t.Fatalf("scanned=%d want 12 (the two most recent runs of this task)", totals["scanned"])
	}
}
