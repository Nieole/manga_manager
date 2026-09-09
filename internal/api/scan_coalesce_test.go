// 守手动**强制**重扫被**合并**掉时端点如实回话：那次发起的任务体不会执行，跑的仍是先排上的
// 那条普通扫描，回一句「已发起」就是骗人。三条边界各一条用例——被合并要说清楚、队列空着时
// 照常新建并保住强制、自动发起被合并时不回这句话（没有人在等答案）。

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"manga-manager/internal/database"
	"manga-manager/internal/task"

	"github.com/go-chi/chi/v5"
)

// scanRoute 起一台挂着**真实路由**的测试服务器，并交回一个已登录的管理员会话。
//
// 用户看见的是那句 HTTP 回话，不是启动函数交回的结构体，因此断言要落在整条链路的出口上：
// 路径由 chi 按注册的那条模式匹配、路径参数由它解、查询串与 locale 由端点自己读、
// 响应体由 jsonResponse 自己编。直调 handler 配一份手搓的路由上下文验不到前两样。
//
// 会话不能省：建立首个管理员之前，鉴权闸门对**所有**非公开端点一律 401（见 authGate 那段
// 关于 setup 窗口的说明），而建管理员这一步顺带就登录了，回话里带着 CSRF 令牌。
func scanRoute(t *testing.T, c *Controller) (*httptest.Server, *http.Client, string) {
	t.Helper()
	r := chi.NewRouter()
	c.SetupRoutes(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	cl := newAuthClient(t)
	resp, data := authDo(t, cl, http.MethodPost, srv.URL+"/api/auth/setup", "",
		map[string]string{"username": "admin", "password": "password1"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("建管理员失败: %d %s", resp.StatusCode, data)
	}
	var session authSessionResponse
	if err := json.Unmarshal(data, &session); err != nil {
		t.Fatalf("解析登录响应失败: %v（原文 %s）", err, data)
	}
	return srv, cl, session.CSRFToken
}

// postScan 打一次扫描请求，交回状态码与解开的响应体。
func postScan(t *testing.T, srv *httptest.Server, cl *http.Client, csrf string, lib database.Library, force bool) (int, map[string]string) {
	t.Helper()
	url := srv.URL + "/api/libraries/" + strconv.FormatInt(lib.ID, 10) + "/scan?force=" + strconv.FormatBool(force)
	resp, data := authDo(t, cl, http.MethodPost, url, csrf, nil)

	var body map[string]string
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("解析扫描响应失败: %v（原文 %s）", err, data)
	}
	return resp.StatusCode, body
}

// queuedScanSeed 是一条守护扫描的播种声明：不带**强制**，与用户接下来点的那一次形成对照。
func queuedScanSeed(lib database.Library) taskSeed {
	return taskSeed{
		Key: scanKeyFor(lib), Identity: libraryTask("scan_library", lib.ID, variantSole),
		Trigger: task.TriggerScheduled, Total: 100,
		Metadata: map[string]string{"force": "false"},
	}
}

// TestForcedRescanSaysItWasCoalescedIntoTheQueuedScan 守票面那个用户故事：这个库已经排着一条
// 守护扫描时，用户点下的强制重扫会被**合并**进那一条，本次的声明与任务体一起被丢掉——
// 跑的仍是那条不带强制的普通扫描。端点回「已发起」，用户就会以为强制正在进行。
//
// 同一条用例顺带守「语义一个字不改」：排队那条的**发起方**与它自己的强制标记都不许被顶替。
func TestForcedRescanSaysItWasCoalescedIntoTheQueuedScan(t *testing.T) {
	controller, lib, _ := newAutomaticScanRig(t)
	srv, cl, csrf := scanRoute(t, controller)
	key := scanKeyFor(lib)

	// 活动一条 + 排队一条：队列上已经有一条守护扫描，下一次发起只能被合并。
	seedTask(t, controller.taskEngine, queuedScanSeed(lib))
	if _, err := trySeedTask(t, controller.taskEngine, queuedScanSeed(lib)); err != errSeededRunQueued {
		t.Fatalf("第二次播种返回 %v, want 停在排队中", err)
	}
	queuedBefore := currentTask(t, controller.taskEngine, key)

	code, body := postScan(t, srv, cl, csrf, lib, true)
	if code != http.StatusOK {
		t.Fatalf("强制重扫返回 %d: %v", code, body)
	}
	if body["status"] == "Scan initiated" {
		t.Fatalf("被合并掉的强制重扫仍回「已发起」：%v —— 用户以为强制在跑，实际跑的是那条普通扫描", body)
	}
	if body["message"] == "" || body["message"] == "library.scan.force_coalesced" {
		t.Fatalf("回话为 %q —— 要么没回，要么文案词条缺了一侧、apiText 把 key 原样交了出来", body["message"])
	}

	// 语义没变：合并仍是合并，排队那条的声明一个字不动。
	queuedAfter := currentTask(t, controller.taskEngine, key)
	if queuedAfter.RunID != queuedBefore.RunID || queuedAfter.CoalescedCount != 1 {
		t.Fatalf("这次发起没有并进排队那条：run_id=%d(want %d) coalesced=%d(want 1)",
			queuedAfter.RunID, queuedBefore.RunID, queuedAfter.CoalescedCount)
	}
	if queuedAfter.Trigger != string(task.TriggerScheduled) || queuedAfter.Params["force"] != "false" {
		t.Fatalf("排队那条被本次发起顶替了：trigger=%q force=%q",
			queuedAfter.Trigger, queuedAfter.Params["force"])
	}
}

// TestForcedRescanKeepsItsForceWhenNothingIsQueued 守窗口的另一头，也是绝大多数情况：
// 这个库正在扫、队列里却**没有**排队运行时，强制重扫照常新建一条排队运行，写下本次的声明，
// 强制完整保住。丢强制只发生在「队列里已经排着一条」那一种。
//
// 这一条必须与上一条一起读：只判「被合并了没有」而不守这一头的话，把每次强制发起都说成
// 被合并也能让上一条变绿，而用户从此再也发不起一次强制重扫。
//
// 这里要 newAutomaticScanRig 注入的那个停在可控点上的扫描器：本次新建的排队运行会在活动那条
// 让开之后真的开跑，跑的是真扫描器；不拦住它，断言就在跟一条正在收尾的运行赛跑。
func TestForcedRescanKeepsItsForceWhenNothingIsQueued(t *testing.T) {
	controller, lib, _ := newAutomaticScanRig(t)
	srv, cl, csrf := scanRoute(t, controller)
	key := scanKeyFor(lib)

	// 只占住活动那一格：队列是空的。
	seedTask(t, controller.taskEngine, queuedScanSeed(lib))

	code, body := postScan(t, srv, cl, csrf, lib, true)
	if code != http.StatusOK {
		t.Fatalf("强制重扫返回 %d: %v", code, body)
	}
	if body["status"] != "Scan initiated" {
		t.Fatalf("没被合并的强制重扫回了 %v, want 照常的「已发起」", body)
	}
	if body["message"] != "" {
		t.Fatalf("没被合并的强制重扫也回了那句合并的话: %q", body["message"])
	}

	queued := currentTask(t, controller.taskEngine, key)
	if queued.Status != "queued" || queued.CoalescedCount != 0 {
		t.Fatalf("强制重扫没有新建一条排队运行：status=%q coalesced=%d", queued.Status, queued.CoalescedCount)
	}
	if queued.Trigger != string(task.TriggerManual) {
		t.Fatalf("排队那条的发起方为 %q, want manual —— 它是用户刚点的那一次", queued.Trigger)
	}
	if queued.Params["force"] != "true" {
		t.Fatalf("排队那条的 force=%q, want \"true\" —— 用户点的强制在路上丢了：%v", queued.Params["force"], queued.Params)
	}
}

// TestOnlyAManualForcedScanGetsTheCoalescedNotice 守守护扫描与监听扫描被**合并**时不回这句话：
// 它们没有人在等答案——守护 tick 什么都不做，监听器按既有的约定出口重新排期。
//
// 断言落在判据本身而不是 HTTP 响应上，因为自动发起那两条路根本没有响应可断：定时那条由守护
// 循环叫起（见 dispatchScheduledScans），监听那条由 runWatchedLibraryScan 叫起，两者都不经过
// 任何 handler。「它们被合并时安安静静」由 TestScheduledScanQueuesBehindAManualScan 与
// TestWatchedScanReportsCoalescedInsteadOfWaitingForever 各守一半，这里守的是**谁有资格拿到
// 那句回话**——把发起方漏出判据是最容易犯的那一种，而它不会有编译错误。
func TestOnlyAManualForcedScanGetsTheCoalescedNotice(t *testing.T) {
	coalesced := task.Launched{Coalesced: true}
	cases := []struct {
		name     string
		launched task.Launched
		force    bool
		trigger  task.Trigger
		want     string
	}{
		{"手动强制被合并", coalesced, true, task.TriggerManual, "library.scan.force_coalesced"},
		{"守护扫描被合并", coalesced, true, task.TriggerScheduled, ""},
		{"监听扫描被合并", coalesced, true, task.TriggerWatch, ""},
		{"手动增量被合并", coalesced, false, task.TriggerManual, ""},
		{"手动强制没被合并", task.Launched{}, true, task.TriggerManual, ""},
	}

	for _, tc := range cases {
		if got := scanCoalescedNoticeCode(tc.launched, tc.force, tc.trigger); got != tc.want {
			t.Errorf("%s：回话文案码为 %q, want %q", tc.name, got, tc.want)
		}
	}
}
