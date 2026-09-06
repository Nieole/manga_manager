// 本包唯一 seam 的共用测试装置：seam 是构造点 New，注入落盘端口、投递函数、开后台 goroutine
// 的能力与可控时钟。把后台能力换成同步执行版后，**终态**在调用返回时就已落定，不必 sleep
// 或轮询——契约用例因此是毫秒级的，也不需要数据库、配置或扫描器。
// 观测面固定为投递出去的载荷，不断言内部字段布局；本文件不含用例。

package task

import (
	"context"
	"sync"
	"testing"
	"time"

	"manga-manager/internal/runhandle"
)

// fakeClock 让节流的时序断言可控。固定 sleep 的用例既慢，又杀不掉
// 「水位只在首次写入、之后再不更新」这类错误实现。
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{now: time.Unix(1700000000, 0)} }

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
// 就等于「每一帧都恰好落在窗口之外」。它测的是水位认不认这个注入的时钟——任务体里若还留着
// 一层按墙上时钟计时的节流，这个时钟怎么走都撬不动它。
type steppingClock struct {
	clock *fakeClock
	step  time.Duration
}

func (c *steppingClock) Now() time.Time {
	now := c.clock.Now()
	c.clock.advance(c.step)
	return now
}

// runBodySynchronously 是「同步执行」版的后台能力：任务体在启动调用返回前就跑完。
func runBodySynchronously(fn func()) { fn() }

// registerOnly 是「只登记不执行」版的后台能力：任务体一步都不跑，运行因此停在**活动态**，
// 可暂停、可取消、可上报。
func registerOnly(func()) {}

// testEngine 是一个后台能力与时钟都可控的引擎，外加它的落盘端口与投递记录。
type testEngine struct {
	engine *Engine
	store  *memStore
	clock  *fakeClock

	mu        sync.Mutex
	published []Snapshot
}

// newTestEngine 造一台引擎，时钟不动：投递窗口内展示态一字未变的那些帧都该被吞掉。
// run 决定任务体何时、乃至是否执行；slots 为 0 时取默认槽位数。
func newTestEngine(t *testing.T, run func(func()), slots int) *testEngine {
	t.Helper()
	return newHarness(t, run, func() int { return slots }, false)
}

// newSteppingTestEngine 造一台时钟每读一次就跨过一个投递窗口的引擎：每一帧都该被放行。
func newSteppingTestEngine(t *testing.T, run func(func()), slots int) *testEngine {
	t.Helper()
	return newHarness(t, run, func() int { return slots }, true)
}

// newSlotTunableTestEngine 造一台上限随时可改的引擎，供「改配置对新的放行生效」那条用例驱动。
// 上限收在一个指针后面而不是重建引擎：重建等于重启进程，那条用例要证的恰恰是不必重启。
func newSlotTunableTestEngine(t *testing.T, run func(func()), slots *int) *testEngine {
	t.Helper()
	return newHarness(t, run, func() int { return *slots }, false)
}

// newHarness 是两个构造点的共同实现。
//
// **磁盘作业**入口一律留 nil：本包的契约用例一次盘都不读。要读盘的用例必须自己交出真 runner，
// 留 nil 的后果见 runhandle.New。
func newHarness(t *testing.T, run func(func()), slots func() int, stepping bool) *testEngine {
	t.Helper()
	harness := &testEngine{store: newMemStore(), clock: newFakeClock()}
	now := harness.clock.Now
	if stepping {
		now = (&steppingClock{clock: harness.clock, step: PublishInterval}).Now
	}
	harness.engine = New(Config{
		Store:         harness.store,
		Publish:       harness.record,
		RunBackground: run,
		Now:           now,
		Slots:         slots,
		ControlCodes: ControlCodes{
			Paused:     "task.msg.control.paused",
			Resumed:    "task.msg.control.resumed",
			Cancelling: "task.msg.control.cancelling",
			Panicked:   "task.msg.control.panicked",
		},
	})
	return harness
}

func (h *testEngine) record(snapshot Snapshot) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.published = append(h.published, snapshot)
}

func (h *testEngine) snapshots() []Snapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]Snapshot(nil), h.published...)
}

// start 发起一次运行并要求它被接纳；被闸门挡下即 t.Fatal。
func (h *testEngine) start(t *testing.T, spec RunSpec, body Body) Run {
	t.Helper()
	launched, err := h.engine.Start(context.Background(), spec, body)
	if err != nil {
		t.Fatalf("发起运行失败: %v", err)
	}
	return launched.Run
}

// load 取回落盘端口里那条运行；取不到即 t.Fatal。断言状态时读它，而不是读发起时的返回值——
// 那份是发起那一刻的快照，之后的每一次变化都不在里面。
func (h *testEngine) load(t *testing.T, runID int64) Run {
	t.Helper()
	run, err := h.store.LoadRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("运行 %d 读不回来: %v", runID, err)
	}
	return run
}

// idleBody 是什么都不做、正常返回的任务体。
func idleBody(context.Context, *runhandle.Handle) (Result, error) { return Result{}, nil }

// libraryScanSpec 是一份最小可用的运行声明：库级作用域、手动发起、可暂停可取消。
func libraryScanSpec(libraryID int64) RunSpec {
	return RunSpec{
		Identity:  Identity{Type: "scan_library", Scope: ScopeLibrary, ScopeID: libraryID, Variant: VariantPrimary},
		Trigger:   TriggerManual,
		Total:     100,
		CanCancel: true,
		CanPause:  true,
	}
}

// lastPublishedFor 返回这条运行最后一帧被投递出去的载荷。
func lastPublishedFor(t *testing.T, snapshots []Snapshot, runID int64) Snapshot {
	t.Helper()
	for i := len(snapshots) - 1; i >= 0; i-- {
		if snapshots[i].Run.ID == runID {
			return snapshots[i]
		}
	}
	t.Fatalf("运行 %d 一帧都没被投递出去", runID)
	return Snapshot{}
}

// publishedCountFor 数这条运行被投递出去的帧数，供「这一帧该不该出去」的用例断言投递次数本身——
// 节流吞掉与句柄没交出去都表现为一条也不多。
func publishedCountFor(snapshots []Snapshot, runID int64) int {
	count := 0
	for _, snapshot := range snapshots {
		if snapshot.Run.ID == runID {
			count++
		}
	}
	return count
}

// publishedStatusesFor 按投递顺序取出这条运行经历过的状态，供状态机用例断言跃迁路径本身。
func publishedStatusesFor(snapshots []Snapshot, runID int64) []RunStatus {
	var statuses []RunStatus
	for _, snapshot := range snapshots {
		if snapshot.Run.ID == runID {
			statuses = append(statuses, snapshot.Run.Status)
		}
	}
	return statuses
}
