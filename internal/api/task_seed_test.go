// 守「往库里放一条**运行**」这件事只能经启动入口发生，以及这套播种脚手架本身。直接插一行也能让
// 消费方跑绿，但准入闸门会就此在整片测试里失去覆盖——它如今是库上那两条部分唯一索引，因此播种
// 必须**同时穿过领域层与落盘层**。播种也不开第二条终态路径：**终态只有一处裁决**——任务体返回的
// 错误，所以播下的活动态任务是一个真的在跑、停在可控点上的任务体。

package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	"manga-manager/internal/runhandle"
)

// errSeededRunQueued 表示这次播种被准入闸门拦在了**排队中**：运行确实落地了（脚手架没有绕开
// 闸门），但任务体还没起，因此没有**运行句柄**可交。
//
// 它是脚手架自己的哨兵，不是生产错误：生产那边「撞上活动运行」如今是一次正常的排队，
// 启动入口返回 nil。用例要断言的是「重复播种没有变出第二条在跑的任务」，这个哨兵正是那句话。
var errSeededRunQueued = errors.New("seeded run is queued")

// taskSeed 描述一条要播下的任务。零值即「不可取消不可暂停、停在运行中」。
//
// 前半段字段刻意平铺而不是内嵌 RunSpec：内嵌能保证任务声明加字段时播种自动跟上，代价是
// 每个播种点都要多写一层 `RunSpec{...}`，而播种点有几十处，可读性是这里更值钱的东西。
// 代价则是 RunSpec 新增字段时本结构不会有编译错误提醒——加字段的人要顺手看一眼这里。
type taskSeed struct {
	Key string

	// Identity 是这条任务的**身份**，与生产同源：播种也得把四要素说全，谁都不从**任务键**反解。
	// 它没有可用的零值——不填就播下一条类型为空串的任务，消费方会以别的理由变红。
	Identity TaskIdentity

	Total     int
	CanCancel bool
	CanPause  bool

	// Metadata 落进重启入参，Labels 落进展示标签，ScopeName 是作用域的显示名。
	Metadata  map[string]string
	Labels    map[string]string
	ScopeName string
	Limits    TaskLimits

	StartCode   string
	StartParams map[string]string

	// Terminal 为空表示任务停在**活动态**：任务体真的在跑、停在可控点上，可暂停、可取消、
	// 可播报进度。否则取 completed / cancelled / failed 之一，播种返回时该**终态**已经落定。
	Terminal string

	// TerminalCode 与 TerminalParams 是终态文案；FailError 只在失败终态下生效，落进 RunStatus.Error。
	TerminalCode   string
	TerminalParams map[string]string
	FailError      string
}

// seededBody 是播下的任务体停在可控点上时交出来的两样：它自己的 ctx 与**运行句柄**。
type seededBody struct {
	ctx    context.Context
	handle *runhandle.Handle
}

// seededRun 是一条播下的任务在脚手架这一侧的把手：任务体停在哪、怎么让它收尾、收尾完了没有。
type seededRun struct {
	body seededBody
	// finish 让任务体带着送进来的错误返回。收尾只有这一条路，与生产同源。
	finish chan error
	// settled 在任务体返回、引擎写完终态之后关闭。
	settled chan struct{}
	once    sync.Once
}

// settle 让任务体带着 err 返回，并等引擎写完终态。重复调用是无操作。
func (r *seededRun) settle(err error) {
	r.once.Do(func() { r.finish <- err })
	<-r.settled
}

// seededRuns 按（引擎，任务键）记住播下的那些把手，供 settleSeededTask 与 seededTaskContext 取用。
//
// 每个用例各建一个引擎，因此键不会跨用例撞上。
var seededRuns sync.Map

type seedRef struct {
	engine *taskEngine
	key    string
}

// seedTask 播下一条任务并返回它的**运行句柄**；被准入闸门拒绝即 t.Fatal。
func seedTask(t testing.TB, e *taskEngine, seed taskSeed) *runhandle.Handle {
	t.Helper()
	handle, err := trySeedTask(t, e, seed)
	if err != nil {
		t.Fatalf("播种任务 %q 失败: %v", seed.Key, err)
	}
	return handle
}

