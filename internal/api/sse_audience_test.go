// 守「任务快照只给管理员」这条不变量在实时事件流上的落地：任务列表接口对普通用户是 403，
// 而同一份快照（作用域显示名、任务参数、失败原因里的宿主机绝对路径）若经 /api/events
// 原样推给普通用户，那条 403 就等于没有。普通用户仍须收到刷新帧，否则修泄露换来功能故障。

package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"manga-manager/internal/database"
	"manga-manager/internal/taskrun"
)

// sseProbeEvent 用于确认连接已进入 broker 的订阅表，sseSentinelEvent 用于收口一轮断言。
// 二者都经 PublishEvent 投递，因此对任何角色都可见——哨兵自己被过滤掉的话，断言就永远等不到头。
const (
	sseProbeEvent    = "sse-audience-probe"
	sseSentinelEvent = "sse-audience-sentinel"
)

// eventStream 是一条已连上的 /api/events 长连接。
type eventStream struct {
	frames <-chan string
	stop   func()
}

func openEventStream(t *testing.T, cl *http.Client, baseURL string) *eventStream {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/events", nil)
	if err != nil {
		cancel()
		t.Fatalf("构造 /api/events 请求失败: %v", err)
	}
	resp, err := cl.Do(req)
	if err != nil {
		cancel()
		t.Fatalf("订阅 /api/events 失败: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		cancel()
		t.Fatalf("/api/events 应 200，实得 %d", resp.StatusCode)
	}
	frames := make(chan string, 256)
	go func() {
		defer close(frames)
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			select {
			case frames <- strings.TrimPrefix(line, "data: "):
			case <-ctx.Done():
				return
			}
		}
	}()
	stream := &eventStream{frames: frames, stop: func() { cancel(); resp.Body.Close() }}
	t.Cleanup(stream.stop)
	return stream
}

// waitReady 反复投递探针帧直到收到，以此确认本连接已完成订阅登记：
// broker 的注册走无缓冲 channel，响应头先于注册写出，只等「读到响应头」会抢跑。
func (s *eventStream) waitReady(t *testing.T, c *Controller) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		c.PublishEvent(sseProbeEvent)
		select {
		case frame, open := <-s.frames:
			if !open {
				t.Fatal("SSE 连接在完成订阅登记前就断开了")
			}
			if frame == sseProbeEvent {
				return
			}
		case <-time.After(20 * time.Millisecond):
		case <-deadline:
			t.Fatal("SSE 连接未在超时内完成订阅登记")
		}
	}
}

// collectUntilSentinel 投递一枚尾哨兵并收齐它之前的全部帧：broker 是单 goroutine 顺序投递，
// 收到哨兵即证明先于它入列的帧都已投完，于是「没收到」是确定结论而不是等得不够久。
func (s *eventStream) collectUntilSentinel(t *testing.T, c *Controller) []string {
	t.Helper()
	c.PublishEvent(sseSentinelEvent)
	var got []string
	deadline := time.After(5 * time.Second)
	for {
		select {
		case frame, open := <-s.frames:
			if !open {
				t.Fatal("SSE 连接提前断开")
			}
			if frame == sseSentinelEvent {
				return got
			}
			if frame == sseProbeEvent {
				continue // waitReady 多投的探针
			}
			got = append(got, frame)
		case <-deadline:
			t.Fatalf("等待尾哨兵超时，已收到 %v", got)
		}
	}
}

// sseAudienceRig 是一套「管理员 + 普通用户」的真实路由装配。
type sseAudienceRig struct {
	controller *Controller
	server     *httptest.Server
}

