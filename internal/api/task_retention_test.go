// 守**分层保留**在接线这一侧：清理是一条看得见的运行、按设置里那三个阈值裁、
// 清不掉仍会变化的运行，也清不掉它自己正在跑的那一条。
//
// 破了只有两种结局，且都不响：历史无限长，一年之后这个单机 SQLite 把自己压垮；
// 或者删掉了还活着的运行，它此后的每一次上报都落空，而任务体仍在动磁盘。

package api

import (
	"testing"

	"manga-manager/internal/config"
	"manga-manager/internal/task"
)

// newCleanupRig 造一台任务体同步执行的 Controller：清理跑完了，启动调用才返回，
// 因此断言不必 sleep 或轮询。落盘是**真的 taskstore**，裁剪的 DELETE 打在真 SQLite 上。
func newCleanupRig(t *testing.T) *Controller {
	t.Helper()
	controller, _, _, _ := newTestController(t)
	controller.taskEngine.runBackground = runTaskBodySynchronously
	return controller
}

// setRetention 改这三个阈值，与经设置页保存走同一条路（换掉配置快照）。
func setRetention(t testing.TB, c *Controller, runsPerTask, terminalDays, sampleDays int) {
	t.Helper()
	cfg := c.currentConfig()
	cfg.Tasks.RetainRunsPerTask = runsPerTask
	cfg.Tasks.RetainTerminalRunDays = terminalDays
	cfg.Tasks.RetainSampleDays = sampleDays
	c.config.Replace(&cfg)
}

// 配置那份默认与领域那份兜底必须相等：错开一次，默认部署删数据的口径就与默认引擎不是一回事。
// config 位于 task 的依赖下游，引用不了那边的常数，只能在这里对一次。
func TestConfigDefaultRetentionMatchesTheEngineDefault(t *testing.T) {
	want := task.DefaultRetention()
	if config.DefaultRetainRunsPerTask != want.RunsPerTask {
		t.Fatalf("配置默认条数 %d 与领域默认 %d 不一致", config.DefaultRetainRunsPerTask, want.RunsPerTask)
	}
	if got := retentionAge(config.DefaultRetainTerminalRunDays); got != want.TerminalAge {
		t.Fatalf("配置默认终态时长 %v 与领域默认 %v 不一致", got, want.TerminalAge)
	}
	if got := retentionAge(config.DefaultRetainSampleDays); got != want.SampleAge {
		t.Fatalf("配置默认采样时长 %v 与领域默认 %v 不一致", got, want.SampleAge)
	}
}

// 三个阈值从**运行时配置**读，改完设置对下一次清理生效，不必重启进程。
//
// 两头的越界值都必须落在「不裁剪」这一侧：天数乘成 time.Duration 会溢出，而绕回来的那个数
// 可能是一个很小的**正**时长——照它办事，下一次清理会把 24 小时前的终态运行全部删掉。
// 归一化本该把这些值挡在配置那一层，这里守的是它漏过来时的方向。
func TestRunRetentionComesFromTheRuntimeConfig(t *testing.T) {
	c := newCleanupRig(t)

	if got := retentionPolicyOf(c.currentConfig()); got != task.DefaultRetention() {
		t.Fatalf("默认配置下的保留策略为 %+v, want %+v", got, task.DefaultRetention())
	}

	setRetention(t, c, 5, 30, 2)
	want := task.RetentionPolicy{RunsPerTask: 5, TerminalAge: retentionAge(30), SampleAge: retentionAge(2)}
	if got := retentionPolicyOf(c.currentConfig()); got != want {
		t.Fatalf("改配置之后的保留策略为 %+v, want %+v", got, want)
	}

	cases := []struct {
		name           string
		runs, terminal int
		samples        int
	}{
		{name: "负数", runs: -1, terminal: -1, samples: -1},
		{name: "负得足够多因而会绕回来", runs: -1 << 40, terminal: -1 << 40, samples: -1 << 40},
	}
	for _, tc := range cases {
		setRetention(t, c, tc.runs, tc.terminal, tc.samples)
		if got := retentionPolicyOf(c.currentConfig()); got != (task.RetentionPolicy{}) {
			t.Fatalf("%s的阈值算出了 %+v, want 零值（这一层不裁剪）—— 正的时长会当场删光历史", tc.name, got)
		}
	}

	setRetention(t, c, 1, 1<<40, 1<<40)
	got := retentionPolicyOf(c.currentConfig())
	if got.TerminalAge != retentionAge(maxRetentionDays) || got.SampleAge != retentionAge(maxRetentionDays) {
		t.Fatalf("大到溢出的天数没有被收在上限内：%+v", got)
	}
}

