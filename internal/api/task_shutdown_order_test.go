// 守停机时「先取消、后放行」这个顺序，与单任务取消一致。
//
// 反过来做的话，两段之间有一个窗口：被放行的**已暂停**任务拿着还没取消的 ctx 回去干活——
// 过可中断点、取**存储令牌**、跑完一整个任务体。任务越多窗口越宽。

package api

import (
	"fmt"
	"sync/atomic"
	"testing"
)

// TestStopAllRuntimesCancelsBeforeResuming 判据落在**每个任务**自己的一对动作上：
// 取消被调到的那一刻，这个任务的**暂停闸门**必须还关着。
func TestStopAllRuntimesCancelsBeforeResuming(t *testing.T) {
	cases := []struct {
		name  string
		tasks int
	}{
		{"一个暂停中的任务", 1},
		{"多个暂停中的任务", 8},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 后台能力只登记不执行：任务体一旦跑起来就会收尾，**已暂停**无从观察。
			engine, _ := newBackgroundTestEngine(func(func()) {}, nil)

			keys := make([]string, 0, tc.tasks)
			for i := range tc.tasks {
				key := fmt.Sprintf("scan_library_%d", i+1)
				seedTask(t, engine, taskSeed{Key: key, Type: "scan_library", Total: 100, CanCancel: true, CanPause: true})
				if err := engine.pause(key); err != nil {
					t.Fatalf("暂停 %q 失败: %v", key, err)
				}
				keys = append(keys, key)
			}

			// 在**运行时句柄**的取消上包一层：调到它的那一刻闸门是开是关，正是两种顺序的分水岭。
			var resumedBeforeCancel atomic.Int64
			engine.mutex.Lock()
			for _, runtime := range engine.runtimes {
				gate, cancel := runtime.PauseGate, runtime.Cancel
				runtime.Cancel = func() {
					if !gate.IsPaused() {
						resumedBeforeCancel.Add(1)
					}
					cancel()
				}
			}
			engine.mutex.Unlock()

			engine.stopAllRuntimes()

			if got := resumedBeforeCancel.Load(); got != 0 {
				t.Fatalf("%d 个任务的闸门赶在自己的 ctx 取消之前被放行：那段窗口里它们拿着还没取消的 ctx 回去干活", got)
			}
			// 反面同样要守：取消要真的发生，放行也不能省——闸门的等待有一条不带 ctx 的分支。
			for _, key := range keys {
				engine.mutex.Lock()
				runtime := engine.runtimes[key]
				engine.mutex.Unlock()
				if runtime == nil {
					t.Fatalf("任务 %q 的运行时句柄不见了", key)
				}
				if runtime.Context.Err() == nil {
					t.Fatalf("停机之后 %q 的 ctx 没有取消：任务体收不到停机信号", key)
				}
				if runtime.PauseGate.IsPaused() {
					t.Fatalf("停机之后 %q 的暂停闸门仍关着：不带 ctx 的等待者永远等不到放行", key)
				}
			}
		})
	}
}
