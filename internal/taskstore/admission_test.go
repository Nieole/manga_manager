// 这些用例守的是**准入由数据库保证**：两条部分唯一索引各自拒绝第二条。
// 破了意味着同一个任务能同时跑两次，或队列里堆出一串一模一样的排队项。

package taskstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"manga-manager/internal/task"
)

func TestActiveIndexRejectsSecondActiveRun(t *testing.T) {
	ctx := context.Background()
	active := task.ActiveStatuses()

	for _, first := range active {
		for _, second := range active {
			t.Run(string(first)+"_then_"+string(second), func(t *testing.T) {
				store := newStoreForTest(t)
				taskID := ensureTask(t, store, 1)
				createRun(t, store, taskID, first, 1)

				_, err := store.CreateRun(ctx, task.Run{
					TaskID: taskID, Trigger: task.TriggerScheduled, NthRun: 2,
					Status: second, UpdatedAt: time.Now(), Sequence: 2,
				})
				if !errors.Is(err, task.ErrRunAlreadyActive) {
					t.Fatalf("second %s run: err=%v want ErrRunAlreadyActive", second, err)
				}
				if got := countRows(t, store, tableRuns); got != 1 {
					t.Fatalf("runs=%d want 1", got)
				}
			})
		}
	}
}

func TestQueuedIndexRejectsSecondQueuedRun(t *testing.T) {
	ctx := context.Background()
	store := newStoreForTest(t)
	taskID := ensureTask(t, store, 1)
	createRun(t, store, taskID, task.StatusQueued, 1)

	_, err := store.CreateRun(ctx, task.Run{
		TaskID: taskID, Trigger: task.TriggerScheduled, NthRun: 2,
		Status: task.StatusQueued, UpdatedAt: time.Now(), Sequence: 2,
	})
	if !errors.Is(err, task.ErrRunAlreadyQueued) {
		t.Fatalf("second queued run: err=%v want ErrRunAlreadyQueued", err)
	}
	if got := countRows(t, store, tableRuns); got != 1 {
		t.Fatalf("runs=%d want 1", got)
	}
}

// **排队中**既不是活动态也不是终态：它与一条活动运行共存，否则冲突就无处可排。
func TestActiveRunAndQueuedRunCoexist(t *testing.T) {
	store := newStoreForTest(t)
	taskID := ensureTask(t, store, 1)

	createRun(t, store, taskID, task.StatusRunning, 1)
	createRun(t, store, taskID, task.StatusQueued, 2)

	if got := countRows(t, store, tableRuns); got != 2 {
		t.Fatalf("runs=%d want 2", got)
	}
}

// 同一任务的**终态**运行不设上限——这正是「重试不覆盖历史」的底座。
func TestTerminalRunsAccumulateForTheSameTask(t *testing.T) {
	store := newStoreForTest(t)
	taskID := ensureTask(t, store, 1)

	terminal := []task.RunStatus{
		task.StatusCompleted, task.StatusCancelled, task.StatusFailed,
		task.StatusInterrupted, task.StatusCompleted,
	}
	for i, status := range terminal {
		createRun(t, store, taskID, status, int64(i+1))
	}

	if got := countRows(t, store, tableRuns); got != len(terminal) {
		t.Fatalf("runs=%d want %d", got, len(terminal))
	}
}

// 索引按任务分区：两个任务各有一条活动运行是常态，不是冲突。
func TestActiveIndexIsPerTask(t *testing.T) {
	store := newStoreForTest(t)

	createRun(t, store, ensureTask(t, store, 1), task.StatusRunning, 1)
	createRun(t, store, ensureTask(t, store, 2), task.StatusRunning, 2)

	if got := countRows(t, store, tableRuns); got != 2 {
		t.Fatalf("runs=%d want 2", got)
	}
}

// 队列放行是一次 UPDATE，它同样过活动索引：撞上了就拿到准入哨兵而不是一条静默的双开运行。
func TestQueuedRunCannotBeReleasedWhileAnotherRunIsActive(t *testing.T) {
	ctx := context.Background()
	store := newStoreForTest(t)
	taskID := ensureTask(t, store, 1)

	createRun(t, store, taskID, task.StatusRunning, 1)
	queued := createRun(t, store, taskID, task.StatusQueued, 2)

	queued.Status = task.StatusRunning
	if err := store.SaveRun(ctx, queued); !errors.Is(err, task.ErrRunAlreadyActive) {
		t.Fatalf("release queued run: err=%v want ErrRunAlreadyActive", err)
	}
}

// 存量库上索引必须跟着领域的活动态集合走：`IF NOT EXISTS` 会让老库停在建库那天的谓词上，
// 而那时候新加的活动态还不在里面——第二条活动运行会被静默放行。
func TestMigrateRebuildsAdmissionIndexOnAnExistingDatabase(t *testing.T) {
	ctx := context.Background()
	store := newStoreForTest(t)

	// 摆出一个「谓词比现在窄」的老库：只认运行中。
	if _, err := store.db.Exec(`DROP INDEX ` + indexRunsOneActivePerTask); err != nil {
		t.Fatalf("drop index failed: %v", err)
	}
	if _, err := store.db.Exec(`CREATE UNIQUE INDEX ` + indexRunsOneActivePerTask +
		` ON ` + tableRuns + `(task_id) WHERE status = 'running'`); err != nil {
		t.Fatalf("create stale index failed: %v", err)
	}
	if err := Migrate(store.db); err != nil {
		t.Fatalf("second migrate failed: %v", err)
	}

	taskID := ensureTask(t, store, 1)
	createRun(t, store, taskID, task.StatusRunning, 1)
	_, err := store.CreateRun(ctx, task.Run{
		TaskID: taskID, Trigger: task.TriggerManual, NthRun: 2,
		Status: task.StatusPaused, UpdatedAt: time.Now(), Sequence: 2,
	})
	if !errors.Is(err, task.ErrRunAlreadyActive) {
		t.Fatalf("paused run after remigrate: err=%v want ErrRunAlreadyActive", err)
	}
}

// 身份的唯一约束比较四列，作用域 id 用 0 表示「系统级没有作用域对象」。
// 留 NULL 的话 SQL 里 NULL 不等于 NULL，同一个系统级身份会被建出任意多条。
func TestEnsureTaskReturnsOneIdentityPerFourFields(t *testing.T) {
	ctx := context.Background()
	store := newStoreForTest(t)

	system := task.Identity{Type: "cleanup_runs", Scope: task.ScopeSystem}
	first, err := store.EnsureTask(ctx, system)
	if err != nil {
		t.Fatalf("ensure system task failed: %v", err)
	}
	again, err := store.EnsureTask(ctx, system)
	if err != nil {
		t.Fatalf("ensure system task again failed: %v", err)
	}
	if first.ID != again.ID {
		t.Fatalf("system identity ids differ: %d vs %d", first.ID, again.ID)
	}

	variant := system
	variant.Variant = "backfill"
	other, err := store.EnsureTask(ctx, variant)
	if err != nil {
		t.Fatalf("ensure variant task failed: %v", err)
	}
	if other.ID == first.ID {
		t.Fatalf("variant reused identity %d", other.ID)
	}
	if got := countRows(t, store, tableTasks); got != 2 {
		t.Fatalf("identities=%d want 2", got)
	}
}
