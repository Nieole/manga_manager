// 这些用例守任务清单那一句 SQL：谁上榜、按什么序、「上次结果」判在哪一条运行上。
// 破了的表现是任务中心下半层默默按错的那次运行给出「上次跑成什么样」，而界面上看不出区别。

package taskstore

import (
	"context"
	"slices"
	"testing"
	"time"

	"manga-manager/internal/task"
)

// taskIDsOf 取一批任务的 id，供断言定序与命中集合。
func taskIDsOf(owners []task.Task) []int64 {
	ids := make([]int64, 0, len(owners))
	for _, owner := range owners {
		ids = append(ids, owner.ID)
	}
	return ids
}

func listTasks(t *testing.T, store *Store, filter task.TaskFilter) []task.Task {
	t.Helper()

	owners, err := store.ListTasks(context.Background(), filter)
	if err != nil {
		t.Fatalf("list tasks failed: %v", err)
	}
	return owners
}

// TestListTasksOrdersByLatestRunAndKeepsNeverRunTasks 守两件事：定序取的是**末次运行**的序号
// （而不是任务自己的 id），以及一次都没跑过的任务仍在清单上、排在最后。
// 后者破了的表现是刚建出来的库在任务清单里根本不出现。
func TestListTasksOrdersByLatestRunAndKeepsNeverRunTasks(t *testing.T) {
	store := newStoreForTest(t)
	older := ensureTask(t, store, 1)
	newer := ensureTask(t, store, 2)
	silent := ensureTask(t, store, 3)

	// 先建的那个任务反而有更新的运行：只按任务 id 排会把两者的次序判反。
	createRun(t, store, newer, task.StatusCompleted, 10)
	createRun(t, store, older, task.StatusCompleted, 20)

	got := taskIDsOf(listTasks(t, store, task.TaskFilter{}))
	want := []int64{older, newer, silent}
	if len(got) != len(want) {
		t.Fatalf("清单条数为 %d, want %d (got=%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("清单定序为 %v, want %v", got, want)
		}
	}
}

// TestListTasksJudgesStatusOnTheLatestRun 守「上次结果」这条谓词判的是最近那一次，
// 而不是「有没有哪一次是这个状态」——重试成功之后那个库不该继续躺在失败清单里。
func TestListTasksJudgesStatusOnTheLatestRun(t *testing.T) {
	store := newStoreForTest(t)
	recovered := ensureTask(t, store, 1)
	broken := ensureTask(t, store, 2)

	createRun(t, store, recovered, task.StatusFailed, 1)
	createRun(t, store, recovered, task.StatusCompleted, 2)
	createRun(t, store, broken, task.StatusCompleted, 3)
	createRun(t, store, broken, task.StatusFailed, 4)

	got := taskIDsOf(listTasks(t, store, task.TaskFilter{LastRunStatuses: []task.RunStatus{task.StatusFailed}}))
	if len(got) != 1 || got[0] != broken {
		t.Fatalf("上次失败的任务为 %v, want [%d]", got, broken)
	}
}

// TestListTasksExcludesNeverRunTasksFromLastRunPredicates 守「没有上次」不满足任何一条末次谓词：
// 左连之下那几列是 NULL，判成命中的话空任务会挤进「上次失败」这类清单里。
func TestListTasksExcludesNeverRunTasksFromLastRunPredicates(t *testing.T) {
	store := newStoreForTest(t)
	silent := ensureTask(t, store, 1)

	if got := taskIDsOf(listTasks(t, store, task.TaskFilter{LastRunStatuses: []task.RunStatus{task.StatusFailed}})); len(got) != 0 {
		t.Fatalf("一次都没跑过的任务命中了状态谓词: %v", got)
	}
	if got := taskIDsOf(listTasks(t, store, task.TaskFilter{LastRunQuery: "scan"})); len(got) != 0 {
		t.Fatalf("一次都没跑过的任务命中了关键词谓词: %v", got)
	}
	// 不筛末次时它照样在清单上。
	if got := taskIDsOf(listTasks(t, store, task.TaskFilter{})); len(got) != 1 || got[0] != silent {
		t.Fatalf("不筛末次时清单为 %v, want [%d]", got, silent)
	}
}

// TestListTasksMatchesIdentityAndKeyword 守身份三项判在任务行上、关键词判在末次运行上。
func TestListTasksMatchesIdentityAndKeyword(t *testing.T) {
	store := newStoreForTest(t)
	first := ensureTask(t, store, 1)
	second := ensureTask(t, store, 2)

	run := createRun(t, store, first, task.StatusCompleted, 1)
	run.ScopeName = "Manga Vault"
	if err := store.SaveRun(context.Background(), run); err != nil {
		t.Fatalf("save run failed: %v", err)
	}
	createRun(t, store, second, task.StatusCompleted, 2)

	scopeID := int64(1)
	if got := taskIDsOf(listTasks(t, store, task.TaskFilter{Scope: task.ScopeLibrary, ScopeID: &scopeID})); len(got) != 1 || got[0] != first {
		t.Fatalf("按作用域 id 筛出 %v, want [%d]", got, first)
	}
	if got := taskIDsOf(listTasks(t, store, task.TaskFilter{Types: []task.Type{"rebuild_index"}})); len(got) != 0 {
		t.Fatalf("按不存在的类型筛出 %v, want 空", got)
	}
	if got := taskIDsOf(listTasks(t, store, task.TaskFilter{LastRunQuery: "MANGA VAULT"})); len(got) != 1 || got[0] != first {
		t.Fatalf("按关键词筛出 %v, want [%d]（大小写无关）", got, first)
	}
}

