// 守任务引擎**启动入口**的契约：任务体返回什么就该进哪条**终态**、同一**任务键**已有**活动态**
// 任务（含**取消中**）时连任务体都不得执行、**运行时句柄**必须在每条退出路径（含 panic）上归还、
// 整份任务声明在诞生那一帧就带齐。
// 破了分别是：任务停在进行中永不收尾、同一个库被并发扫描两遍、每次退出泄漏一份 ctx 与暂停闸门、
// 任务先以无名形态出现在列表里。装置见 newBackgroundTestEngine。

package api

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// specForTest 造一份最小可用的任务声明：三条终态分支各有自己的默认文案码，
// 用例据此分辨引擎选了哪条分支。
func specForTest(key string) TaskSpec {
	return TaskSpec{
		Key:          key,
		Type:         "scan_library",
		StartCode:    "spec.start",
		StartParams:  map[string]string{"name": "Main"},
		Total:        10,
		CanCancel:    true,
		CanPause:     true,
		CompleteCode: "spec.complete",
		CancelCode:   "spec.cancelled",
		FailCode:     "spec.failed",
	}
}

// TestRunSettlesByBodyError 钉住三条终态分支：任务体只返回错误，由引擎裁决进哪一条。
// 包裹过的 context.Canceled 同样要进**已取消**，而不是掉进「其余错误」那条。
func TestRunSettlesByBodyError(t *testing.T) {
	cases := []struct {
		name       string
		bodyErr    error
		wantStatus string
		wantCode   string
		wantError  string
	}{
		{"正常返回即完成", nil, "completed", "spec.complete", ""},
		{"取消错误进已取消", context.Canceled, "cancelled", "spec.cancelled", ""},
		{"包裹过的取消错误同样进已取消", fmt.Errorf("scan aborted: %w", context.Canceled), "cancelled", "spec.cancelled", ""},
		{"其余错误进失败", errors.New("disk on fire"), "failed", "spec.failed", "disk on fire"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, snapshots := newBackgroundTestEngine(runTaskBodySynchronously)

			const key = "scan_library_1"
			if err := e.Run(specForTest(key), func(context.Context, *TaskProgress) (TaskResult, error) {
				return TaskResult{}, tc.bodyErr
			}); err != nil {
				t.Fatalf("启动入口返回了 %v，应为 nil", err)
			}

			task := lastPublishedTask(t, snapshots(), key)
			if task.Status != tc.wantStatus {
				t.Fatalf("任务终态为 %q, want %q", task.Status, tc.wantStatus)
			}
			if task.MessageCode != tc.wantCode {
				t.Fatalf("终态文案码为 %q, want %q —— 任务声明里的默认码没被用上", task.MessageCode, tc.wantCode)
			}
			if task.Error != tc.wantError {
				t.Fatalf("失败原因为 %q, want %q —— 用户看不到真正的出错原因", task.Error, tc.wantError)
			}
			if task.FinishedAt == nil {
				t.Fatal("终态没有落 FinishedAt")
			}
			if task.CanCancel || task.CanPause || task.CanResume {
				t.Fatalf("已终结的任务仍带着控制能力：cancel=%v pause=%v resume=%v", task.CanCancel, task.CanPause, task.CanResume)
			}
		})
	}
}

