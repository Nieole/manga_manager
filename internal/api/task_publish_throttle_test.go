// 守逐条目任务进度的 SSE 节流：窗口内展示态不变的纯计数推进要被吞，阶段/文案跃迁与终态、
// 控制路径一律无条件放行，而内存状态无论是否投递都得是最新的。
//
// 没有节流，重建文件标识这类任务对**每本书**回调一次、一次回调连着两次引擎调用，每次都在锁内
// json.Marshal 并投递一条全量快照：5 万本书就是 10 万次锁内序列化与 10 万条 SSE，按实测载荷约
// 1.1KB/条算是 110MB 出网，而且是**每个打开的浏览器标签**各一份。

package api

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// newThrottleTestEngine 造一个只统计投递条数的引擎，时钟由 fakeClock 注入。
func newThrottleTestEngine(clock *fakeClock) (*taskEngine, *[]string) {
	var published []string
	var mu sync.Mutex
	e := newTaskEngine(nil, func(payload string) {
		mu.Lock()
		defer mu.Unlock()
		published = append(published, payload)
		// 后台能力取同步执行版：本文件不启动任务体，只是不留 nil 给后来者踩。
	}, nil, func(fn func()) { fn() }, nil)
	e.now = clock.Now
	return e, &published
}

func TestTaskProgressPublishThrottle(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1700000000, 0)}
	e, published := newThrottleTestEngine(clock)

	const key = "rebuild_file_identities"
	progress := seedTask(t, e, taskSeed{Key: key, Type: "rebuild_file_identities", Total: 1000})
	startCount := len(*published)
	if startCount != 1 {
		t.Fatalf("启动应当投递 1 条，实际 %d", startCount)
	}

	// 首帧：phase 由 "" 跃迁到 "hashing"，展示态变了，必须放行。
	progress.Phase("hashing", "", nil)
	if got := len(*published) - startCount; got != 1 {
		t.Fatalf("首帧应当放行，实际投递 %d 条", got)
	}

	// 同一窗口内、展示态不变的纯**计数推进**：全部吞掉。
	for i := 1; i <= 50; i++ {
		progress.Advance(i, 1000, "", nil)
	}
	if got := len(*published) - startCount; got != 1 {
		t.Fatalf("窗口内的纯计数推进应当被吞，实际投递 %d 条", got)
	}

	// 但内存状态必须是最新的——节流跳过的是投递，不是状态更新。
	e.mutex.Lock()
	current := e.tasks[key].Current
	e.mutex.Unlock()
	if current != 50 {
		t.Fatalf("被节流期间内存进度停在 %d，应为 50 —— 节流不该跳过状态更新", current)
	}

	// 越过窗口：放行一条，且必须带上累积后的最新值（载荷是全量快照）。
	clock.advance(taskProgressPublishInterval * 2)
	progress.Advance(51, 1000, "", nil)
	if got := len(*published) - startCount; got != 2 {
		t.Fatalf("越过窗口应当再放行 1 条，实际共 %d 条", got)
	}
	if last := (*published)[len(*published)-1]; !strings.Contains(last, `"current":51`) {
		t.Fatalf("放行的载荷没有带上最新进度：%s", last)
	}
}

// TestTaskProgressPublishesOnDisplayChange 守卫「阶段/文案跃迁不被吞」。
// 阶段名是用户在等的语义变化，被计数器节流吞掉会让气泡长时间停在过期阶段上。
func TestTaskProgressPublishesOnDisplayChange(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1700000000, 0)}
	e, published := newThrottleTestEngine(clock)

	const key = "rebuild_thumbnails"
	progress := seedTask(t, e, taskSeed{Key: key, Type: "rebuild_thumbnails", Total: 100})
	base := len(*published)

	// 同一毫秒内连发三帧，但每帧的 phase 都不同：三帧都必须放行。
	for _, phase := range []string{"clearing_cache", "scanning", "writing"} {
		progress.Phase(phase, "", nil)
	}
	if got := len(*published) - base; got != 3 {
		t.Fatalf("阶段跃迁被节流吞掉了：应投递 3 条，实际 %d", got)
	}
}

