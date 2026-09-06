// 这些用例守的是运行行的存取本身：一列都不许在写入或读回的路上丢掉，定序只认序号，
// 以及侧数据随运行级联而去。破了意味着重启后进度、文案或入参凭空少一块。

package taskstore

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"manga-manager/internal/task"
)

func TestRunRoundTripsEveryColumn(t *testing.T) {
	ctx := context.Background()
	store := newStoreForTest(t)
	taskID := ensureTask(t, store, 7)

	pausedAt := time.Now().Add(-3 * time.Minute).UTC().Truncate(time.Millisecond)
	finishedAt := time.Now().Add(-time.Minute).UTC().Truncate(time.Millisecond)
	want := task.Run{
		TaskID:              taskID,
		Trigger:             task.TriggerWatch,
		NthRun:              4,
		Status:              task.StatusFailed,
		Phase:               "covers",
		CurrentItem:         "第 3 卷.cbz",
		Current:             12,
		Total:               40,
		PausedAt:            &pausedAt,
		ControlPausedMillis: 4200,
		CoalescedCount:      2,
		MessageCode:         "task.scan.failed",
		MessageParams:       map[string]string{"library": "主库"},
		Error:               "open archive: EOF",
		StartedAt:           time.Now().Add(-10 * time.Minute).UTC().Truncate(time.Millisecond),
		UpdatedAt:           time.Now().UTC().Truncate(time.Millisecond),
		FinishedAt:          &finishedAt,
		Sequence:            91,
	}

	created, err := store.CreateRun(ctx, want)
	if err != nil {
		t.Fatalf("create run failed: %v", err)
	}
	want.ID = created.ID

	loaded, err := store.LoadRun(ctx, created.ID)
	if err != nil {
		t.Fatalf("load run failed: %v", err)
	}
	if !reflect.DeepEqual(loaded, want) {
		t.Fatalf("loaded run mismatch:\n got %+v\nwant %+v", loaded, want)
	}
}

// **排队中**的运行留零值开始时刻：写入队时刻的话，排了一小时队的运行会被算成跑了一小时。
func TestQueuedRunKeepsZeroStartedAt(t *testing.T) {
	ctx := context.Background()
	store := newStoreForTest(t)

	queued := createRun(t, store, ensureTask(t, store, 1), task.StatusQueued, 1)
	loaded, err := store.LoadRun(ctx, queued.ID)
	if err != nil {
		t.Fatalf("load run failed: %v", err)
	}
	if !loaded.StartedAt.IsZero() {
		t.Fatalf("queued run started_at=%v want zero", loaded.StartedAt)
	}
}

func TestLoadAndSaveReportMissingRun(t *testing.T) {
	ctx := context.Background()
	store := newStoreForTest(t)

	if _, err := store.LoadRun(ctx, 404); !errors.Is(err, task.ErrRunNotFound) {
		t.Fatalf("load missing run: err=%v want ErrRunNotFound", err)
	}
	err := store.SaveRun(ctx, task.Run{ID: 404, TaskID: 1, Status: task.StatusCompleted})
	if !errors.Is(err, task.ErrRunNotFound) {
		t.Fatalf("save missing run: err=%v want ErrRunNotFound", err)
	}
}

// 一字未变的回写不是「行不存在」：SQLite 数的是 WHERE 命中的行，而不是值真的变了的行。
// 若哪天不是这样，落盘 goroutine 的重复刷盘会开始报运行找不到。
func TestSaveRunWithoutChangesSucceeds(t *testing.T) {
	ctx := context.Background()
	store := newStoreForTest(t)

	run := createRun(t, store, ensureTask(t, store, 1), task.StatusRunning, 1)
	if err := store.SaveRun(ctx, run); err != nil {
		t.Fatalf("save unchanged run failed: %v", err)
	}
}

// 定序的主键是序号，不是时间列：把时间列排成与序号相反的次序，列表仍按序号走。
func TestListRunsOrdersBySequenceNotTimestamps(t *testing.T) {
	store := newStoreForTest(t)
	taskID := ensureTask(t, store, 1)

	base := time.Now()
	var ordered []int64
	for i, sequence := range []int64{3, 1, 2} {
		run, err := store.CreateRun(context.Background(), task.Run{
			TaskID: taskID, Trigger: task.TriggerManual, NthRun: i + 1,
			Status: task.StatusCompleted, Sequence: sequence,
			UpdatedAt: base.Add(time.Duration(-sequence) * time.Hour),
		})
		if err != nil {
			t.Fatalf("create run failed: %v", err)
		}
		ordered = append(ordered, run.ID)
	}

	ascending := runIDs(t, store, task.RunFilter{Order: task.OrderSequenceAsc})
	if want := []int64{ordered[1], ordered[2], ordered[0]}; !reflect.DeepEqual(ascending, want) {
		t.Fatalf("ascending=%v want %v", ascending, want)
	}
	descending := runIDs(t, store, task.RunFilter{Order: task.OrderSequenceDesc})
	if want := []int64{ordered[0], ordered[2], ordered[1]}; !reflect.DeepEqual(descending, want) {
		t.Fatalf("descending=%v want %v", descending, want)
	}
}

func TestCountRunsIgnoresLimitAndFiltersByStatus(t *testing.T) {
	ctx := context.Background()
	store := newStoreForTest(t)
	taskID := ensureTask(t, store, 1)

	createRun(t, store, taskID, task.StatusRunning, 1)
	createRun(t, store, taskID, task.StatusQueued, 2)
	createRun(t, store, taskID, task.StatusCompleted, 3)

	active, err := store.CountRuns(ctx, task.RunFilter{TaskID: taskID, Statuses: task.ActiveStatuses(), Limit: 1})
	if err != nil {
		t.Fatalf("count active runs failed: %v", err)
	}
	if active != 1 {
		t.Fatalf("active=%d want 1", active)
	}
	all, err := store.CountRuns(ctx, task.RunFilter{Limit: 1})
	if err != nil {
		t.Fatalf("count all runs failed: %v", err)
	}
	if all != 3 {
		t.Fatalf("all=%d want 3", all)
	}
}

// 「第几次」取最大值：裁掉早先那几条终态运行之后，下一次运行不得拿到一个已经用过的编号。
func TestMaxNthRunSurvivesPruning(t *testing.T) {
	ctx := context.Background()
	store := newStoreForTest(t)
	taskID := ensureTask(t, store, 1)

	for i := 1; i <= 3; i++ {
		createRun(t, store, taskID, task.StatusCompleted, int64(i))
	}
	if _, err := store.PruneRuns(ctx, task.RetentionPolicy{RunsPerTask: 1}); err != nil {
		t.Fatalf("prune failed: %v", err)
	}

	highest, err := store.MaxNthRun(ctx, taskID)
	if err != nil {
		t.Fatalf("max nth run failed: %v", err)
	}
	if highest != 3 {
		t.Fatalf("max nth run=%d want 3", highest)
	}
	sequence, err := store.MaxRunSequence(ctx)
	if err != nil {
		t.Fatalf("max sequence failed: %v", err)
	}
	if sequence != 3 {
		t.Fatalf("max sequence=%d want 3", sequence)
	}
}
