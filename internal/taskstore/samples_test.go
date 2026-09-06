// 这些用例守的是只有真 SQLite 才答得出的那一半：**采样**点的每一格原样读得回来、
// 读回顺序是时刻升序（曲线按它画），以及条数上限截的是**最近的**那一段。
// 采样随运行级联删除由 cascade_test.go 守，按时长裁剪由 prune_test.go 守。

package taskstore

import (
	"context"
	"testing"
	"time"

	"manga-manager/internal/task"
)

// appendSampleSeries 落 count 个点，第 n 个的计数是 n，时刻按 step 递增。
func appendSampleSeries(t *testing.T, store *Store, runID int64, at time.Time, step time.Duration, count int) {
	t.Helper()
	samples := make([]task.Sample, 0, count)
	for i := 1; i <= count; i++ {
		samples = append(samples, task.Sample{
			At:            at.Add(time.Duration(i) * step),
			Current:       i,
			RatePerMinute: float64(i),
		})
	}
	if err := store.AppendRunSamples(context.Background(), runID, samples); err != nil {
		t.Fatalf("落采样失败: %v", err)
	}
}

// TestRunSamplesRoundTripEveryColumn 守一个点的三格都落得下也读得回。
//
// 吞吐是浮点数：接成整数不会有编译错误，后果是每分钟不到一条的那些运行在曲线上一律画成零，
// 而它们正是最像「卡住了」的那一批。
func TestRunSamplesRoundTripEveryColumn(t *testing.T) {
	ctx := context.Background()
	store := newStoreForTest(t)
	taskID := ensureTask(t, store, 1)
	run := createRun(t, store, taskID, task.StatusRunning, 1)

	at := time.Now().Truncate(time.Millisecond).UTC()
	want := []task.Sample{
		{At: at, Current: 12, RatePerMinute: 72.5},
		{At: at.Add(10 * time.Second), Current: 12, RatePerMinute: 0},
	}
	if err := store.AppendRunSamples(ctx, run.ID, want); err != nil {
		t.Fatalf("落采样失败: %v", err)
	}

	got, err := store.ListRunSamples(ctx, run.ID, 0)
	if err != nil {
		t.Fatalf("读回采样失败: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("读回 %d 个点，想要 %d 个", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 个点读回 %+v，想要 %+v", i+1, got[i], want[i])
		}
	}
}

// TestRunSamplesComeBackOldestFirst 守读回顺序是落下顺序，哪怕全落在同一毫秒里。
//
// 曲线按这个顺序画：按时间列定序的话，同一毫秒里的两个点谁先谁后由实现随手决定，
// 而画出来的折线会来回折。
func TestRunSamplesComeBackOldestFirst(t *testing.T) {
	ctx := context.Background()
	store := newStoreForTest(t)
	taskID := ensureTask(t, store, 1)
	run := createRun(t, store, taskID, task.StatusRunning, 1)

	at := time.Now().Truncate(time.Millisecond).UTC()
	appendSampleSeries(t, store, run.ID, at, 0, 5)

	got, err := store.ListRunSamples(ctx, run.ID, 0)
	if err != nil {
		t.Fatalf("读回采样失败: %v", err)
	}
	for i, sample := range got {
		if sample.Current != i+1 {
			t.Fatalf("第 %d 个点的计数为 %d，想要 %d —— 读回顺序不是落下顺序", i+1, sample.Current, i+1)
		}
	}
}

// TestRunSampleLimitKeepsTheMostRecentStretch 守条数上限截的是**最近的**那一段，
// 且交回来仍是时刻升序。
//
// 与**运行事件**截头部正好相反，理由也正好相反：曲线答的是「它此刻是不是卡住了」，
// 掐掉最近的一段等于把唯一答得出这个问题的部分掐掉。
func TestRunSampleLimitKeepsTheMostRecentStretch(t *testing.T) {
	ctx := context.Background()
	store := newStoreForTest(t)
	taskID := ensureTask(t, store, 1)
	run := createRun(t, store, taskID, task.StatusRunning, 1)

	at := time.Now().Truncate(time.Millisecond).UTC()
	appendSampleSeries(t, store, run.ID, at, 10*time.Second, 10)

	got, err := store.ListRunSamples(ctx, run.ID, 3)
	if err != nil {
		t.Fatalf("读回采样失败: %v", err)
	}
	want := []int{8, 9, 10}
	if len(got) != len(want) {
		t.Fatalf("读回 %d 个点，想要 %d 个", len(got), len(want))
	}
	for i := range want {
		if got[i].Current != want[i] {
			t.Fatalf("截断后第 %d 个点的计数为 %d，想要 %d —— 截的不是最近那一段或没倒回升序",
				i+1, got[i].Current, want[i])
		}
	}
}

// TestRunSamplesOfAnotherRunNeverLeakIn 守曲线只画这一次运行的点：谓词漏掉 run_id 的话，
// 同一个库的历次扫描会连成一条谁也解释不了的锯齿。
func TestRunSamplesOfAnotherRunNeverLeakIn(t *testing.T) {
	ctx := context.Background()
	store := newStoreForTest(t)
	taskID := ensureTask(t, store, 1)
	mine := createRun(t, store, taskID, task.StatusCompleted, 1)
	theirs := createRun(t, store, taskID, task.StatusRunning, 2)

	at := time.Now().Truncate(time.Millisecond).UTC()
	appendSampleSeries(t, store, mine.ID, at, time.Second, 2)
	appendSampleSeries(t, store, theirs.ID, at, time.Second, 5)

	got, err := store.ListRunSamples(ctx, mine.ID, 0)
	if err != nil {
		t.Fatalf("读回采样失败: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("读回 %d 个点, want 2 —— 别的运行的点混进来了", len(got))
	}
}