// TestTerminalAndControlPublishesAreNeverThrottled 守卫「终态与控制路径无条件投递」。
func TestTerminalAndControlPublishesAreNeverThrottled(t *testing.T) {
	cases := []struct {
		name   string
		finish func(e *taskEngine, key string)
	}{
		// 完成与失败经引擎的终态裁决处落定，与任务体正常返回 / 返回错误时走的是同一条路。
		{"完成", func(e *taskEngine, key string) { settleSeededTask(e, key, nil) }},
		{"失败", func(e *taskEngine, key string) { settleSeededTask(e, key, errors.New("boom")) }},
		{"取消", func(e *taskEngine, key string) { _ = e.cancel(key) }},
		{"暂停", func(e *taskEngine, key string) { _ = e.pause(key) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clock := &fakeClock{now: time.Unix(1700000000, 0)}
			e, published := newThrottleTestEngine(clock)

			const key = "scan_library_1"
			progress := seedTask(t, e, taskSeed{Key: key, Type: "scan_library", Total: 100, CanCancel: true, CanPause: true})
			// 先把水位顶到「刚刚发布过」的状态。
			progress.Phase("scanning", "", nil)
			before := len(*published)

			// 时钟一动不动 —— 若终态也走节流，它会被窗口吞掉。
			tc.finish(e, key)
			if got := len(*published) - before; got != 1 {
				t.Fatalf("%s 应当无条件投递 1 条，实际 %d —— 用户会看到任务永远停在进行中", tc.name, got)
			}
		})
	}
}

// TestPublishGateClearedOnTerminal 守卫同名任务重跑：首帧不能被上一轮的残留水位吞掉。
func TestPublishGateClearedOnTerminal(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1700000000, 0)}
	e, published := newThrottleTestEngine(clock)

	const key = "scan_library_1"
	progress := seedTask(t, e, taskSeed{Key: key, Type: "scan_library", Total: 100})
	progress.Phase("scanning", "", nil)
	settleSeededTask(e, key, nil)

	e.mutex.Lock()
	_, stillGated := e.publishGates[key]
	e.mutex.Unlock()
	if stillGated {
		t.Fatal("终态后水位没被清掉 —— 同名任务重跑时首帧会被上一轮的水位吞掉")
	}

	// 时钟不动，同 key 重跑：首帧必须放行。
	before := len(*published)
	rerun := seedTask(t, e, taskSeed{Key: key, Type: "scan_library", Total: 100})
	rerun.Phase("scanning", "", nil)
	if got := len(*published) - before; got != 2 {
		t.Fatalf("重跑的启动 + 首帧应投递 2 条，实际 %d", got)
	}
}

// TestTaskProgressThrottleIsSlidingWindow 证明水位每次放行都会前移，
// 因而持续高频回调下的投递速率有确定上界（每窗口至多一条）。
//
// 这条能杀掉「水位只在首次写入、之后再不更新」的错误实现——那种实现在首条之后
// 会把所有后续帧都放行，节流形同虚设，而单靠「窗口内被吞」的用例是抓不到的。
func TestTaskProgressThrottleIsSlidingWindow(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1700000000, 0)}
	e, published := newThrottleTestEngine(clock)

	const key = "rebuild_book_hashes"
	progress := seedTask(t, e, taskSeed{Key: key, Type: "rebuild_book_hashes", Total: 10000})
	base := len(*published)

	// 模拟 1 秒钟内以 10ms 的间隔持续回调（100Hz，接近真实的哈希回填速率）。
	const step = 10 * time.Millisecond
	const span = time.Second
	for elapsed := time.Duration(0); elapsed < span; elapsed += step {
		progress.Advance(int(elapsed/step)+1, 10000, "", nil)
		clock.advance(step)
	}

	got := len(*published) - base
	// 1 秒 / 200ms 窗口 = 至多 5 条（首帧此时还没有水位可比，必然放行，故正好 5）。
	if got != 5 {
		t.Fatalf("1 秒内 100 次回调投递了 %d 条, want 5 —— 节流没有形成速率上界", got)
	}
	// 未节流时是 100 条：这一条把收益量化下来。
	if got >= 100 {
		t.Fatalf("投递条数与回调次数相当（%d），节流完全没生效", got)
	}
}
