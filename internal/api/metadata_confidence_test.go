// 业务说明：本文件是业务回归测试，属于后端 HTTP API 层，负责把前端请求转换为数据库、扫描器、图片处理和元数据服务调用。
// 它通过自动化断言保护对应业务场景在扫描、读取、展示或配置变更后仍保持兼容。
// 维护时应让用例名称、测试数据和断言结果直接反映真实用户流程，而不是只覆盖实现细节。

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
