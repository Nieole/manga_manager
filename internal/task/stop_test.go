// 守停机时「先取消、后放行」这个顺序，与单条运行的取消一致。
//
// 反过来做的话，两段之间有一个窗口：被放行的**已暂停**运行拿着还没取消的 ctx 回去干活——
// 过可中断点、取**存储令牌**、跑完一整个任务体。运行越多窗口越宽。

package task

import (
	"sync/atomic"
	"testing"

	"manga-manager/internal/taskcontrol"
)

// TestStopAllCancelsBeforeResuming 判据落在**每条运行**自己的一对动作上：
// 取消被调到的那一刻，这条运行的**暂停闸门**必须还关着。
func TestStopAllCancelsBeforeResuming(t *testing.T) {
	cases := []struct {
		name string
		runs int
	}{
		{"一条暂停中的运行", 1},
		{"多条暂停中的运行", 8},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 后台能力只登记不执行：任务体一旦跑起来就会收尾，**已暂停**无从观察。
			harness := newTestEngine(t, registerOnly, tc.runs+1)

			runIDs := make([]int64, 0, tc.runs)
			for i := range tc.runs {
				run := harness.start(t, libraryScanSpec(int64(i+1)), idleBody)
				if err := harness.engine.Pause(run.ID); err != nil {
					t.Fatalf("暂停运行 %d 失败: %v", run.ID, err)
				}
				runIDs = append(runIDs, run.ID)
			}

			// 在**运行时句柄**的取消上包一层：调到它的那一刻闸门是开是关，正是两种顺序的分水岭。
			var resumedBeforeCancel atomic.Int64
			harness.engine.mu.Lock()
			for _, rt := range harness.engine.runtimes {
				gate, cancel := rt.gate, rt.cancel
				rt.cancel = func() {
					if !gate.IsPaused() {
						resumedBeforeCancel.Add(1)
					}
					cancel()
				}
			}
			harness.engine.mu.Unlock()

			harness.engine.StopAll()

			if got := resumedBeforeCancel.Load(); got != 0 {
				t.Fatalf("%d 条运行的闸门赶在自己的 ctx 取消之前被放行：那段窗口里它们拿着还没取消的 ctx 回去干活", got)
			}
			// 反面同样要守：取消要真的发生，放行也不能省——闸门的等待有一条不带 ctx 的分支。
			for _, runID := range runIDs {
				harness.engine.mu.Lock()
				rt := harness.engine.runtimes[runID]
				harness.engine.mu.Unlock()
				if rt == nil {
					t.Fatalf("运行 %d 的运行时句柄不见了", runID)
				}
				if rt.ctx.Err() == nil {
					t.Fatalf("停机之后运行 %d 的 ctx 没有取消：任务体收不到停机信号", runID)
				}
				if gate := taskcontrol.FromContext(rt.ctx); gate == nil || gate.IsPaused() {
					t.Fatalf("停机之后运行 %d 的暂停闸门仍关着：不带 ctx 的等待者永远等不到放行", runID)
				}
			}
		})
	}
}
