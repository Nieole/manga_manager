// 守读取面与删除面里**只有 SQL 才答得出**的那几条：身份谓词连的是身份表而不是运行行、
// 任务键精确匹配、关键词在落盘侧就滤掉、活动优先的定序，以及删除永不带走仍会变化的运行。

package taskstore

import (
	"context"
	"testing"

	"manga-manager/internal/task"
)

// seedRun 落一条带任务键与状态的运行，用例只关心其中几项时其余取默认值。
func seedRun(t *testing.T, store *Store, taskID int64, key string, status task.RunStatus, sequence int64) task.Run {
	t.Helper()
	run, err := store.CreateRun(context.Background(), task.Run{
		TaskID:   taskID,
		Key:      key,
		Trigger:  task.TriggerManual,
		NthRun:   1,
		Status:   status,
		Sequence: sequence,
	})
	if err != nil {
		t.Fatalf("落运行 %q 失败: %v", key, err)
	}
	return run
}

func listKeys(t *testing.T, store *Store, filter task.RunFilter) []string {
	t.Helper()
	runs, err := store.ListRuns(context.Background(), filter)
	if err != nil {
		t.Fatalf("列运行失败: %v", err)
	}
	keys := make([]string, 0, len(runs))
	for _, run := range runs {
		keys = append(keys, run.Key)
	}
	return keys
}

