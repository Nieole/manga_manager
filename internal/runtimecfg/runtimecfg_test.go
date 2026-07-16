package runtimecfg

import (
	"testing"

	"manga-manager/internal/config"
	"manga-manager/internal/images"
	"manga-manager/internal/logger"
	"manga-manager/internal/parser"
)

// TestApplyRebuildsRuntimeResources 验证 Apply 按配置重建全部三类派生资源，且再次以不同值调用会重建
// （幂等重建，而非只首次生效）——这是「文件热重载」与「API 保存」共享同一副作用集合的基础。
func TestApplyRebuildsRuntimeResources(t *testing.T) {
	cfg := &config.Config{}
	cfg.Scanner.ArchivePoolSize = 7
	cfg.Scanner.MaxAiConcurrency = 4
	cfg.Logging.Level = "warn"
	if err := Apply(cfg); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := parser.PoolMaxSize(); got != 7 {
		t.Fatalf("archive pool size = %d, want 7", got)
	}
	if got := images.AIConcurrency(); got != 4 {
		t.Fatalf("AI concurrency = %d, want 4", got)
	}
	if got := logger.CurrentLevel(); got != "warn" {
		t.Fatalf("log level = %q, want warn", got)
	}

	cfg.Scanner.ArchivePoolSize = 3
	cfg.Scanner.MaxAiConcurrency = 1
	cfg.Logging.Level = "debug"
	if err := Apply(cfg); err != nil {
		t.Fatalf("Apply (reapply): %v", err)
	}
	if got := parser.PoolMaxSize(); got != 3 {
		t.Fatalf("archive pool size after reapply = %d, want 3", got)
	}
	if got := images.AIConcurrency(); got != 1 {
		t.Fatalf("AI concurrency after reapply = %d, want 1", got)
	}
	if got := logger.CurrentLevel(); got != "debug" {
		t.Fatalf("log level after reapply = %q, want debug", got)
	}
}
