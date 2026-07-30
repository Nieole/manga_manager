// 业务说明：本文件守卫逐条目任务进度的 SSE 节流。
//
// 重建文件标识这类任务对**每本书**都回调一次，而一次回调里连着两次引擎调用
// （updateTaskDetailsMsg + mergeTaskParams），每次都在锁内 json.Marshal 并投递一条全量快照。
// 5 万本书就是 10 万次锁内序列化与 10 万条 SSE，按实测载荷约 1.1KB/条算是 110MB 出网，
// 而且是**每个打开的浏览器标签**各一份。
//
// 节流只跳过投递，不跳过内存状态更新；载荷是全量快照，被跳过期间的进度会由下一条带出去。

package api

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeClock 让节流的时序断言可控。固定 sleep 的用例既慢，又杀不掉
// 「水位只在首次写入、之后再不更新」这类错误实现。
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// newThrottleTestEngine 造一个只统计投递条数的引擎。
func newThrottleTestEngine(clock *fakeClock) (*taskEngine, *[]string) {
	var published []string
	var mu sync.Mutex
	e := newTaskEngine(nil, func(payload string) {
		mu.Lock()
		defer mu.Unlock()
		published = append(published, payload)
	}, nil)
	e.now = clock.Now
	return e, &published
}

func TestTaskProgressPublishThrottle(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1700000000, 0)}
	e, published := newThrottleTestEngine(clock)

	const key = "rebuild_file_identities"
	e.startTask(key, "rebuild_file_identities", "start", 1000)
	startCount := len(*published)
	if startCount != 1 {
		t.Fatalf("启动应当投递 1 条，实际 %d", startCount)
	}

	// 首帧：phase 由 "" 跃迁到 "hashing"，展示态变了，必须放行。
	e.updateTaskDetails(key, 1, 1000, "", "hashing", "", nil, nil)
	if got := len(*published) - startCount; got != 1 {
		t.Fatalf("首帧应当放行，实际投递 %d 条", got)
	}

	// 同一窗口内、展示态不变的纯计数器推进：全部吞掉。
	for i := 2; i <= 50; i++ {
		e.updateTaskDetails(key, i, 1000, "", "hashing", "", nil, nil)
	}
	if got := len(*published) - startCount; got != 1 {
		t.Fatalf("窗口内的纯计数器推进应当被吞，实际投递 %d 条", got)
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
	e.updateTaskDetails(key, 51, 1000, "", "hashing", "", nil, nil)
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
	e.startTask(key, "rebuild_thumbnails", "start", 100)
	base := len(*published)

	// 同一毫秒内连发三帧，但每帧的 phase 都不同：三帧都必须放行。
	for _, phase := range []string{"clearing_cache", "scanning", "writing"} {
		e.updateTaskDetails(key, 1, 100, "", phase, "", nil, nil)
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
		{"完成", func(e *taskEngine, key string) { e.finishTask(key, "done") }},
		{"失败", func(e *taskEngine, key string) { e.failTask(key, "boom") }},
		{"取消", func(e *taskEngine, key string) { _ = e.cancel(key) }},
		{"暂停", func(e *taskEngine, key string) { _ = e.pause(key) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clock := &fakeClock{now: time.Unix(1700000000, 0)}
			e, published := newThrottleTestEngine(clock)

			const key = "scan_library_1"
			e.startPausableCancelableTask(key, "scan_library", "scanning", 100)
			e.newTaskContext(key)
			// 先把水位顶到「刚刚发布过」的状态。
			e.updateTaskDetails(key, 1, 100, "", "scanning", "", nil, nil)
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
	e.startTask(key, "scan_library", "scanning", 100)
	e.updateTaskDetails(key, 1, 100, "", "scanning", "", nil, nil)
	e.finishTask(key, "done")

	e.mutex.Lock()
	_, stillGated := e.publishGates[key]
	e.mutex.Unlock()
	if stillGated {
		t.Fatal("终态后水位没被清掉 —— 同名任务重跑时首帧会被上一轮的水位吞掉")
	}

	// 时钟不动，同 key 重跑：首帧必须放行。
	before := len(*published)
	e.startTask(key, "scan_library", "scanning", 100)
	e.updateTaskDetails(key, 1, 100, "", "scanning", "", nil, nil)
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
	e.startTask(key, "rebuild_book_hashes", "start", 10000)
	base := len(*published)

	// 模拟 1 秒钟内以 10ms 的间隔持续回调（100Hz，接近真实的哈希回填速率）。
	const step = 10 * time.Millisecond
	const span = time.Second
	for elapsed := time.Duration(0); elapsed < span; elapsed += step {
		e.updateTaskDetails(key, int(elapsed/step)+1, 10000, "", "hashing", "", nil, nil)
		clock.advance(step)
	}

	got := len(*published) - base
	// 1 秒 / 200ms 窗口 = 至多 5 条（首帧因 phase 跃迁必然放行，故正好 5）。
	if got != 5 {
		t.Fatalf("1 秒内 100 次回调投递了 %d 条, want 5 —— 节流没有形成速率上界", got)
	}
	// 未节流时是 100 条：这一条把收益量化下来。
	if got >= 100 {
		t.Fatalf("投递条数与回调次数相当（%d），节流完全没生效", got)
	}
}
