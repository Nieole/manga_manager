// 守 ctx handler 的四条：ctx 带键就附成属性、不带就一字不改、slog.With 派生后仍带、
// 不走 ctx 的调用（slog.Info 一族）本就带不上——最后这条是本层的**边界**，不是缺陷。

package logger

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
)

// newCapturingLogger 造一个写进 buffer 的 logger，handler 与生产同一层包法。
func newCapturingLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	handler := NewContextHandler(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return slog.New(handler), &buf
}

func TestContextHandlerAttachesTaskKeyFromContext(t *testing.T) {
	cases := []struct {
		name    string
		ctx     context.Context
		wantKey string
	}{
		{"ctx 带任务键就附上", WithTaskKey(context.Background(), "scan_library_1"), "scan_library_1"},
		{"ctx 不带就一字不改", context.Background(), ""},
		{"空串按不带处理", WithTaskKey(context.Background(), ""), ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			log, buf := newCapturingLogger()
			log.InfoContext(tc.ctx, "scan step")

			line := buf.String()
			if tc.wantKey == "" {
				if strings.Contains(line, TaskKeyAttr+"=") {
					t.Fatalf("这行本不该带任务键: %q", line)
				}
				return
			}
			if !strings.Contains(line, TaskKeyAttr+"="+tc.wantKey) {
				t.Fatalf("日志行缺少 %s=%s: %q", TaskKeyAttr, tc.wantKey, line)
			}
		})
	}
}

// TestContextHandlerAttachesRunIDFromContext 守运行标识与任务键各走各的：
// 同一个库连着扫三次，只按任务键过滤会把三次的日志混在一起，而排障要的恰恰是其中一次。
func TestContextHandlerAttachesRunIDFromContext(t *testing.T) {
	cases := []struct {
		name string
		ctx  context.Context
		want string
	}{
		{"ctx 带运行标识就附上", WithRunID(context.Background(), 42), RunIDAttr + "=42"},
		{"非正数按不带处理", WithRunID(context.Background(), 0), ""},
		{"ctx 不带就一字不改", context.Background(), ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			log, buf := newCapturingLogger()
			log.InfoContext(tc.ctx, "scan step")

			line := buf.String()
			if tc.want == "" {
				if strings.Contains(line, RunIDAttr+"=") {
					t.Fatalf("这行本不该带运行标识: %q", line)
				}
				return
			}
			if !strings.Contains(line, tc.want) {
				t.Fatalf("日志行缺少 %s: %q", tc.want, line)
			}
		})
	}
}

// TestContextHandlerAttachesBothKeyAndRunID 守两样同时在时一起附上：只带其中一样的话，
// 「查看日志」按任务键过滤得到的是三次运行混在一起的流水。
func TestContextHandlerAttachesBothKeyAndRunID(t *testing.T) {
	ctx := WithRunID(WithTaskKey(context.Background(), "scan_library_1"), 7)
	log, buf := newCapturingLogger()
	log.InfoContext(ctx, "scan step")

	line := buf.String()
	if !strings.Contains(line, TaskKeyAttr+"=scan_library_1") || !strings.Contains(line, RunIDAttr+"=7") {
		t.Fatalf("日志行没有同时带上任务键与运行标识: %q", line)
	}
}

// TestContextHandlerSurvivesWithAttrs 守派生出来的 logger 照样带任务键。
// 破了是静默的：slog.With 一句话就把这层剥掉，之后那个 logger 写的日志一行都不带。
func TestContextHandlerSurvivesWithAttrs(t *testing.T) {
	ctx := WithTaskKey(context.Background(), "rebuild_thumbnails")

	t.Run("With 派生后仍带", func(t *testing.T) {
		log, buf := newCapturingLogger()
		log.With("library_id", 3).InfoContext(ctx, "step")
		if !strings.Contains(buf.String(), TaskKeyAttr+"=rebuild_thumbnails") {
			t.Fatalf("slog.With 之后任务键掉了: %q", buf.String())
		}
	})

	// 开了组之后属性会带上组名前缀（scan.task_key=…），查看侧的子串过滤照旧命中——
	// 断言按的就是那条子串口径。
	t.Run("WithGroup 派生后仍带", func(t *testing.T) {
		log, buf := newCapturingLogger()
		log.WithGroup("scan").InfoContext(ctx, "step")
		if !strings.Contains(buf.String(), TaskKeyAttr+"=rebuild_thumbnails") {
			t.Fatalf("slog.WithGroup 之后任务键掉了: %q", buf.String())
		}
	})
}

// TestContextHandlerLeavesRecordUntouched 守同一条记录交给 Handle 两次，第二次不比第一次多一个属性。
func TestContextHandlerLeavesRecordUntouched(t *testing.T) {
	ctx := WithTaskKey(context.Background(), "scrape")
	inner := &countingHandler{}
	handler := NewContextHandler(inner)

	record := slog.Record{Level: slog.LevelInfo, Message: "step"}
	if err := handler.Handle(ctx, record); err != nil {
		t.Fatalf("Handle 报错: %v", err)
	}
	if err := handler.Handle(ctx, record); err != nil {
		t.Fatalf("Handle 报错: %v", err)
	}

	for i, got := range inner.attrCounts {
		if got != 1 {
			t.Fatalf("第 %d 次收到 %d 个属性, want 1 —— 原记录被就地改过了", i+1, got)
		}
	}
}

// countingHandler 只数每条记录带了几个属性。
type countingHandler struct {
	attrCounts []int
}

func (h *countingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *countingHandler) Handle(_ context.Context, record slog.Record) error {
	h.attrCounts = append(h.attrCounts, record.NumAttrs())
	return nil
}

func (h *countingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h *countingHandler) WithGroup(string) slog.Handler { return h }

// TestTaskKeyFromToleratesNilContext 守取值侧对 nil ctx 不炸：日志 handler 在任何一条路径上
// 报 panic 都会顺着 slog 冒到调用它的业务代码里。
func TestTaskKeyFromToleratesNilContext(t *testing.T) {
	//nolint:staticcheck // 故意传 nil，守的就是这条路径
	if key := TaskKeyFrom(nil); key != "" {
		t.Fatalf("nil ctx 取出了 %q", key)
	}
	//nolint:staticcheck // 同上
	if ctx := WithTaskKey(nil, "scan_library_1"); ctx != nil {
		t.Fatal("nil ctx 上不该凭空造出一个 ctx")
	}
}

// TestInitInstallsContextHandler 守生产接线：Init 装上的默认 logger 就是这层 ctx handler。
// 少了它，本文件其余用例自己包一层照样全绿，而进程真正写出去的日志一行都不带任务键。
func TestInitInstallsContextHandler(t *testing.T) {
	initFileLoggingInTempDir(t)

	slog.InfoContext(WithTaskKey(context.Background(), "scan_library_1"), "scan step")

	written, err := os.ReadFile(LogFilePath())
	if err != nil {
		t.Fatalf("读日志文件失败: %v", err)
	}
	if !strings.Contains(string(written), TaskKeyAttr+"=scan_library_1") {
		t.Fatalf("Init 装的 handler 不认 ctx，任务键没落到日志上:\n%s", written)
	}
}
