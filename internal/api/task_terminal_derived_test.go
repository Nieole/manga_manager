// 守**派生字段**跟得上刚写进任务表的那一帧：percent 由当帧计数算出，ETA 只属于**活动态**，
// 速率的分母里不含任务没在干活的那些时段。破了的表现是终态任务显示上一帧的陈值（`2 / 2` 配
// `50.0%`）、一个已经停了的任务还挂着「预计剩余时间」、**中断**任务的速率被整段停机时长稀释，
// 以及**暂停**过的任务被那段等人回来的时间稀释。

package api

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"manga-manager/internal/runhandle"
	"manga-manager/internal/task"
)

// TestTerminalTaskDerivedFieldsFollowFinalCount 钉住经引擎收尾的三种终态：percent 与终帧计数
// 一律对得上，且一律不带 ETA。
func TestTerminalTaskDerivedFieldsFollowFinalCount(t *testing.T) {
	cases := []struct {
		name        string
		total       int
		reported    int
		bodyErr     error
		wantCurrent int
		wantPercent float64
	}{
		{"完成时百分比跟着补齐后的计数走", 2, 1, nil, 2, 100},
		{"已取消时百分比是真正处理到的比例", 1000, 30, context.Canceled, 30, 3},
		{"失败时百分比是真正处理到的比例", 1000, 30, errors.New("disk on fire"), 30, 3},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, snapshots := newBackgroundTestEngine(t, runTaskBodySynchronously, nil)

			const key = "refresh_koreader_matching"
			handle := seedTask(t, e, taskSeed{Key: key, Identity: systemTask("refresh_koreader_matching", variantSole), Total: tc.total, CanCancel: true})
			current := tc.reported
			handle.Report(runhandle.Frame{Current: &current})
			settleSeededTask(t, e, key, tc.bodyErr)

			task := lastPublishedTask(t, snapshots(), key)
			if task.Current != tc.wantCurrent {
				t.Fatalf("终态 %q 的计数为 %d, want %d", task.Status, task.Current, tc.wantCurrent)
			}
			if task.Percent == nil {
				t.Fatalf("终态 %q 一个百分比都没带", task.Status)
			}
			if *task.Percent != tc.wantPercent {
				t.Fatalf("终态 %q 显示 %d / %d 配 %.1f%%, want %.1f%% —— 百分比是上一帧的陈值，与计数自相矛盾",
					task.Status, task.Current, task.Total, *task.Percent, tc.wantPercent)
			}
			if task.EtaSeconds != nil {
				t.Fatalf("终态 %q 还挂着 %d 秒的 ETA —— 任务已经停了，剩余时间没有意义", task.Status, *task.EtaSeconds)
			}
		})
	}
}

// TestInterruptedTaskHasNoEta 钉住第四种终态：**中断**由服务重启时的批量转写产生，
// 任务中心读回它时同样不该算出 ETA——它是可重试的，一个「预计剩余时间」会让用户以为它还在跑。
func TestInterruptedTaskHasNoEta(t *testing.T) {
	task := interruptRecoveredTask(t, func(handle *runhandle.Handle) {
		current := 30
		total := 1000
		handle.Report(runhandle.Frame{Current: &current, Total: &total})
	})

	if task.Percent == nil || *task.Percent != 3 {
		t.Fatalf("中断运行的百分比为 %v, want 3", task.Percent)
	}
	if task.EtaSeconds != nil {
		t.Fatalf("中断运行还挂着 %d 秒的 ETA —— 它已经停了，用户要看的是能不能重试", *task.EtaSeconds)
	}
}