// TestListTasksMatchesLastRunScopeName 守任务清单这一句的关键词与运行列表**同口径**：判的是末次
// 运行上的显示名、文案码与错误。两处分岔的话，同一个词在任务中心上下两层筛出来的行对不上。
func TestListTasksMatchesLastRunScopeName(t *testing.T) {
	store := newStoreForTest(t)
	corpus := seedScopeNameCorpus(t, store)

	cases := []struct {
		name  string
		query string
		want  []int64
	}{
		{name: "打库名筛出该库那一行", query: "MANGA VAULT", want: []int64{corpus.library.TaskID}},
		{name: "打系列名筛出该系列那一行", query: "one piece", want: []int64{corpus.series.TaskID}},
		{name: "打类型名仍然命中，那来自文案码", query: "scan_series", want: []int64{corpus.series.TaskID}},
		{name: "任务键那种内部串筛不出东西", query: "scan_library_1", want: nil},
		{name: "显示名为空的任务照样按文案码命中", query: "rebuild_index", want: []int64{corpus.system.TaskID}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := taskIDsOf(listTasks(t, store, task.TaskFilter{LastRunQuery: tc.query}))
			if !slices.Equal(got, tc.want) {
				t.Fatalf("按 %q 筛出 %v, want %v", tc.query, got, tc.want)
			}
		})
	}
}

// TestLatestRunsPicksTheHighestSequencePerTask 守批量取回的确实是每个任务**最近**那一次，
// 而不是随便一条：拿错一条的表现是清单上写着一个早已被重试盖过去的结果。
func TestLatestRunsPicksTheHighestSequencePerTask(t *testing.T) {
	store := newStoreForTest(t)
	first := ensureTask(t, store, 1)
	second := ensureTask(t, store, 2)
	silent := ensureTask(t, store, 3)

	createRun(t, store, first, task.StatusFailed, 1)
	wantFirst := createRun(t, store, first, task.StatusCompleted, 5)
	wantSecond := createRun(t, store, second, task.StatusRunning, 3)

	latest, err := store.LatestRuns(context.Background(), []int64{first, second, silent})
	if err != nil {
		t.Fatalf("latest runs failed: %v", err)
	}
	if got := latest[first].ID; got != wantFirst.ID {
		t.Fatalf("任务 %d 的末次运行为 %d, want %d", first, got, wantFirst.ID)
	}
	if got := latest[second].ID; got != wantSecond.ID {
		t.Fatalf("任务 %d 的末次运行为 %d, want %d", second, got, wantSecond.ID)
	}
	if _, ok := latest[silent]; ok {
		t.Fatalf("一次都没跑过的任务 %d 出现在末次运行里", silent)
	}
}

// TestLatestRunsReadsBackTheWholeRow 守末次运行读回来的是整行而不是几列：
// 清单那一行要显示计数、文案与收尾时刻，少一列的表现是界面上那格空着而不是报错。
func TestLatestRunsReadsBackTheWholeRow(t *testing.T) {
	store := newStoreForTest(t)
	taskID := ensureTask(t, store, 1)

	finishedAt := time.Now().Truncate(time.Millisecond)
	written := task.Run{
		TaskID: taskID, Key: "scan_library_1", ScopeName: "主库", Trigger: task.TriggerScheduled,
		NthRun: 4, Status: task.StatusCompleted, Phase: "writing_database", Current: 7, Total: 7,
		MessageCode: "task.msg.scan.done", MessageParams: map[string]string{"count": "7"},
		StartedAt: finishedAt.Add(-time.Minute), UpdatedAt: finishedAt, FinishedAt: &finishedAt, Sequence: 9,
	}
	if _, err := store.CreateRun(context.Background(), written); err != nil {
		t.Fatalf("create run failed: %v", err)
	}

	latest, err := store.LatestRuns(context.Background(), []int64{taskID})
	if err != nil {
		t.Fatalf("latest runs failed: %v", err)
	}
	got := latest[taskID]
	if got.Key != written.Key || got.ScopeName != written.ScopeName || got.Trigger != written.Trigger {
		t.Fatalf("末次运行读回 key=%q scope_name=%q trigger=%q", got.Key, got.ScopeName, got.Trigger)
	}
	if got.NthRun != written.NthRun || got.Current != written.Current || got.Total != written.Total {
		t.Fatalf("末次运行读回 nth=%d current=%d total=%d", got.NthRun, got.Current, got.Total)
	}
	if got.MessageCode != written.MessageCode || got.MessageParams["count"] != "7" {
		t.Fatalf("末次运行读回文案 %q %v", got.MessageCode, got.MessageParams)
	}
	if got.FinishedAt == nil || !got.FinishedAt.Equal(finishedAt) {
		t.Fatalf("末次运行读回收尾时刻 %v, want %v", got.FinishedAt, finishedAt)
	}
}
