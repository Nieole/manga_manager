// 本文件是业务回归测试，属于运行时配置管理层，负责读取、归一化和持久化漫画库、扫描、元数据、AI 和服务端选项。
// 它通过自动化断言保护对应业务场景在扫描、读取、展示或配置变更后仍保持兼容。
// 维护时应让用例名称、测试数据和断言结果直接反映真实用户流程，而不是只覆盖实现细节。

package config

import (
	"path/filepath"
	"testing"
)

func TestManagerSnapshotAndReplace(t *testing.T) {
	initial := &Config{}
	initial.Server.Port = 8080
	initial.Cache.Dir = "./data/cache"

	manager := NewManager(initial)
	snapshot := manager.Snapshot()
	if snapshot.Server.Port != 8080 {
		t.Fatalf("expected initial port 8080, got %d", snapshot.Server.Port)
	}

	updated := &Config{}
	updated.Server.Port = 9090
	updated.Cache.Dir = "./tmp/cache"
	manager.Replace(updated)

	snapshot = manager.Snapshot()
	if snapshot.Server.Port != 9090 {
		t.Fatalf("expected updated port 9090, got %d", snapshot.Server.Port)
	}
	if snapshot.Cache.Dir != "./tmp/cache" {
		t.Fatalf("expected updated cache dir, got %q", snapshot.Cache.Dir)
	}
}

func TestNormalizeConfigDefaultsLogLevel(t *testing.T) {
	cfg := &Config{}

	NormalizeConfig(cfg)

	if cfg.Logging.Level != LogLevelInfo {
		t.Fatalf("expected default log level %q, got %q", LogLevelInfo, cfg.Logging.Level)
	}
	if cfg.Server.Host != "0.0.0.0" {
		t.Fatalf("expected default server host 0.0.0.0, got %q", cfg.Server.Host)
	}
	if len(cfg.Server.AllowedOrigins) != 2 {
		t.Fatalf("expected default CORS origins, got %+v", cfg.Server.AllowedOrigins)
	}
	if cfg.Scanner.ScanProfile != ScanProfileMetadata {
		t.Fatalf("expected default scan profile %q, got %q", ScanProfileMetadata, cfg.Scanner.ScanProfile)
	}
	if cfg.Protocols.OPDS.Enabled || cfg.Protocols.Mihon.Enabled {
		t.Fatalf("expected external protocols disabled by default, got OPDS=%v Mihon=%v", cfg.Protocols.OPDS.Enabled, cfg.Protocols.Mihon.Enabled)
	}
	if cfg.Library.StorageProfile != StorageProfileAuto {
		t.Fatalf("expected default storage profile %q, got %q", StorageProfileAuto, cfg.Library.StorageProfile)
	}
	// 没写就是默认 2，不是 0：0 会让每一条后台运行都排在队里等一个永远不会空出来的槽位。
	if cfg.Tasks.RunSlots != DefaultRunSlots {
		t.Fatalf("expected default run slots %d, got %d", DefaultRunSlots, cfg.Tasks.RunSlots)
	}
}

// TestNormalizeConfigDefaultsResumeAfterRestartToOn 守**可续跑**的全局开关默认**开着**，
// 而明确关掉的那份配置不会被归一化重新打开。
//
// 布尔值的默认为真只能靠指针表达：配置文件里没写与写了 false 都是零值，两者混一起的话，
// 升级上来的每一份老配置都会被当成「用户关掉了续跑」。
func TestNormalizeConfigDefaultsResumeAfterRestartToOn(t *testing.T) {
	cfg := &Config{}
	NormalizeConfig(cfg)
	if cfg.Tasks.ResumeAfterRestart == nil || !*cfg.Tasks.ResumeAfterRestart {
		t.Fatalf("配置文件里没写时的续跑开关为 %v, want 开着", cfg.Tasks.ResumeAfterRestart)
	}

	off := false
	closed := &Config{}
	closed.Tasks.ResumeAfterRestart = &off
	NormalizeConfig(closed)
	if closed.Tasks.ResumeAfterRestart == nil || *closed.Tasks.ResumeAfterRestart {
		t.Fatalf("明确关掉的续跑开关被归一化成了 %v", closed.Tasks.ResumeAfterRestart)
	}
}

// TestCloneConfigCopiesTheResumeSwitch 守深拷贝把那个指针也复制一份：只复制指针的话，
// 两份「独立」快照写的是同一个布尔值。
func TestCloneConfigCopiesTheResumeSwitch(t *testing.T) {
	on := true
	cfg := Config{}
	cfg.Tasks.ResumeAfterRestart = &on

	clone := CloneConfig(cfg)
	if clone.Tasks.ResumeAfterRestart == cfg.Tasks.ResumeAfterRestart {
		t.Fatal("克隆出来的续跑开关与原件是同一个指针")
	}
	*clone.Tasks.ResumeAfterRestart = false
	if !*cfg.Tasks.ResumeAfterRestart {
		t.Fatal("改克隆改到了原件上")
	}
}

