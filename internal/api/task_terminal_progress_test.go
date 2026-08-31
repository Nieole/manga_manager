// 守四种**终态**各自的**计数推进**：只有**完成**把计数补到总数，其余三种一律停在任务真正
// 处理到的条目数。破了的表现是取消/失败/中断的任务在任务中心显示满格进度，那个数就答不出
// 「做完了多少」，用户据此以为活已经干完。
//
// **完成**那条补齐不是冗余：`rebuild_index`、`cleanup_library`、`ai_grouping` 这类任务把
// 总数声明成阶段数却从不推进计数，靠的就是它才显示成 1 / 1。

package api

import (
	"context"
	"errors"
	"testing"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/taskrun"
)

// TestTerminalStateAdvanceCount 钉住经引擎收尾的三种终态：完成补齐，已取消与失败保留实数。
func TestTerminalStateAdvanceCount(t *testing.T) {
	const noAdvance = -1

	cases := []struct {
		name        string
		total       int
		reported    int
		bodyErr     error
		wantCurrent int
	}{
		{"完成把计数补到总数", 1000, 30, nil, 1000},
		{"完成时任务体一次都没推进计数也补到总数", 1, noAdvance, nil, 1},
		{"已取消停在真正处理到的条目数", 1000, 30, context.Canceled, 30},
		{"失败停在真正处理到的条目数", 1000, 30, errors.New("disk on fire"), 30},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, snapshots := newBackgroundTestEngine(runTaskBodySynchronously, nil)

			const key = "scan_library_1"
			handle := seedTask(t, e, taskSeed{Key: key, Type: "scan_library", Total: tc.total, CanCancel: true})
			if tc.reported != noAdvance {
				current := tc.reported
				handle.Report(taskrun.Frame{Current: &current})
			}
			settleSeededTask(e, key, tc.bodyErr)

			task := lastPublishedTask(t, snapshots(), key)
			if task.Current != tc.wantCurrent || task.Total != tc.total {
				t.Fatalf("终态 %q 显示 %d / %d, want %d / %d —— 这个数不是真正处理到的条目数",
					task.Status, task.Current, task.Total, tc.wantCurrent, tc.total)
			}
		})
	}
}

// TestInterruptedTaskKeepsAdvanceCount 钉住第四种终态：服务重启把活动态任务转成**中断**时
// 只改状态，计数留在重启前那一刻——中断的任务可重试，把它显示成满格会让用户以为没什么可重试的。
func TestInterruptedTaskKeepsAdvanceCount(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	ctx := context.Background()
	now := time.Now().Add(-time.Minute)

	if err := store.UpsertTask(ctx, database.TaskRecord{
		Key:       "scan_library_1",
		Type:      "scan_library",
		Scope:     "library",
		Status:    "running",
		Current:   30,
		Total:     1000,
		Retryable: true,
		StartedAt: now,
		UpdatedAt: now,
		Sequence:  1,
	}); err != nil {
		t.Fatalf("落一条运行中的任务失败: %v", err)
	}

	controller.recoverInterruptedTasks()

	records, err := store.ListTasks(ctx, database.TaskFilters{})
	if err != nil {
		t.Fatalf("读回任务失败: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("读回 %d 条任务, want 1", len(records))
	}
	if records[0].Status != "interrupted" {
		t.Fatalf("任务状态为 %q, want interrupted", records[0].Status)
	}
	if records[0].Current != 30 {
		t.Fatalf("中断任务的计数为 %d, want 30 —— 转入中断时不该动计数", records[0].Current)
	}
}