// TestRunResultOverridesTerminalCode 钉住文案覆盖机制：常规任务不为收尾写任何代码（用声明里的
// 默认码），而「部分成功」「第一阶段失败」这类变体由任务体在返回时指定，不必被迫走通用路径。
func TestRunResultOverridesTerminalCode(t *testing.T) {
	cases := []struct {
		name     string
		result   TaskResult
		bodyErr  error
		wantCode string
	}{
		{"完成分支被覆盖", TaskResult{Code: "body.partial"}, nil, "body.partial"},
		{"取消分支被覆盖", TaskResult{Code: "body.cancelled_in_phase_two"}, context.Canceled, "body.cancelled_in_phase_two"},
		{"失败分支被覆盖", TaskResult{Code: "body.phase_one_failed"}, errors.New("boom"), "body.phase_one_failed"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, snapshots := newBackgroundTestEngine(runTaskBodySynchronously)

			const key = "scan_library_1"
			tc.result.Params = map[string]string{"written": "3"}
			if err := e.Run(specForTest(key), func(context.Context, *TaskProgress) (TaskResult, error) {
				return tc.result, tc.bodyErr
			}); err != nil {
				t.Fatalf("启动入口返回了 %v，应为 nil", err)
			}

			task := lastPublishedTask(t, snapshots(), key)
			if task.MessageCode != tc.wantCode {
				t.Fatalf("终态文案码为 %q, want %q —— 任务体的覆盖没有生效", task.MessageCode, tc.wantCode)
			}
			if task.MessageParams["written"] != "3" {
				t.Fatalf("任务体给的占位参数没落到终态文案上：%v", task.MessageParams)
			}
		})
	}
}

// TestRunKeepsSpecCodeWhenResultCodeEmpty 钉住零值语义：`TaskResult{}` 表示「用声明里的默认码」，
// 而不是「把文案清空」。绝大多数任务体走的正是这条路。
func TestRunKeepsSpecCodeWhenResultCodeEmpty(t *testing.T) {
	e, snapshots := newBackgroundTestEngine(runTaskBodySynchronously)

	const key = "scan_library_1"
	if err := e.Run(specForTest(key), func(context.Context, *TaskProgress) (TaskResult, error) {
		return TaskResult{Params: map[string]string{"name": "Main"}}, nil
	}); err != nil {
		t.Fatalf("启动入口返回了 %v，应为 nil", err)
	}

	task := lastPublishedTask(t, snapshots(), key)
	if task.MessageCode != "spec.complete" {
		t.Fatalf("零值 Code 下终态文案码为 %q, want spec.complete", task.MessageCode)
	}
	if task.MessageParams["name"] != "Main" {
		t.Fatalf("默认码的占位参数丢了：%v", task.MessageParams)
	}
}

// TestRunClaimsSlotSynchronouslyAndDefersBody 钉住那条被刻意保留的不变量：
// 槽位闸门同步、任务体异步。启动入口返回时任务已在列表里且任务体尚未开跑，HTTP 层才能立即返回 202。
func TestRunClaimsSlotSynchronouslyAndDefersBody(t *testing.T) {
	var deferred []func()
	e, snapshots := newBackgroundTestEngine(func(fn func()) { deferred = append(deferred, fn) })

	const key = "scan_library_1"
	bodyRan := false
	if err := e.Run(specForTest(key), func(context.Context, *TaskProgress) (TaskResult, error) {
		bodyRan = true
		return TaskResult{}, nil
	}); err != nil {
		t.Fatalf("启动入口返回了 %v，应为 nil", err)
	}

	if bodyRan {
		t.Fatal("启动入口返回时任务体已经跑起来了 —— HTTP 层将被任务体阻塞，无法立即返回 202")
	}
	if task := lastPublishedTask(t, snapshots(), key); task.Status != "running" {
		t.Fatalf("启动入口返回时任务状态为 %q, want running —— 槽位闸门没有同步执行", task.Status)
	}
	if len(deferred) != 1 {
		t.Fatalf("任务体交给注入的后台能力 %d 次，应为 1 —— 引擎绕开了停机管辖", len(deferred))
	}

	deferred[0]()
	if !bodyRan {
		t.Fatal("后台能力放行之后任务体仍未执行")
	}
	if task := lastPublishedTask(t, snapshots(), key); task.Status != "completed" {
		t.Fatalf("任务体跑完后任务状态为 %q, want completed", task.Status)
	}
}

