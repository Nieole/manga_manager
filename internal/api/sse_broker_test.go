package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestSSEBrokerDeliversPublishedEventToClient 验证抽出的 sseBroker 端到端可用：注册的客户端能收到 publish
// 的事件。newClients 无缓冲，故 `b.newClients <- client` 返回时该客户端已在 broker 单 goroutine 内注册完毕，
// 后续 publish 必然可达——无需靠 sleep 等待注册，测试是确定性的。
func TestSSEBrokerDeliversPublishedEventToClient(t *testing.T) {
	b := newSSEBroker()
	done := make(chan struct{})
	go b.run(done)
	defer close(done)

	client := make(chan string, 4)
	b.newClients <- client // 阻塞至 broker 完成注册

	b.publish("hello")

	select {
	case msg := <-client:
		if msg != "hello" {
			t.Fatalf("client received %q, want %q", msg, "hello")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("registered client did not receive the published event")
	}

	// 注销后不再收到事件。
	b.defunctClients <- client // 阻塞至 broker 处理注销（并 close(client)）
	if _, open := <-client; open {
		t.Fatal("client channel should be closed after deregistration")
	}
}

// TestSSEShutdownReleasesInFlightConnections 锁住停机时 SSE 长连接会被主动切断。
//
// SSE 是长连接，http.Server.Shutdown 的「排空在途请求」对它永远不会自然完成：
// 只要有一个浏览器标签开着，每次优雅停机都必然跑满 20 秒超时。
//
// 不去等「客户端已注册」这一中间态：无论 serveHTTP 当前停在注册处还是已进入主循环，
// closeClients 之后它都必须返回，这正是要锁的性质，且断言天然无时序依赖。
func TestSSEShutdownReleasesInFlightConnections(t *testing.T) {
	broker := newSSEBroker()
	done := make(chan struct{})
	defer close(done)
	go broker.run(done)

	finished := make(chan struct{})
	go func() {
		broker.serveHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/events", nil))
		close(finished)
	}()

	broker.closeClients()

	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("serveHTTP did not return after shutdown was signalled")
	}

	// 幂等：重复调用不得 panic（close of closed channel）。
	broker.closeClients()
}

// TestSSEMainLoopExitsOnShutdown 覆盖「已完成注册、正停在主循环里」的那条路径。
// 直接把客户端通道塞进 broker（无缓冲，返回即已注册完毕），确保停机信号能中断主循环。
func TestSSEMainLoopExitsOnShutdown(t *testing.T) {
	broker := newSSEBroker()
	done := make(chan struct{})
	defer close(done)
	go broker.run(done)

	// 无缓冲 channel：这一步返回时，broker 的单 goroutine 已把该客户端登记完毕。
	client := make(chan string, 1)
	broker.newClients <- client

	broker.publish("hello")
	select {
	case <-client:
	case <-time.After(2 * time.Second):
		t.Fatal("broker did not deliver to a registered client")
	}

	// 此时事件循环确实在跑；停机后它应立刻退出而不是等到 done。
	broker.closeClients()
	select {
	case <-broker.shutdown:
	default:
		t.Fatal("closeClients did not signal shutdown")
	}
}

// TestSSERegistrationDoesNotBlockAfterShutdown 覆盖「broker 已退出后仍有新连接进来」。
// newClients 是无缓冲的，裸写会永久挂住那个请求 goroutine。
func TestSSERegistrationDoesNotBlockAfterShutdown(t *testing.T) {
	broker := newSSEBroker()
	broker.closeClients() // run 循环从未启动，等价于已停机

	finished := make(chan struct{})
	go func() {
		broker.serveHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/events", nil))
		close(finished)
	}()

	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("serveHTTP blocked forever registering with a stopped broker")
	}
}
