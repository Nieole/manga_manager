// 本文件守的是任务与运行的表确实随 Migrate 一起落地，且 tasks 这个名字归了**身份**表。
// 破了意味着引擎启动时表不在，或者那张没有任何读写方的旧表还占着这个名字。

package database

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestMigrateCreatesTaskRunTablesAndDropsLegacyTasks(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "taskstore.db")
	if err := Migrate(dbPath); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}
	db, err := sql.Open("sqlite", sqliteDSN(dbPath))
	if err != nil {
		t.Fatalf("open db failed: %v", err)
	}
	defer db.Close()

	for _, table := range []string{
		"tasks", "runs", "run_events", "run_samples",
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

	// 全新库上那张 tasks 必须是**身份**表：旧任务表与它同名，只查名字答不出是哪一张。
	assertIdentityTableShape(t, db)
}