// TestRunRejectsDuplicateActiveKey 钉住闸门：同一任务键已有**活动态**任务时，启动入口返回
// 「同类任务已在运行」哨兵错误，**且第二个任务体一步都不执行**——那正是重复扫描会造成的损害。
func TestRunRejectsDuplicateActiveKey(t *testing.T) {
	// 后台能力只登记不执行：第一个任务因此一直停在 running，占着这个任务键。
	var handedOff int
	e, _ := newBackgroundTestEngine(func(func()) { handedOff++ })

	const key = "scan_library_1"
	if err := e.Run(specForTest(key), func(context.Context, *TaskProgress) (TaskResult, error) {
		return TaskResult{}, nil
	}); err != nil {
		t.Fatalf("第一次启动返回了 %v，应为 nil", err)
	}

	secondBodyRan := false
	err := e.Run(specForTest(key), func(context.Context, *TaskProgress) (TaskResult, error) {
		secondBodyRan = true
		return TaskResult{}, nil
	})
	if !errors.Is(err, errTaskAlreadyRunning) {
		t.Fatalf("同键重复启动返回 %v, want errTaskAlreadyRunning —— 拒绝的原因在门口丢失了", err)
	}
	if secondBodyRan {
		t.Fatal("闸门拒绝了启动，第二个任务体却还是跑了 —— 同一个库会被并发扫描两遍")
	}
	if handedOff != 1 {
		t.Fatalf("后台能力被交付 %d 次，应为 1 —— 被拒绝的任务也占用了一个 goroutine", handedOff)
	}
}

// TestRunRejectsWhileCancelling 钉住「**取消中**属于**活动态**」：取消已请求但任务体尚未收尾时，
// 同一任务键不得再次启动，否则新旧两个任务体会同时在跑。
func TestRunRejectsWhileCancelling(t *testing.T) {
	var deferred []func()
	e, _ := newBackgroundTestEngine(func(fn func()) { deferred = append(deferred, fn) })

	const key = "scan_library_1"
	if err := e.Run(specForTest(key), func(context.Context, *TaskProgress) (TaskResult, error) {
		return TaskResult{}, nil
	}); err != nil {
		t.Fatalf("第一次启动返回了 %v，应为 nil", err)
	}
	if err := e.cancel(key); err != nil {
		t.Fatalf("取消失败: %v", err)
	}

	if err := e.Run(specForTest(key), func(context.Context, *TaskProgress) (TaskResult, error) {
		return TaskResult{}, nil
	}); !errors.Is(err, errTaskAlreadyRunning) {
		t.Fatalf("取消中的任务键被再次启动，返回 %v, want errTaskAlreadyRunning", err)
	}
	if len(deferred) != 1 {
		t.Fatalf("后台能力被交付 %d 次，应为 1", len(deferred))
	}
}

// TestRunReleasesRuntimeOnEveryExitPath 钉住**运行时句柄**在每条退出路径（含 panic）上都归还：
// 漏掉一条就泄漏一份 ctx 与**暂停闸门**，且那个任务键再也起不来。
func TestRunReleasesRuntimeOnEveryExitPath(t *testing.T) {
	cases := []struct {
		name string
		body func(context.Context, *TaskProgress) (TaskResult, error)
	}{
		{"完成", func(context.Context, *TaskProgress) (TaskResult, error) { return TaskResult{}, nil }},
		{"已取消", func(context.Context, *TaskProgress) (TaskResult, error) { return TaskResult{}, context.Canceled }},
		{"失败", func(context.Context, *TaskProgress) (TaskResult, error) { return TaskResult{}, errors.New("boom") }},
		{"panic", func(context.Context, *TaskProgress) (TaskResult, error) { panic("boom") }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, _ := newBackgroundTestEngine(runTaskBodySynchronously)

			const key = "scan_library_1"
			if err := e.Run(specForTest(key), tc.body); err != nil {
				t.Fatalf("启动入口返回了 %v，应为 nil", err)
			}

			e.mutex.Lock()
			leaked := len(e.runtimes)
			e.mutex.Unlock()
			if leaked != 0 {
				t.Fatalf("退出路径「%s」上残留了 %d 个运行时句柄", tc.name, leaked)
			}
			if err := e.Run(specForTest(key), tc.body); err != nil {
				t.Fatalf("退出路径「%s」之后同一任务键再也起不来：%v", tc.name, err)
			}
		})
	}
}

