// 钉住三家 LLM provider 的状态出口：模型没给、给了不认识的写法、或自己说 unknown 时都留空。
// 留空是提案侧「空提案不入队」赖以生效的前提——折成 "unknown" 会让每次刮削都提议
// 把系列已有的正确状态改写成 unknown。

package metadata

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// llmProbe 抹平三家 provider 的差异：怎么按 endpoint 造出来，以及模型输出被包进哪种响应体。
type llmProbe struct {
	name     string
	build    func(endpoint string) Provider
	envelope func(modelOutput string) string
}

func llmProbes() []llmProbe {
	return []llmProbe{
		{
			name:  "ollama",
			build: func(endpoint string) Provider { return NewOllamaProvider(endpoint, "test-model", 5) },
			envelope: func(out string) string {
				return `{"response":` + jsonString(out) + `,"done":true}`
			},
		},
		{
			name:  "openai",
			build: func(endpoint string) Provider { return NewOpenAIProvider(endpoint, "test-model", "", 5) },
			envelope: func(out string) string {
				return `{"output":[{"type":"message","content":[{"type":"output_text","text":` + jsonString(out) + `}]}]}`
			},
		},
		{
			name:  "openai-legacy",
			build: func(endpoint string) Provider { return NewOpenAILegacyProvider(endpoint, "test-model", "", 5) },
			envelope: func(out string) string {
				return `{"choices":[{"message":{"role":"assistant","content":` + jsonString(out) + `}}]}`
			},
		},
	}
}

func jsonString(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

// fetchWithModelOutput 让 provider 去问一个只会回放 modelOutput 的假上游。
func fetchWithModelOutput(t *testing.T, probe llmProbe, modelOutput string) *SeriesMetadata {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(probe.envelope(modelOutput)))
	}))
	defer server.Close()

	meta, err := probe.build(server.URL).FetchSeriesMetadata(context.Background(), "海贼王")
	if err != nil {
		t.Fatalf("%s: FetchSeriesMetadata 失败: %v", probe.name, err)
	}
	if meta == nil {
		t.Fatalf("%s: FetchSeriesMetadata 返回 nil，用例需要一份结果", probe.name)
	}
	return meta
}

func TestLLMProvidersReportOnlyTheStatusModelGave(t *testing.T) {
	cases := []struct {
		name        string
		modelOutput string
		want        string
	}{
		{
			// 本地小模型漏字段很常见，这是本用例的主场景。
			name:        "模型没给 status 字段：留空",
			modelOutput: `{"title":"海贼王","summary":"草帽一伙的冒险","confidence":0.8}`,
			want:        "",
		},
		{
			name:        "模型给了合法 status：照常带出",
			modelOutput: `{"title":"海贼王","summary":"草帽一伙的冒险","status":"ongoing"}`,
			want:        "ongoing",
		},
		{
			name:        "繁体写法：认成 ongoing",
			modelOutput: `{"title":"海贼王","summary":"草帽一伙的冒险","status":"連載中"}`,
			want:        "ongoing",
		},
		{
			name:        "英文变体：认成 ongoing",
			modelOutput: `{"title":"海贼王","summary":"草帽一伙的冒险","status":"serialization"}`,
			want:        "ongoing",
		},
		{
			name:        "无法识别的写法：留空",
			modelOutput: `{"title":"海贼王","summary":"草帽一伙的冒险","status":"大概还在画吧"}`,
			want:        "",
		},
		{
			// 提示词把 unknown 列为可选值，模型照着填时说的是「我不知道」，不是一个事实。
			name:        "模型自己说 unknown：留空",
			modelOutput: `{"title":"海贼王","summary":"草帽一伙的冒险","status":"unknown"}`,
			want:        "",
		},
	}

	for _, probe := range llmProbes() {
		for _, tc := range cases {
			t.Run(probe.name+"/"+tc.name, func(t *testing.T) {
				meta := fetchWithModelOutput(t, probe, tc.modelOutput)
				if meta.Status != tc.want {
					t.Errorf("Status = %q, want %q", meta.Status, tc.want)
				}
			})
		}
	}
}

// TestProvidersDoNotFoldStatusToUnknown 是给将来新增 provider 的护栏：包内的数据源实现
// 一律走 sourceStatusCode，写入端专用的 StatusCodeOrUnknown 只许留在 locale.go。
// 命名与注释挡不住：每家 provider 的状态出口都是各写各的，抄错一处就够毁掉一批状态。
func TestProvidersDoNotFoldStatusToUnknown(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("读取包目录失败: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "locale.go" {
			continue
		}
		content, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", name, err)
		}
		if strings.Contains(string(content), "StatusCodeOrUnknown") {
			t.Errorf("%s 用了写入端的 StatusCodeOrUnknown：数据源交出结果时改用 sourceStatusCode，"+
				"把没给、认不出的状态留空，否则每次刮削都会提议把系列已有的状态改成 unknown", name)
		}
	}
}
