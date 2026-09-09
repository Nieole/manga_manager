// 守重试与禁用 / 启用三条端点在**真路由**上各取各的路径参数。
//
// 三条挂在同一段路径的同一个位置上（`/api/system/tasks/{…}/动词`），只在末段动词上分岔。
// 路由里写的参数名与 handler 读的那个一旦错开，chi 交出来的是空串，而 handler 拿着空串照样往下走——
// 直接调 handler 的用例把参数硬塞进路由上下文，正好绕开了这个缝，只有整棵路由树走一遍才看得见。

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"

	"manga-manager/internal/database"
)

// taskRouteRig 是一台起在**真路由**上、已登录管理员的服务器。
//
// 走真会话而不是绕过鉴权：这三条端点都在 /api 组里，改写方法还要 CSRF——
// 绕过去的话，用例守住的就不是用户真正打到的那条路径。
type taskRouteRig struct {
	controller *Controller
	server     *httptest.Server
	client     *http.Client
	csrf       string
}

func newTaskRouteRig(t *testing.T) *taskRouteRig {
	t.Helper()
	c, store, _, _ := newTestController(t)
	_ = mkTestUser(t, store, "admin", database.RoleAdmin)

	router := chi.NewRouter()
	c.SetupRoutes(router)
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)

	cl := newAuthClient(t)
	resp, data := authDo(t, cl, http.MethodPost, srv.URL+"/api/auth/login", "",
		map[string]string{"username": "admin", "password": "password1"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("管理员登录返回 %d, want 200: %s", resp.StatusCode, data)
	}
	var session authSessionResponse
	if err := json.Unmarshal(data, &session); err != nil {
		t.Fatalf("解登录响应失败: %v (raw=%s)", err, data)
	}
	return &taskRouteRig{controller: c, server: srv, client: cl, csrf: session.CSRFToken}
}

// post 经真路由打一次任务端点，交回状态码与响应体。
func (r *taskRouteRig) post(t *testing.T, path string) (int, string) {
	t.Helper()
	resp, data := authDo(t, r.client, http.MethodPost, r.server.URL+path, r.csrf, nil)
	return resp.StatusCode, string(data)
}

// TestTaskEndpointsTakeTheirOwnPathParam 钉住重试与禁用 / 启用在真路由上各取各的参数：
// 三条都按**任务 id** 寻址，同一个数字打到哪条动词就只做哪一件事。
//
// 破了的表现是用户点重试，任务被禁用了；或者反过来——点重试什么都没发生，界面上却弹了成功。
func TestTaskEndpointsTakeTheirOwnPathParam(t *testing.T) {
	rig := newTaskRouteRig(t)

	seedTask(t, rig.controller.taskEngine, taskSeed{
		Key: "scan_series_999", Identity: seriesTask("scan_series", 999, variantSole), Total: 1,
		Terminal: "failed",
	})
	taskID := summaryOf(t, rig.controller).TaskID
	path := "/api/system/tasks/" + strconv.FormatInt(taskID, 10)

	// 重试：只发起新一次运行，一个字都不动那个任务的自动发起开关。
	if code, body := rig.post(t, path+"/retry"); code != http.StatusAccepted {
		t.Fatalf("按任务 id 重试返回 %d, want 202: %s —— 重试没走到它自己那个路径参数上", code, body)
	}
	if summaryOf(t, rig.controller).Disabled {
		t.Fatal("重试之后任务被禁用了 —— 这一下打到了禁用那条端点上")
	}

	// 禁用 / 启用：同一个数字、同一段路径，只翻自动发起那个开关。
	if code, body := rig.post(t, path+"/disable"); code != http.StatusAccepted {
		t.Fatalf("禁用返回 %d, want 202: %s", code, body)
	}
	if !summaryOf(t, rig.controller).Disabled {
		t.Fatal("禁用之后任务没被禁用 —— 这一下打到了重试那条端点上")
	}
	if code, body := rig.post(t, path+"/enable"); code != http.StatusAccepted {
		t.Fatalf("启用返回 %d, want 202: %s", code, body)
	}
	if summaryOf(t, rig.controller).Disabled {
		t.Fatal("启用之后任务仍是禁用的")
	}
}

// TestRetryRejectsATaskKeyInThePath 钉住**任务键**不再是重试的寻址方式：把它填进路径的旧客户端
// 拿到的是 400，而不是静默重试了别的什么。
func TestRetryRejectsATaskKeyInThePath(t *testing.T) {
	rig := newTaskRouteRig(t)

	seedTask(t, rig.controller.taskEngine, taskSeed{
		Key: "scan_series_999", Identity: seriesTask("scan_series", 999, variantSole), Total: 1,
		Terminal: "failed",
	})

	code, body := rig.post(t, "/api/system/tasks/scan_series_999/retry")
	if code != http.StatusBadRequest {
		t.Fatalf("拿**任务键**重试返回 %d, want 400: %s", code, body)
	}
}