// trySeedTask 与 seedTask 相同，但把闸门的拒绝作为错误返回，供「同一身份重复播种」这类用例断言。
//
// 它临时换掉引擎注入的后台运行能力，好在这一刻决定任务体跑在哪条 goroutine 上。这正是把
// 「开一个受停机管辖的 goroutine」做成注入依赖所买到的确定性：播种返回时任务体一定已经开跑并停在
// 可控点上（活动态），或者已经收尾完毕（终态）——不必 sleep 或轮询去等。
//
// **调用约束**：只能在测试自己的 goroutine 上、且此刻没有任何任务体在飞时调用。runBackground
// 属于引擎「装配期注入、之后只读」的那组字段，不受 mutex 保护，这里的换入换出因此不是并发安全的。
func trySeedTask(t testing.TB, e *taskEngine, seed taskSeed) (*runhandle.Handle, error) {
	t.Helper()

	// 这张表是终态裁决的逆：那边把任务体返回的错误翻成终态，这边把想要的终态翻回错误，
	// 好让播种和生产落进同一处裁决。改动任一侧都要照着另一侧看一眼——两边都不会有编译错误。
	var bodyErr error
	switch seed.Terminal {
	case "", "completed":
	case "cancelled":
		bodyErr = context.Canceled
	case "failed":
		bodyErr = errors.New(firstNonEmptyTaskValue(seed.FailError, "seeded failure"))
	default:
		return nil, fmt.Errorf("未知的终态 %q，可用：completed / cancelled / failed", seed.Terminal)
	}

	spec := RunSpec{
		Key:         seed.Key,
		StartCode:   seed.StartCode,
		StartParams: seed.StartParams,
		Total:       seed.Total,
		CanCancel:   seed.CanCancel,
		CanPause:    seed.CanPause,
		Metadata:    seed.Metadata,
		Labels:      seed.Labels,
		ScopeName:   seed.ScopeName,
		Limits:      seed.Limits,
	}
	result := TaskResult{Code: seed.TerminalCode, Params: seed.TerminalParams}

	run := &seededRun{finish: make(chan error, 1), settled: make(chan struct{})}
	started := make(chan seededBody, 1)
	// 终态播种：让任务体一进来就拿到它该返回的错误，于是播种返回时终态已经落定。
	if seed.Terminal != "" {
		run.once.Do(func() { run.finish <- bodyErr })
	}

	restore := e.runBackground
	e.runBackground = func(fn func()) {
		go func() {
			defer close(run.settled)
			fn()
		}()
	}
	err := e.Run(seed.Identity, spec, func(ctx context.Context, handle *runhandle.Handle) (TaskResult, error) {
		started <- seededBody{ctx: ctx, handle: handle}
		return result, <-run.finish
	})
	e.runBackground = restore
	if err != nil {
		return nil, err
	}
	// 播种被闸门拦在了**排队中**：运行落地了，任务体却还没起，因此没有句柄可交。
	//
	// 顺手把收尾用的错误备好：万一后续动作腾出了槽位把它放行，那个任务体会当场收尾，
	// 而不是卡在一个永远等不到的 finish 上，把放行它的那次调用连同用例一起吊死。
	if queued, lookupErr := e.latestRunByKey(context.Background(), seed.Key); lookupErr == nil && queued.Status == "queued" {
		run.once.Do(func() { run.finish <- context.Canceled })
		return nil, errSeededRunQueued
	}

	run.body = <-started
	if seed.Terminal != "" {
		<-run.settled
		return run.body.handle, nil
	}
	seededRuns.Store(seedRef{engine: e, key: seed.Key}, run)
	// 用例结束时必须让任务体退出，否则它连同它的 goroutine 一起留到进程结束。
	t.Cleanup(func() { run.settle(context.Canceled) })
	return run.body.handle, nil
}

// seededRunFor 取回这条任务键的把手；没有即 t.Fatal。
func seededRunFor(t testing.TB, e *taskEngine, key string) *seededRun {
	t.Helper()
	stored, ok := seededRuns.Load(seedRef{engine: e, key: key})
	if !ok {
		t.Fatalf("任务 %q 没有播下的任务体 —— 它没经启动入口播种，或播下时就是终态", key)
		return nil
	}
	return stored.(*seededRun)
}

// settleSeededTask 让一条 seedTask 播下、仍停在**活动态**的任务按「任务体返回了 err」收尾，
// 与生产共用同一处终态裁决。终态本身可以直接由 taskSeed.Terminal 播出来，这个函数是给那些
// 「收尾就是被测行为」的用例用的——它们要在播种与收尾之间插进别的动作。
func settleSeededTask(t testing.TB, e *taskEngine, key string, err error) {
	t.Helper()
	seededRunFor(t, e, key).settle(err)
}