// 清理是一条可见的运行：**发起方串联**、系统作用域、在任务中心里能看到它清了多少。
func TestRunHistoryCleanupIsAVisibleRun(t *testing.T) {
	c := newCleanupRig(t)

	if err := c.launchCleanupRunHistoryTask(); err != nil {
		t.Fatalf("发起清理失败: %v", err)
	}

	run := currentTask(t, c.taskEngine, cleanupRunHistoryTaskKey)
	if run.Trigger != string(task.TriggerChained) {
		t.Fatalf("清理运行的发起方为 %q, want chained", run.Trigger)
	}
	if run.Scope != taskScopeSystem || run.Status != "completed" {
		t.Fatalf("清理运行的作用域 %q、状态 %q, want system / completed", run.Scope, run.Status)
	}
	if run.MessageCode != "task.msg.cleanup_run_history.complete" {
		t.Fatalf("清理运行的终态文案码为 %q", run.MessageCode)
	}
	// 清了多少：文案占位参数给用户读，指标给聚合查询。
	for _, key := range []string{"runs", "events", "samples"} {
		if _, ok := run.MessageParams[key]; !ok {
			t.Fatalf("终态文案缺少 %q，「清了多少」无处可读：%v", key, run.MessageParams)
		}
	}
	for _, key := range []string{"pruned_runs", "pruned_events", "pruned_samples"} {
		if _, ok := run.Metrics[key]; !ok {
			t.Fatalf("清理运行没报出指标 %q：%v", key, run.Metrics)
		}
	}
	// 这一次按什么口径清的，事后仍答得出——阈值改过之后再回头看这条运行，看到的是当时那一份。
	if run.Labels["runs_per_task"] != "20" || run.Labels["terminal_run_days"] != "90" || run.Labels["sample_days"] != "7" {
		t.Fatalf("清理运行没记下当时生效的三个阈值：%v", run.Labels)
	}
}

// 改了阈值，**下一次清理**就按新的裁——不必重启进程。
func TestChangedThresholdsApplyToTheNextCleanup(t *testing.T) {
	c := newCleanupRig(t)
	for range 3 {
		seedTask(t, c.taskEngine, taskSeed{
			Key: "scan_library_1", Identity: libraryTask("scan_library", 1, variantSole), Terminal: "completed",
		})
	}

	if err := c.launchCleanupRunHistoryTask(); err != nil {
		t.Fatalf("发起清理失败: %v", err)
	}
	if got := currentTask(t, c.taskEngine, cleanupRunHistoryTaskKey).Metrics["pruned_runs"]; got != 0 {
		t.Fatalf("默认阈值下清掉了 %d 条运行, want 0 —— 三条历史远在配额之内", got)
	}

	setRetention(t, c, 1, 90, 7)
	if err := c.launchCleanupRunHistoryTask(); err != nil {
		t.Fatalf("改完阈值再发起清理失败: %v", err)
	}
	if got := currentTask(t, c.taskEngine, cleanupRunHistoryTaskKey).Metrics["pruned_runs"]; got != 2 {
		t.Fatalf("改成留 1 条之后清掉了 %d 条运行, want 2", got)
	}
	if got := len(runsForKey(t, c, "scan_library_1")); got != 1 {
		t.Fatalf("扫描任务还剩 %d 条运行, want 1", got)
	}
}

// **活动态与排队中的运行永不被清理带走**（用户故事 34）：阈值再紧也一样。
// 删掉一条还在跑的运行，它后续的每一次上报都会落空，而任务体仍在动磁盘。
func TestRunHistoryCleanupNeverTakesLiveRuns(t *testing.T) {
	c := newCleanupRig(t)
	// 一条都不留、一天都不留：只有「还活着」这条判据能让它们活下来。
	setRetention(t, c, 1, 1, 1)

	const key = "scan_library_1"
	seedTask(t, c.taskEngine, scanSeed(1))
	// 同一身份再发一次：它落在**排队中**——既不是活动态，也不是终态。
	if _, err := trySeedTask(t, c.taskEngine, scanSeed(1)); err != errSeededRunQueued {
		t.Fatalf("同一身份的第二次发起返回 %v, want 停在排队中", err)
	}

	if err := c.launchCleanupRunHistoryTask(); err != nil {
		t.Fatalf("发起清理失败: %v", err)
	}

	statuses := map[string]bool{}
	for _, run := range runsForKey(t, c, key) {
		statuses[run.Status] = true
	}
	if !statuses["running"] || !statuses["queued"] {
		t.Fatalf("清理带走了仍会变化的运行，剩下的状态是 %v —— 活动态与排队中都不该被清", statuses)
	}
}

// 清理不清掉自己正在跑的那一条，但它自己产生的历史照样受同一套保留策略约束——不开特例。
func TestRunHistoryCleanupPrunesItsOwnHistoryButNotItself(t *testing.T) {
	c := newCleanupRig(t)
	setRetention(t, c, 1, 90, 7)

	for range 3 {
		if err := c.launchCleanupRunHistoryTask(); err != nil {
			t.Fatalf("发起清理失败: %v", err)
		}
	}

	runs := runsForKey(t, c, cleanupRunHistoryTaskKey)
	// 第三次跑的时候库里是两条终态加它自己：配额裁掉最老那条终态，它自己活着因此留下。
	if len(runs) != 2 {
		t.Fatalf("清理任务下剩了 %d 条运行, want 2（配额内的一条终态 + 它自己）", len(runs))
	}
	latest := currentTask(t, c.taskEngine, cleanupRunHistoryTaskKey)
	if latest.Status != "completed" {
		t.Fatalf("最后一条清理运行的状态为 %q, want completed —— 它把自己清掉了", latest.Status)
	}
	if got := latest.Metrics["pruned_runs"]; got != 1 {
		t.Fatalf("最后一次清理清掉了 %d 条运行, want 1（它自己那条更早的历史）", got)
	}
}
