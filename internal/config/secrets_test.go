package config

import "testing"

func TestMaskAndRestoreSecrets(t *testing.T) {
	var cfg Config
	cfg.LLM.APIKey = "realkey"
	cfg.Scrapers.ComicVineAPIKey = "realtoken"

	masked := MaskSecrets(cfg)
	if masked.LLM.APIKey != SecretMask {
		t.Fatalf("api_key not masked: %q", masked.LLM.APIKey)
	}
	if masked.Scrapers.ComicVineAPIKey != SecretMask {
		t.Fatalf("comicvine key not masked: %q", masked.Scrapers.ComicVineAPIKey)
	}
	// MaskSecrets 不得修改入参。
	if cfg.LLM.APIKey != "realkey" || cfg.Scrapers.ComicVineAPIKey != "realtoken" {
		t.Fatal("MaskSecrets mutated its input")
	}

	// 用户原样回传脱敏配置 -> 用当前值回填，真实密钥不被覆盖。
	unchanged := masked
	RestoreMaskedSecrets(&unchanged, cfg)
	if unchanged.LLM.APIKey != "realkey" || unchanged.Scrapers.ComicVineAPIKey != "realtoken" {
		t.Fatalf("masked secret not restored: %+v", unchanged.LLM.APIKey)
	}

	// 用户填入新密钥 -> 原样保留。
	updated := masked
	updated.LLM.APIKey = "newkey"
	RestoreMaskedSecrets(&updated, cfg)
	if updated.LLM.APIKey != "newkey" {
		t.Fatalf("new key should be kept, got %q", updated.LLM.APIKey)
	}

	// 用户清空密钥 -> 保持为空（不回填）。
	var cleared Config
	RestoreMaskedSecrets(&cleared, cfg)
	if cleared.LLM.APIKey != "" || cleared.Scrapers.ComicVineAPIKey != "" {
		t.Fatal("cleared secret should remain empty")
	}

	// 未设置的密钥 mask 后仍为空，便于前端区分“已设置/未设置”。
	empty := MaskSecrets(Config{})
	if empty.LLM.APIKey != "" || empty.Scrapers.ComicVineAPIKey != "" {
		t.Fatal("empty secret should not be masked to placeholder")
	}
}