func newSSEAudienceRig(t *testing.T) *sseAudienceRig {
	t.Helper()
	c, store, _, _ := newTestController(t)
	ctx := context.Background()
	for _, account := range []struct {
		username string
		role     string
	}{
		{"boss", database.RoleAdmin},
		{"member", database.RoleRegular},
	} {
		hash, err := hashPassword("password1")
		if err != nil {
			t.Fatalf("hashPassword: %v", err)
		}
		if _, err := store.CreateUser(ctx, database.CreateUserParams{
			Username: account.username, PasswordHash: hash, Role: account.role,
		}); err != nil {
			t.Fatalf("建账号 %q 失败: %v", account.username, err)
		}
	}
	c.auth.markUsersExist()
	// newTestController 不起后台服务，broker 的事件循环得自己拉起来，
	// 否则订阅登记走的无缓冲 channel 会把请求 goroutine 挂死。
	c.runBackground(func() { c.sse.run(c.lifecycleDone()) })

	router := chi.NewRouter()
	c.SetupRoutes(router)
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return &sseAudienceRig{controller: c, server: srv}
}

// loginClient 返回一个已持有该账号会话 Cookie 的客户端。
func (r *sseAudienceRig) loginClient(t *testing.T, username string) *http.Client {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	cl := &http.Client{Jar: jar}
	body, _ := json.Marshal(map[string]string{"username": username, "password": "password1"})
	resp, err := cl.Post(r.server.URL+"/api/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("%s 登录失败: %v", username, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s 登录应 200，实得 %d", username, resp.StatusCode)
	}
	return cl
}

// runFailingTask 跑一个必然失败的任务，让引擎投出带宿主机绝对路径的失败快照，并等它进入终态。
func (r *sseAudienceRig) runFailingTask(t *testing.T, secretPath string) {
	t.Helper()
	const key = "scan_library_7"
	err := r.controller.taskEngine.Run(TaskSpec{
		Key:       key,
		Type:      "scan_library",
		ScopeName: "资料库A",
		Metadata:  map[string]string{"library_path": secretPath},
	}, func(context.Context, *taskrun.Handle) (TaskResult, error) {
		return TaskResult{}, &taskTestError{msg: secretPath + ": permission denied"}
	})
	if err != nil {
		t.Fatalf("启动任务失败: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		r.controller.taskEngine.mutex.Lock()
		task, ok := r.controller.taskEngine.tasks[key]
		r.controller.taskEngine.mutex.Unlock()
		if ok && !taskIsActive(task.Status) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("任务未在超时内进入终态")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

type taskTestError struct{ msg string }

func (e *taskTestError) Error() string { return e.msg }

// TestServerEventsFiltersTaskFramesByRole 是这条不变量的判据：
// 普通用户订阅到的帧里不得出现任何任务快照，管理员则必须照收不误。
func TestServerEventsFiltersTaskFramesByRole(t *testing.T) {
	const secretPath = "/srv/private-manga/资料库A/秘密.cbz"

	for _, tc := range []struct {
		name          string
		username      string
		wantTaskFrame bool
	}{
		{name: "普通用户收不到任务快照", username: "member", wantTaskFrame: false},
		{name: "管理员照收任务快照", username: "boss", wantTaskFrame: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rig := newSSEAudienceRig(t)
			stream := openEventStream(t, rig.loginClient(t, tc.username), rig.server.URL)
			stream.waitReady(t, rig.controller)

			rig.controller.PublishEvent("refresh")
			rig.runFailingTask(t, secretPath)
			frames := stream.collectUntilSentinel(t, rig.controller)

			var sawRefresh, sawTask bool
			for _, frame := range frames {
				switch {
				case frame == "refresh":
					sawRefresh = true
				case strings.HasPrefix(frame, "task_progress:"):
					sawTask = true
					if !tc.wantTaskFrame {
						t.Errorf("普通用户收到了任务快照: %s", frame)
					}
				}
				if !tc.wantTaskFrame && strings.Contains(frame, secretPath) {
					t.Errorf("普通用户收到了宿主机绝对路径: %s", frame)
				}
			}
			if !sawRefresh {
				t.Error("刷新帧必须照常送达：整条关掉事件流会把泄露换成功能故障")
			}
			if sawTask != tc.wantTaskFrame {
				t.Errorf("任务快照送达情况 = %v，期望 %v", sawTask, tc.wantTaskFrame)
			}
		})
	}
}
