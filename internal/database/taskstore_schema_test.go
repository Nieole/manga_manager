// 本文件守的是任务与运行的新表确实随 Migrate 一起落地，且旧 tasks 表在此期间一列不变。
// 破了意味着新引擎接线时表不在，或者旧引擎在过渡期里读写一张被改过形状的表。

package database

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestMigrateCreatesTaskRunTablesAndKeepsLegacyTasks(t *testing.T) {
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

	// 旧引擎此刻仍在读写这张表，它的主键必须还是任务键。
	var legacy string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'tasks'`).Scan(&legacy); err != nil {
		t.Fatalf("legacy tasks table missing: %v", err)
	}
	var pkColumn string
	if err := db.QueryRow(`SELECT name FROM pragma_table_info('tasks') WHERE pk = 1`).Scan(&pkColumn); err != nil {
		t.Fatalf("read legacy primary key failed: %v", err)
	}
	if pkColumn != "key" {
		t.Fatalf("legacy tasks primary key=%q want \"key\"", pkColumn)
	}
}
