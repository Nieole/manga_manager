// 守**重启函数**把任务重启回它自己那条跑法，以及重试撞上**活动态**时那件事只会排队、不会被起两遍。
//
// 一个任务类型下可以有多个**变体**（哈希重建有前台与低优先级回填两个），只按类型分发会把回填
// 重启成前台档：原来那条仍停在终态，任务中心里多出一条同名任务，用的是它刻意避开的抢盘跑法。

package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"manga-manager/internal/config"
)

// newHashRebuildRetryRig 拼出重试哈希重建所需的那几样：维护任务的装配 + 重试注册表 + 打开的
// KOReader 二进制哈希档（两个变体的前置条件都看它）。
//
// 注册表在生产由 newControllerCore 填，而这套装配只建任务引擎，因此要在这里补上——
// 任务的 Retryable 在落地那一刻由注册表派生，它必须先于播种就位。
func newHashRebuildRetryRig(t *testing.T) (*Controller, func() []RunStatus) {
	t.Helper()
	store := &maintenanceStore{candidates: seedIdentityCandidates(t, 1)}
	c, snapshots, _ := newMaintenanceRig(t, store)
	cfg := c.currentConfig()
	cfg.KOReader.Enabled = true
	cfg.KOReader.MatchMode = config.KOReaderMatchModeBinaryHash
	c.config = config.NewManager(&cfg)
	c.taskEngine.dispatch = c.buildTaskDispatch()
	return c, snapshots
}

func retryTaskByKey(t *testing.T, c *Controller, key string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	c.retryTask(rec, requestWithRouteParam(http.MethodPost, "/api/system/tasks/"+key+"/retry", nil, "taskKey", key))
	return rec
}

// TestRetryRestartsTheSameVariant 钉住同一个任务类型下的每个**变体**各自重启回自己。
func TestRetryRestartsTheSameVariant(t *testing.T) {
	cases := []struct {
		name       string
		key        string
		variant    TaskVariant
		otherKey   string
		wantCode   string
		wantParams map[string]string
	}{
		{
			name:     "低优先级回填重试回低优先级回填",
			key:      lowPriorityBookHashTaskKey,
			variant:  variantHashRebuildBackfill,
			otherKey: rebuildBookHashesTaskKey,
			wantCode: "task.msg.book_hash_backfill.complete",
			// 档位与匹配模式是这个变体的全部意义：批次压低、匹配模式钉死二进制哈希。
			wantParams: map[string]string{"profile": "full_hash_low_priority", "match_mode": config.KOReaderMatchModeBinaryHash},
		},
		{
			name:     "前台重建重试回前台重建",
			key:      rebuildBookHashesTaskKey,
			variant:  variantHashRebuildForeground,
			otherKey: lowPriorityBookHashTaskKey,
			wantCode: "task.msg.koreader_rebuild_hashes.complete",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, snapshots := newHashRebuildRetryRig(t)

			const seededFailCode = "seed.hash_rebuild.failed"
			seedTask(t, c.taskEngine, taskSeed{
				Key: tc.key, Identity: systemTask("rebuild_book_hashes", tc.variant), Total: 1,
				Metadata: map[string]string{"reason": "scan_library"},
				Terminal: "failed", TerminalCode: seededFailCode,
			})

			rec := retryTaskByKey(t, c, tc.key)
			if rec.Code != http.StatusAccepted {
				t.Fatalf("重试返回 %d, body=%s", rec.Code, rec.Body.String())
			}

			if got := publishedCountFor(snapshots(), tc.otherKey); got != 0 {
				t.Fatalf("重试 %q 起了 %d 条 %q —— 任务中心里多出一条同名任务，原来那条还停在终态",
					tc.key, got, tc.otherKey)
			}
			task := lastPublishedTask(t, snapshots(), tc.key)
			if task.MessageCode == seededFailCode {
				t.Fatalf("任务 %q 原地不动，仍是播种时那条失败快照：%+v", tc.key, task)
			}
			if task.Status != "completed" || task.MessageCode != tc.wantCode {
				t.Fatalf("重启后的 %q 终态为 %q / %q, want completed + %s", tc.key, task.Status, task.MessageCode, tc.wantCode)
			}
			for name, want := range tc.wantParams {
				if task.Params[name] != want {
					t.Fatalf("重启后的 %q 参数 %s=%q, want %q —— 它换成了另一条键的跑法：%v",
						tc.key, name, task.Params[name], want, task.Params)
				}
			}
		})
	}
}

// TestRetryOfAnActiveTaskQueues 钉住重试撞上**活动态**时那件事不会被起两遍——它进**排队中**，
// 等在跑的那条收尾。**取消中**同样占着运行槽位，因此同样只能排队。
//
// 「不会被起两遍」如今由准入那一处保证（撞上活动运行即排队），而不是由端点自己再判一次：
// 判两次的话，「什么叫已经在跑」就有两个答案，而其中一个会把这次重试整个丢掉。
func TestRetryOfAnActiveTaskQueues(t *testing.T) {
	cases := []struct {
		name    string
		status  string
		control func(e *taskEngine, key string) error
	}{
		{"运行中", "running", func(*taskEngine, string) error { return nil }},
		{"已暂停", "paused", func(e *taskEngine, key string) error { return pauseByKey(e, key) }},
		{"取消中", "cancelling", func(e *taskEngine, key string) error { return cancelByKey(e, key) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newHashRebuildRetryRig(t)
			// 后台能力只登记不执行：任务体一旦跑起来就会收尾，活动态无从观察。
			c.taskEngine.runBackground = func(func()) {}

			key := lowPriorityBookHashTaskKey
			seedTask(t, c.taskEngine, taskSeed{Key: key, Identity: systemTask("rebuild_book_hashes", variantHashRebuildBackfill), Total: 1, CanCancel: true, CanPause: true})
			if err := tc.control(c.taskEngine, key); err != nil {
				t.Fatalf("把任务转入 %q 失败: %v", tc.status, err)
			}
			activeID := currentTask(t, c.taskEngine, key).RunID

			rec := retryTaskByKey(t, c, key)
			if rec.Code != http.StatusAccepted {
				t.Fatalf("重试一个 %q 的任务返回 %d, want 202, body=%s", tc.status, rec.Code, rec.Body.String())
			}
			queued := currentTask(t, c.taskEngine, key)
			if queued.Status != "queued" {
				t.Fatalf("重试一个 %q 的任务落成了 %q, want queued", tc.status, queued.Status)
			}
			if queued.RunID == activeID {
				t.Fatalf("重试并进了那条 %q 的运行 —— 合并只发生在排队中的运行上", tc.status)
			}
			// 在跑的那条一动不动：它没有被重新起过，也没有被这次重试改掉状态。
			active, err := c.taskEngine.engine.RunSnapshot(context.Background(), activeID)
			if err != nil {
				t.Fatalf("取回那条 %q 的运行失败: %v", tc.status, err)
			}
			if string(active.Run.Status) != tc.status {
				t.Fatalf("重试之后那条运行的状态变成了 %q, want %q", active.Run.Status, tc.status)
			}
		})
	}
}
