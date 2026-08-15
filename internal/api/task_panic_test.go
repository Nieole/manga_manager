// 守「任务体 panic → 该任务置为失败态」这条兜底，以及 panic 之后任务键与运行时句柄都回到干净状态。
//
// 破了是用户可观察的：panic 的任务体走不到任何收尾，任务永远停在 running；而活动任务既不被
// pruneTasksLocked 淘汰、也不被 clearTasks 删除，那个**任务键**从此恒定返回 409「已在运行」，
// 同类任务在进程重启前再也发不起来，同时每次 panic 泄漏一份 ctx 与暂停闸门。

package api

import (
	"strings"
	"testing"
)

func TestTaskBodyPanicMarksTaskFailed(t *testing.T) {
	e, snapshots := newBackgroundTestEngine(runTaskBodySynchronously)

	const key = "rebuild_thumbnails"
	seedTask(t, e, taskSeed{Key: key, Type: "rebuild_thumbnails", Total: 100})

	e.runTaskGoroutine(key, func() { panic("boom") })

	task := lastPublishedTask(t, snapshots(), key)
	if task.Status != "failed" {
		t.Fatalf("任务体 panic 后任务停在 %q，应为 failed —— 该任务键会从此恒定返回 409", task.Status)
	}
	if !strings.Contains(task.Error, "boom") {
		t.Fatalf("失败原因里没有带上 panic 值，用户看不到原因：%q", task.Error)
	}
	if task.FinishedAt == nil {
		t.Fatal("失败态没有落 FinishedAt")
	}
	if task.CanCancel || task.CanPause || task.CanResume {
		t.Fatalf("已终结的任务仍带着控制能力：cancel=%v pause=%v resume=%v", task.CanCancel, task.CanPause, task.CanResume)
	}
}

// TestTaskPanicSpeaksInMessageCode 守「消息词汇只有 i18n 码一种」在引擎自己下发的那句文案上
// 也成立（理由见 taskPanicMessageCode）。破了，非英文用户在任务中心看到的是一句谁都翻译不了的话。
func TestTaskPanicSpeaksInMessageCode(t *testing.T) {
	e, snapshots := newBackgroundTestEngine(runTaskBodySynchronously)

	const key = "rebuild_thumbnails"
	seedTask(t, e, taskSeed{Key: key, Type: "rebuild_thumbnails"})

	e.runTaskGoroutine(key, func() { panic("boom") })

	task := lastPublishedTask(t, snapshots(), key)
	if task.MessageCode != taskPanicMessageCode {
		t.Fatalf("panic 兜底下发的文案码为 %q, want %q", task.MessageCode, taskPanicMessageCode)
	}
	if task.Message != "" {
		t.Fatalf("引擎给用户下发了一句字面量文案 %q —— 它绕过了整套 i18n，谁都翻译不了", task.Message)
	}
}

// TestTaskPanicReleasesKeyAndRuntime 断言 panic 之后任务键与运行时句柄都回到干净状态：
// 同一任务键可以再次启动（这是上一条用例保护的行为的用户可见面），且运行时句柄不泄漏在内存里。
func TestTaskPanicReleasesKeyAndRuntime(t *testing.T) {
	e, _ := newBackgroundTestEngine(runTaskBodySynchronously)

	const key = "rebuild_index"
	seedTask(t, e, taskSeed{Key: key, Type: "rebuild_index"})
	e.runTaskGoroutine(key, func() { panic("boom") })

	e.mutex.Lock()
	_, leaked := e.runtimes[key]
	e.mutex.Unlock()
	if leaked {
		t.Fatal("panic 后运行时句柄仍留在表里 —— 每个 panic 的任务都会泄漏一份 ctx 与暂停闸门")
	}

	if _, err := trySeedTask(e, taskSeed{Key: key, Type: "rebuild_index"}); err != nil {
		t.Fatalf("panic 之后同一任务键再也起不来（%v）—— 用户要重启进程才能重试", err)
	}
}

// TestTaskBodyRunsThroughInjectedBackgroundCapability 守卫「引擎不自己开 goroutine」
// （这条约束的代价见 taskEngine.runBackground 字段注释）。
func TestTaskBodyRunsThroughInjectedBackgroundCapability(t *testing.T) {
	var handedOff int
	// 这个后台能力登记了请求却不执行任务体，模拟「控制器已关闭，拒绝新任务」。
	e, _ := newBackgroundTestEngine(func(func()) { handedOff++ })

	const key = "scan_library_1"
	seedTask(t, e, taskSeed{Key: key, Type: "scan_library", Total: 10})

	bodyRan := false
	e.runTaskGoroutine(key, func() { bodyRan = true })

	if handedOff != 1 {
		t.Fatalf("任务体交给注入的后台能力 %d 次，应为 1 —— 引擎绕开了停机管辖", handedOff)
	}
	if bodyRan {
		t.Fatal("后台能力拒绝执行，任务体却还是跑起来了")
	}
}

// TestTaskGoroutineOnlyGuardsPanics 划出 runTaskGoroutine 的职责边界：它只接管 panic，
// 正常返回的任务一个字段都不该被它动过。这条与「引擎决定终态」并不冲突——那由启动入口
// 包在任务体外侧的一层承担，而不是由这个 goroutine 启动器代劳，否则会双重收尾。
func TestTaskGoroutineOnlyGuardsPanics(t *testing.T) {
	e, snapshots := newBackgroundTestEngine(runTaskBodySynchronously)

	const key = "scan_library_1"
	seedTask(t, e, taskSeed{Key: key, Type: "scan_library", Total: 10})
	e.runTaskGoroutine(key, func() {})

	if task := lastPublishedTask(t, snapshots(), key); task.Status != "running" {
		t.Fatalf("任务体正常返回后引擎把任务改成了 %q，应仍为 running", task.Status)
	}
}
