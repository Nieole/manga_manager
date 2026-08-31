// 本文件是业务回归测试，属于后端 HTTP API 层，负责把前端请求转换为数据库、扫描器、图片处理和元数据服务调用。
// 它通过自动化断言保护对应业务场景在扫描、读取、展示或配置变更后仍保持兼容。
// 维护时应让用例名称、测试数据和断言结果直接反映真实用户流程，而不是只覆盖实现细节。

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetSystemLogsHonorsFilterAndLimit(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	cfg := controller.currentConfig()
	logPath := filepath.Join(filepath.Dir(cfg.Database.Path), "manga_manager.log")

	content := "" +
		"time=2026-01-01T00:00:00Z level=DEBUG msg=\"trace\"\n" +
		"time=2026-01-01T00:00:00Z level=INFO msg=\"boot\"\n" +
		"time=2026-01-01T00:01:00Z level=ERROR msg=\"first\"\n" +
		"time=2026-01-01T00:02:00Z level=WARN msg=\"warn\"\n" +
		"time=2026-01-01T00:03:00Z level=ERROR msg=\"second\"\n" +
		"time=2026-01-01T00:04:00Z level=ERROR msg=\"third\"\n"
	if err := os.WriteFile(logPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write log file failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/system/logs?level=ERROR&limit=2", nil)
	rec := httptest.NewRecorder()
	controller.getSystemLogs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var response LogsResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode logs response failed: %v", err)
	}
	if len(response.Items) != 2 {
		t.Fatalf("expected 2 log items, got %d", len(response.Items))
	}
	if response.Items[0].Msg != "third" || response.Items[1].Msg != "second" {
		t.Fatalf("expected latest error logs first, got %+v", response.Items)
	}
	if response.Summary.ByLevel["ERROR"] != 3 {
		t.Fatalf("expected error summary count 3, got %+v", response.Summary.ByLevel)
	}
	if response.Summary.ByLevel["DEBUG"] != 1 {
		t.Fatalf("expected debug summary count 1, got %+v", response.Summary.ByLevel)
	}
}

func TestGetSystemLogsTaskKeyFilter(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	cfg := controller.currentConfig()
	logPath := filepath.Join(filepath.Dir(cfg.Database.Path), "manga_manager.log")

	content := "" +
		"time=2026-01-01T00:00:00Z level=ERROR msg=\"unrelated\"\n" +
		"time=2026-01-01T00:01:00Z level=ERROR msg=\"scan failure\" task_key=scan_library_1\n" +
		"time=2026-01-01T00:02:00Z level=ERROR msg=\"scrape failure\" task_key=scrape_library_2\n" +
		"time=2026-01-01T00:03:00Z level=ERROR msg=\"scan retry\" task_key=scan_library_1\n"
	if err := os.WriteFile(logPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write log file failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/system/logs?level=ERROR&task_key=scan_library_1", nil)
	rec := httptest.NewRecorder()
	controller.getSystemLogs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var response LogsResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode logs response failed: %v", err)
	}
	if len(response.Items) != 2 {
		t.Fatalf("expected 2 log items filtered by task_key, got %d", len(response.Items))
	}
	for _, item := range response.Items {
		if item.Msg != "scan failure" && item.Msg != "scan retry" {
			t.Fatalf("unexpected item leaked into task_key filter: %+v", item)
		}
	}
}