// seededTaskContext 取出启动入口交给任务体的那个 ctx：可取消，带**暂停闸门**，并带着**任务键**
// 与运行标识。供那些要断言「取消真的传到了任务体」「暂停真的挡住了可中断点」的用例使用。
func seededTaskContext(t testing.TB, e *taskEngine, key string) context.Context {
	t.Helper()
	return seededRunFor(t, e, key).body.ctx
}

// TestSeedTaskGoesThroughTheAdmissionGate 守卫脚手架没有绕开准入闸门。
//
// 闸门如今的表现是**排队中**而不是一个错误：重复播种落下的是一条排着队、任务体没起的运行。
// 脚手架据此交出 errSeededRunQueued——它拿不到句柄，因为那条运行还没开跑。
func TestSeedTaskGoesThroughTheAdmissionGate(t *testing.T) {
	e, _ := newBackgroundTestEngine(t, runTaskBodySynchronously, nil)

	const key = "scan_library_1"
	seedTask(t, e, taskSeed{Key: key, Identity: libraryTask("scan_library", 1, variantSole), Total: 100})

	if _, err := trySeedTask(t, e, taskSeed{Key: key, Identity: libraryTask("scan_library", 1, variantSole)}); !errors.Is(err, errSeededRunQueued) {
		t.Fatalf("同一身份重复播种返回 %v, want errSeededRunQueued —— 脚手架绕过了闸门，整片测试就此失去这条覆盖", err)
	}

	settleSeededTask(t, e, key, errors.New("done"))
	// 上一条播种落下的那条排队运行在收尾时被放行、当场收尾，因此这里播下的又是一条全新的活动运行。
	if _, err := trySeedTask(t, e, taskSeed{Key: key, Identity: libraryTask("scan_library", 1, variantSole)}); err != nil {
		t.Fatalf("落定终态之后同一身份播不下去了: %v", err)
	}
}

// TestSeededActiveTaskIsControllable 守卫播下的活动态任务确实可控：启动入口登记了**运行时句柄**，
// 暂停与取消才有 ctx 与**暂停闸门**可操作。漏掉这一步的话，消费方拿到的是一条 pause 一律 409 的假任务。
func TestSeededActiveTaskIsControllable(t *testing.T) {
	e, _ := newBackgroundTestEngine(t, runTaskBodySynchronously, nil)

	const key = "scan_library_1"
	seedTask(t, e, taskSeed{Key: key, Identity: libraryTask("scan_library", 1, variantSole), Total: 100, CanCancel: true, CanPause: true})

	if err := pauseByKey(e, key); err != nil {
		t.Fatalf("暂停播下的任务失败: %v", err)
	}
	if err := resumeByKey(e, key); err != nil {
		t.Fatalf("恢复播下的任务失败: %v", err)
	}
	if err := cancelByKey(e, key); err != nil {
		t.Fatalf("取消播下的任务失败: %v", err)
	}
}

// TestSeedTaskLandsRequestedShape 守卫播种参数确实落到了任务上——消费方的 arrange 全靠它，
// 而一条形状不对的任务只会让它们以别的理由变红，排查要绕一大圈。
func TestSeedTaskLandsRequestedShape(t *testing.T) {
	cases := []struct {
		name       string
		terminal   string
		wantStatus string
	}{
		{"活动态", "", "running"},
		{"完成", "completed", "completed"},
		{"已取消", "cancelled", "cancelled"},
		{"失败", "failed", "failed"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, snapshots := newBackgroundTestEngine(t, runTaskBodySynchronously, nil)

			const key = "scan_library_7"
			seedTask(t, e, taskSeed{
				Key: key, Identity: libraryTask("scan_library", 7, variantSole), Total: 100,
				CanCancel: true, CanPause: true,
				ScopeName: "Main", Metadata: map[string]string{"force": "true"},
				Terminal: tc.terminal, FailError: "archive error",
			})

			task := lastPublishedTask(t, snapshots(), key)
			if task.Status != tc.wantStatus {
				t.Fatalf("播下的任务状态为 %q, want %q", task.Status, tc.wantStatus)
			}
			if task.Type != "scan_library" || task.Scope != "library" || task.ScopeID == nil || *task.ScopeID != 7 {
				t.Fatalf("类型与作用域不对：type=%q scope=%q id=%v", task.Type, task.Scope, task.ScopeID)
			}
			if task.Total != 100 || task.ScopeName != "Main" || task.Params["force"] != "true" {
				t.Fatalf("总数 / 作用域名 / 元数据不对：total=%d scopeName=%q params=%v", task.Total, task.ScopeName, task.Params)
			}
			if tc.wantStatus == "running" && (!task.CanCancel || !task.CanPause) {
				t.Fatalf("活动态任务没带上控制能力：cancel=%v pause=%v", task.CanCancel, task.CanPause)
			}
			if tc.wantStatus == "failed" && task.Error != "archive error" {
				t.Fatalf("失败原因为 %q, want archive error", task.Error)
			}
		})
	}
}

