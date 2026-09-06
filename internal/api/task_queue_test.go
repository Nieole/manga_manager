// 守接线之后的队列与槽位：闸门真的关得住（超限的第三条排队），上限真的来自设置（改了对新的
// 放行生效），**合并**计数真的发得出去，而「全部暂停」的闸门在实况帧上说得出口。
//
// 领域侧那套规则由 internal/task 的契约用例守（纯内存、可控时钟）。这里守的是**接线**：
// 上限从配置读到了引擎、排队与合并落到了对外契约上——断在这一段的话，界面做出来了看着就像成了，
// 而闸门其实没接上。

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"manga-manager/internal/config"
	"manga-manager/internal/runhandle"
	"manga-manager/internal/task"
)

// scanSeed 是一条库级扫描的播种声明：每个库一个身份，因此可以同时活着好几条。
func scanSeed(libraryID int64) taskSeed {
	return taskSeed{
		Key:       "scan_library_" + strconv.FormatInt(libraryID, 10),
		Identity:  libraryTask("scan_library", libraryID, variantSole),
		Total:     100,
		CanCancel: true,
		CanPause:  true,
	}
}

// setSlots 把这台 Controller 的并发上限改成 slots，与经设置页保存走同一条路（换掉配置快照）。
func setSlots(t testing.TB, c *Controller, slots int) {
	t.Helper()
	cfg := c.currentConfig()
	cfg.Tasks.RunSlots = slots
	c.config.Replace(&cfg)
}

// TestConfigDefaultSlotsMatchTheEngineDefault 守两个默认值相等：配置那个是「配置文件里没写」时
// 的默认，领域那个是「装配方什么都没说」时的兜底。错开一次，默认部署与默认引擎会给出两个分母。
//
// config 位于 task 的依赖下游，因此那边引用不了这个常数，只能在这里对一次。
func TestConfigDefaultSlotsMatchTheEngineDefault(t *testing.T) {
	if config.DefaultRunSlots != task.DefaultSlots {
		t.Fatalf("配置默认槽位 %d 与领域默认 %d 不一致", config.DefaultRunSlots, task.DefaultSlots)
	}
}

// TestSlotLimitComesFromTheRuntimeConfig 守上限是**从配置读**的，不是装配期定死的一个数：
// 改完设置立刻生效，不必重启进程。
func TestSlotLimitComesFromTheRuntimeConfig(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	if got := liveFrame(t, controller).Slots; got != config.DefaultRunSlots {
		t.Fatalf("默认配置下实况帧报出的槽位上限为 %d, want %d", got, config.DefaultRunSlots)
	}

	setSlots(t, controller, 5)
	if got := liveFrame(t, controller).Slots; got != 5 {
		t.Fatalf("改配置之后实况帧报出的槽位上限为 %d, want 5", got)
	}
}

// TestThirdRunStaysQueuedAtTheSlotLimit 是**闸门真的关得住**那条断言：上限为 2 时第三条进
// **排队中**，实况帧上占用是 2/2、排队 1。
//
// 它守的是接线，不是规则：上限没接上去的话，界面照样画得出「槽位 2/2」，而三条运行同时在跑。
func TestThirdRunStaysQueuedAtTheSlotLimit(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	setSlots(t, controller, 2)

	seedTask(t, controller.taskEngine, scanSeed(1))
	seedTask(t, controller.taskEngine, scanSeed(2))

	// 第三条经生产的启动入口发起：它是另一个身份，因此挡下它的只可能是槽位。
	third := scanSeed(3)
	if _, err := trySeedTask(t, controller.taskEngine, third); err != errSeededRunQueued {
		t.Fatalf("超出槽位的第三条发起返回 %v, want 停在排队中", err)
	}
	if got := currentTask(t, controller.taskEngine, third.Key).Status; got != "queued" {
		t.Fatalf("第三条运行的状态为 %q, want queued", got)
	}

	frame := liveFrame(t, controller)
	if frame.Active != 2 || frame.Queued != 1 || frame.Slots != 2 {
		t.Fatalf("实况帧为 active=%d queued=%d slots=%d, want 2/1/2", frame.Active, frame.Queued, frame.Slots)
	}
}