// TestActiveTaskKeepsPercentAndEta 守住终态那道收敛没有误伤**活动态**：运行中、已暂停、取消中
// 三种都还要有百分比与 ETA——用户正是靠它们判断还要等多久。
func TestActiveTaskKeepsPercentAndEta(t *testing.T) {
	cases := []struct {
		name    string
		status  string
		control func(e *taskEngine, key string) error
	}{
		{"运行中", "running", func(*taskEngine, string) error { return nil }},
		{"已暂停", "paused", func(e *taskEngine, key string) error { return e.pause(key) }},
		{"取消中", "cancelling", func(e *taskEngine, key string) error { return e.cancel(key) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 后台能力只登记不执行：任务体一旦跑起来就会收尾，活动态无从观察。
			e, snapshots := newBackgroundTestEngine(t, func(func()) {}, nil)

			const key = "scan_library_1"
			handle := seedTask(t, e, taskSeed{Key: key, Identity: libraryTask("scan_library", 1, variantSole), Total: 1000, CanCancel: true, CanPause: true})
			backdateTaskStart(t, e, key, time.Minute)
			current := 30
			handle.Report(runhandle.Frame{Current: &current})
			if err := tc.control(e, key); err != nil {
				t.Fatalf("把任务转入 %q 失败: %v", tc.status, err)
			}

			task := lastPublishedTask(t, snapshots(), key)
			if task.Status != tc.status {
				t.Fatalf("任务状态为 %q, want %q", task.Status, tc.status)
			}
			if task.Percent == nil || *task.Percent != 3 {
				t.Fatalf("活动态 %q 的百分比为 %v, want 3", task.Status, task.Percent)
			}
			if task.EtaSeconds == nil {
				t.Fatalf("活动态 %q 没带 ETA —— 用户看不到还要等多久", task.Status)
			}
		})
	}
}

// TestInterruptedRunReportsRateFromLastHeartbeat 守**中断**运行的分母是「最后一次上报减开始
// 时刻」，不是「重启时刻减开始时刻」——停机的那一段里任务一条都没处理。
//
// 它是唯一一种收尾时刻不由引擎当场盖上的状态，因此单独守一条：破了的表现是一个跑了 10 分钟的
// 扫描被算成跑了 8 小时 10 分钟，速率稀释到四十九分之一，而停得越久这个数越离谱。
func TestInterruptedRunReportsRateFromLastHeartbeat(t *testing.T) {
	const (
		ranFor      = 10 * time.Minute
		downFor     = 8 * time.Hour
		wantRate    = 60.0
		rateEpsilon = 1.0
	)

	controller, store, _, tempDir := newTestController(t)

	const key = "scan_library_1"
	handle := seedTask(t, controller.taskEngine, taskSeed{
		Key: key, Identity: libraryTask("scan_library", 1, variantSole), Total: 10000,
	})
	// 任务体跑了 10 分钟、处理了 600 条就随进程一起没了：真实速率 60/min。
	current := 600
	handle.Report(runhandle.Frame{Current: &current})
	backdateSeededRun(t, controller.taskEngine, key, ranFor, downFor)

	tasks, body := restartAndListInterrupted(t, controller, store, tempDir)
	if diff := tasks[0].RatePerMinute - wantRate; diff > rateEpsilon || diff < -rateEpsilon {
		t.Fatalf("停机 %v 之后中断运行的速率为 %.2f/min, want 约 %.0f/min —— 分母把整段停机时长算成了在干活",
			downFor, tasks[0].RatePerMinute, wantRate)
	}
	if !strings.Contains(body, "rate_per_minute") {
		t.Fatalf("载荷里没有 rate_per_minute，中断记录仍然是残废的: %s", body)
	}
	// 同一帧里其它派生字段不受牵连：「做完了多少」仍要答得出。
	if tasks[0].Current != 600 || tasks[0].Percent == nil || *tasks[0].Percent != 6 {
		t.Fatalf("中断运行的计数 / 百分比为 %d / %v, want 600 / 6", tasks[0].Current, tasks[0].Percent)
	}
}

