// 这些用例守的是外键级联：删掉一条运行，挂在它上面的六样侧数据一并消失。
// 破了意味着裁剪之后库里留下一堆再也读不到的孤儿行，一年之后以「库怎么这么大」的形式暴露。

package taskstore

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"manga-manager/internal/task"
)

// writeSideData 把六样侧数据各写一份到这条运行上。
func writeSideData(t *testing.T, store *Store, runID int64) {
	t.Helper()
	ctx := context.Background()

	if err := store.SaveRunLimits(ctx, runID, task.Limits{
		ScanProfile: "balanced", ScannerWorkersConfigured: 4, ScannerWorkersEffective: 3,
		StorageProfile: "hdd", VolumeKey: "vol-1", ScanConcurrency: 2,
		ArchiveOpenConcurrency: 3, CoverConcurrency: 1, HashConcurrency: 2,
		PauseBackgroundWhenReading: true, IdleOnlyHeavyTasks: true, DisableSameDiskPageCache: true,
	}); err != nil {
		t.Fatalf("save run limits failed: %v", err)
	}
	if err := store.MergeRunArgs(ctx, runID, map[string]string{"library_id": "7"}); err != nil {
		t.Fatalf("merge run args failed: %v", err)
	}
	if err := store.MergeRunLabels(ctx, runID, map[string]string{"source": "comicvine"}); err != nil {
		t.Fatalf("merge run labels failed: %v", err)
	}
	if err := store.SetRunMetrics(ctx, runID, map[string]int64{"scanned": 10}); err != nil {
		t.Fatalf("set run metrics failed: %v", err)
	}
	if err := store.AppendRunEvents(ctx, runID, []task.Event{
		{At: time.Now(), Kind: task.EventPhase, Phase: "covers"},
	}); err != nil {
		t.Fatalf("append run events failed: %v", err)
	}
	if err := store.AppendRunSamples(ctx, runID, []task.Sample{
		{At: time.Now(), Current: 5, RatePerMinute: 30},
	}); err != nil {
		t.Fatalf("append run samples failed: %v", err)
	}
}

var sideTables = []string{"run_limits", "run_args", "run_labels", "run_metrics", "run_events", "run_samples"}

func TestDeletingRunCascadesToSideTables(t *testing.T) {
	store := newStoreForTest(t)
	taskID := ensureTask(t, store, 1)
	doomed := createRun(t, store, taskID, task.StatusCompleted, 1)
	kept := createRun(t, store, taskID, task.StatusRunning, 2)
	writeSideData(t, store, doomed.ID)
	writeSideData(t, store, kept.ID)

	if _, err := store.db.Exec(`DELETE FROM `+tableRuns+` WHERE id = ?`, doomed.ID); err != nil {
		t.Fatalf("delete run failed: %v", err)
	}

	for _, table := range sideTables {
		if got := countRows(t, store, table); got != 1 {
			t.Fatalf("%s=%d want 1 (only the surviving run's row)", table, got)
		}
	}
}

func TestDeletingTaskCascadesToRuns(t *testing.T) {
	store := newStoreForTest(t)
	taskID := ensureTask(t, store, 1)
	run := createRun(t, store, taskID, task.StatusCompleted, 1)
	writeSideData(t, store, run.ID)

	if _, err := store.db.Exec(`DELETE FROM `+tableTasks+` WHERE id = ?`, taskID); err != nil {
		t.Fatalf("delete task failed: %v", err)
	}

	if got := countRows(t, store, tableRuns); got != 0 {
		t.Fatalf("runs=%d want 0", got)
	}
	for _, table := range sideTables {
		if got := countRows(t, store, table); got != 0 {
			t.Fatalf("%s=%d want 0", table, got)
		}
	}
}

// 指标两条写入路各有各的语义：设是「我握着全量当前值」，加是「我只知道这一份报文的增量」。
// 挑错一个不会有编译错误，后果是指标要么翻倍、要么只剩最后一份报文。
func TestMetricsSetOverwritesWhileAddAccumulates(t *testing.T) {
	ctx := context.Background()
	store := newStoreForTest(t)
	taskID := ensureTask(t, store, 1)
	run := createRun(t, store, taskID, task.StatusRunning, 1)

	if err := store.SetRunMetrics(ctx, run.ID, map[string]int64{"scanned": 10}); err != nil {
		t.Fatalf("set metrics failed: %v", err)
	}
	if err := store.AddRunMetrics(ctx, run.ID, map[string]int64{"scanned": 5, "failed": 2}); err != nil {
		t.Fatalf("add metrics failed: %v", err)
	}
	if err := store.SetRunMetrics(ctx, run.ID, map[string]int64{"failed": 1}); err != nil {
		t.Fatalf("set metrics again failed: %v", err)
	}

	totals, err := store.SumRunMetrics(ctx, taskID, 10)
	if err != nil {
		t.Fatalf("sum metrics failed: %v", err)
	}
	if totals["scanned"] != 15 {
		t.Fatalf("scanned=%d want 15", totals["scanned"])
	}
	if totals["failed"] != 1 {
		t.Fatalf("failed=%d want 1", totals["failed"])
	}
}

// 键值侧表按键合并而不是整份覆盖：一次只带一个键的报文不得把别的键抹掉。
func TestKeyValueSideTablesMergeByKey(t *testing.T) {
	ctx := context.Background()
	store := newStoreForTest(t)
	run := createRun(t, store, ensureTask(t, store, 1), task.StatusRunning, 1)

	if err := store.MergeRunArgs(ctx, run.ID, map[string]string{"library_id": "7", "mode": "full"}); err != nil {
		t.Fatalf("merge args failed: %v", err)
	}
	if err := store.MergeRunArgs(ctx, run.ID, map[string]string{"mode": "incremental"}); err != nil {
		t.Fatalf("merge args again failed: %v", err)
	}

	args := map[string]string{}
	rows, err := store.db.QueryContext(ctx, `SELECT key, value FROM run_args WHERE run_id = ?`, run.ID)
	if err != nil {
		t.Fatalf("read args failed: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			t.Fatalf("scan args failed: %v", err)
		}
		args[key] = value
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate args failed: %v", err)
	}
	if args["library_id"] != "7" || args["mode"] != "incremental" {
		t.Fatalf("args=%v want library_id=7 mode=incremental", args)
	}
}

// 外键关掉时级联不发生而 DELETE 照跑，孤儿行是静默的，因此迁移当场拦下来。
func TestMigrateRefusesWhenForeignKeysAreDisabled(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "no-fk.db"))
	if err != nil {
		t.Fatalf("open db failed: %v", err)
	}
	defer db.Close()

	if err := Migrate(db); !errors.Is(err, ErrForeignKeysDisabled) {
		t.Fatalf("migrate without foreign keys: err=%v want ErrForeignKeysDisabled", err)
	}
}
