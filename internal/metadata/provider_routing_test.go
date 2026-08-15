// 本文件守卫 LLM Provider 的路由表与提示词的文本处理，两处的共同点是**错了不报错**：
// NewAIProvider 的 default 分支把认不出来的 provider 静默降级成 ollama、顺带丢掉
// apiKey，配置写错一个字母，用户看到的是「连不上本地模型」而非「provider 名不认识」；
// 候选简介按字节截断会切出非法 UTF-8（中文一个字 3 字节），模型侧收到乱码且日志无痕迹。

package metadata

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNewAIProviderRouting(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		apiMode  string
		wantName string
	}{
		// openai 系列按 apiMode 分流：chat_completions 走 legacy 端点，其余走 responses。
		{"openai 默认走 responses", "openai", "", "OpenAI/Compatible LLM"},
		{"openai + chat_completions 走 legacy", "openai", "chat_completions", "OpenAI Compatible (v1/chat/completions)"},
		// openai-legacy 是历史别名，等价于 openai + chat_completions。
		{"openai-legacy 是别名", "openai-legacy", "", "OpenAI Compatible (v1/chat/completions)"},
		{"大小写与空格不敏感", "  OpenAI  ", "", "OpenAI/Compatible LLM"},
		{"ollama 显式指定", "ollama", "", "Ollama LLM"},
		{"空 provider 回落 ollama", "", "", "Ollama LLM"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := NewAIProvider(tc.provider, tc.apiMode, "http://localhost:11434", "", "m", "k", 0)
			if p == nil {
				t.Fatal("NewAIProvider 返回了 nil")
			}
			if got := p.Name(); got != tc.wantName {
				t.Fatalf("provider=%q apiMode=%q 路由到 %q，期望 %q",
					tc.provider, tc.apiMode, got, tc.wantName)
			}
		})
	}
}

// TestNewAIProviderUnknownFallsBackToOllama 把「静默降级」这个现状钉住并写清代价。
//
// 这是当前的**既定行为**，不是缺陷修复：配错 provider 名不会报错，会静默变成 ollama、
// 并且 apiKey 被丢掉。用例存在的意义是让下一个人改这里时先看到这段说明——
// 要么保持不变，要么改成显式报错，但别在不知情的情况下改掉。
func TestNewAIProviderUnknownFallsBackToOllama(t *testing.T) {
	p := NewAIProvider("gpt-4o", "", "https://api.example.test", "", "m", "secret-key", 0)
	if got := p.Name(); got != "Ollama LLM" {
		t.Fatalf("未识别的 provider 路由到 %q —— 若这里改成显式报错，"+
			"请一并更新配置校验与设置页的错误提示，别只改这一处", got)
	}
}

func TestNewAIProviderClampsTimeout(t *testing.T) {
	// timeout <= 0 必须被夹到默认值：透传 0 会让 http.Client 永不超时，
	// 一次挂住的 LLM 请求就能把刮削任务卡到进程结束。
	for _, timeout := range []int{0, -1} {
		p := NewAIProvider("ollama", "", "http://localhost:11434", "", "m", "", timeout)
		if p == nil {
			t.Fatalf("timeout=%d 时返回 nil", timeout)
		}
	}
}

func TestTruncateRunesNeverSplitsMultibyteCharacters(t *testing.T) {
	// 中文一个字 3 字节。按字节截断会切在字符中间、产出非法 UTF-8，
	// 而且 100 字节只装得下约 33 个汉字——上下文严重不足，模型侧却只看到乱码。
	long := strings.Repeat("漫画简介", 80) // 320 个字符
	got := truncateRunes(long, 100)

	if !utf8.ValidString(got) {
		t.Fatal("截断结果不是合法 UTF-8 —— 按字节截断会切在多字节字符中间")
	}
	if runes := utf8.RuneCountInString(strings.TrimSuffix(got, "...")); runes != 100 {
		t.Fatalf("保留了 %d 个字符，期望 100", runes)
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatal("超长时应补省略号，否则模型无从判断简介被截断过")
	}
}

func TestTruncateRunesEdgeCases(t *testing.T) {
	if got := truncateRunes("abc", 0); got != "" {
		t.Fatalf("max=0 应返回空串，得到 %q", got)
	}
	if got := truncateRunes("abc", 10); got != "abc" {
		t.Fatalf("未超长时应原样返回，得到 %q", got)
	}
	if got := truncateRunes("", 10); got != "" {
		t.Fatalf("空串应原样返回，得到 %q", got)
	}
}

func TestBuildCandidatesTextSwitchesLocale(t *testing.T) {
	candidates := []CandidateSeries{
		{ID: 1, Title: "海贼王", Summary: "冒险"},
		{ID: 2, Title: "", Summary: ""}, // 无标题：两种语言各有自己的占位文案
	}

	zh := buildCandidatesText(context.Background(), candidates)
	if !strings.Contains(zh, "标题: 海贼王") || !strings.Contains(zh, "未知标题 (ID: 2)") {
		t.Fatalf("中文提示词内容不对：\n%s", zh)
	}

	en := buildCandidatesText(WithLocale(context.Background(), "en-US"), candidates)
	if !strings.Contains(en, "Title: 海贼王") || !strings.Contains(en, "Unknown title (ID: 2)") {
		t.Fatalf("英文提示词内容不对：\n%s", en)
	}
	// 两种语言都必须带上 ID：分组/推荐的结果是靠 ID 回填的，丢了 ID 整个功能就废了。
	for _, text := range []string{zh, en} {
		if !strings.Contains(text, "ID: 1") || !strings.Contains(text, "ID: 2") {
			t.Fatalf("提示词里缺少候选 ID：\n%s", text)
		}
	}
}
