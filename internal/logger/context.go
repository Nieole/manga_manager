// 日志的 ctx 通道：任务键在 ctx 里传，由 handler 在写出前附成属性，调用点不必手写它。

package logger

import (
	"context"
	"log/slog"
)

// TaskKeyAttr 是任务键落在日志行上的属性名。
//
// 导出它是为了让写入侧与查看侧只有一处依据：查看接口按 `task_key=<键>` 子串过滤原始日志，
// 两侧各自写死一份字面量的话，改名只会让那个过滤器静默地一条都匹配不上。
const TaskKeyAttr = "task_key"

// taskKeyContextKey 是任务键在 ctx 里的键。用未导出的空结构体而不是字符串：
// 包外因此无法凭一个同名字符串把别的值塞进这个位置。
type taskKeyContextKey struct{}

// WithTaskKey 把任务键放进 ctx。派生出的 ctx 上写的每一行日志都会带上它，
// 前提是那行日志走的是带 ctx 的调用（slog.InfoContext 一族）。
//
// 空串按「不带」处理：一个空的 task_key= 属性只会让查看侧的子串过滤多出一堆假阳性。
func WithTaskKey(ctx context.Context, key string) context.Context {
	if ctx == nil || key == "" {
		return ctx
	}
	return context.WithValue(ctx, taskKeyContextKey{}, key)
}

// TaskKeyFrom 取出 ctx 携带的任务键，没有则返回空串。
func TaskKeyFrom(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	key, _ := ctx.Value(taskKeyContextKey{}).(string)
	return key
}

// NewContextHandler 把一个 slog.Handler 包成会读 ctx 的版本：写出前把 ctx 里的任务键
// 附成属性，ctx 里没有就原样放行。
//
// 归属在 handler 而不是调用点：要在每个调用点手写一遍才带得上的属性，绝大多数调用点都不会带。
func NewContextHandler(next slog.Handler) slog.Handler {
	return contextHandler{next: next}
}

type contextHandler struct {
	next slog.Handler
}

func (h contextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle 附加 ctx 上的属性后交给内层 handler。
//
// 改的是 Record 的副本：调用方仍持有原来那份，就地 AddAttrs 会让同一条记录经过多个
// handler 时属性越积越多。
func (h contextHandler) Handle(ctx context.Context, record slog.Record) error {
	key := TaskKeyFrom(ctx)
	if key == "" {
		return h.next.Handle(ctx, record)
	}
	clone := record.Clone()
	clone.AddAttrs(slog.String(TaskKeyAttr, key))
	return h.next.Handle(ctx, clone)
}

// WithAttrs 与 WithGroup 必须重新包一层再返回。直接把内层的结果交出去，
// 等于任何一次 slog.With 都把这层悄悄剥掉，之后那个 logger 写的日志再也不带任务键。
func (h contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return contextHandler{next: h.next.WithAttrs(attrs)}
}

func (h contextHandler) WithGroup(name string) slog.Handler {
	return contextHandler{next: h.next.WithGroup(name)}
}
