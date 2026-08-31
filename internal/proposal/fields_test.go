package proposal

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"manga-manager/internal/database"
	"manga-manager/internal/metadata"
)

// TestDefaultConfidenceByKeyAndDisplayName 钉住兜底置信度对两种 provider 写法一视同仁。
//
// 调用方传进来的既可能是 provider key（配置里的 "ollama"），也可能是 provider.Name() 的
// 显示名（"Ollama LLM"）。只认 key 的话，同一个 LLM 数据源会因为入口不同拿到 0.6 或 0.5，
// 收件箱里的排序与置信度徽章随之飘移。
func TestDefaultConfidenceByKeyAndDisplayName(t *testing.T) {
	cases := []struct {
		name string
		want float64
	}{
		// provider key 形式
		{"bangumi", 0.9},
		{"Bangumi", 0.9},
		{"openai", 0.6},
		{"ollama", 0.6},
		{"llm", 0.6},
		{"openai-legacy", 0.6},
		// provider.Name() 显示名形式
		{"Ollama LLM", 0.6},
		{"OpenAI/Compatible LLM", 0.6},
		{"OpenAI Compatible (v1/chat/completions)", 0.6},
		// 未知来源回退默认
		{"something-else", 0.5},
		{"", 0.5},
	}
	for _, tc := range cases {
		if got := defaultConfidence(tc.name); got != tc.want {
			t.Errorf("defaultConfidence(%q) = %v，期望 %v", tc.name, got, tc.want)
		}
	}
}

// TestBuildFieldDraftsStatusFromLLMScrape 复现用户报的垃圾提案：一个状态本来就正确的系列，
// 被 LLM 刮削后多出一条「连载中 → unknown」的提案，误点应用就把正确状态抹掉。
// 这里从假 LLM 上游一路走到字段提案，因为病灶在 provider 交出结果那一步，不在提案侧。
func TestBuildFieldDraftsStatusFromLLMScrape(t *testing.T) {
	series := database.Series{
		ID:      1,
		Name:    "海贼王",
		Title:   sql.NullString{String: "海贼王", Valid: true},
		Summary: sql.NullString{String: "草帽一伙的冒险", Valid: true},
		Status:  sql.NullString{String: "ongoing", Valid: true},
	}

	cases := []struct {
		name        string
		modelOutput string
		wantStatus  string // 空串表示不该产生 status 提案
	}{
		{
			name:        "模型没给 status：不产生 status 提案",
			modelOutput: `{"title":"海贼王","summary":"草帽一伙的冒险","confidence":0.8}`,
			wantStatus:  "",
		},
		{
			name:        "模型给了合法 status：照常入队",
			modelOutput: `{"title":"海贼王","summary":"草帽一伙的冒险","status":"completed"}`,
			wantStatus:  "completed",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				payload, err := json.Marshal(map[string]any{"response": tc.modelOutput, "done": true})
				if err != nil {
					t.Errorf("编码假上游响应失败: %v", err)
					return
				}
				_, _ = w.Write(payload)
			}))
			defer server.Close()

			result, err := metadata.NewOllamaProvider(server.URL, "test-model", 5).
				FetchSeriesMetadata(context.Background(), series.Name)
			if err != nil {
				t.Fatalf("刮削失败: %v", err)
			}

			drafts, _ := buildFieldDrafts(series, nil, nil, result, 0.6)
			var got string
			for _, draft := range drafts {
				if draft.Name == "status" {
					got = draft.Proposed
				}
			}
			if got != tc.wantStatus {
				t.Errorf("status 提案 = %q（当前值 %q），期望 %q", got, series.Status.String, tc.wantStatus)
			}
		})
	}
}
