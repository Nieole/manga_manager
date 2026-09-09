// 守这条用户可观察的行为：**重试不再抹掉上一次**。
//
// 旧模型里**任务键**是主键、写入走 upsert，一个键在库里永远只有一行：上周那次扫描花了多久、
// 扫出多少、失败了哪些，重试一次就没了；用户点下重试的那一刻，他正在看的那条**中断**记录消失。
// 新模型里重试是同一个任务的**新一次运行**，`nth_run` 递增，上一次原样留着。

package api

import (
	"context"
	"net/http"
	"testing"

	"manga-manager/internal/task"
)

// runsForKey 取这个**任务键**名下的全部运行，由新到旧。
func runsForKey(t *testing.T, c *Controller, key string) []RunStatus {
	t.Helper()
	items, err := c.taskEngine.listRunStatuses(context.Background(), taskFilters{})
	if err != nil {
		t.Fatalf("列任务失败: %v", err)
	}
	matched := make([]RunStatus, 0, 2)
	for _, item := range items {
		if belongsToKey(t, item, key) {
			matched = append(matched, item)
		}
	}
	return matched
}

// TestRetryKeepsThePreviousRun 钉住重试之后库里有两条运行，上一次那条原样还在。
func TestRetryKeepsThePreviousRun(t *testing.T) {
	c, _ := newHashRebuildRetryRig(t)

	const key = lowPriorityBookHashTaskKey
	seedTask(t, c.taskEngine, taskSeed{
		Key: key, Identity: systemTask("rebuild_book_hashes", variantHashRebuildBackfill), Total: 10,
		Metadata: map[string]string{"reason": "scan_chain"},
		Terminal: "failed", FailError: "disk on fire",
	})

	before := runsForKey(t, c, key)
	if len(before) != 1 {
		t.Fatalf("重试之前该有 1 条运行，实际 %d 条", len(before))
	}

	if rec := retryTaskByKey(t, c, key); rec.Code != http.StatusAccepted {
		t.Fatalf("重试返回 %d, want 202: %s", rec.Code, rec.Body.String())
	}

	after := runsForKey(t, c, key)
	if len(after) != 2 {
		t.Fatalf("重试之后该有 2 条运行，实际 %d 条 —— 新一次运行又把上一次盖掉了", len(after))
	}

	// 上一次那条一字未动：状态、失败原因与它的运行标识都还在。
	var previous *RunStatus
	for i := range after {
		if after[i].RunID == before[0].RunID {
			previous = &after[i]
		}
	}
	if previous == nil {
		t.Fatalf("上一次那条运行（run_id=%d）不见了 —— 用户点下重试的那一刻就失去了正在看的东西", before[0].RunID)
	}
	if previous.Status != "failed" || previous.Error != "disk on fire" {
		t.Fatalf("上一次那条运行被改动了：status=%q error=%q", previous.Status, previous.Error)
	}
}

// TestRetryStartsTheNextRunOfTheSameTask 钉住新起的那条确实是**同一个任务**的下一次运行，
// 而不是另起一个任务：任务清单要能把历次运行归到一行下面，靠的就是这一点。
func TestRetryStartsTheNextRunOfTheSameTask(t *testing.T) {
	c, _ := newHashRebuildRetryRig(t)

	const key = lowPriorityBookHashTaskKey
	seedTask(t, c.taskEngine, taskSeed{
		Key: key, Identity: systemTask("rebuild_book_hashes", variantHashRebuildBackfill), Total: 10,
		Terminal: "failed",
	})
	if rec := retryTaskByKey(t, c, key); rec.Code != http.StatusAccepted {
		t.Fatalf("重试返回 %d, want 202: %s", rec.Code, rec.Body.String())
	}

	runs := runsForKey(t, c, key)
	if len(runs) != 2 {
		t.Fatalf("该有 2 条运行，实际 %d 条", len(runs))
	}
	if runs[0].RunID == runs[1].RunID {
		t.Fatal("两条运行拿到了同一个运行标识")
	}
	// 身份四要素一字不差：同一个任务的两次运行。
	for _, run := range runs {
		if run.Type != "rebuild_book_hashes" || run.Variant != variantHashRebuildBackfill || run.Scope != taskScopeSystem {
			t.Fatalf("运行 %d 的身份为 %q/%q/%q —— 重试另起了一个任务，历次运行归不到一行下面",
				run.RunID, run.Type, run.Scope, run.Variant)
		}
	}
}

// TestRepeatedStartsReuseTheSameIdentity 守**身份懒建**：按四要素查，没有就建一条，
// 重复发起不会建出第二条。建重了的后果是历次运行归到几个不同的任务下面，
// 而「上次成功是什么时候」「连败几次」这些挂在身份上的东西各算各的。
func TestRepeatedStartsReuseTheSameIdentity(t *testing.T) {
	e, _ := newBackgroundTestEngine(t, runTaskBodySynchronously, nil)

	const key = "scan_library_1"
	for range 3 {
		seedTask(t, e, taskSeed{
			Key: key, Identity: libraryTask("scan_library", 1, variantSole), Total: 1, Terminal: "completed",
		})
	}

	runs, err := e.runStore.ListRuns(context.Background(), task.RunFilter{TaskID: taskIDForKey(t, e, key)})
	if err != nil {
		t.Fatalf("列运行失败: %v", err)
	}
	if len(runs) != 3 {
		t.Fatalf("三次发起落了 %d 条运行, want 3", len(runs))
	}
	for _, run := range runs[1:] {
		if run.TaskID != runs[0].TaskID {
			t.Fatalf("同一个身份发起的运行挂到了两个任务上（%d 与 %d）—— 身份被建重了", runs[0].TaskID, run.TaskID)
		}
	}
}
