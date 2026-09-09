// 本包的用例一律对真 SQLite 跑，且只问**只有 SQL 才答得出**的问题：约束拒了谁、级联带走了谁、
// DELETE 选中了哪些行、聚合算出什么。状态机那些纯内存规则由 internal/task 守。

package taskstore

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"manga-manager/internal/task"
)

// newDBForTest 在临时目录开一个照生产连接参数来的库，**不迁移**：迁移本身要守的那几条
// 得从一个空库起步。
func newDBForTest(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "taskstore.db")+
		"?_pragma=foreign_keys(1)&_pragma=busy_timeout=15000&_txlock=immediate")
	if err != nil {
		t.Fatalf("open db failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// newStoreForTest 在临时目录建一个开着外键的库并迁移到位。
func newStoreForTest(t *testing.T) *Store {
	t.Helper()

	db := newDBForTest(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}
	return New(db)
}

func libraryIdentity(scopeID int64) task.Identity {
	return task.Identity{Type: "scan_library", Scope: task.ScopeLibrary, ScopeID: scopeID}
}

// ensureTask 建一个库级身份，返回它的 id。
func ensureTask(t *testing.T, store *Store, scopeID int64) int64 {
	t.Helper()

	owner, err := store.EnsureTask(context.Background(), libraryIdentity(scopeID))
	if err != nil {
		t.Fatalf("ensure task failed: %v", err)
	}
	return owner.ID
}

// createRun 落一条运行，用例只关心其中几个字段时其余取默认值。
func createRun(t *testing.T, store *Store, taskID int64, status task.RunStatus, sequence int64) task.Run {
	t.Helper()

	run, err := store.CreateRun(context.Background(), task.Run{
		TaskID:    taskID,
		Trigger:   task.TriggerManual,
		NthRun:    int(sequence),
		Status:    status,
		UpdatedAt: time.Now(),
		Sequence:  sequence,
	})
	if err != nil {
		t.Fatalf("create %s run failed: %v", status, err)
	}
	return run
}

// finishRun 把一条运行写成**终态**并指定它的收尾时刻，供保留裁剪的用例摆出「多久以前跑完的」。
func finishRun(t *testing.T, store *Store, run task.Run, status task.RunStatus, finishedAt time.Time) task.Run {
	t.Helper()

	run.Status = status
	run.FinishedAt = &finishedAt
	run.UpdatedAt = finishedAt
	if err := store.SaveRun(context.Background(), run); err != nil {
		t.Fatalf("finish run failed: %v", err)
	}
	return run
}

func countRows(t *testing.T, store *Store, table string) int {
	t.Helper()

	var count int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil {
		t.Fatalf("count %s failed: %v", table, err)
	}
	return count
}

func runIDs(t *testing.T, store *Store, filter task.RunFilter) []int64 {
	t.Helper()

	runs, err := store.ListRuns(context.Background(), filter)
	if err != nil {
		t.Fatalf("list runs failed: %v", err)
	}
	ids := make([]int64, 0, len(runs))
	for _, run := range runs {
		ids = append(ids, run.ID)
	}
	return ids
}
