// 守推送通道的两件事：一是**投递链**——每一帧都指得回上一帧，丢了一帧前端才发现得了；
// 二是**实况汇总**那种轻量帧的内容与投递时机。节流水位本身归 task_publish_throttle_test.go。

package api

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// newPushTestEngine 造一个把推送通道上**每一帧**原样收下的引擎（快照与**实况汇总**都收）。
func newPushTestEngine(t testing.TB, clock *fakeClock) (*taskEngine, func() []RunPush) {
	t.Helper()
	var mu sync.Mutex
	var frames []RunPush
	e := newTaskEngine(taskEngineConfig{
		Store: newTaskTestStore(t),
		Publish: func(payload string) {
			event := runSnapshotEventPrefix
			if strings.HasPrefix(payload, runLiveEventPrefix) {
				event = runLiveEventPrefix
			}
			frame, ok := decodePushFrame(payload, event)
			if !ok {
				t.Errorf("推送通道上出现了解不开的载荷: %s", payload)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			frames = append(frames, frame)
		},
		RunBackground: func(fn func()) { fn() },
		Now:           clock.Now,
	})
	return e, func() []RunPush {
		mu.Lock()
		defer mu.Unlock()
		return append([]RunPush(nil), frames...)
	}
}

// liveFramesOf 挑出推送通道上的**实况汇总**帧。
func liveFramesOf(frames []RunPush) []RunPush {
	var live []RunPush
	for _, frame := range frames {
		if frame.Live != nil {
			live = append(live, frame)
		}
	}
	return live
}

// TestPushFramesChainToThePreviousFrame 守**投递链**：每一帧的 Prev 都是上一帧的序号。
//
// 这是前端认漏帧的唯一凭据。光看序号连不连续是不行的——逐条目进度按水位节流，被吞掉的那些
// 照样取了号，于是送达的序号本来就带空档；把「序号跳了」当成漏帧的话，一次繁忙的扫描会让界面
// 每收一帧就整份重拉一次。
func TestPushFramesChainToThePreviousFrame(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1700000000, 0)}
	e, collect := newPushTestEngine(t, clock)

	const key = "scan_library_1"
	progress := seedTask(t, e, scanSeed(1))
	// 同一窗口内连报 20 帧：绝大多数被水位吞掉，它们取走的序号因此不会出现在通道上。
	for i := 1; i <= 20; i++ {
		progress.Advance(i, 100, "", nil)
	}
	clock.advance(taskProgressPublishInterval * 2)
	progress.Phase("hashing", "", nil)
	settleSeededTask(t, e, key, nil)

	frames := collect()
	if len(frames) < 3 {
		t.Fatalf("推送通道上只有 %d 帧，用例需要至少 3 帧才谈得上链", len(frames))
	}
	if frames[0].Prev != 0 {
		t.Errorf("首帧的 Prev 为 %d, want 0", frames[0].Prev)
	}
	sawGap := false
	for i := 1; i < len(frames); i++ {
		if frames[i].Prev != frames[i-1].Sequence {
			t.Fatalf("第 %d 帧的 Prev 为 %d，上一帧的序号是 %d —— 链断了，前端会当成漏帧",
				i, frames[i].Prev, frames[i-1].Sequence)
		}
		if frames[i].Sequence <= frames[i-1].Sequence {
			t.Fatalf("第 %d 帧的序号 %d 没有比上一帧的 %d 大 —— 序号不再单调",
				i, frames[i].Sequence, frames[i-1].Sequence)
		}
		if frames[i].Sequence > frames[i-1].Sequence+1 {
			sawGap = true
		}
	}
	// 这一条把上面那段说明钉住：序号本来就会跳，所以判漏帧只能靠 Prev。
	if !sawGap {
		t.Error("被节流吞掉的那些帧没有在序号上留下空档 —— 用例没有覆盖到它要守的那种情形")
	}
}

// TestSnapshotFrameCarriesTheWholeRun 守推来的快照仍是**全量**的：不发增量帧。
//
// 增量的合并逻辑要前后端各维护一套，而合并错一个字段不会报错，只会让界面安静地停在错值上。
func TestSnapshotFrameCarriesTheWholeRun(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1700000000, 0)}
	e, collect := newPushTestEngine(t, clock)

	progress := seedTask(t, e, taskSeed{
		Key: "scan_library_1", Identity: libraryTask("scan_library", 1, variantSole),
		Total: 100, ScopeName: "甲库", CanCancel: true,
	})
	clock.advance(taskProgressPublishInterval * 2)
	progress.Advance(7, 100, "", nil)

	frames := collect()
	last := frames[len(frames)-1]
	if last.Run == nil {
		t.Fatalf("最后一帧不是运行快照: %+v", last)
	}
	// 一帧计数推进带着的是整条运行，不只是变了的那两个数。
	if last.Run.Key != "scan_library_1" || last.Run.Type != "scan_library" ||
		last.Run.ScopeName != "甲库" || last.Run.Total != 100 || last.Run.Current != 7 {
		t.Errorf("推来的不是全量快照: %+v", *last.Run)
	}
}