// TestRunPanicStillMarksTaskFailed 钉住启动入口没有把 panic 兜底吃掉：任务体在收尾之前 panic 时，
// 任务仍必须落到失败态，而不是停在 running 让那个任务键恒定返回 409。
func TestRunPanicStillMarksTaskFailed(t *testing.T) {
	e, snapshots := newBackgroundTestEngine(runTaskBodySynchronously)

	const key = "scan_library_1"
	if err := e.Run(specForTest(key), func(context.Context, *TaskProgress) (TaskResult, error) {
		panic("boom")
	}); err != nil {
		t.Fatalf("启动入口返回了 %v，应为 nil", err)
	}

	task := lastPublishedTask(t, snapshots(), key)
	if task.Status != "failed" {
		t.Fatalf("任务体 panic 后任务停在 %q，应为 failed", task.Status)
	}
	if task.Error == "" {
		t.Fatal("失败原因为空，用户看不到 panic 值")
	}
}

// TestRunLandsWholeSpecAtBirth 钉住任务声明一次性落地：作用域名、元数据与并发上限必须在诞生
// 那一帧就带齐。拆成启动之后的多次补写，任务会先以无名形态出现在列表里，而补写的那几帧
// 还会被首帧刚写下的节流水位吞掉。
func TestRunLandsWholeSpecAtBirth(t *testing.T) {
	// 后台能力只登记不执行：观测的是任务**诞生那一刻**的首帧，任务体跑不跑无关。
	e, snapshots := newBackgroundTestEngine(func(func()) {})

	const key = "scan_library_7"
	spec := specForTest(key)
	spec.ScopeName = "Main Library"
	spec.Metadata = map[string]string{"force": "true", "scan_profile": "balanced"}
	spec.Limits = TaskLimits{ScanProfile: "balanced", ScanConcurrency: 4}
	if err := e.Run(spec, func(context.Context, *TaskProgress) (TaskResult, error) {
		return TaskResult{}, nil
	}); err != nil {
		t.Fatalf("启动入口返回了 %v，应为 nil", err)
	}

	task := firstPublishedTask(t, snapshots(), key)
	if task.Status != "running" {
		t.Fatalf("首帧状态为 %q, want running", task.Status)
	}
	if task.ScopeName != "Main Library" {
		t.Fatalf("首帧就没有作用域名（%q）—— 任务会先以无名的形态出现在列表里", task.ScopeName)
	}
	if task.Params["force"] != "true" || task.Params["scan_profile"] != "balanced" {
		t.Fatalf("首帧缺少任务声明里的元数据：%v", task.Params)
	}
	if task.EffectiveLimit == nil || task.EffectiveLimit.ScanConcurrency != 4 {
		t.Fatalf("首帧缺少任务声明里的并发上限：%+v", task.EffectiveLimit)
	}
	if task.Scope != "library" || task.ScopeID == nil || *task.ScopeID != 7 {
		t.Fatalf("作用域推导结果不对：scope=%q id=%v", task.Scope, task.ScopeID)
	}
	if task.MessageCode != "spec.start" || task.MessageParams["name"] != "Main" {
		t.Fatalf("起始文案不对：code=%q params=%v", task.MessageCode, task.MessageParams)
	}
	if task.Total != 10 || !task.CanCancel || !task.CanPause {
		t.Fatalf("首帧的总数/控制能力不对：total=%d cancel=%v pause=%v", task.Total, task.CanCancel, task.CanPause)
	}
}

