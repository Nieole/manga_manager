package config

import "testing"

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
}

func TestValidateConfigRejectsInvalidLogLevel(t *testing.T) {
	cfg := &Config{}
	cfg.Server.Port = 8080
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