// TestRaisingTheSlotLimitAdmitsTheNextRun 守上限调大对**新的放行**生效，而不打断在跑的。
func TestRaisingTheSlotLimitAdmitsTheNextRun(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	setSlots(t, controller, 2)

	seedTask(t, controller.taskEngine, scanSeed(1))
	seedTask(t, controller.taskEngine, scanSeed(2))
	blocked := scanSeed(3)
	if _, err := trySeedTask(t, controller.taskEngine, blocked); err != errSeededRunQueued {
		t.Fatalf("上限为 2 时第三条发起返回 %v, want 停在排队中", err)
	}

	setSlots(t, controller, 4)
	// 调大之后新发起的那条当场开跑；已经排上的那条要等一次放行（收尾时才发生），不受这次发起影响。
	seedTask(t, controller.taskEngine, scanSeed(4))
	if got := currentTask(t, controller.taskEngine, "scan_library_4").Status; got != "running" {
		t.Fatalf("上限调大之后新发起的运行状态为 %q, want running", got)
	}
	if got := currentTask(t, controller.taskEngine, blocked.Key).Status; got != "queued" {
		t.Fatalf("调大上限动了已经排上的那条：状态为 %q", got)
	}
}

// TestSavingASmallerThenLargerLimitDrainsTheQueue 守调大上限那一刻队列真的往前走。
//
// 平时放行由收尾触发，而调大上限的那一刻没有任何运行收尾：不催一下的话，用户把上限从 2 调到 5
// 之后界面上什么都不会变，那条排队要等到某条正在跑的运行结束——可能是几小时之后。
func TestSavingASmallerThenLargerLimitDrainsTheQueue(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	setSlots(t, controller, 1)

	seedTask(t, controller.taskEngine, scanSeed(1))
	blocked := scanSeed(2)
	if _, err := trySeedTask(t, controller.taskEngine, blocked); err != errSeededRunQueued {
		t.Fatalf("上限为 1 时第二条发起返回 %v, want 停在排队中", err)
	}

	cfg := controller.currentConfig()
	cfg.Tasks.RunSlots = 3
	if err := controller.persistConfig(&cfg); err != nil {
		t.Fatalf("保存配置失败: %v", err)
	}

	// 断言「不再排队」而不是「正在跑」：放行是同步的（状态在锁内就写成了运行中），
	// 而它的任务体在另一条 goroutine 上，读到的可能已经是收尾之后的终态。
	if got := currentTask(t, controller.taskEngine, blocked.Key).Status; got == "queued" {
		t.Fatal("上限调大之后队列没往前走 —— 那条排队要等到某条在跑的运行结束才轮得到")
	}
}

// TestCoalescedCountReachesTheContract 守**合并**计数落到了对外契约上：没有它，用户看到的是
// 一条孤零零的排队，不知道它代表了几次发起。
func TestCoalescedCountReachesTheContract(t *testing.T) {
	e, _ := newBackgroundTestEngine(t, func(func()) {}, nil)

	const key = "scan_library_1"
	seedTask(t, e, scanSeed(1))
	for i := 0; i < 3; i++ {
		if _, err := trySeedTask(t, e, scanSeed(1)); err != errSeededRunQueued {
			t.Fatalf("第 %d 次重复发起返回 %v, want 停在排队中", i+2, err)
		}
	}

	queued := currentTask(t, e, key)
	if queued.Status != "queued" {
		t.Fatalf("重复发起之后最近那条运行的状态为 %q, want queued", queued.Status)
	}
	// 第一次重复建出排队那条（计数 0），后两次并进去。
	if queued.CoalescedCount != 2 {
		t.Fatalf("合并计数为 %d, want 2", queued.CoalescedCount)
	}

	payload, err := json.Marshal(queued)
	if err != nil {
		t.Fatalf("序列化运行快照失败: %v", err)
	}
	if !jsonHasField(payload, "coalesced_count") {
		t.Fatalf("载荷里没有 coalesced_count: %s", payload)
	}
}

