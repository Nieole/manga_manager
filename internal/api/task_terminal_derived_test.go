// 守**派生字段**跟得上刚写进任务表的那一帧：percent 由当帧计数算出，ETA 只属于**活动态**，
// 速率只算给分母可信的那些状态。破了的表现是终态任务显示上一帧的陈值（`2 / 2` 配 `50.0%`）、
// 一个已经停了的任务还挂着「预计剩余时间」，以及**中断**任务的速率被整段停机时长稀释。

package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/taskrun"
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
			e, snapshots := newBackgroundTestEngine(runTaskBodySynchronously, nil)

			const key = "refresh_koreader_matching"
			handle := seedTask(t, e, taskSeed{Key: key, Type: "refresh_koreader_matching", Total: tc.total, CanCancel: true})
			current := tc.reported
			handle.Report(taskrun.Frame{Current: &current})
			settleSeededTask(e, key, tc.bodyErr)

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

// TestInterruptedTaskHasNoEta 钉住第四种终态：**中断**由服务重启时的落盘记录转入，任务中心
// 从库里读回它时同样不该算出 ETA——它是可重试的，一个「预计剩余时间」会让用户以为它还在跑。
func TestInterruptedTaskHasNoEta(t *testing.T) {
	startedAt := time.Now().Add(-10 * time.Minute)
	finishedAt := startedAt.Add(5 * time.Minute)

	task := taskStatusFromRecord(database.TaskRecord{
		Key:        "scan_library_1",
		Type:       "scan_library",
		Scope:      "library",
		Status:     "interrupted",
		Current:    30,
		Total:      1000,
		Retryable:  true,
		StartedAt:  startedAt,
		UpdatedAt:  finishedAt,
		FinishedAt: &finishedAt,
	})

	if task.Percent == nil || *task.Percent != 3 {
		t.Fatalf("中断任务的百分比为 %v, want 3", task.Percent)
	}
	if task.EtaSeconds != nil {
		t.Fatalf("中断任务还挂着 %d 秒的 ETA —— 它已经停了，用户要看的是能不能重试", *task.EtaSeconds)
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
			e, snapshots := newBackgroundTestEngine(func(func()) {}, nil)

			const key = "scan_library_1"
			handle := seedTask(t, e, taskSeed{Key: key, Type: "scan_library", Total: 1000, CanCancel: true, CanPause: true})
			backdateTaskStart(e, key, time.Minute)
			current := 30
			handle.Report(taskrun.Frame{Current: &current})
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

// TestInterruptedTaskOmitsRate 钉住**中断**任务一个处理速率都不发。
//
// 它走真库与真 SQL：MarkInterruptedTasks 在服务下次启动时才把 finished_at 盖成重启时刻，
// 于是「跑了 10 分钟、停机 9 小时 50 分」的任务被按 10 小时算分母，速率掉到实际的六十分之一。
func TestInterruptedTaskOmitsRate(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	ctx := context.Background()

	startedAt := time.Now().Add(-10 * time.Hour)
	// 任务体只跑了 10 分钟就随进程一起没了：600 条 / 10 分钟，真实速率 60/min。
	lastFrameAt := startedAt.Add(10 * time.Minute)
	if err := store.UpsertTask(ctx, database.TaskRecord{
		Key:       "scan_library_1",
		Type:      "scan_library",
		Scope:     "library",
		Status:    "running",
		Current:   600,
		Total:     10000,
		Retryable: true,
		StartedAt: startedAt,
		UpdatedAt: lastFrameAt,
		Sequence:  1,
	}); err != nil {
		t.Fatalf("落一条运行中的任务失败: %v", err)
	}

	controller.recoverInterruptedTasks()

	req := httptest.NewRequest(http.MethodGet, "/api/system/tasks", nil)
	rec := httptest.NewRecorder()
	controller.listTasks(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("列任务返回 %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	var tasks []TaskStatus
	if err := json.Unmarshal([]byte(body), &tasks); err != nil {
		t.Fatalf("解析任务列表失败: %v", err)
	}
	if len(tasks) != 1 || tasks[0].Status != "interrupted" {
		t.Fatalf("读回 %+v, want 一条 interrupted 任务", tasks)
	}
	if tasks[0].RatePerMinute != 0 {
		t.Fatalf("中断任务下发了 %.2f/min，真实速率是 60/min —— 分母从开始时刻一路量到重启时刻，"+
			"整段停机时长都被算成了在干活", tasks[0].RatePerMinute)
	}
	if strings.Contains(body, "rate_per_minute") {
		t.Fatalf("载荷里还留着 rate_per_minute —— 那一刻没人知道任务停在哪一秒，这个数只能是编的: %s", body)
	}
	// 同一帧里其它派生字段不受牵连：「做完了多少」仍要答得出。
	if tasks[0].Current != 600 || tasks[0].Percent == nil || *tasks[0].Percent != 6 {
		t.Fatalf("中断任务的计数 / 百分比为 %d / %v, want 600 / 6", tasks[0].Current, tasks[0].Percent)
	}
}

// TestTaskRateSurvivesEveryStatusButInterrupted 守住那道收敛只掐掉**中断**一种：另外六种状态的
// 分母都是真的——活动态量到此刻，其余三种终态由引擎在任务体返回的那一刻盖上 finished_at。
func TestTaskRateSurvivesEveryStatusButInterrupted(t *testing.T) {
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
			e, snapshots := newBackgroundTestEngine(func(func()) {}, nil)

			const key = "scan_library_1"
			handle := seedTask(t, e, taskSeed{Key: key, Type: "scan_library", Total: 1000, CanCancel: true, CanPause: true})
			backdateTaskStart(e, key, time.Minute)
			current := 30
			handle.Report(taskrun.Frame{Current: &current})
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
			e, snapshots := newBackgroundTestEngine(runTaskBodySynchronously, nil)

			const key = "refresh_koreader_matching"
			handle := seedTask(t, e, taskSeed{Key: key, Type: "refresh_koreader_matching", Total: 1000, CanCancel: true})
			backdateTaskStart(e, key, time.Minute)
			current := 30
			handle.Report(taskrun.Frame{Current: &current})
			settleSeededTask(e, key, tc.bodyErr)

			task := lastPublishedTask(t, snapshots(), key)
			if task.Status != tc.status {
				t.Fatalf("任务状态为 %q, want %q", task.Status, tc.status)
			}
			if task.RatePerMinute <= 0 {
				t.Fatalf("终态 %q 没带速率 —— 它的 finished_at 是引擎当场盖的，分母是真的", task.Status)
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
func backdateTaskStart(e *taskEngine, key string, d time.Duration) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	task, ok := e.tasks[key]
	if !ok {
		return
	}
	task.StartedAt = task.StartedAt.Add(-d)
	e.tasks[key] = task
}
