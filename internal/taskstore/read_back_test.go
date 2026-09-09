// 守读取面与删除面里**只有 SQL 才答得出**的那几条：身份谓词连的是身份表而不是运行行、
// 任务键精确匹配、关键词在落盘侧就滤掉、活动优先的定序，以及删除永不带走仍会变化的运行。

package taskstore

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"manga-manager/internal/task"
)

// seedRun 落一条带作用域显示名与状态的运行，用例只关心其中几项时其余取默认值。
// 用例拿显示名当这条运行的标签：契约与库上都不再有**任务键**（ADR 0007），而这一列认得出是哪一条。
func seedRun(t *testing.T, store *Store, taskID int64, scopeName string, status task.RunStatus, sequence int64) task.Run {
	t.Helper()
	run, err := store.CreateRun(context.Background(), task.Run{
		TaskID:    taskID,
		ScopeName: scopeName,
		Trigger:   task.TriggerManual,
		NthRun:    1,
		Status:    status,
		Sequence:  sequence,
	})
	if err != nil {
		t.Fatalf("落运行 %q 失败: %v", scopeName, err)
	}
	return run
}

func listScopeNames(t *testing.T, store *Store, filter task.RunFilter) []string {
	t.Helper()
	runs, err := store.ListRuns(context.Background(), filter)
	if err != nil {
		t.Fatalf("列运行失败: %v", err)
	}
	names := make([]string, 0, len(runs))
	for _, run := range runs {
		names = append(names, run.ScopeName)
	}
	return names
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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := listScopeNames(t, store, tc.filter)
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
	if got := listScopeNames(t, store, task.RunFilter{ScopeID: &systemScope}); len(got) != 1 || got[0] != "rebuild_index" {
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

// TestQueryMatchesScopeName 守运行列表的关键词匹配的是作用域**显示名**、文案码与错误这三格。
// 用户在搜索框里打的是界面上看得见的库名与系列名，而任务键是界面上从不出现的内部串。
func TestQueryMatchesScopeName(t *testing.T) {
	store := newStoreForTest(t)
	corpus := seedScopeNameCorpus(t, store)

	cases := []struct {
		name  string
		query string
		want  []int64
	}{
		{name: "打库名筛出该库的运行", query: "manga vault", want: []int64{corpus.library.ID}},
		{name: "打系列名筛出该系列的运行", query: "one piece", want: []int64{corpus.series.ID}},
		{name: "打类型名仍然命中，那来自文案码", query: "scan_library", want: []int64{corpus.library.ID}},
		{name: "任务键那种内部串筛不出东西", query: "scan_library_1", want: nil},
		{name: "显示名为空的运行照样按文案码命中", query: "rebuild_index", want: []int64{corpus.system.ID}},
		{name: "显示名为空不把它卷进别人的搜索", query: "piece", want: []int64{corpus.series.ID}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := runIDs(t, store, task.RunFilter{Query: tc.query})
			if !slices.Equal(got, tc.want) {
				t.Fatalf("按 %q 筛出 %v, want %v", tc.query, got, tc.want)
			}
		})
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

	got := listScopeNames(t, store, task.RunFilter{Order: task.OrderLiveFirst})
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
	if got := listScopeNames(t, store, task.RunFilter{}); len(got) != 4 {
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

	if _, err := store.DeleteRuns(ctx, task.RunFilter{TaskID: run.TaskID}); err != nil {
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

// TestScopeNameSurvivesTheRoundTrip 守**作用域显示名**那一列写得进去也读得回来：
// 任务清单与运行列表的关键词搜索判在它身上，读不回来就等于打库名一条也搜不到。
func TestScopeNameSurvivesTheRoundTrip(t *testing.T) {
	store := newStoreForTest(t)

	created, err := store.CreateRun(context.Background(), task.Run{
		TaskID:    ensureTask(t, store, 1),
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
	if readBack.ScopeName != "Main" {
		t.Fatalf("作用域显示名读回来是 %q", readBack.ScopeName)
	}
}

// TestTaskAttributesSurviveTheRoundTrip 守**退避**与禁用那四列写得进去也读得回来。
//
// 只有 SQL 答得出：两个可空的时刻列存的是 epoch 毫秒，而「没有值」与「1970 年」在那一列里
// 长得一样。读回来落成零值时刻的话，一个从没成功过的任务会显示成「1970 年成功过一次」，
// 而一条到期时刻被读丢的退避会当场失效——那块坏盘原封不动地每小时再转一遍。
func TestTaskAttributesSurviveTheRoundTrip(t *testing.T) {
	store := newStoreForTest(t)
	ctx := context.Background()

	fresh := ensureTask(t, store, 1)
	written := ensureTask(t, store, 2)
	if owner := loadTask(t, store, fresh); owner.Disabled || owner.FailStreak != 0 ||
		owner.LastSuccessAt != nil || owner.BackoffUntil != nil {
		t.Fatalf("新建的身份带着值出生了：%+v", owner.TaskAttributes)
	}

	// 毫秒之下的精度存不下，用例因此按毫秒对齐——断言的是「这一列没丢」，不是「纳秒也保住了」。
	succeeded := time.UnixMilli(1_700_000_000_123).UTC()
	until := succeeded.Add(2 * time.Hour)
	attrs := task.TaskAttributes{Disabled: true, LastSuccessAt: &succeeded, FailStreak: 3, BackoffUntil: &until}
	if err := store.SaveTaskAttributes(ctx, written, attrs); err != nil {
		t.Fatalf("写长期属性失败: %v", err)
	}

	owner := loadTask(t, store, written)
	if !owner.Disabled || owner.FailStreak != 3 {
		t.Fatalf("读回的开关与连败是 %v / %d, want true / 3", owner.Disabled, owner.FailStreak)
	}
	if owner.LastSuccessAt == nil || !owner.LastSuccessAt.Equal(succeeded) {
		t.Fatalf("读回的上次成功时刻是 %v, want %v", owner.LastSuccessAt, succeeded)
	}
	if owner.BackoffUntil == nil || !owner.BackoffUntil.Equal(until) {
		t.Fatalf("读回的退避到期时刻是 %v, want %v", owner.BackoffUntil, until)
	}
	// 另一条身份一个字段都没被带上：属性是按 id 写的，写串了两个库会共用一份退避。
	if other := loadTask(t, store, fresh); other.Disabled || other.FailStreak != 0 {
		t.Fatalf("写属性时波及了别的身份：%+v", other.TaskAttributes)
	}

	// 清回去也要读得回来：复位写的正是这一步，两个可空列都得重新变成「没有值」。
	if err := store.SaveTaskAttributes(ctx, written, task.TaskAttributes{}); err != nil {
		t.Fatalf("复位长期属性失败: %v", err)
	}
	if reset := loadTask(t, store, written); reset.Disabled || reset.FailStreak != 0 ||
		reset.LastSuccessAt != nil || reset.BackoffUntil != nil {
		t.Fatalf("复位之后读回的仍是 %+v", reset.TaskAttributes)
	}
}

// TestSaveTaskAttributesReportsMissingTask 守按 id 写一个不存在的身份是**错误**而不是无操作：
// 静默吞掉的话，禁用一个已删的库只会得到一个什么也没发生的 202。
func TestSaveTaskAttributesReportsMissingTask(t *testing.T) {
	store := newStoreForTest(t)
	if err := store.SaveTaskAttributes(context.Background(), 4242, task.TaskAttributes{Disabled: true}); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("写不存在的身份返回 %v, want ErrTaskNotFound", err)
	}
}

// loadTask 按 id 读回一条身份；读不到即 t.Fatal。
func loadTask(t *testing.T, store *Store, taskID int64) task.Task {
	t.Helper()
	owners, err := store.LoadTasks(context.Background(), []int64{taskID})
	if err != nil {
		t.Fatalf("读回身份失败: %v", err)
	}
	owner, ok := owners[taskID]
	if !ok {
		t.Fatalf("身份 %d 读不回来", taskID)
	}
	return owner
}