// TestRunLeavesLimitUnsetWhenSpecOmitsIt 钉住零值语义：没有并发上限可报的任务（多数维护任务如此）
// 不该凭空多出一份全零的上限，否则任务面板会显示一组「0 并发」的假数据。
func TestRunLeavesLimitUnsetWhenSpecOmitsIt(t *testing.T) {
	e, snapshots := newBackgroundTestEngine(runTaskBodySynchronously)

	const key = "rebuild_index"
	if err := e.Run(specForTest(key), func(context.Context, *TaskProgress) (TaskResult, error) {
		return TaskResult{}, nil
	}); err != nil {
		t.Fatalf("启动入口返回了 %v，应为 nil", err)
	}

	if task := firstPublishedTask(t, snapshots(), key); task.EffectiveLimit != nil {
		t.Fatalf("任务声明没给并发上限，却凭空多出一份：%+v", task.EffectiveLimit)
	}
}

// TestTaskProgressAdvanceAndPhaseAreIndependent 钉住进度接口按用途切开的边界：**计数推进**只回答
// 「做完了多少」，**阶段**只回答「在做什么」，条目名/指标/标签各管各的字段，谁都不许越界改别人的。
func TestTaskProgressAdvanceAndPhaseAreIndependent(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1700000000, 0)}
	e, snapshots := newBackgroundTestEngine(runTaskBodySynchronously)
	e.now = clock.Now

	const key = "scan_library_1"
	if err := e.Run(specForTest(key), func(_ context.Context, tp *TaskProgress) (TaskResult, error) {
		tp.Advance(3, 20, "progress.scanning", map[string]string{"current": "3"})
		if task := lastPublishedTask(t, snapshots(), key); task.Current != 3 || task.Total != 20 || task.Phase != "" {
			t.Fatalf("计数推进之后 current=%d total=%d phase=%q，它不该碰阶段", task.Current, task.Total, task.Phase)
		}

		tp.Phase("hashing", "progress.hashing", nil)
		if task := lastPublishedTask(t, snapshots(), key); task.Phase != "hashing" || task.Current != 3 || task.Total != 20 {
			t.Fatalf("阶段播报之后 phase=%q current=%d total=%d，它不该碰计数与总数", task.Phase, task.Current, task.Total)
		}

		// 条目名、指标与标签都不改变展示态，因而会被节流水位吞掉；越过窗口才看得到它们的那一帧。
		clock.advance(taskProgressPublishInterval * 2)
		tp.Item("volume_03.cbz")
		clock.advance(taskProgressPublishInterval * 2)
		tp.Metrics(map[string]int64{"hashed_files": 12})
		clock.advance(taskProgressPublishInterval * 2)
		tp.Labels(map[string]string{"provider_name": "Bangumi"})

		task := lastPublishedTask(t, snapshots(), key)
		if task.CurrentItem != "volume_03.cbz" {
			t.Fatalf("条目名没落到任务上：%q", task.CurrentItem)
		}
		if task.Metrics["hashed_files"] != 12 {
			t.Fatalf("指标没落到任务上：%v", task.Metrics)
		}
		if task.Labels["provider_name"] != "Bangumi" {
			t.Fatalf("标签没落到任务上：%v", task.Labels)
		}
		if task.Phase != "hashing" || task.Current != 3 || task.Total != 20 || task.MessageCode != "progress.hashing" {
			t.Fatalf("条目名与指标动了不属于它们的字段：phase=%q current=%d total=%d code=%q",
				task.Phase, task.Current, task.Total, task.MessageCode)
		}
		return TaskResult{}, nil
	}); err != nil {
		t.Fatalf("启动入口返回了 %v，应为 nil", err)
	}
}