// TestIdentityPredicatesJoinTheIdentityTable 守类型、作用域与作用域 id 三条谓词判的是**任务行**
// 上的列——它们不在运行行上，判错就等于回到从任务键的字符串里猜作用域。
func TestIdentityPredicatesJoinTheIdentityTable(t *testing.T) {
	store := newStoreForTest(t)
	ctx := context.Background()

	libraryScan := ensureTask(t, store, 1)
	otherLibrary := ensureTask(t, store, 2)
	systemTask, err := store.EnsureTask(ctx, task.Identity{Type: "rebuild_index", Scope: task.ScopeSystem})
	if err != nil {
		t.Fatalf("建系统级身份失败: %v", err)
	}

	seedRun(t, store, libraryScan, "scan_library_1", task.StatusCompleted, 1)
	seedRun(t, store, otherLibrary, "scan_library_2", task.StatusCompleted, 2)
	seedRun(t, store, systemTask.ID, "rebuild_index", task.StatusCompleted, 3)

	scopeID := int64(1)
	cases := []struct {
		name   string
		filter task.RunFilter
		want   []string
	}{
		{"按类型", task.RunFilter{Types: []task.Type{"scan_library"}}, []string{"scan_library_1", "scan_library_2"}},
		{"按作用域", task.RunFilter{Scope: task.ScopeSystem}, []string{"rebuild_index"}},
		{"按作用域 id", task.RunFilter{Scope: task.ScopeLibrary, ScopeID: &scopeID}, []string{"scan_library_1"}},
		{"按任务键精确匹配", task.RunFilter{Key: "scan_library_1"}, []string{"scan_library_1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := listKeys(t, store, tc.filter)
			if len(got) != len(tc.want) {
				t.Fatalf("取回 %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("取回 %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// TestSystemScopeIDIsAddressable 守「只看系统级」这条筛选表达得出来。
//
// 系统级身份的作用域 id **就是** 0，因此谓词用 0 当「不筛」的哨兵是不行的——那正是它是指针的原因。
func TestSystemScopeIDIsAddressable(t *testing.T) {
	store := newStoreForTest(t)

	owner, err := store.EnsureTask(context.Background(), task.Identity{Type: "rebuild_index", Scope: task.ScopeSystem})
	if err != nil {
		t.Fatalf("建系统级身份失败: %v", err)
	}
	seedRun(t, store, owner.ID, "rebuild_index", task.StatusCompleted, 1)
	seedRun(t, store, ensureTask(t, store, 1), "scan_library_1", task.StatusCompleted, 2)

	systemScope := int64(0)
	if got := listKeys(t, store, task.RunFilter{ScopeID: &systemScope}); len(got) != 1 || got[0] != "rebuild_index" {
		t.Fatalf("按系统级作用域 id 取回 %v, want [rebuild_index]", got)
	}
}

// TestQueryPredicateIsPushedDown 守关键词在落盘侧就滤掉：取回内存再滤的话，
// Limit 截断的是过滤**之前**的那一页，用户搜出来的东西会随页数变。
func TestQueryPredicateIsPushedDown(t *testing.T) {
	store := newStoreForTest(t)
	owner := ensureTask(t, store, 1)

	seedRun(t, store, owner, "scan_library_1", task.StatusCompleted, 1)
	failed := seedRun(t, store, owner, "scan_library_1", task.StatusFailed, 2)
	failed.Error = "Disk Full"
	if err := store.SaveRun(context.Background(), failed); err != nil {
		t.Fatalf("写回运行失败: %v", err)
	}

	runs, err := store.ListRuns(context.Background(), task.RunFilter{Query: "disk full", Limit: 1})
	if err != nil {
		t.Fatalf("列运行失败: %v", err)
	}
	if len(runs) != 1 || runs[0].ID != failed.ID {
		t.Fatalf("按关键词取回 %+v, want 只有那条带错误串的运行", runs)
	}
}

// TestLiveFirstOrderPutsChangingRunsUpFront 守任务中心第一页的定序：仍会变化的运行在前。
//
// 序号只在有更新时才递增，一个长时间不上报进度的大库扫描会被后来的大量短任务超过；
// 只按序号排的话，用户正等着看的那一条恰好会掉出第一页——而它是唯一一个还能变的。
func TestLiveFirstOrderPutsChangingRunsUpFront(t *testing.T) {
	store := newStoreForTest(t)

	seedRun(t, store, ensureTask(t, store, 1), "scan_library_1", task.StatusRunning, 1)
	seedRun(t, store, ensureTask(t, store, 2), "scan_library_2", task.StatusCompleted, 20)
	seedRun(t, store, ensureTask(t, store, 3), "scan_library_3", task.StatusCompleted, 30)

	got := listKeys(t, store, task.RunFilter{Order: task.OrderLiveFirst})
	want := []string{"scan_library_1", "scan_library_3", "scan_library_2"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("定序为 %v, want %v —— 活动运行被历史挤下去了", got, want)
		}
	}
}

// TestDeleteRunsNeverTakesLiveRuns 守删除永不带走仍会变化的运行，且这条不经调用方。
//
// 删掉一条还在跑的运行，它后续的每一次上报都会落空，而任务体仍在动磁盘。
func TestDeleteRunsNeverTakesLiveRuns(t *testing.T) {
	store := newStoreForTest(t)
	ctx := context.Background()

	for i, status := range []task.RunStatus{
		task.StatusRunning, task.StatusPaused, task.StatusCancelling, task.StatusQueued,
	} {
		seedRun(t, store, ensureTask(t, store, int64(i+1)), string(status), status, int64(i+1))
	}
	doomed := ensureTask(t, store, 90)
	seedRun(t, store, doomed, "done", task.StatusCompleted, 90)

	removed, err := store.DeleteRuns(ctx, task.RunFilter{})
	if err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if removed != 1 {
		t.Fatalf("删掉了 %d 条, want 1 —— 仍会变化的运行被带走了", removed)
	}
	if got := listKeys(t, store, task.RunFilter{}); len(got) != 4 {
		t.Fatalf("删除之后剩 %v, want 四条仍会变化的运行", got)
	}
}

// TestDeleteRunsCascadesSideTables 守侧表随运行级联删除：不然事件与采样会原地变成孤儿行，
// 一年之后才以「库怎么这么大」的形式暴露。
func TestDeleteRunsCascadesSideTables(t *testing.T) {
	store := newStoreForTest(t)
	ctx := context.Background()

	run := seedRun(t, store, ensureTask(t, store, 1), "scan_library_1", task.StatusCompleted, 1)
	if err := store.MergeRunArgs(ctx, run.ID, map[string]string{"force": "true"}); err != nil {
		t.Fatalf("写入参失败: %v", err)
	}
	if err := store.SetRunMetrics(ctx, run.ID, map[string]int64{"opened_archives": 3}); err != nil {
		t.Fatalf("写指标失败: %v", err)
	}

	if _, err := store.DeleteRuns(ctx, task.RunFilter{Key: "scan_library_1"}); err != nil {
		t.Fatalf("删除失败: %v", err)
	}

	side, err := store.LoadRunSideData(ctx, []int64{run.ID})
	if err != nil {
		t.Fatalf("读回侧数据失败: %v", err)
	}
	if len(side[run.ID].Args) != 0 || len(side[run.ID].Metrics) != 0 {
		t.Fatalf("运行删掉之后侧表还留着孤儿行：%+v", side[run.ID])
	}
}

// TestLoadTasksAndSideDataRoundTrip 守读回面把写进去的东西一样不少地取回来，且按 id 分得清。
func TestLoadTasksAndSideDataRoundTrip(t *testing.T) {
	store := newStoreForTest(t)
	ctx := context.Background()

	first := ensureTask(t, store, 1)
	second := ensureTask(t, store, 2)
	runA := seedRun(t, store, first, "scan_library_1", task.StatusCompleted, 1)
	runB := seedRun(t, store, second, "scan_library_2", task.StatusCompleted, 2)

	if err := store.MergeRunArgs(ctx, runA.ID, map[string]string{"force": "true"}); err != nil {
		t.Fatalf("写入参失败: %v", err)
	}
	if err := store.MergeRunLabels(ctx, runA.ID, map[string]string{"provider_name": "AniList"}); err != nil {
		t.Fatalf("写标签失败: %v", err)
	}
	if err := store.AddRunMetrics(ctx, runA.ID, map[string]int64{"opened_archives": 3}); err != nil {
		t.Fatalf("写指标失败: %v", err)
	}
	if err := store.SaveRunLimits(ctx, runA.ID, task.Limits{ScanProfile: "identity", ScanConcurrency: 2}); err != nil {
		t.Fatalf("写上限失败: %v", err)
	}

	owners, err := store.LoadTasks(ctx, []int64{first, second})
	if err != nil {
		t.Fatalf("读回身份失败: %v", err)
	}
	if owners[first].Type != "scan_library" || owners[first].Scope != task.ScopeLibrary || owners[first].ScopeID != 1 {
		t.Fatalf("读回的身份不对：%+v", owners[first])
	}
	if owners[second].ScopeID != 2 {
		t.Fatalf("两条身份被读串了：%+v", owners)
	}

	side, err := store.LoadRunSideData(ctx, []int64{runA.ID, runB.ID})
	if err != nil {
		t.Fatalf("读回侧数据失败: %v", err)
	}
	got := side[runA.ID]
	if got.Args["force"] != "true" || got.Labels["provider_name"] != "AniList" || got.Metrics["opened_archives"] != 3 {
		t.Fatalf("侧数据读回来缺了东西：%+v", got)
	}
	if got.Limits == nil || got.Limits.ScanProfile != "identity" || got.Limits.ScanConcurrency != 2 {
		t.Fatalf("并发上限读回来不对：%+v", got.Limits)
	}
	if other := side[runB.ID]; other.Limits != nil || len(other.Args) != 0 {
		t.Fatalf("没有侧数据的那条运行被读进了别人的：%+v", other)
	}
}

// TestTaskKeyColumnSurvivesTheRoundTrip 守**过渡期**那两列写得进去也读得回来：
// 六个控制端点与对外契约今天全靠任务键寻址，读不回来就等于每条历史运行都点不动。
func TestTaskKeyColumnSurvivesTheRoundTrip(t *testing.T) {
	store := newStoreForTest(t)

	created, err := store.CreateRun(context.Background(), task.Run{
		TaskID:    ensureTask(t, store, 1),
		Key:       "scan_library_1",
		ScopeName: "Main",
		Trigger:   task.TriggerManual,
		NthRun:    1,
		Status:    task.StatusRunning,
	})
	if err != nil {
		t.Fatalf("落运行失败: %v", err)
	}

	readBack, err := store.LoadRun(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("读回运行失败: %v", err)
	}
	if readBack.Key != "scan_library_1" || readBack.ScopeName != "Main" {
		t.Fatalf("过渡期的两列读回来是 %q / %q", readBack.Key, readBack.ScopeName)
	}
}
