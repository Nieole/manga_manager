package api

import "testing"

func TestMetadataDefaultConfidenceByKeyAndDisplayName(t *testing.T) {
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
		// provider.Name() 显示名形式（queueMetadataReview 实际传入的值）
		{"Ollama LLM", 0.6},
		{"OpenAI/Compatible LLM", 0.6},
		{"OpenAI Compatible (v1/chat/completions)", 0.6},
		// 未知来源回退默认
		{"something-else", 0.5},
		{"", 0.5},
	}
	for _, tc := range cases {
		if got := metadataDefaultConfidence(tc.name); got != tc.want {
			t.Errorf("metadataDefaultConfidence(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}
