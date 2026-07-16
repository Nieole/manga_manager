package api

import (
	"testing"

	"manga-manager/internal/images"
	"manga-manager/internal/parser"
)

// TestPersistConfigRebuildsDerivedResources 守卫「配置双路径一致」修复：经 API 保存配置（persistConfig）
// 必须像文件热重载一样重建 parser 池与 images 处理器，而不仅仅 Replace + SetLevel。否则经 UI 改
// archive_pool_size / max_ai_concurrency 会静默不生效（需重启或依赖文件监听回环）。
func TestPersistConfigRebuildsDerivedResources(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	cfg := controller.config.Snapshot()
	cfg.Scanner.ArchivePoolSize = 9
	cfg.Scanner.MaxAiConcurrency = 6
	if err := controller.persistConfig(&cfg); err != nil {
		t.Fatalf("persistConfig: %v", err)
	}

	if got := parser.PoolMaxSize(); got != 9 {
		t.Fatalf("persistConfig should rebuild archive pool to 9, got %d", got)
	}
	if got := images.AIConcurrency(); got != 6 {
		t.Fatalf("persistConfig should rebuild AI concurrency to 6, got %d", got)
	}
}
