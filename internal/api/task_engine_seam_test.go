// 本文件是任务引擎唯一 seam 的共用测试装置：该 seam 是构造点 `newTaskEngine`，注入落盘存储、
// SSE 投递、停机信号与「开一个受停机管辖的 goroutine」的能力。把后台能力换成同步执行版后，
// 任务终态在调用返回时就已落定，不必 sleep 或轮询去等真实 goroutine——契约用例能做到毫秒级、
// 且不需要数据库/配置/扫描器，原因即在此。观测面固定为投递出去的载荷，与 SSE 订阅者看到的
// 完全一致，不断言内部字段布局；本文件不含用例，只供同包的契约用例消费。

package api

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"manga-manager/internal/diskwork"
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

// steppingClock 每读一次就自己往前走一步。引擎每投递一帧读一次时钟，因此步长取一个投递窗口
// 就等于「每一帧都恰好落在窗口之外」。它测的是水位认不认这个注入的时钟——任务体里若还留着一层
// 按墙上时钟计时的节流，这个时钟怎么走都撬不动它。
type steppingClock struct {
	clock *fakeClock
	step  time.Duration
}

func (c *steppingClock) Now() time.Time {
	now := c.clock.Now()
	c.clock.advance(c.step)
	return now
}

// frozenClock 是不动的时钟：窗口内展示态一字未变的那些帧都该被吞掉。
func frozenClock() func() time.Time {
	return (&fakeClock{now: time.Unix(1700000000, 0)}).Now
}

// windowSteppingClock 每读一次跨过一个投递窗口：每一帧都该被放行。
func windowSteppingClock() func() time.Time {
	return (&steppingClock{clock: &fakeClock{now: time.Unix(1700000000, 0)}, step: taskProgressPublishInterval}).Now
}

// newBackgroundTestEngine 造一个后台能力可控的引擎：run 决定任务体何时、乃至是否执行。
//
// diskWork 是交给**任务句柄**的**磁盘作业**入口，与生产同样在构造期收下：多数用例的任务体一次盘
// 都不读，传 nil 即可；要读盘的用例必须在这里交出真 runner，留 nil 的后果见 taskrun.New。
func newBackgroundTestEngine(run func(func()), diskWork *diskwork.Runner) (*taskEngine, func() []TaskStatus) {
	var mu sync.Mutex
	var published []TaskStatus
	e := newTaskEngine(nil, func(payload string) {
		var task TaskStatus
		if err := json.Unmarshal([]byte(strings.TrimPrefix(payload, "task_progress:")), &task); err != nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		published = append(published, task)
	}, nil, run, diskWork)
	return e, func() []TaskStatus {
		mu.Lock()
		defer mu.Unlock()
		return append([]TaskStatus(nil), published...)
	}
}

// runTaskBodySynchronously 是「同步执行」版的后台能力：任务体在启动调用返回前就跑完。
func runTaskBodySynchronously(fn func()) { fn() }

// lastPublishedTask 返回该任务键最后一条被投递出去的快照。
func lastPublishedTask(t *testing.T, snapshots []TaskStatus, key string) TaskStatus {
	t.Helper()
	for i := len(snapshots) - 1; i >= 0; i-- {
		if snapshots[i].Key == key {
			return snapshots[i]
		}
	}
	t.Fatalf("任务 %q 一条快照都没被投递出去", key)
	return TaskStatus{}
}

// publishedCountFor 数一数该任务键被投递出去的快照条数，供「该不该投递这一条」的用例断言
// 投递次数本身——节流吞掉与句柄没交出去都表现为一条也不多。
func publishedCountFor(snapshots []TaskStatus, key string) int {
	count := 0
	for _, snapshot := range snapshots {
		if snapshot.Key == key {
			count++
		}
	}
	return count
}

// publishedTasksWithCode 按投递顺序取出该任务键带指定文案码的全部载荷。终态会改掉文案码，
// 所以中途那些帧只能这样取——lastPublishedTask 拿到的永远是收尾那一条。
// 「这一帧该不该出去」那类断言数的就是它的长度：节流吞掉与句柄没交出去都表现为一条也不多。
func publishedTasksWithCode(snapshots []TaskStatus, key, code string) []TaskStatus {
	var matched []TaskStatus
	for _, snapshot := range snapshots {
		if snapshot.Key == key && snapshot.MessageCode == code {
			matched = append(matched, snapshot)
		}
	}
	return matched
}

// publishedTaskWithCode 返回该任务键带指定文案码的最后一条快照；一条都没有即 t.Fatal。
func publishedTaskWithCode(t *testing.T, snapshots []TaskStatus, key, code string) TaskStatus {
	t.Helper()
	matched := publishedTasksWithCode(snapshots, key, code)
	if len(matched) == 0 {
		t.Fatalf("任务 %q 没有投递过任何带文案码 %q 的载荷", key, code)
		return TaskStatus{}
	}
	return matched[len(matched)-1]
}

// firstPublishedTask 返回该任务键**第一条**被投递出去的快照，用于断言任务诞生那一刻就已带齐
// 作用域、元数据与并发上限，不得拆成启动之后的多次独立写入、中间留下可被观察到的空窗。
func firstPublishedTask(t *testing.T, snapshots []TaskStatus, key string) TaskStatus {
	t.Helper()
	for _, snapshot := range snapshots {
		if snapshot.Key == key {
			return snapshot
		}
	}
	t.Fatalf("任务 %q 一条快照都没被投递出去", key)
	return TaskStatus{}
}