// TestTaskProgressIgnoredAfterTerminal 钉住进度句柄的失效边界：任务已进入**终态**之后
// 迟到的进度回调（扫描器的 goroutine 不在任务体调用栈上，晚一拍很常见）不得把它拽回运行中。
func TestTaskProgressIgnoredAfterTerminal(t *testing.T) {
	e, snapshots := newBackgroundTestEngine(runTaskBodySynchronously)

	const key = "scan_library_1"
	var handle *TaskProgress
	if err := e.Run(specForTest(key), func(_ context.Context, tp *TaskProgress) (TaskResult, error) {
		handle = tp
		return TaskResult{}, nil
	}); err != nil {
		t.Fatalf("启动入口返回了 %v，应为 nil", err)
	}

	handle.Phase("hashing", "progress.hashing", nil)
	task := lastPublishedTask(t, snapshots(), key)
	if task.Status != "completed" || task.Phase == "hashing" {
		t.Fatalf("终态之后的迟到进度改写了任务：status=%q phase=%q", task.Status, task.Phase)
	}
}

// TestTaskMapsAreOwnedByTheEngine 钉住「任务上的每个可变 map 都归引擎所有」这条约定：
// 无论 map 是随任务声明进来的，还是随某一帧进来的，引擎都不得存下调用方那一份。
//
// 破了就是 taskEngine 符号 doc 写的那种 fatal error——runtime throw，拦不住。
func TestTaskMapsAreOwnedByTheEngine(t *testing.T) {
	// 后台能力只登记不执行：任务停在活动态，进度写得进去。
	e, _ := newBackgroundTestEngine(func(func()) {})

	metadata := map[string]string{"provider": "anilist"}
	labels := map[string]string{"provider_name": "AniList"}
	startParams := map[string]string{"name": "Main"}
	handle := seedTask(t, e, taskSeed{
		Key: "scan_library_1", Type: "scan_library",
		Metadata: metadata, Labels: labels,
		StartCode: "spec.start", StartParams: startParams,
	})

	frameParams := map[string]string{"count": "7"}
	handle.MergeParams(map[string]string{"scanned_series": "7"})
	handle.Labels(map[string]string{"current_series": "Beta"})
	handle.Report(TaskFrame{Code: "progress.scanning", Params: frameParams})

	// 从引擎那侧写一笔：调用方那几份跟着变，就说明存的是同一个 map header。
	e.mutex.Lock()
	stored := e.tasks["scan_library_1"]
	stored.Params["probe"] = "1"
	stored.Labels["probe"] = "1"
	stored.MessageParams["probe"] = "1"
	e.mutex.Unlock()

	for name, callerOwned := range map[string]map[string]string{
		"任务声明的 Metadata":    metadata,
		"任务声明的 Labels":      labels,
		"任务声明的 StartParams": startParams,
		"某一帧的 Params":       frameParams,
	} {
		if _, leaked := callerOwned["probe"]; leaked {
			t.Fatalf("引擎存下了调用方那份 map（%s）：%v", name, callerOwned)
		}
	}
}

// TestRejectedLaunchLeavesTheRunningTaskControllable 钉住**运行时句柄**与任务行同生：
// 被**任务键**闸门挡下的那次启动一步都不得往前走。抢在闸门之前建句柄的话，第二次启动会把
// 正在跑的那个任务的 ctx 与**暂停闸门**换成一份没人持有的，那个任务从此暂停不了也取消不了。
func TestRejectedLaunchLeavesTheRunningTaskControllable(t *testing.T) {
	e, _ := newBackgroundTestEngine(func(func()) {})

	const key = "scan_library_1"
	seedTask(t, e, taskSeed{Key: key, Type: "scan_library", CanCancel: true, CanPause: true})
	running := seededTaskContext(t, e, key)

	if _, err := trySeedTask(e, taskSeed{Key: key, Type: "scan_library", CanCancel: true, CanPause: true}); !errors.Is(err, errTaskAlreadyRunning) {
		t.Fatalf("同键第二次启动返回 %v, want errTaskAlreadyRunning", err)
	}

	if seededTaskContext(t, e, key) != running {
		t.Fatal("被闸门挡下的那次启动换掉了在跑任务的运行时句柄")
	}
	if err := e.cancel(key); err != nil {
		t.Fatalf("在跑的任务取消不了了: %v", err)
	}
}
