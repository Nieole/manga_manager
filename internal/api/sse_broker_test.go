package api

import (
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