// TestQueuedRunOmitsStartedAt 守**排队中**的运行不带开始时刻：发一个零值时刻出去的话，
// 运行详情里那格「开始时间」会写着它公元 1 年就开始了。
func TestQueuedRunOmitsStartedAt(t *testing.T) {
	e, _ := newBackgroundTestEngine(t, func(func()) {}, nil)

	seedTask(t, e, scanSeed(1))
	if _, err := trySeedTask(t, e, scanSeed(1)); err != errSeededRunQueued {
		t.Fatalf("重复发起返回 %v, want 停在排队中", err)
	}

	queued := currentTask(t, e, "scan_library_1")
	if queued.StartedAt != nil {
		t.Fatalf("排队中的运行带着开始时刻 %v —— 它还没开跑", *queued.StartedAt)
	}
	payload, err := json.Marshal(queued)
	if err != nil {
		t.Fatalf("序列化运行快照失败: %v", err)
	}
	if jsonHasField(payload, "started_at") {
		t.Fatalf("载荷里仍带着 started_at: %s", payload)
	}
	// 分母不存在，速率与 ETA 因此一个都不发——伪造成 0 会让界面画出一条 0/min 的曲线。
	if queued.RatePerMinute != 0 || queued.EtaSeconds != nil {
		t.Fatalf("排队中的运行带着速率 %.2f / ETA %v", queued.RatePerMinute, queued.EtaSeconds)
	}
}

// TestQueuedRunCanBeCancelledThroughTheEndpoint 守排队中的运行在界面上按得掉：它按**运行 id**
// 寻址，因此按下的是排队那张卡片上的取消，而不是同一个键上正在跑的那条。
func TestQueuedRunCanBeCancelledThroughTheEndpoint(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	seedTask(t, controller.taskEngine, scanSeed(1))
	activeID := currentTask(t, controller.taskEngine, "scan_library_1").RunID
	if _, err := trySeedTask(t, controller.taskEngine, scanSeed(1)); err != errSeededRunQueued {
		t.Fatalf("重复发起返回 %v, want 停在排队中", err)
	}
	queuedID := currentTask(t, controller.taskEngine, "scan_library_1").RunID
	if queuedID == activeID {
		t.Fatal("排队那条与在跑那条是同一条运行")
	}

	runID := strconv.FormatInt(queuedID, 10)
	rec := httptest.NewRecorder()
	controller.cancelRun(rec, requestWithRouteParam(http.MethodPost, "/api/system/runs/"+runID+"/cancel", nil, "runID", runID))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("取消排队中的运行返回 %d, body=%s", rec.Code, rec.Body.String())
	}

	snapshot, err := controller.taskEngine.engine.RunSnapshot(context.Background(), queuedID)
	if err != nil {
		t.Fatalf("取回被取消的运行失败: %v", err)
	}
	if snapshot.Run.Status != task.StatusCancelled {
		t.Fatalf("排队中被取消后状态为 %q, want cancelled", snapshot.Run.Status)
	}
	// 同一个键上在跑的那条一动不动：按运行寻址就是为了这条。
	active, err := controller.taskEngine.engine.RunSnapshot(context.Background(), activeID)
	if err != nil {
		t.Fatalf("取回在跑的运行失败: %v", err)
	}
	if active.Run.Status != task.StatusRunning {
		t.Fatalf("取消排队那条动到了在跑的那条：状态为 %q", active.Run.Status)
	}
}

