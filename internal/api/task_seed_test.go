// 守「往任务表里放一条任务」这件事只能经启动入口发生，以及这套播种脚手架本身。
//
// 直接写内存任务表也能让消费方跑绿，但「同一**任务键**同时只能有一个**活动态**任务」这道闸门
// 会就此在整片测试里失去覆盖——而它是同一个资料库不会被并发扫描两遍的唯一原因。

package api

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"manga-manager/internal/taskrun"
)

// taskSeed 描述一条要播下的任务。零值即「系统**作用域**、不可取消不可暂停、停在运行中」。
//
// 作用域本身不在这里：它由 Type 与 Key 推导（inferTaskScope），播种与生产同源——
// 想要某个作用域，挑相应的任务类型与任务键即可。
//
// 前半段字段刻意平铺而不是内嵌 TaskSpec：内嵌能保证任务声明加字段时播种自动跟上，代价是
// 每个播种点都要多写一层 `TaskSpec{...}`，而播种点有几十处，可读性是这里更值钱的东西。
// 代价则是 TaskSpec 新增字段时本结构不会有编译错误提醒——加字段的人要顺手看一眼这里。
type taskSeed struct {
	Key  string
	Type string

	Total     int
	CanCancel bool
	CanPause  bool

	// Metadata 落进任务参数（**重启函数**从这里读回原始入参），Labels 落进展示标签，
	// ScopeName 是作用域的显示名。
	Metadata  map[string]string
	Labels    map[string]string
	ScopeName string
	Limits    TaskLimits

	StartCode   string
	StartParams map[string]string

	// Terminal 为空表示任务停在**活动态**：**运行时句柄**已登记，可暂停、可取消、可播报进度。
	// 否则取 completed / cancelled / failed 之一，播种返回时该**终态**已经落定。
	Terminal string

	// TerminalCode 与 TerminalParams 是终态文案；FailError 只在失败终态下生效，落进 TaskStatus.Error。
	TerminalCode   string
	TerminalParams map[string]string
	FailError      string
}

// seedTask 播下一条任务并返回它的**任务句柄**；被**任务键**闸门拒绝即 t.Fatal。
func seedTask(t testing.TB, e *taskEngine, seed taskSeed) *taskrun.Handle {
	t.Helper()
	progress, err := trySeedTask(e, seed)
	if err != nil {
		t.Fatalf("播种任务 %q 失败: %v", seed.Key, err)
	}
	return progress
}

// trySeedTask 与 seedTask 相同，但把闸门的拒绝作为错误返回，供「同键重复播种」这类用例断言。
//
// 它临时换掉引擎注入的后台运行能力，好在这一刻决定任务体何时执行：活动态播种要的是
// 「任务行已落地、任务体不跑」，终态播种要的是「任务体同步跑完并收尾」。这正是把「开一个
// 受停机管辖的 goroutine」做成注入依赖所买到的确定性——不必 sleep 或轮询去等一个真实 goroutine。
//
// **调用约束**：只能在测试自己的 goroutine 上、且此刻没有任何任务体在飞时调用。runBackground
// 属于引擎「装配期注入、之后只读」的那组字段，不受 mutex 保护，这里的换入换出因此不是并发安全的；
// 与并发启动的任务撞上时 -race 也未必抓得到（那要恰好两条 goroutine 同时摸到这个字段）。
func trySeedTask(e *taskEngine, seed taskSeed) (*taskrun.Handle, error) {
	// 这张表是 settleTask 的逆：那边把任务体返回的错误翻成终态，这边把想要的终态翻回错误，
	// 好让播种和生产落进同一处裁决。改动任一侧都要照着另一侧看一眼——两边都不会有编译错误。
	var bodyErr error
	switch seed.Terminal {
	case "", "completed":
	case "cancelled":
		bodyErr = context.Canceled
	case "failed":
		bodyErr = errors.New(firstNonEmptyTaskValue(seed.FailError, "seeded failure"))
	default:
		return nil, fmt.Errorf("未知的终态 %q，可用：completed / cancelled / failed", seed.Terminal)
	}

	spec := TaskSpec{
		Key:         seed.Key,
		Type:        seed.Type,
		StartCode:   seed.StartCode,
		StartParams: seed.StartParams,
		Total:       seed.Total,
		CanCancel:   seed.CanCancel,
		CanPause:    seed.CanPause,
		Metadata:    seed.Metadata,
		Labels:      seed.Labels,
		ScopeName:   seed.ScopeName,
		Limits:      seed.Limits,
	}
	result := TaskResult{Code: seed.TerminalCode, Params: seed.TerminalParams}

	var body func()
	restore := e.runBackground
	defer func() { e.runBackground = restore }()
	e.runBackground = func(fn func()) { body = fn }

	if err := e.Run(spec, func(context.Context, *taskrun.Handle) (TaskResult, error) {
		return result, bodyErr
	}); err != nil {
		return nil, err
	}
	if seed.Terminal != "" {
		body()
	}
	// 与启动入口交给任务体的那个句柄同源：两处都走 newTaskHandle，键在闭包里绑定。
	return e.newTaskHandle(seed.Key), nil
}

// settleSeededTask 让一条 seedTask 播下、仍停在**活动态**的任务按「任务体返回了 err」收尾，
// 与生产共用同一处终态裁决。终态本身可以直接由 taskSeed.Terminal 播出来，这个函数是给那些
// 「收尾就是被测行为」的用例用的——它们要在播种与收尾之间插进别的动作。
func settleSeededTask(e *taskEngine, key string, err error) {
	e.settleTask(TaskSpec{Key: key}, TaskResult{}, err)
}

