// 守停机把信号真的送到了任务体：它的 ctx 被取消，它的**暂停闸门**被放行。
//
// 「先取消、后放行」那个顺序本身由 internal/task 的契约用例守（它要包住运行时句柄上的取消函数，
// 而那是领域引擎的内部）。这里守的是接线之后那条信号确实通到了 api 交出去的那个 ctx 上——
// 断了的话，停机时暂停中的任务体永远等不到放行，优雅关闭会一直等下去。

package api

import (
	"fmt"
	"testing"

	"manga-manager/internal/taskcontrol"
)

func TestStopAllRuntimesReachesTheTaskBody(t *testing.T) {
	// 后台能力只登记不执行：任务体一旦跑起来就会收尾，**已暂停**无从观察。
	engine, _ := newBackgroundTestEngine(t, func(func()) {}, nil)

	keys := make([]string, 0, 8)
	for i := range 8 {
		key := fmt.Sprintf("scan_library_%d", i+1)
		seedTask(t, engine, taskSeed{Key: key, Identity: libraryTask("scan_library", int64(i+1), variantSole), Total: 100, CanCancel: true, CanPause: true})
		if err := engine.pause(key); err != nil {
			t.Fatalf("暂停 %q 失败: %v", key, err)
		}
		keys = append(keys, key)
	}

	engine.stopAllRuntimes()

	for _, key := range keys {
		ctx := seededTaskContext(t, engine, key)
		if ctx.Err() == nil {
			t.Fatalf("停机之后 %q 的 ctx 没有取消：任务体收不到停机信号", key)
		}
		gate := taskcontrol.FromContext(ctx)
		if gate == nil {
			t.Fatalf("任务 %q 的 ctx 上没有暂停闸门", key)
		}
		if gate.IsPaused() {
			t.Fatalf("停机之后 %q 的暂停闸门仍关着：不带 ctx 的等待者永远等不到放行", key)
		}
	}
}