// TestPauseAllGateReachesTheLiveFrame 守「全部暂停」的闸门发得到界面上。
//
// 闸门关着而被暂停的那几条已经被取消或跑完时，`paused` 回到 false 而队列仍被拦着——
// 界面只按 `paused` 决定「全部恢复」可不可按的话，那个按钮会灰在唯一能重新放开队列的位置上。
func TestPauseAllGateReachesTheLiveFrame(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	if liveFrame(t, controller).PausedAll {
		t.Fatal("没人按过全部暂停，实况帧却说闸门关着")
	}

	rec := httptest.NewRecorder()
	controller.pauseAllTasks(rec, httptest.NewRequest(http.MethodPost, "/api/system/tasks/pause-all", nil))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("全部暂停返回 %d, body=%s", rec.Code, rec.Body.String())
	}

	frame := liveFrame(t, controller)
	if !frame.PausedAll {
		t.Fatal("按下全部暂停之后实况帧没说闸门关着 —— 界面因此看不出队列被谁拦着")
	}
	if frame.Paused {
		t.Fatal("一条运行都没有，paused 却是 true —— 它答的是「有没有运行被暂停」")
	}

	rec = httptest.NewRecorder()
	controller.resumeAllTasks(rec, httptest.NewRequest(http.MethodPost, "/api/system/tasks/resume-all", nil))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("全部恢复返回 %d, body=%s", rec.Code, rec.Body.String())
	}
	if liveFrame(t, controller).PausedAll {
		t.Fatal("全部恢复之后闸门还关着")
	}
}

// TestPauseAllHoldsNewRunsInTheQueue 守闸门关着时新发起的运行进排队：只拦已经排上的那些的话，
// 一次新发起就能绕过它，盘照样转起来。
func TestPauseAllHoldsNewRunsInTheQueue(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	rec := httptest.NewRecorder()
	controller.pauseAllTasks(rec, httptest.NewRequest(http.MethodPost, "/api/system/tasks/pause-all", nil))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("全部暂停返回 %d", rec.Code)
	}

	if _, err := trySeedTask(t, controller.taskEngine, scanSeed(1)); err != errSeededRunQueued {
		t.Fatalf("全部暂停期间发起的运行返回 %v, want 停在排队中", err)
	}
	if got := currentTask(t, controller.taskEngine, "scan_library_1").Status; got != "queued" {
		t.Fatalf("全部暂停期间发起的运行状态为 %q, want queued", got)
	}
}

// TestCoalescedRunKeepsTheFirstBody 守被合并掉的那份任务体不会另跑一遍：排在前面那条跑的是
// 同一件事，各跑一遍等于同一个库被扫两次。
func TestCoalescedRunKeepsTheFirstBody(t *testing.T) {
	var pending []func()
	e, _ := newBackgroundTestEngine(t, func(fn func()) { pending = append(pending, fn) }, nil)

	bodies := 0
	body := func(context.Context, *runhandle.Handle) (TaskResult, error) {
		bodies++
		return TaskResult{}, nil
	}
	identity := libraryTask("scan_library", 1, variantSole)
	spec := RunSpec{Key: "scan_library_1", Total: 1, CanCancel: true}
	for i := 0; i < 4; i++ {
		if err := e.Run(identity, task.TriggerManual, spec, body); err != nil {
			t.Fatalf("第 %d 次发起失败: %v", i+1, err)
		}
	}

	// 依次放行：第一条收尾时腾出身份，排队那条接上；合并掉的两次不再有第三条。
	for i := 0; i < len(pending); i++ {
		pending[i]()
	}
	if bodies != 2 {
		t.Fatalf("任务体跑了 %d 遍, want 2（活动那条 + 排队那条，另外两次被合并掉）", bodies)
	}
}

// jsonHasField 判一份 JSON 对象里有没有这个顶层字段，供 omitempty 的断言使用。
func jsonHasField(payload []byte, field string) bool {
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return false
	}
	_, ok := decoded[field]
	return ok
}