// TestNeverReportedInterruptedRunOmitsRate 守边界：速率的缺席只由数据决定，不由状态决定。
// 一帧进度都没报过的**中断**运行分子分母都是零，仍然不发——那是真的没有数据。
//
// 分母的两端一并钉住：收尾时刻必须停在最后一次上报处，而不是滑到重启时刻。眼下分子为零，
// 滑不滑都算不出速率，但一条运行的耗时不得随它停机多久而变长，这条路上也不例外。
func TestNeverReportedInterruptedRunOmitsRate(t *testing.T) {
	const downFor = 8 * time.Hour

	controller, store, _, tempDir := newTestController(t)

	const key = "scan_library_1"
	seedTask(t, controller.taskEngine, taskSeed{
		Key: key, Identity: libraryTask("scan_library", 1, variantSole), Total: 10000,
	})
	// 一帧都没报过就停机一夜：最后一次上报的时刻与开始时刻仍然重合。
	backdateSeededRun(t, controller.taskEngine, key, 0, downFor)

	tasks, body := restartAndListInterrupted(t, controller, store, tempDir)
	if tasks[0].RatePerMinute != 0 {
		t.Fatalf("从未上报过进度的中断运行下发了 %.2f/min —— 它一条都没处理过，速率无从谈起", tasks[0].RatePerMinute)
	}
	if strings.Contains(body, "rate_per_minute") {
		t.Fatalf("载荷里还留着 rate_per_minute: %s", body)
	}
	if tasks[0].FinishedAt == nil || !tasks[0].FinishedAt.Equal(tasks[0].StartedAt) {
		t.Fatalf("结束时刻为 %v, want 与开始时刻 %v 相等 —— 一帧都没报过，收尾时刻就该停在那里",
			tasks[0].FinishedAt, tasks[0].StartedAt)
	}
}

// backdateSeededRun 把一条已播种的运行整体挪进过去：开始时刻落在 ran+down 之前、最后心跳落在
// down 之前，于是它「跑了 ran 就随进程一起没了，机器又停了 down 才被重新拉起来」。
//
// 两个时刻必须一起挪。只挪开始时刻的话心跳还停在此刻，停机那一段根本不存在，用例于是分不出
// 「结束时刻取心跳」与「结束时刻取重启时刻」这两种写法——而那正是它要守的区别。
func backdateSeededRun(t testing.TB, e *taskEngine, key string, ran, down time.Duration) {
	t.Helper()
	now := time.Now()
	mutateSeededRun(t, e, key, func(run *task.Run) {
		run.StartedAt = now.Add(-down - ran)
		run.UpdatedAt = now.Add(-down)
	})
}

// TestTaskRateSurvivesActiveAndSettledStatuses 守分母在引擎当场收尾的六种状态下都还原得出来：
// 三种活动态量到此刻，三种终态量到任务体返回那一刻盖上的 finished_at，而其中的**暂停**时长
// 引擎逐段记下过、扣掉即可（见 TestPausedTimeStaysOutOfTheRateDenominator）。
//
// 第七种状态**中断**的 finished_at 由重启时的批量转写盖上，见
// TestInterruptedRunReportsRateFromLastHeartbeat。
func TestTaskRateSurvivesActiveAndSettledStatuses(t *testing.T) {
	activeCases := []struct {
		name    string
		status  string
		control func(e *taskEngine, key string) error
	}{
		{"运行中", "running", func(*taskEngine, string) error { return nil }},
		{"已暂停", "paused", func(e *taskEngine, key string) error { return e.pause(key) }},
		{"取消中", "cancelling", func(e *taskEngine, key string) error { return e.cancel(key) }},
	}
	for _, tc := range activeCases {
		t.Run(tc.name, func(t *testing.T) {
			// 后台能力只登记不执行：任务体一旦跑起来就会收尾，活动态无从观察。
			e, snapshots := newBackgroundTestEngine(t, func(func()) {}, nil)

			const key = "scan_library_1"
			handle := seedTask(t, e, taskSeed{Key: key, Identity: libraryTask("scan_library", 1, variantSole), Total: 1000, CanCancel: true, CanPause: true})
			backdateTaskStart(t, e, key, time.Minute)
			current := 30
			handle.Report(runhandle.Frame{Current: &current})
			if err := tc.control(e, key); err != nil {
				t.Fatalf("把任务转入 %q 失败: %v", tc.status, err)
			}

			task := lastPublishedTask(t, snapshots(), key)
			if task.Status != tc.status {
				t.Fatalf("任务状态为 %q, want %q", task.Status, tc.status)
			}
			if task.RatePerMinute <= 0 {
				t.Fatalf("活动态 %q 没带速率 —— 用户看不到它跑得快不快", task.Status)
			}
		})
	}

	terminalCases := []struct {
		name    string
		status  string
		bodyErr error
	}{
		{"完成", "completed", nil},
		{"已取消", "cancelled", context.Canceled},
		{"失败", "failed", errors.New("disk on fire")},
	}
	for _, tc := range terminalCases {
		t.Run(tc.name, func(t *testing.T) {
			e, snapshots := newBackgroundTestEngine(t, runTaskBodySynchronously, nil)

			const key = "refresh_koreader_matching"
			handle := seedTask(t, e, taskSeed{Key: key, Identity: systemTask("refresh_koreader_matching", variantSole), Total: 1000, CanCancel: true})
			backdateTaskStart(t, e, key, time.Minute)
			current := 30
			handle.Report(runhandle.Frame{Current: &current})
			settleSeededTask(t, e, key, tc.bodyErr)

			task := lastPublishedTask(t, snapshots(), key)
			if task.Status != tc.status {
				t.Fatalf("任务状态为 %q, want %q", task.Status, tc.status)
			}
			if task.RatePerMinute <= 0 {
				t.Fatalf("终态 %q 没带速率 —— 它的起止时刻都是引擎当场盖的，分母还原得出来", task.Status)
			}
		})
	}
}

