// 本文件守的是任务与运行的表确实随 Migrate 一起落地，且旧任务表已被整表丢弃。
// 破了意味着引擎启动时表不在，或者那张没有任何读写方的旧表还占着库。

package database

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestMigrateCreatesTaskRunTablesAndDropsLegacyTasks(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "taskrun.db")
	if err := Migrate(dbPath); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}
	db, err := sql.Open("sqlite", sqliteDSN(dbPath))
	if err != nil {
		t.Fatalf("open db failed: %v", err)
	}
	defer db.Close()

	for _, table := range []string{
		"task_identities", "runs", "run_events", "run_samples",
		"run_metrics", "run_limits", "run_args", "run_labels",
	} {
		var name string
		err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
		if err != nil {
			t.Fatalf("table %s missing after migrate: %v", table, err)
		}
	}
	for _, index := range []string{"idx_runs_one_active_per_task", "idx_runs_one_queued_per_task"} {
		var partial string
		err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?`, index).Scan(&partial)
		if err != nil {
			t.Fatalf("index %s missing after migrate: %v", index, err)
		}
	}

	// 旧任务表没有任何读写方了，全新库上也不该再被建出来。
	var legacy int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'tasks'`,
	).Scan(&legacy); err != nil {
		t.Fatalf("read sqlite_master failed: %v", err)
	}
	if legacy != 0 {
		t.Fatalf("legacy tasks table still exists after migrate")
	}
}