// TestGetSystemLogsSurvivesOversizedLine 锁住「一行坏数据打死整个端点」：
// 元数据源上游返回整页 HTML 错误页时会落下一行远超 bufio.Scanner 默认 64KB 上限的日志，
// 此后在轮转掉之前，管理员在最需要日志时连能解释这次失败的那几条都读不到。
func TestGetSystemLogsSurvivesOversizedLine(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	cfg := controller.currentConfig()
	logPath := filepath.Join(filepath.Dir(cfg.Database.Path), "manga_manager.log")

	huge := strings.Repeat("A", 200*1024)
	content := "" +
		"time=2026-01-01T00:00:00Z level=ERROR msg=\"before\"\n" +
		"time=2026-01-01T00:01:00Z level=ERROR msg=\"oversized\" body=" + huge + "\n" +
		"time=2026-01-01T00:02:00Z level=ERROR msg=\"after\"\n"
	if err := os.WriteFile(logPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write log file failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/system/logs?level=ERROR&limit=100", nil)
	rec := httptest.NewRecorder()
	controller.getSystemLogs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("一行超长日志把端点打成了 %d：%s", rec.Code, rec.Body.String())
	}

	var response LogsResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode logs response failed: %v", err)
	}

	msgs := make([]string, 0, len(response.Items))
	for _, item := range response.Items {
		msgs = append(msgs, item.Msg)
	}
	t.Run("超长行前后的正常日志照常返回", func(t *testing.T) {
		for _, want := range []string{"before", "after"} {
			found := false
			for _, msg := range msgs {
				if msg == want {
					found = true
				}
			}
			if !found {
				t.Fatalf("缺少 %q，实际返回 %v", want, msgs)
			}
		}
	})

	t.Run("超长行本身被截断呈现而非静默丢弃", func(t *testing.T) {
		var oversized *LogEntry
		for i := range response.Items {
			if response.Items[i].Msg == "oversized" {
				oversized = &response.Items[i]
			}
		}
		if oversized == nil {
			t.Fatalf("超长行被整条丢掉了，实际返回 %v", msgs)
		}
		if len(oversized.Raw) > 128*1024 {
			t.Fatalf("超长行没有被截断，raw 仍有 %d 字节", len(oversized.Raw))
		}
		if !strings.Contains(oversized.Raw, "truncated") {
			t.Fatalf("截断后的行缺少可辨认的标注：%q", oversized.Raw[max(0, len(oversized.Raw)-80):])
		}
	})

	t.Run("统计口径把超长行算进 ERROR", func(t *testing.T) {
		if response.Summary.ByLevel["ERROR"] != 3 {
			t.Fatalf("expected error summary count 3, got %+v", response.Summary.ByLevel)
		}
	})
}

// TestForEachLogLineReadsOrdinaryContentUnchanged 守住降级读法的另一半：
// 换成 bufio.Reader 自己切行后，普通日志（含 CRLF、空行、末行无换行）必须逐字不变地读出来。
func TestForEachLogLineReadsOrdinaryContentUnchanged(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []string
	}{
		{"普通多行", "alpha\nbeta\ngamma\n", []string{"alpha", "beta", "gamma"}},
		{"末行没有换行符", "alpha\nbeta", []string{"alpha", "beta"}},
		{"CRLF 行尾", "alpha\r\nbeta\r\n", []string{"alpha", "beta"}},
		{"保留空行", "alpha\n\nbeta\n", []string{"alpha", "", "beta"}},
		{"空文件", "", nil},
		{"恰好等于上限的行不加标记", strings.Repeat("x", maxLogLineBytes) + "\n", []string{strings.Repeat("x", maxLogLineBytes)}},
		{"超过读取缓冲区的行按上限截断并标注", strings.Repeat("y", logReadBufferBytes+7) + "\n", []string{strings.Repeat("y", logReadBufferBytes+7)[:maxLogLineBytes] + logLineTruncatedMarker}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			if err := forEachLogLine(strings.NewReader(tc.input), func(line string) {
				got = append(got, line)
			}); err != nil {
				t.Fatalf("forEachLogLine 报错: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("读出 %d 行，期望 %d 行：%q", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("第 %d 行 = %q，期望 %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestForEachLogLineKeepsTruncatedLineValidUTF8 保证按字节截断不会在页面上留下半个汉字。
func TestForEachLogLineKeepsTruncatedLineValidUTF8(t *testing.T) {
	// 每字 3 字节，maxLogLineBytes 能被 3 整除时截点正好落在字符边界，故意多垫 1 字节错开。
	input := "x" + strings.Repeat("中", maxLogLineBytes) + "\n"

	var got []string
	if err := forEachLogLine(strings.NewReader(input), func(line string) { got = append(got, line) }); err != nil {
		t.Fatalf("forEachLogLine 报错: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("期望 1 行，得到 %d 行", len(got))
	}
	if !strings.HasSuffix(got[0], logLineTruncatedMarker) {
		t.Fatalf("超长行缺少截断标记：%q", got[0][max(0, len(got[0])-40):])
	}
	if strings.Contains(strings.TrimSuffix(got[0], logLineTruncatedMarker), "�") {
		t.Fatal("截断切碎了多字节字符，页面上会出现乱码")
	}
}
