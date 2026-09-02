// 本文件把服务端事件推送（SSE）从 Controller 上帝对象里抽成独立组件。sseBroker 用「单 goroutine
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
	// clients 的值是该订阅者是不是管理员，决定它能收到哪一档事件（见 sseEvent）。
	clients        map[chan string]bool
	newClients     chan sseSubscriber
	defunctClients chan chan string
	messages       chan sseEvent
	// shutdown 在服务开始停机时关闭，让在途的 serveHTTP 立刻返回。
	// 没有它，任何开着页面的浏览器标签都会让 srv.Shutdown 一直等到 20 秒超时——
	// SSE 是长连接，Shutdown 的「排空在途请求」对它永远不会自然完成。
	shutdown chan struct{}
}

// sseSubscriber 是一次订阅登记：投递通道加上该订阅者的角色。
type sseSubscriber struct {
	ch    chan string
	admin bool
}

// sseEvent 是一帧待广播事件。adminOnly 的帧只投给管理员订阅者：任务快照带着作用域显示名、
// 任务参数与失败原因（里面是宿主机绝对路径），而任务列表接口对普通用户是 403——
// 事件流不按同一把尺子过滤，那条 403 就等于没有。
type sseEvent struct {
	data      string
	adminOnly bool
}

func newSSEBroker() *sseBroker {
	return &sseBroker{
		clients:        make(map[chan string]bool),
		newClients:     make(chan sseSubscriber),
		defunctClients: make(chan chan string),
		messages:       make(chan sseEvent, 64),
		shutdown:       make(chan struct{}),
	}
}

// closeClients 通知所有在途 SSE 连接立即结束。幂等，可安全重复调用。
// 由 http.Server.RegisterOnShutdown 在停机开始时调用——必须早于 Shutdown 排空在途请求。
func (b *sseBroker) closeClients() {
	if b == nil {
		return
	}
	select {
	case <-b.shutdown:
		// 已经关过
	default:
		close(b.shutdown)
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
			b.clients[s.ch] = s.admin
		case s := <-b.defunctClients:
			if _, ok := b.clients[s]; ok {
				delete(b.clients, s)
				close(s)
			}
		case msg := <-b.messages:
			for s, admin := range b.clients {
				if msg.adminOnly && !admin {
					continue
				}
				select {
				case s <- msg.data:
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

// publish 非阻塞投递一帧所有角色都能看的事件。供 Scanner / FileWatcher 等外部经
// Controller.PublishEvent 调用。载荷带宿主机路径或任务细节的事件必须走 publishAdmin。
func (b *sseBroker) publish(event string) {
	b.enqueue(sseEvent{data: event})
}

// publishAdmin 非阻塞投递一帧只给管理员看的事件（任务快照走这条）。
func (b *sseBroker) publishAdmin(event string) {
	b.enqueue(sseEvent{data: event, adminOnly: true})
}

// enqueue 是两条投递入口共用的出口（buffer 满则丢弃并告警）。
func (b *sseBroker) enqueue(event sseEvent) {
	if b == nil || b.messages == nil {
		return
	}
	select {
	case b.messages <- event:
	default:
		slog.Warn("SSE broker channel full, dropping event", "event_prefix", eventPrefix(event.data))
	}
}

// serveHTTP 为单个 SSE 客户端流式推送事件：注册通道、监听断开、发送心跳。
// admin 决定这条连接能不能收到 adminOnly 的帧，由调用方按请求上下文里的当前用户判定。
func (b *sseBroker) serveHTTP(w http.ResponseWriter, r *http.Request, admin bool) {
	// 设置 SSE 需要响应头
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// 这里不得设 Access-Control-Allow-Origin：CORS 由 main.go 的 cors 中间件按
	// server.allowed_origins 统一决定，硬写 "*" 会覆盖那份白名单，等于给这个
	// 端点单独开了个全放通的口子；而同源的 EventSource 本就不需要 ACAO 头。

	flusher, _ := w.(http.Flusher)

	// 提示客户端断线重连间隔（毫秒），并立刻刷一次响应头
	if _, err := w.Write([]byte("retry: 5000\n\n")); err != nil {
		return
	}
	if flusher != nil {
		flusher.Flush()
	}

	// 注册客户端通道。newClients 是无缓冲的，若 broker 的 run 循环已经退出，
	// 裸写会永久阻塞住这个请求 goroutine，因此必须同时监听停机信号。
	messageChan := make(chan string, 64)
	select {
	case b.newClients <- sseSubscriber{ch: messageChan, admin: admin}:
	case <-b.shutdown:
		return
	}

	// 监听从客户端意外断开链接。注销同样要能被停机打断：defunctClients 也是无缓冲的，
	// broker 退出后这个 goroutine 会永久挂住（每个断开过的连接泄漏一个）。
	notify := r.Context().Done()
	go func() {
		select {
		case <-notify:
		case <-b.shutdown:
			return
		}
		select {
		case b.defunctClients <- messageChan:
		case <-b.shutdown:
		}
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
		case <-b.shutdown:
			// 停机：立即结束长连接，让 srv.Shutdown 能真正排空而不是干等 20 秒超时。
			return
		}
	}
}