// TestSeedTaskRejectsUnknownTerminal 守卫拼错终态名不会被当成「留在活动态」静默放过——
// 那会让消费方对着一条还在跑的任务断言它已经结束。
func TestSeedTaskRejectsUnknownTerminal(t *testing.T) {
	e, _ := newBackgroundTestEngine(t, runTaskBodySynchronously, nil)

	if _, err := trySeedTask(t, e, taskSeed{Key: "scan_library_1", Identity: libraryTask("scan_library", 1, variantSole), Terminal: "done"}); err == nil {
		t.Fatal("未知的终态名被静默接受了")
	}
}

// TestSeededRunCarriesItsRunID 守卫播下的运行在对外快照上带着**运行标识**：
// 同一个任务键从此可以有多条运行，只认键的消费方分不出「哪一次」。
func TestSeededRunCarriesItsRunID(t *testing.T) {
	e, snapshots := newBackgroundTestEngine(t, runTaskBodySynchronously, nil)

	const key = "scan_library_1"
	seedTask(t, e, taskSeed{Key: key, Identity: libraryTask("scan_library", 1, variantSole), Terminal: "completed"})
	first := lastPublishedTask(t, snapshots(), key)
	seedTask(t, e, taskSeed{Key: key, Identity: libraryTask("scan_library", 1, variantSole), Terminal: "completed"})
	second := lastPublishedTask(t, snapshots(), key)

	if first.RunID <= 0 || second.RunID <= 0 {
		t.Fatalf("运行标识没发出来：first=%d second=%d", first.RunID, second.RunID)
	}
	if first.RunID == second.RunID {
		t.Fatalf("同一个任务键的两次运行拿到了同一个运行标识 %d —— 重试又把上一次盖掉了", first.RunID)
	}
}

// ---- 按**任务键**寻址的控制动作（仅用例） ----
//
// 生产按**运行 id** 寻址（见 taskEngine.pauseRun）：同一个键此刻可以有两条仍会变化的运行，
// 键答不出用例按的是哪一条。而用例手里往往只有键，因此在这里补一道解析：取这个键最近的那一次
// 运行再控制它——正是这三个方法从生产里搬走之前的口径。要指名道姓控制某一条的用例，
// 直接调 pauseRun / resumeRun / cancelRun。

func controlByKey(e *taskEngine, key string, action func(int64) error) error {
	run, err := e.latestRunByKey(context.Background(), key)
	if err != nil {
		return err
	}
	return action(run.ID)
}

func pauseByKey(e *taskEngine, key string) error  { return controlByKey(e, key, e.pauseRun) }
func resumeByKey(e *taskEngine, key string) error { return controlByKey(e, key, e.resumeRun) }
func cancelByKey(e *taskEngine, key string) error { return controlByKey(e, key, e.cancelRun) }

// runControlRequest 向暂停 / 恢复 / 取消三个端点之一发一次请求，寻址用这个**任务键**最近那次
// 运行的 id——端点按运行 id 寻址（见 Controller.pauseRun），而用例手里往往只有键。
func runControlRequest(t testing.TB, c *Controller, handler http.HandlerFunc, key string) *httptest.ResponseRecorder {
	t.Helper()
	run, err := c.taskEngine.latestRunByKey(context.Background(), key)
	if err != nil {
		t.Fatalf("任务键 %q 取不到运行: %v", key, err)
	}
	runID := strconv.FormatInt(run.ID, 10)
	req := requestWithRouteParam(http.MethodPost, "/api/system/runs/"+runID+"/control", nil, "runID", runID)
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}