// TestNormalizeConfigRejectsNonPositiveRunSlots 守负数与 0 一样被改写成默认值。
func TestNormalizeConfigRejectsNonPositiveRunSlots(t *testing.T) {
	cfg := &Config{}
	cfg.Tasks.RunSlots = -3

	NormalizeConfig(cfg)

	if cfg.Tasks.RunSlots != DefaultRunSlots {
		t.Fatalf("expected negative run slots normalized to %d, got %d", DefaultRunSlots, cfg.Tasks.RunSlots)
	}
}

func TestNormalizeConfigCleansAllowedOrigins(t *testing.T) {
	cfg := &Config{}
	cfg.Server.AllowedOrigins = []string{" https://reader.example.com ", "", "https://reader.example.com", "http://localhost:8080"}

	NormalizeConfig(cfg)

	want := []string{"https://reader.example.com", "http://localhost:8080"}
	if len(cfg.Server.AllowedOrigins) != len(want) {
		t.Fatalf("unexpected origins: %+v", cfg.Server.AllowedOrigins)
	}
	for i := range want {
		if cfg.Server.AllowedOrigins[i] != want[i] {
			t.Fatalf("expected origin %d to be %q, got %q", i, want[i], cfg.Server.AllowedOrigins[i])
		}
	}
}

func TestExternalHDDStorageProfileDefaultsToLowImpactPolicy(t *testing.T) {
	cfg := &Config{}
	cfg.Library.StorageProfile = StorageProfileHDDExternal

	NormalizeConfig(cfg)

	policy := cfg.Library.IOPolicy
	if policy.ArchiveOpenConcurrency != 1 || policy.CoverConcurrency != 1 || policy.HashConcurrency != 1 {
		t.Fatalf("expected external HDD low-impact concurrency of 1, got %+v", policy)
	}
	if !policy.PauseBackgroundWhenReading || !policy.IdleOnlyHeavyTasks || !policy.DisableSameDiskPageCache {
		t.Fatalf("expected external HDD low-impact toggles enabled, got %+v", policy)
	}
}

func TestResolveStoragePolicyUsesMostSpecificPathOverride(t *testing.T) {
	root := t.TempDir()
	mangaRoot := filepath.Join(root, "Manga")
	externalRoot := filepath.Join(mangaRoot, "External")
	bookPath := filepath.Join(externalRoot, "Series", "Book.cbz")
	cfg := Config{}
	cfg.Library.StorageProfile = StorageProfileAuto
	cfg.Library.StoragePolicies = []LibraryStoragePolicy{
		{Path: mangaRoot, StorageProfile: StorageProfileSSD},
		{Path: externalRoot, StorageProfile: StorageProfileHDDExternal},
	}

	resolved := ResolveStoragePolicy(cfg, bookPath)

	if resolved.StorageProfile != StorageProfileHDDExternal {
		t.Fatalf("expected most specific external HDD profile, got %+v", resolved)
	}
	if resolved.IOPolicy.ArchiveOpenConcurrency != 1 {
		t.Fatalf("expected low-impact archive concurrency, got %+v", resolved.IOPolicy)
	}
}

func TestNormalizeScanProfile(t *testing.T) {
	if got := NormalizeScanProfile(" FAST_SCAN "); got != ScanProfileFast {
		t.Fatalf("expected fast scan profile, got %q", got)
	}
	if got := NormalizeScanProfile("unknown"); got != ScanProfileMetadata {
		t.Fatalf("expected unknown scan profile to fall back to metadata, got %q", got)
	}
}

func TestValidateConfigRejectsInvalidLogLevel(t *testing.T) {
	cfg := &Config{}
	cfg.Server.Port = 8080
	cfg.Server.Host = "0.0.0.0"
	cfg.Server.AllowedOrigins = []string{"http://*"}
	cfg.Database.Path = "./data/manga.db"
	cfg.Cache.Dir = "."
	cfg.Logging.Level = "verbose"
	cfg.Scanner.ArchivePoolSize = 5
	cfg.Scanner.MaxAiConcurrency = 3
	cfg.Scanner.ThumbnailFormat = "webp"
	cfg.LLM.Provider = "ollama"
	cfg.LLM.BaseURL = "http://localhost:11434"
	cfg.LLM.Model = "qwen2.5"
	cfg.LLM.Timeout = 120
	cfg.KOReader.BasePath = "/koreader"
	cfg.KOReader.MatchMode = KOReaderMatchModeBinaryHash

	validation := ValidateConfig(cfg)
	if validation.Valid {
		t.Fatal("expected validation to fail for invalid log level")
	}

	found := false
	for _, issue := range validation.Issues {
		if issue.Field == "logging.level" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected logging.level validation issue, got %+v", validation.Issues)
	}
}
