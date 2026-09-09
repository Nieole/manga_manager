// 守**投递出去的**运行快照上不再有**任务键**那一格（ADR 0007：键退出寻址，不退出日志）。
// 它是 task_key_logging_test.go 的另一半：那边守键仍写在每一行日志上，这边守它不再出现在契约里。
// 破了的表现是前端又能按键寻址，而同一个键此刻可以有两条仍会变化的运行。

package api

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"manga-manager/internal/runhandle"
	"manga-manager/internal/task"
)

// TestPublishedRunSnapshotCarriesNoTaskKey 断言看的是**原始载荷**而不是解出来的结构体：
// 契约上没有那一格，说的正是「投递出去的 JSON 里没有这个名字」——SSE 订阅者拿到的就是这份串。
// 解成结构体再断言零值杀不掉「字段还在、只是没填」这种实现。
func TestPublishedRunSnapshotCarriesNoTaskKey(t *testing.T) {
	var mu sync.Mutex
	var payloads []string
	engine := newTaskEngine(taskEngineConfig{
		Store: newTaskTestStore(t),
		Publish: func(payload string) {
			if !strings.HasPrefix(payload, runSnapshotEventPrefix) {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			payloads = append(payloads, strings.TrimPrefix(payload, runSnapshotEventPrefix))
		},
		RunBackground: runTaskBodySynchronously,
	})

	err := engine.Run(
		libraryTask("scan_library", 1, variantSole),
		task.TriggerManual,
		RunSpec{Key: "scan_library_1", ScopeName: "Main", StartCode: "task.msg.scan.start"},
		func(context.Context, *runhandle.Handle) (TaskResult, error) { return TaskResult{}, nil },
	)
	if err != nil {
		t.Fatalf("发起任务失败: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(payloads) == 0 {
		t.Fatal("一帧运行快照都没投递出去 —— 这条用例什么都没守到")
	}
	for i, payload := range payloads {
		var frame struct {
			Run map[string]json.RawMessage `json:"run"`
		}
		if err := json.Unmarshal([]byte(payload), &frame); err != nil {
			t.Fatalf("第 %d 帧解不开: %v", i, err)
		}
		if _, present := frame.Run["key"]; present {
			t.Fatalf("第 %d 帧的运行快照上还带着 key 那一格: %s", i, payload)
		}
	}
}