// backdateTaskStart 把已播种任务的开始时刻往前挪，给派生字段一个确定的分母。
//
// 不挪的话，分母是「播种到上报」之间的真实间隔：macOS/Linux 上是微秒级正数，而 Windows 的
// 时钟粒度约 15.6ms，同一个时间片内 time.Since 返回 0，enrichTaskProgress 的 elapsed <= 0
// 守卫会把速率与 ETA 一并掐掉——用例于是在 Windows 上红、在别处绿。真实任务从启动到上报进度
// 远不止一个时间片，这道守卫本身是对的，该确定下来的是用例的分母。
func backdateTaskStart(t testing.TB, e *taskEngine, key string, d time.Duration) {
	t.Helper()
	mutateSeededRun(t, e, key, func(run *task.Run) { run.StartedAt = run.StartedAt.Add(-d) })
}

// mutateSeededRun 绕到落盘那一侧改一条运行的时刻列。
//
// 引擎每次动一条运行都先从库里读回它，因此改库就等于改了引擎下一步看到的东西——这条路不引入
// 第二份真相，只是把墙上时钟往前拨。注入时钟做不到：这几条用例要的是「开始时刻在十分钟前」，
// 而时钟一拨，落盘时刻与断言时刻会一起动。
func mutateSeededRun(t testing.TB, e *taskEngine, key string, mutate func(*task.Run)) {
	t.Helper()
	status := currentTask(t, e, key)
	run, err := e.runStore.LoadRun(context.Background(), status.RunID)
	if err != nil {
		t.Fatalf("读回运行 %d 失败: %v", status.RunID, err)
	}
	mutate(&run)
	if err := e.runStore.SaveRun(context.Background(), run); err != nil {
		t.Fatalf("写回运行 %d 失败: %v", run.ID, err)
	}
}

// backdateTaskPause 模拟「这次暂停已经持续了 d」：把暂停开始时刻往前挪，同时把任务开始时刻挪
// 同样的距离——暂停的这段时间里墙上时钟一样在走，两处只挪一处就等于凭空多出或少掉一段时长。
func backdateTaskPause(t *testing.T, e *taskEngine, key string, d time.Duration) {
	t.Helper()
	mutateSeededRun(t, e, key, func(run *task.Run) {
		if run.PausedAt == nil {
			t.Fatalf("任务 %q 不在暂停中，挪不动暂停开始时刻", key)
			return
		}
		pausedAt := run.PausedAt.Add(-d)
		run.PausedAt = &pausedAt
		run.StartedAt = run.StartedAt.Add(-d)
	})
}

