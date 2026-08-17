package proposal

import "testing"

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
