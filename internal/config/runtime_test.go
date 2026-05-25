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
