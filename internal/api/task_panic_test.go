// 守「任务体 panic → 那次**运行**置为失败态」这条兜底经启动入口也成立，以及 panic 之后
// **运行时句柄**与准入闸门都回到干净状态。破了的话，panic 的运行永远停在 running，
// 既占着准入闸门又泄漏一份 ctx 与**暂停闸门**，那件事在进程重启前再也发不起来。
// 兜底本身的裁决（进哪条终态、文案码是什么）由 internal/task 的契约用例守。

package api

import (
	"context"
	"strings"
	"testing"

	"manga-manager/internal/runhandle"
)

// runPanickingTask 经启动入口起一个当场 panic 的任务体。
func runPanickingTask(t *testing.T, e *taskEngine, identity TaskIdentity, key string) {
	t.Helper()
	err := e.Run(identity, RunSpec{Key: key, Total: 100}, func(context.Context, *runhandle.Handle) (TaskResult, error) {
		panic("boom")
	})
	if err != nil {
		t.Fatalf("起 panic 任务失败: %v", err)
	}
}

func TestTaskBodyPanicMarksTaskFailed(t *testing.T) {
	e, snapshots := newBackgroundTestEngine(t, runTaskBodySynchronously, nil)

	const key = "rebuild_thumbnails"
	runPanickingTask(t, e, systemTask("rebuild_thumbnails", variantSole), key)

	task := lastPublishedTask(t, snapshots(), key)
	if task.Status != "failed" {
		t.Fatalf("任务体 panic 后运行停在 %q，应为 failed —— 那个任务会从此恒定返回 409", task.Status)
	}
	if !strings.Contains(task.Error, "boom") {
		t.Fatalf("失败原因里没有带上 panic 值，用户看不到原因：%q", task.Error)
	}
	if task.FinishedAt == nil {
		t.Fatal("失败态没有落 FinishedAt")
	}
	if task.CanCancel || task.CanPause || task.CanResume {
		t.Fatalf("已终结的运行仍带着控制能力：cancel=%v pause=%v resume=%v", task.CanCancel, task.CanPause, task.CanResume)
	}
}

// TestTaskPanicSpeaksInMessageCode 守「消息词汇只有 i18n 码一种」在引擎自己下发的那句文案上
// 也成立（理由见 taskPanicMessageCode）。破了，非英文用户在任务中心看到的是一句谁都翻译不了的话。
func TestTaskPanicSpeaksInMessageCode(t *testing.T) {
	e, snapshots := newBackgroundTestEngine(t, runTaskBodySynchronously, nil)

	const key = "rebuild_thumbnails"
	runPanickingTask(t, e, systemTask("rebuild_thumbnails", variantSole), key)

	task := lastPublishedTask(t, snapshots(), key)
	if task.MessageCode != taskPanicMessageCode {
		t.Fatalf("panic 兜底下发的文案码为 %q, want %q", task.MessageCode, taskPanicMessageCode)
	}
	if task.Message != "" {
		t.Fatalf("引擎给用户下发了一句字面量文案 %q —— 它绕过了整套 i18n，谁都翻译不了", task.Message)
	}
}

// TestTaskPanicReleasesAdmissionAndRuntime 断言 panic 之后准入闸门与**运行时句柄**都回到干净状态：
// 同一个身份可以再次发起，且运行时句柄不泄漏在内存里。
func TestTaskPanicReleasesAdmissionAndRuntime(t *testing.T) {
	e, snapshots := newBackgroundTestEngine(t, runTaskBodySynchronously, nil)

	const key = "rebuild_index"
	runPanickingTask(t, e, systemTask("rebuild_index", variantSole), key)

	failed := lastPublishedTask(t, snapshots(), key)
	if _, live := e.engine.Handle(failed.RunID); live {
		t.Fatal("panic 后运行时句柄仍留在表里 —— 每个 panic 的运行都会泄漏一份 ctx 与暂停闸门")
	}

	if _, err := trySeedTask(t, e, taskSeed{Key: key, Identity: systemTask("rebuild_index", variantSole)}); err != nil {
		t.Fatalf("panic 之后同一个身份再也起不来（%v）—— 用户要重启进程才能重试", err)
	}
}

// TestTaskBodyRunsThroughInjectedBackgroundCapability 守卫「引擎不自己开 goroutine」
// （这条约束的代价见 taskEngine.runBackground 字段注释）。
func TestTaskBodyRunsThroughInjectedBackgroundCapability(t *testing.T) {
	var handedOff int
	// 这个后台能力登记了请求却不执行任务体，模拟「控制器已关闭，拒绝新任务」。
	e, snapshots := newBackgroundTestEngine(t, func(func()) { handedOff++ }, nil)

	const key = "scan_library_1"
	bodyRan := false
	err := e.Run(libraryTask("scan_library", 1, variantSole), RunSpec{Key: key, Total: 10},
		func(context.Context, *runhandle.Handle) (TaskResult, error) {
			bodyRan = true
			return TaskResult{}, nil
		})
	if err != nil {
		t.Fatalf("起任务失败: %v", err)
	}

	if handedOff != 1 {
		t.Fatalf("任务体交给注入的后台能力 %d 次，应为 1 —— 引擎绕开了停机管辖", handedOff)
	}
	if bodyRan {
		t.Fatal("后台能力拒绝执行，任务体却还是跑起来了")
	}
	// 运行行照样落地并投递：准入是同步的，只有任务体是异步的。
	if task := lastPublishedTask(t, snapshots(), key); task.Status != "running" {
		t.Fatalf("任务体没跑，运行却是 %q，应为 running", task.Status)
	}
}
