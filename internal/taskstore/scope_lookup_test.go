// 这些用例守的是健康报告那个「查看日志」按钮指向的是**最近**那次运行，而且不会指错作用域。
// 破了意味着用户点开的是上上次扫描的日志，或者另一个资料库的。

package taskstore

import (
	"context"
	"testing"
	"time"

	"manga-manager/internal/task"
)

// finishedRun 落一条已收尾的运行，交回它的标识。
func finishedRun(t *testing.T, store *Store, taskID int64, sequence int64) int64 {
	t.Helper()

	finishedAt := time.Now()
	created, err := store.CreateRun(context.Background(), task.Run{
		TaskID: taskID, Trigger: task.TriggerManual, NthRun: int(sequence),
		Status: task.StatusCompleted, StartedAt: finishedAt.Add(-time.Minute),
		UpdatedAt: finishedAt, FinishedAt: &finishedAt, Sequence: sequence,
	})
	if err != nil {
		t.Fatalf("落一条运行失败: %v", err)
	}
	return created.ID
}

func TestLastRunIDsForScopesTakesTheMostRecentRunPerScope(t *testing.T) {
	ctx := context.Background()
	store := newStoreForTest(t)

	first := ensureTask(t, store, 1)
	second := ensureTask(t, store, 2)
	finishedRun(t, store, first, 1)
	newest := finishedRun(t, store, first, 3)
	secondRun := finishedRun(t, store, second, 2)

	latest, err := store.LastRunIDsForScopes(ctx, []ScopeRef{
		{Scope: task.ScopeLibrary, ScopeID: 1},
		{Scope: task.ScopeLibrary, ScopeID: 2},
		{Scope: task.ScopeLibrary, ScopeID: 99},
	})
	if err != nil {
		t.Fatalf("取最近运行失败: %v", err)
	}
	if got := latest[ScopeRef{Scope: task.ScopeLibrary, ScopeID: 1}]; got != newest {
		t.Errorf("库 1 取回运行 %d，想要序号最大的那次 %d", got, newest)
	}
	if got := latest[ScopeRef{Scope: task.ScopeLibrary, ScopeID: 2}]; got != secondRun {
		t.Errorf("库 2 取回运行 %d：另一个库的运行不该串过来", got)
	}
	if _, ok := latest[ScopeRef{Scope: task.ScopeLibrary, ScopeID: 99}]; ok {
		t.Error("没跑过任何任务的库不该出现在结果里")
	}
}

// 运行标识与**任务键**无关：不带键的运行照样跳得过去，而按键跳转的那条路正是这一票撤掉的。
func TestLastRunIDsForScopesIgnoresTheTaskKey(t *testing.T) {
	ctx := context.Background()
	store := newStoreForTest(t)

	taskID := ensureTask(t, store, 1)
	finishedRun(t, store, taskID, 1)
	keyless := finishedRun(t, store, taskID, 2)

	latest, err := store.LastRunIDsForScopes(ctx, []ScopeRef{{Scope: task.ScopeLibrary, ScopeID: 1}})
	if err != nil {
		t.Fatalf("取最近运行失败: %v", err)
	}
	if got := latest[ScopeRef{Scope: task.ScopeLibrary, ScopeID: 1}]; got != keyless {
		t.Errorf("取回运行 %d，想要最近那次 %d", got, keyless)
	}
}
