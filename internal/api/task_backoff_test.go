// 守**退避**与禁用在接线这一侧：三个阈值从运行时配置读、禁用那两个端点按**任务 id** 寻址，
// 以及禁用之后自动发起真的不再建运行而手动仍建得出。
//
// 破了的表现是那个挂在被拔掉的移动硬盘上的库继续每小时失败一次、在任务中心刷屏；
// 或者反过来——用户按下禁用，盘照转。

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"manga-manager/internal/config"
	"manga-manager/internal/runhandle"
	"manga-manager/internal/task"
)

// setBackoff 改这三个阈值，与经设置页保存走同一条路（换掉配置快照）。
func setBackoff(t testing.TB, c *Controller, factor, maxHours, stopAfter int) {
	t.Helper()
	cfg := c.currentConfig()
	cfg.Tasks.BackoffFactor = factor
	cfg.Tasks.BackoffMaxHours = maxHours
	cfg.Tasks.BackoffStopAfter = stopAfter
	c.config.Replace(&cfg)
}

// autoLaunchRequest 打一次禁用 / 启用端点，返回状态码。路由经 chi 走一遍而不是直接调 handler：
// 这两个端点与重试端点占着同一段路径，只在末段动词上分岔。
// 三条各取各的参数由 TestTaskEndpointsTakeTheirOwnPathParam 在**真路由**上守着。
func autoLaunchRequest(t *testing.T, controller *Controller, taskID int64, action string) int {
	t.Helper()
	router := chi.NewRouter()
	router.Post("/api/system/tasks/{taskID}/retry", controller.retryTask)
	router.Post("/api/system/tasks/{taskID}/disable", controller.disableTask)
	router.Post("/api/system/tasks/{taskID}/enable", controller.enableTask)

	rec := httptest.NewRecorder()
	path := "/api/system/tasks/" + strconv.FormatInt(taskID, 10) + "/" + action
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))
	return rec.Code
}

// summaryOf 取任务清单上唯一那一行。
func summaryOf(t *testing.T, controller *Controller) TaskSummary {
	t.Helper()
	rec := httptest.NewRecorder()
	controller.listTaskSummaries(rec, httptest.NewRequest(http.MethodGet, "/api/system/tasks/summary", nil))
	var summaries []TaskSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &summaries); err != nil {
		t.Fatalf("解任务清单失败: %v (raw=%s)", err, rec.Body.String())
	}
	if len(summaries) != 1 {
		t.Fatalf("清单条数为 %d, want 1", len(summaries))
	}
	return summaries[0]
}

// 配置那份默认与领域那份兜底必须相等：错开一次，默认部署的退避口径就与默认引擎不是一回事。
// config 位于 task 的依赖下游，引用不了那边的常数，只能在这里对一次。
func TestConfigDefaultBackoffMatchesTheEngineDefault(t *testing.T) {
	want := task.DefaultBackoff()
	if config.DefaultBackoffFactor != want.Factor {
		t.Fatalf("配置默认倍率 %d 与领域默认 %d 不一致", config.DefaultBackoffFactor, want.Factor)
	}
	if got := backoffMaxDelay(config.DefaultBackoffMaxHours); got != want.MaxDelay {
		t.Fatalf("配置默认封顶 %v 与领域默认 %v 不一致", got, want.MaxDelay)
	}
	if config.DefaultBackoffStopAfter != want.StopAfter {
		t.Fatalf("配置默认停发阈值 %d 与领域默认 %d 不一致", config.DefaultBackoffStopAfter, want.StopAfter)
	}
}

// 三个阈值从**运行时配置**读，改完设置对下一次自动发起生效，不必重启进程。
//
// 两头的越界值都必须落在「退回默认」这一侧：小时数乘成 time.Duration 会溢出，绕回来的可能是个
// 很小的正数，甚至负数——两者都等于退避当场失效，那块坏盘原封不动地每小时再转一遍。
func TestBackoffPolicyComesFromTheRuntimeConfig(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	if got := backoffPolicyOf(controller.currentConfig()); got != task.DefaultBackoff() {
		t.Fatalf("默认配置下的退避策略为 %+v, want %+v", got, task.DefaultBackoff())
	}

	setBackoff(t, controller, 3, 6, 4)
	want := task.BackoffPolicy{Factor: 3, MaxDelay: 6 * time.Hour, StopAfter: 4}
	if got := backoffPolicyOf(controller.currentConfig()); got != want {
		t.Fatalf("改配置之后的退避策略为 %+v, want %+v", got, want)
	}

	// 大到溢出的小时数收在上限内；负数交出 0，由领域退回默认封顶。
	setBackoff(t, controller, 2, 1<<50, 6)
	if got := backoffPolicyOf(controller.currentConfig()).MaxDelay; got != backoffMaxDelay(maxBackoffHours) {
		t.Fatalf("大到溢出的封顶算出了 %v, want 收在 %v", got, backoffMaxDelay(maxBackoffHours))
	}
	setBackoff(t, controller, 2, -1<<50, 6)
	if got := backoffPolicyOf(controller.currentConfig()).MaxDelay; got != 0 {
		t.Fatalf("负得足够多的封顶算出了 %v, want 0（交给领域退回默认）—— 负时长会让退避当场失效", got)
	}
}

