// 守**派生字段**跟得上刚写进任务表的那一帧：percent 由当帧计数算出，ETA 只属于**活动态**。
// 破了的表现是终态任务显示上一帧的陈值（`2 / 2` 配 `50.0%`），以及一个已经停了的任务
// 还挂着「预计剩余时间」——对可重试的**中断**任务尤其误导。

package api

import (
	"context"
	"errors"
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
