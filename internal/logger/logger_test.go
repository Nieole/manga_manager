// 业务说明：本文件是业务回归测试，属于日志基础设施，负责统一后端运行日志的格式、级别和输出位置。
// 它通过自动化断言保护对应业务场景在扫描、读取、展示或配置变更后仍保持兼容。
// 维护时应让用例名称、测试数据和断言结果直接反映真实用户流程，而不是只覆盖实现细节。

package logger

import "testing"

func TestSetLevelAndCurrentLevel(t *testing.T) {
	levels := []string{"debug", "info", "warn", "error"}

	for _, level := range levels {
		if err := SetLevel(level); err != nil {
			t.Fatalf("SetLevel(%q) returned error: %v", level, err)
		}
		if got := CurrentLevel(); got != level {
			t.Fatalf("expected CurrentLevel %q, got %q", level, got)
		}
	}
}

func TestSetLevelRejectsInvalidValue(t *testing.T) {
	if err := SetLevel("verbose"); err == nil {
		t.Fatal("expected invalid log level to return an error")
	}
}