// 禁用作用在**任务**上：定时与监听不再为它发起，手动仍发得出，而那条正在跑的运行不受影响。
func TestDisabledTaskStopsAutomaticLaunchesButNotManualOnes(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	controller.taskEngine.runBackground = runTaskBodySynchronously
	identity := libraryTask("scan_library", 1, variantSole)

	seedTask(t, controller.taskEngine, taskSeed{
		Key: "scan_library_1", Identity: identity, Total: 1, Terminal: "completed",
	})
	summary := summaryOf(t, controller)

	if got := autoLaunchRequest(t, controller, summary.TaskID, "disable"); got != http.StatusAccepted {
		t.Fatalf("禁用端点的状态码为 %d, want 202", got)
	}
	if disabled := summaryOf(t, controller); !disabled.Disabled || disabled.StallReason != string(task.StallDisabled) {
		t.Fatalf("禁用之后清单行是 %+v, want 已禁用且停发原因为 disabled", disabled)
	}

	launched, err := controller.taskEngine.start(identity, task.TriggerScheduled, RunSpec{Key: "scan_library_1", Total: 1}, idleTaskBody)
	if err != nil {
		t.Fatalf("禁用之后的守护发起返回错误: %v", err)
	}
	if launched.Stalled != task.StallDisabled || launched.Run.ID != 0 {
		t.Fatalf("禁用之后的守护发起落成 %+v, want 被 disabled 挡下且不建运行", launched)
	}

	manual, err := controller.taskEngine.start(identity, task.TriggerManual, RunSpec{Key: "scan_library_1", Total: 1}, idleTaskBody)
	if err != nil || manual.Run.ID == 0 {
		t.Fatalf("禁用期间手动发起落成 %+v (err=%v), want 建出一条运行", manual, err)
	}

	if got := autoLaunchRequest(t, controller, summary.TaskID, "enable"); got != http.StatusAccepted {
		t.Fatalf("启用端点的状态码为 %d, want 202", got)
	}
	if enabled := summaryOf(t, controller); enabled.Disabled || enabled.StallReason != "" {
		t.Fatalf("启用之后清单行是 %+v, want 未禁用且不停发", enabled)
	}
}

// 找不到那个任务是 404 而不是 500：按 id 寻址的入口只有这一条，答错了用户会以为服务坏了。
func TestAutoLaunchSwitchOnAnUnknownTaskIs404(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	if got := autoLaunchRequest(t, controller, 4242, "disable"); got != http.StatusNotFound {
		t.Fatalf("禁用一个不存在的任务返回 %d, want 404", got)
	}
}

// 连败到阈值就停发，界面上那一行同时写得出**为什么**：连败几次，加上最后一次的错误。
// 只标红不写原因的话，用户只看到一个红点，答不出「我该去修什么」。
func TestFailStreakStopsAutomaticLaunchesAndSaysWhy(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	controller.taskEngine.runBackground = runTaskBodySynchronously
	identity := libraryTask("scan_library", 1, variantSole)
	// 时钟由用例驱动：第一次失败之后这个任务就在**退避**里，靠等真实时间跨过它要两小时。
	clock := &fakeClock{now: time.Unix(1700000000, 0)}
	controller.taskEngine.now = clock.Now
	// 阈值压到 2，好让这条用例只播两次种；曲线本身由领域的毫秒级用例守。
	setBackoff(t, controller, 2, 24, 2)

	for range 2 {
		seedTask(t, controller.taskEngine, taskSeed{
			Key: "scan_library_1", Identity: identity, Trigger: task.TriggerScheduled,
			Total: 1, Terminal: "failed", FailError: "drive is unplugged",
		})
		clock.advance(48 * time.Hour)
	}

	summary := summaryOf(t, controller)
	if summary.StallReason != string(task.StallFailLimit) {
		t.Fatalf("停发原因为 %q, want fail_limit", summary.StallReason)
	}
	if summary.FailStreak != 2 {
		t.Fatalf("连败次数为 %d, want 2", summary.FailStreak)
	}
	if summary.LastRun == nil || summary.LastRun.Error != "drive is unplugged" {
		t.Fatalf("清单行没有带上最后一次的错误: %+v", summary.LastRun)
	}

	launched, err := controller.taskEngine.start(identity, task.TriggerScheduled, RunSpec{Key: "scan_library_1", Total: 1}, idleTaskBody)
	if err != nil {
		t.Fatalf("停发之后的守护发起返回错误: %v", err)
	}
	if launched.Stalled != task.StallFailLimit {
		t.Fatalf("停发之后的守护发起落成 %+v, want 被 fail_limit 挡下", launched)
	}

	// 手动发起一次即复位：修好之后不必等退避走完，也不必去设置里改阈值。
	if _, err := controller.taskEngine.start(identity, task.TriggerManual, RunSpec{Key: "scan_library_1", Total: 1}, idleTaskBody); err != nil {
		t.Fatalf("手动发起失败: %v", err)
	}
	if reset := summaryOf(t, controller); reset.FailStreak != 0 || reset.StallReason != "" {
		t.Fatalf("手动发起之后清单行是 %+v, want 连败归零且不再停发", reset)
	}
}

// idleTaskBody 是什么都不做、正常返回的任务体。
func idleTaskBody(context.Context, *runhandle.Handle) (TaskResult, error) { return TaskResult{}, nil }
