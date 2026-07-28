package api

import (
	"os"
	"runtime"
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

// TestPersistConfigWritesOwnerOnlyPerm 守卫 UI 保存这条路径的落盘权限。
//
// 这条路径与 config 包那一例不重复：persistConfig 写的是 RestoreMaskedSecrets 回填后的
// **真实** api_key，是明文密钥真正落盘的地方；而 createDefaultConfig 生成时配置里还没有密钥。
// 断言同样用字面量 0o600 而非 config.ConfigFilePerm，避免常量被改宽时期望值跟着变。
//
// 注意：persistConfig 末尾会走 runtimecfg.Apply，改的是进程级单例（parser 池、images 并发），
// 与同文件的 TestPersistConfigRebuildsDerivedResources 共享全局状态。这里刻意原样回写
// Snapshot()，使全局值不发生变化，两个用例的执行顺序因而互不影响。
func TestPersistConfigWritesOwnerOnlyPerm(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows 无 POSIX 权限位")
	}

	const ownerOnly os.FileMode = 0o600

	controller, _, _, _ := newTestController(t)

	cfg := controller.config.Snapshot()
	cfg.LLM.APIKey = "sk-should-not-be-world-readable"
	if err := controller.persistConfig(&cfg); err != nil {
		t.Fatalf("persistConfig: %v", err)
	}

	info, err := os.Stat(controller.configPath)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if got := info.Mode().Perm(); got != ownerOnly {
		t.Fatalf("UI 保存后的 config.yaml 权限 = %04o, want %04o", got, ownerOnly)
	}
}
