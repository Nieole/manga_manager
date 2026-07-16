// 业务说明：本文件把服务端事件推送（SSE）从 Controller 上帝对象里抽成独立组件。sseBroker 用「单 goroutine
// 事件循环 + channel」的 actor 模式管理订阅者集合：run() 是唯一读写 clients 的 goroutine，故无需加锁；
// serveHTTP 通过 channel 注册/注销自己，publish 非阻塞投递事件，背压时主动断开卡死的消费者让其自动重连。
// Controller 仅持有 *sseBroker 引用并做请求编排（PublishEvent 保留为薄委托，供 Scanner / FileWatcher 调用）。

package api

import (
	"log/slog"
	"net/http"
	"time"
)

type sseBroker struct {
	clients        map[chan string]bool
	newClients     chan chan string
	defunctClients chan chan string
	messages       chan string
}

func newSSEBroker() *sseBroker {
	return &sseBroker{
		clients:        make(map[chan string]bool),
		newClients:     make(chan chan string),
		defunctClients: make(chan chan string),
		messages:       make(chan string, 64),
	}
}

// run 驱动 broker 事件循环直至 done 关闭。经 Controller.runBackground 登记，随 Close 一同退出。
// clients 仅在本 goroutine 内被读写，故整个集合无需加锁。
func (b *sseBroker) run(done <-chan struct{}) {
	for {
		select {
		case <-done:
			return
		case s := <-b.newClients:
			b.clients[s] = true
		case s := <-b.defunctClients:
			if _, ok := b.clients[s]; ok {
				delete(b.clients, s)
				close(s)
			}
		case msg := <-b.messages:
			for s := range b.clients {
				select {
				case s <- msg:
				default:
					// 客户端 buffer 已满（默认 64 条），说明该消费者卡死或网络背压。
					// 主动断开它的 channel，serveHTTP 会在下一轮 select 收到关闭信号并退出，
					// 浏览器端 EventSource 会按 retry 间隔自动重连。
					slog.Warn("SSE client backpressure, dropping client connection")
					delete(b.clients, s)
					close(s)
				}
			}
		}
	}
}

// publish 非阻塞投递事件（buffer 满则丢弃）。供 Scanner / FileWatcher 等外部经 Controller.PublishEvent 调用。
func (b *sseBroker) publish(event string) {
	if b == nil || b.messages == nil {
		return
	}
	select {
	case b.messages <- event:
	default:
		slog.Warn("SSE broker channel full, dropping event", "event_prefix", eventPrefix(event))
	}
}

// serveHTTP 为单个 SSE 客户端流式推送事件：注册通道、监听断开、发送心跳。
func (b *sseBroker) serveHTTP(w http.ResponseWriter, r *http.Request) {
	// 设置 SSE 需要响应头
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// 允许跨域及凭证支持长链接
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, _ := w.(http.Flusher)

	// 提示客户端断线重连间隔（毫秒），并立刻刷一次响应头
	if _, err := w.Write([]byte("retry: 5000\n\n")); err != nil {
		return
	}
	if flusher != nil {
		flusher.Flush()
	}

	// 注册客户端通道
	messageChan := make(chan string, 64)
	b.newClients <- messageChan

	// 监听从客户端意外断开链接
	notify := r.Context().Done()
	go func() {
		<-notify
		b.defunctClients <- messageChan
	}()

	// 心跳：每 25 秒发送一次 SSE 注释行，避免反向代理（nginx/cloudflare 等）
	// 在长时间空闲时切断空连接。注释行以 `:` 开头，浏览器会忽略。
	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case msg, open := <-messageChan:
			if !open {
				return // 连接已从服务端侧切断（例如 broker 检测到客户端积压）
			}
			if _, err := w.Write([]byte("data: " + msg + "\n\n")); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		case <-heartbeat.C:
			if _, err := w.Write([]byte(": ping\n\n")); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		case <-notify:
			return
		}
	}
}
