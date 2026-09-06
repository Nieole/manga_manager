// 这些用例守的是健康报告那个「查看日志」按钮指向的是**最近**那次运行，而且不会指错作用域。
// 破了意味着用户点开的是上上次扫描的日志，或者另一个资料库的。

package taskstore

import (
	"context"
	"testing"
	"time"

	"manga-manager/internal/task"
)

// keyedRun 落一条已收尾、带任务键的运行。
func keyedRun(t *testing.T, store *Store, taskID int64, key string, sequence int64) {
	t.Helper()

	finishedAt := time.Now()
	if _, err := store.CreateRun(context.Background(), task.Run{
		TaskID: taskID, Key: key, Trigger: task.TriggerManual, NthRun: int(sequence),
		Status: task.StatusCompleted, StartedAt: finishedAt.Add(-time.Minute),
		UpdatedAt: finishedAt, FinishedAt: &finishedAt, Sequence: sequence,
	}); err != nil {
		t.Fatalf("落一条运行失败: %v", err)
	}
}

func TestLastRunKeysForScopesTakesTheMostRecentRunPerScope(t *testing.T) {
	ctx := context.Background()
	store := newStoreForTest(t)

	first := ensureTask(t, store, 1)
	second := ensureTask(t, store, 2)
	keyedRun(t, store, first, "scan_library_1", 1)
	keyedRun(t, store, first, "scan_library_1_retry", 3)
	keyedRun(t, store, second, "scan_library_2", 2)

	latest, err := store.LastRunKeysForScopes(ctx, []ScopeRef{
		{Scope: "library", ScopeID: 1},
		{Scope: "library", ScopeID: 2},
		{Scope: "library", ScopeID: 99},
	})
	if err != nil {
		t.Fatalf("取最近运行失败: %v", err)
	}
	if got := latest[ScopeRef{Scope: "library", ScopeID: 1}]; got != "scan_library_1_retry" {
		t.Errorf("库 1 取回 %q，想要序号最大的那次 scan_library_1_retry", got)
	}
	if got := latest[ScopeRef{Scope: "library", ScopeID: 2}]; got != "scan_library_2" {
		t.Errorf("库 2 取回 %q：另一个库的运行不该串过来", got)
	}
	if _, ok := latest[ScopeRef{Scope: "library", ScopeID: 99}]; ok {
		t.Error("没跑过任何任务的库不该出现在结果里")
	}
}

// 任务键是**过渡期**列，运行可以不带；取回一个空串只会让界面上多一个点不动的按钮。
func TestLastRunKeysForScopesSkipsRunsWithoutKey(t *testing.T) {
	ctx := context.Background()
	store := newStoreForTest(t)

	taskID := ensureTask(t, store, 1)
	keyedRun(t, store, taskID, "scan_library_1", 1)
	keyedRun(t, store, taskID, "", 2)

	latest, err := store.LastRunKeysForScopes(ctx, []ScopeRef{{Scope: "library", ScopeID: 1}})
	if err != nil {
		t.Fatalf("取最近运行失败: %v", err)
	}
	if got := latest[ScopeRef{Scope: "library", ScopeID: 1}]; got != "scan_library_1" {
		t.Errorf("取回 %q，想要那条带键的 scan_library_1", got)
	}
}
