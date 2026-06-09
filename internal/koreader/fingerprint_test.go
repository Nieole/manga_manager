// 业务说明：本文件是业务回归测试，属于 KOReader 集成链路，负责识别设备阅读记录并与本地漫画阅读进度对齐。
// 它通过自动化断言保护对应业务场景在扫描、读取、展示或配置变更后仍保持兼容。
// 维护时应让用例名称、测试数据和断言结果直接反映真实用户流程，而不是只覆盖实现细节。

package koreader

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFingerprintQuickFileChangesWithTailContent(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.bin")
	second := filepath.Join(dir, "second.bin")

	if err := os.WriteFile(first, []byte("abcdef0123456789"), 0o644); err != nil {
		t.Fatalf("write first file failed: %v", err)
	}
	if err := os.WriteFile(second, []byte("abcdef0123456799"), 0o644); err != nil {
		t.Fatalf("write second file failed: %v", err)
	}

	a, err := FingerprintQuickFile(first)
	if err != nil {
		t.Fatalf("FingerprintQuickFile first failed: %v", err)
	}
	b, err := FingerprintQuickFile(second)
	if err != nil {
		t.Fatalf("FingerprintQuickFile second failed: %v", err)
	}
	if a == b {
		t.Fatal("expected different quick fingerprints for different file content")
	}
}