// TestPausedTimeStaysOutOfTheRateDenominator 钉住**暂停**时长不进速率与 ETA 的分母。
//
// 用户暂停一小时去吃饭，任务一条都没多处理、也一条都没少处理，回来时的速率却掉到十四分之一、
// ETA 从两个多小时变成十八小时——这个数会让用户以为这台机器慢得离谱，进而去动一堆并发设置。
// 与**中断**那条的区别在于分母有得换：暂停的起止时刻引擎自己写下过，累加起来扣掉即可。
func TestPausedTimeStaysOutOfTheRateDenominator(t *testing.T) {
	// 任务真干了 10 分钟处理 600 条：真实速率 60/min，剩下 9400 条的真实 ETA 是 9400 秒。
	const (
		workedFor   = 10 * time.Minute
		pausedFor   = time.Hour
		wantRate    = 60.0
		wantEta     = int64(9400)
		rateEpsilon = 1.0
		etaEpsilon  = int64(120)
	)

	cases := []struct {
		name       string
		wantStatus string
		wantEta    bool
		// after 在暂停被挪长之后执行，决定任务停在哪个状态上收场。
		after func(t *testing.T, e *taskEngine, key string)
	}{
		{
			name:       "恢复之后照常在跑",
			wantStatus: "running",
			wantEta:    true,
			after: func(t *testing.T, e *taskEngine, key string) {
				if err := e.resume(key); err != nil {
					t.Fatalf("恢复失败: %v", err)
				}
			},
		},
		{
			name:       "恢复之后被取消",
			wantStatus: "cancelled",
			after: func(t *testing.T, e *taskEngine, key string) {
				if err := e.resume(key); err != nil {
					t.Fatalf("恢复失败: %v", err)
				}
				settleSeededTask(t, e, key, context.Canceled)
			},
		},
		{
			name:       "暂停中直接取消",
			wantStatus: "cancelled",
			after: func(t *testing.T, e *taskEngine, key string) {
				if err := e.cancel(key); err != nil {
					t.Fatalf("取消失败: %v", err)
				}
				settleSeededTask(t, e, key, context.Canceled)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 后台能力只登记不执行：任务体何时收尾由用例自己决定。
			e, snapshots := newBackgroundTestEngine(t, func(func()) {}, nil)

			const key = "scan_library_1"
			handle := seedTask(t, e, taskSeed{Key: key, Identity: libraryTask("scan_library", 1, variantSole), Total: 10000, CanCancel: true, CanPause: true})
			backdateTaskStart(t, e, key, workedFor)
			current := 600
			handle.Report(runhandle.Frame{Current: &current})
			if err := e.pause(key); err != nil {
				t.Fatalf("暂停失败: %v", err)
			}
			backdateTaskPause(t, e, key, pausedFor)
			tc.after(t, e, key)

			task := lastPublishedTask(t, snapshots(), key)
			if task.Status != tc.wantStatus {
				t.Fatalf("任务状态为 %q, want %q", task.Status, tc.wantStatus)
			}
			if diff := task.RatePerMinute - wantRate; diff > rateEpsilon || diff < -rateEpsilon {
				t.Fatalf("暂停 %v 之后速率为 %.2f/min, want %.0f/min —— 那一小时一条都没处理，却被算成了在干活",
					pausedFor, task.RatePerMinute, wantRate)
			}
			if !tc.wantEta {
				return
			}
			if task.EtaSeconds == nil {
				t.Fatal("活动态没带 ETA —— 用户看不到还要等多久")
			}
			if diff := *task.EtaSeconds - wantEta; diff > etaEpsilon || diff < -etaEpsilon {
				t.Fatalf("暂停 %v 之后 ETA 为 %d 秒, want 约 %d 秒", pausedFor, *task.EtaSeconds, wantEta)
			}
		})
	}
}