// seededTaskContext 取出启动入口本该交给任务体的那个 ctx：可取消，且带**暂停闸门**。
// 供那些要断言「取消真的传到了任务体」「暂停真的挡住了可中断点」的用例使用。
func seededTaskContext(t testing.TB, e *taskEngine, key string) context.Context {
	t.Helper()
	e.mutex.Lock()
	defer e.mutex.Unlock()
	runtime := e.runtimes[key]
	if runtime == nil {
		t.Fatalf("任务 %q 没有运行时句柄 —— 它没经启动入口播种，或已经进入终态", key)
		return nil
	}
	return runtime.Context
}

// TestSeedTaskGoesThroughTheTaskKeyGate 守卫脚手架没有绕开**任务键**闸门。
func TestSeedTaskGoesThroughTheTaskKeyGate(t *testing.T) {
	e, _ := newBackgroundTestEngine(runTaskBodySynchronously)

	const key = "scan_library_1"
	seedTask(t, e, taskSeed{Key: key, Type: "scan_library", Total: 100})

	if _, err := trySeedTask(e, taskSeed{Key: key, Type: "scan_library"}); !errors.Is(err, errTaskAlreadyRunning) {
		t.Fatalf("同键重复播种返回 %v, want errTaskAlreadyRunning —— 脚手架绕过了闸门，整片测试就此失去这条覆盖", err)
	}

	settleSeededTask(e, key, nil)
	if _, err := trySeedTask(e, taskSeed{Key: key, Type: "scan_library"}); err != nil {
		t.Fatalf("落定终态之后同一任务键播不下去了: %v", err)
	}
}

// TestSeededActiveTaskIsControllable 守卫播下的活动态任务确实可控：启动入口登记了**运行时句柄**，
// 暂停与取消才有 ctx 与**暂停闸门**可操作。漏掉这一步的话，消费方拿到的是一条 pause 一律 409 的假任务。
func TestSeededActiveTaskIsControllable(t *testing.T) {
	// 后台能力只登记不执行：播种之外的任何任务体都不该在本用例里跑起来。
	e, _ := newBackgroundTestEngine(func(func()) {})

	const key = "scan_library_1"
	seedTask(t, e, taskSeed{Key: key, Type: "scan_library", Total: 100, CanCancel: true, CanPause: true})

	if err := e.pause(key); err != nil {
		t.Fatalf("暂停播下的任务失败: %v", err)
	}
	if err := e.resume(key); err != nil {
		t.Fatalf("恢复播下的任务失败: %v", err)
	}
	if err := e.cancel(key); err != nil {
		t.Fatalf("取消播下的任务失败: %v", err)
	}
}

// TestSeedTaskLandsRequestedShape 守卫播种参数确实落到了任务上——消费方的 arrange 全靠它，
// 而一条形状不对的任务只会让它们以别的理由变红，排查要绕一大圈。
func TestSeedTaskLandsRequestedShape(t *testing.T) {
	cases := []struct {
		name       string
		terminal   string
		wantStatus string
	}{
		{"活动态", "", "running"},
		{"完成", "completed", "completed"},
		{"已取消", "cancelled", "cancelled"},
		{"失败", "failed", "failed"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, snapshots := newBackgroundTestEngine(runTaskBodySynchronously)

			const key = "scan_library_7"
			seedTask(t, e, taskSeed{
				Key: key, Type: "scan_library", Total: 100,
				CanCancel: true, CanPause: true,
				ScopeName: "Main", Metadata: map[string]string{"force": "true"},
				Terminal: tc.terminal, FailError: "archive error",
			})

			task := lastPublishedTask(t, snapshots(), key)
			if task.Status != tc.wantStatus {
				t.Fatalf("播下的任务状态为 %q, want %q", task.Status, tc.wantStatus)
			}
			if task.Type != "scan_library" || task.Scope != "library" || task.ScopeID == nil || *task.ScopeID != 7 {
				t.Fatalf("类型与作用域不对：type=%q scope=%q id=%v", task.Type, task.Scope, task.ScopeID)
			}
			if task.Total != 100 || task.ScopeName != "Main" || task.Params["force"] != "true" {
				t.Fatalf("总数 / 作用域名 / 元数据不对：total=%d scopeName=%q params=%v", task.Total, task.ScopeName, task.Params)
			}
			if tc.wantStatus == "running" && (!task.CanCancel || !task.CanPause) {
				t.Fatalf("活动态任务没带上控制能力：cancel=%v pause=%v", task.CanCancel, task.CanPause)
			}
			if tc.wantStatus == "failed" && task.Error != "archive error" {
				t.Fatalf("失败原因为 %q, want archive error", task.Error)
			}
		})
	}
}

// TestSeedTaskRejectsUnknownTerminal 守卫拼错终态名不会被当成「留在活动态」静默放过——
// 那会让消费方对着一条还在跑的任务断言它已经结束。
func TestSeedTaskRejectsUnknownTerminal(t *testing.T) {
	e, _ := newBackgroundTestEngine(runTaskBodySynchronously)

	if _, err := trySeedTask(e, taskSeed{Key: "scan_library_1", Type: "scan_library", Terminal: "done"}); err == nil {
		t.Fatal("未知的终态名被静默接受了")
	}
}