// TestLiveFrameCarriesSlotsAndCounts 守**实况汇总**帧的内容：槽位占用、活动数、排队数与全局暂停。
// 实况区据此不必自己从运行列表里数——浏览器手上那份列表丢过一帧就少一条。
func TestLiveFrameCarriesSlotsAndCounts(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1700000000, 0)}
	e, collect := newPushTestEngine(t, clock)
	e.slots = func() int { return 3 }

	seedTask(t, e, scanSeed(1))
	// 按运行 id 暂停在跑的那条：重复发起之后，同一个任务键上最近的那条已经是排在队里的运行。
	activeID := currentTask(t, e, "scan_library_1").RunID
	if _, err := trySeedTask(t, e, scanSeed(1)); err != errSeededRunQueued {
		t.Fatalf("重复发起返回 %v, want 停在排队中", err)
	}
	if err := e.pauseRun(activeID); err != nil {
		t.Fatalf("暂停失败: %v", err)
	}

	live := liveFramesOf(collect())
	if len(live) == 0 {
		t.Fatal("一帧实况汇总都没有出去")
	}
	last := *live[len(live)-1].Live
	if last.Active != 1 || last.Queued != 1 {
		t.Errorf("汇总的活动数 / 排队数为 %d / %d, want 1 / 1", last.Active, last.Queued)
	}
	if last.Slots != 3 {
		t.Errorf("汇总的槽位上限为 %d, want 3", last.Slots)
	}
	if !last.Paused {
		t.Error("有一条运行被暂停，汇总却说没有")
	}
	if last.PausedAll {
		t.Error("按下的是单条暂停，汇总却说全部暂停的闸门关着")
	}
}

// TestLiveFrameSkippedOnCounterAdvance 守**计数推进**不投汇总帧：一条运行从 3 报到 4
// 不改变盘上有几件事，跟着投等于把汇总也变成一路噪音，还每帧白查一次库。
func TestLiveFrameSkippedOnCounterAdvance(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1700000000, 0)}
	e, collect := newPushTestEngine(t, clock)

	progress := seedTask(t, e, scanSeed(1))
	before := len(liveFramesOf(collect()))
	for i := 1; i <= 5; i++ {
		clock.advance(taskProgressPublishInterval * 2) // 越过水位，让每一帧计数推进都真的被投出去
		progress.Advance(i, 100, "", nil)
	}
	if got := len(liveFramesOf(collect())) - before; got != 0 {
		t.Fatalf("5 次计数推进带出了 %d 帧实况汇总, want 0", got)
	}
}

// TestLiveFramePublishedWhenPauseAllHasNothingToPause 守「闸门关上了」这件事本身要发出去。
//
// 盘上一条运行都没有时按下全部暂停，没有任何一条运行会跃迁；不单独投这一帧的话，前端不知道
// 闸门关着——此后发起的运行会一直排在队里，而「全部恢复」是唯一能重新放开它的按钮。
func TestLiveFramePublishedWhenPauseAllHasNothingToPause(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1700000000, 0)}
	e, collect := newPushTestEngine(t, clock)

	if _, err := e.pauseAll(context.Background()); err != nil {
		t.Fatalf("全部暂停失败: %v", err)
	}

	live := liveFramesOf(collect())
	if len(live) != 1 {
		t.Fatalf("一条运行都没有时按下全部暂停投了 %d 帧汇总, want 1", len(live))
	}
	if !live[0].Live.PausedAll {
		t.Error("汇总帧没有说闸门关着")
	}
	if live[0].Live.Paused {
		t.Error("一条运行都没有，汇总却说有运行被暂停")
	}
}

// TestLiveFrameSummarizedOncePerBatch 守批量控制整批只算一次**实况汇总**。
//
// 「全部暂停」逐条按下，每条都是一次状态跃迁；每条都算一遍等于把同一个答案在锁内查 N 遍，
// 而中间那 N-1 个答案没有一个会被投出去——汇总变了也会被下一条盖掉。
func TestLiveFrameSummarizedOncePerBatch(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1700000000, 0)}
	e, collect := newPushTestEngine(t, clock)
	e.slots = func() int { return 4 }

	for id := int64(1); id <= 3; id++ {
		seedTask(t, e, scanSeed(id))
	}
	before := len(liveFramesOf(collect()))

	paused, err := e.pauseAll(context.Background())
	if err != nil {
		t.Fatalf("全部暂停失败: %v", err)
	}
	if paused != 3 {
		t.Fatalf("按下的条数为 %d, want 3", paused)
	}

	live := liveFramesOf(collect())
	if got := len(live) - before; got != 1 {
		t.Fatalf("三条运行的全部暂停投了 %d 帧汇总, want 1", got)
	}
	last := *live[len(live)-1].Live
	if last.Active != 3 || !last.Paused || !last.PausedAll {
		t.Errorf("整批收尾那一帧汇总没说清最终态: %+v", last)
	}
}

// TestLiveFramePublishedWhenSlotLimitChanges 守调大**运行槽位**上限之后界面上那个 N 会动。
// 队列是空的时候没有任何一条运行会跃迁，不单独投这一帧的话，「槽位 0/2」会一直挂到下一次跃迁。
func TestLiveFramePublishedWhenSlotLimitChanges(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1700000000, 0)}
	e, collect := newPushTestEngine(t, clock)
	limit := 2
	e.slots = func() int { return limit }

	e.releaseQueued()
	before := liveFramesOf(collect())
	if len(before) != 1 || before[0].Live.Slots != 2 {
		t.Fatalf("首次催队列应当投出一帧写着上限 2 的汇总，实得 %+v", before)
	}

	limit = 5
	e.releaseQueued()
	after := liveFramesOf(collect())
	if len(after) != 2 {
		t.Fatalf("上限改掉之后投了 %d 帧汇总, want 2", len(after))
	}
	if after[1].Live.Slots != 5 {
		t.Errorf("汇总里的槽位上限为 %d, want 5", after[1].Live.Slots)
	}

	// 上限没变时不该再投：汇总没变的帧一律不出去。
	e.releaseQueued()
	if got := len(liveFramesOf(collect())); got != 2 {
		t.Errorf("上限没变时又投了汇总帧，累计 %d 帧, want 2", got)
	}
}
