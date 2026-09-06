// 这些用例守的是只有真 SQLite 才答得出的那一半：四类**运行事件**的每一格原样读得回来、
// 读回顺序就是落下顺序（阶段耗时靠相邻两条相减）、条数上限截的是靠前那一段。

package taskstore

import (
	"context"
	"testing"
	"time"

	"manga-manager/internal/task"
)

// TestRunEventsRoundTripEveryKind 守四类事件各自那几格都落得下也读得回。
//
// 表上只有一列 payload，编解码归适配器；接错一格不会有编译错误，后果是详情页上失败明细的原因
// 变成空白、或者控制动作显示成一句阶段名。
func TestRunEventsRoundTripEveryKind(t *testing.T) {
	ctx := context.Background()
	store := newStoreForTest(t)
	taskID := ensureTask(t, store, 1)
	run := createRun(t, store, taskID, task.StatusRunning, 1)

	at := time.Now().Truncate(time.Millisecond).UTC()
	want := []task.Event{
		{At: at, Kind: task.EventPhase, Phase: "reading_metadata"},
		{At: at.Add(time.Second), Kind: task.EventItem, Item: "/lib/vol01.cbz", Reason: "zip: not a valid archive"},
		{At: at.Add(2 * time.Second), Kind: task.EventControl, Action: task.ControlPaused},
		{At: at.Add(3 * time.Second), Kind: task.EventWarn, Code: "item_failures_omitted", Detail: "guardrail", Count: 42},
	}
	if err := store.AppendRunEvents(ctx, run.ID, want); err != nil {
		t.Fatalf("落事件失败: %v", err)
	}

	got, err := store.ListRunEvents(ctx, run.ID, 0)
	if err != nil {
		t.Fatalf("读回事件失败: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("读回 %d 条事件，想要 %d 条", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 条读回 %+v，想要 %+v", i+1, got[i], want[i])
		}
	}
}

// TestRunEventsComeBackInInsertionOrder 守读回顺序就是落下顺序，哪怕全落在同一毫秒里。
//
// 阶段时间线是相邻两条相减出来的：按时间列定序的话，同一毫秒里的两条谁先谁后由实现随手决定，
// 而算出来的段长会变成负数。
func TestRunEventsComeBackInInsertionOrder(t *testing.T) {
	ctx := context.Background()
	store := newStoreForTest(t)
	taskID := ensureTask(t, store, 1)
	run := createRun(t, store, taskID, task.StatusRunning, 1)

	at := time.Now().Truncate(time.Millisecond).UTC()
	phases := []string{"discovering", "reading_metadata", "ingesting", "generating_covers"}
	for _, phase := range phases {
		if err := store.AppendRunEvents(ctx, run.ID, []task.Event{{At: at, Kind: task.EventPhase, Phase: phase}}); err != nil {
			t.Fatalf("落事件失败: %v", err)
		}
	}

	got, err := store.ListRunEvents(ctx, run.ID, 0)
	if err != nil {
		t.Fatalf("读回事件失败: %v", err)
	}
	if len(got) != len(phases) {
		t.Fatalf("读回 %d 条事件，想要 %d 条", len(got), len(phases))
	}
	for i, phase := range phases {
		if got[i].Phase != phase {
			t.Fatalf("第 %d 条是 %q，想要 %q —— 顺序被打乱了", i+1, got[i].Phase, phase)
		}
	}
}

// TestRunEventsLimitTakesTheHead 守条数上限截的是**靠前**那一段：时间线从头往后相减，
// 从中间截断的话第一段的起点就没了。
func TestRunEventsLimitTakesTheHead(t *testing.T) {
	ctx := context.Background()
	store := newStoreForTest(t)
	taskID := ensureTask(t, store, 1)
	run := createRun(t, store, taskID, task.StatusRunning, 1)

	at := time.Now().Truncate(time.Millisecond).UTC()
	for _, phase := range []string{"first", "second", "third"} {
		if err := store.AppendRunEvents(ctx, run.ID, []task.Event{{At: at, Kind: task.EventPhase, Phase: phase}}); err != nil {
			t.Fatalf("落事件失败: %v", err)
		}
	}

	got, err := store.ListRunEvents(ctx, run.ID, 2)
	if err != nil {
		t.Fatalf("读回事件失败: %v", err)
	}
	if len(got) != 2 || got[0].Phase != "first" || got[1].Phase != "second" {
		t.Fatalf("按上限读回 %+v，想要靠前那两条", got)
	}
}

// TestRunEventsAreScopedToTheirRun 守事件不串运行：同一个任务的两次运行各有各的事件流，
// 而「这次是不是比上次慢」正是靠分得开才答得出。
func TestRunEventsAreScopedToTheirRun(t *testing.T) {
	ctx := context.Background()
	store := newStoreForTest(t)
	taskID := ensureTask(t, store, 1)
	first := createRun(t, store, taskID, task.StatusCompleted, 1)
	second := createRun(t, store, taskID, task.StatusRunning, 2)

	at := time.Now().Truncate(time.Millisecond).UTC()
	if err := store.AppendRunEvents(ctx, first.ID, []task.Event{{At: at, Kind: task.EventItem, Item: "/old.cbz"}}); err != nil {
		t.Fatalf("落事件失败: %v", err)
	}
	if err := store.AppendRunEvents(ctx, second.ID, []task.Event{{At: at, Kind: task.EventItem, Item: "/new.cbz"}}); err != nil {
		t.Fatalf("落事件失败: %v", err)
	}

	got, err := store.ListRunEvents(ctx, second.ID, 0)
	if err != nil {
		t.Fatalf("读回事件失败: %v", err)
	}
	if len(got) != 1 || got[0].Item != "/new.cbz" {
		t.Fatalf("第二次运行读回 %+v，想要只有它自己那一条", got)
	}
}
