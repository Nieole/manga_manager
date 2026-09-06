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

// RunIDAttr 是**运行**标识落在日志行上的属性名。
//
// 它与任务键是两条不同的问题：任务键回答「这行属于哪件事」，运行标识回答「属于那件事的第几次」。
// 同一个库连着扫三次，按任务键过滤会把三次的日志混在一起，而排障要的恰恰是其中一次。
const RunIDAttr = "run_id"

// taskKeyContextKey 与 runIDContextKey 是两样东西在 ctx 里的键。用未导出的空结构体而不是
// 字符串：包外因此无法凭一个同名字符串把别的值塞进这两个位置。
type taskKeyContextKey struct{}

type runIDContextKey struct{}

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

// WithRunID 把运行标识放进 ctx，规则与 WithTaskKey 一致：非正数按「不带」处理。
//
// 两样各占一个 ctx 键而不是合成一个结构体：它们由同一处一起写入，但读取侧（查看日志的过滤器）
// 各按各的属性名过滤，合成一个只会让其中一个的缺席变成另一个的零值。
func WithRunID(ctx context.Context, runID int64) context.Context {
	if ctx == nil || runID <= 0 {
		return ctx
	}
	return context.WithValue(ctx, runIDContextKey{}, runID)
}

// RunIDFrom 取出 ctx 携带的运行标识，没有则返回 0。
func RunIDFrom(ctx context.Context) int64 {
	if ctx == nil {
		return 0
	}
	runID, _ := ctx.Value(runIDContextKey{}).(int64)
	return runID
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
	runID := RunIDFrom(ctx)
	if key == "" && runID == 0 {
		return h.next.Handle(ctx, record)
	}
	clone := record.Clone()
	if key != "" {
		clone.AddAttrs(slog.String(TaskKeyAttr, key))
	}
	if runID != 0 {
		clone.AddAttrs(slog.Int64(RunIDAttr, runID))
	}
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
