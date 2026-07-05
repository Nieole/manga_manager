// 业务说明：本文件是业务回归测试，覆盖运行时配置的密钥脱敏往返、归一化默认/夹取、LLM 端点推导与校验。
// 这些是设置页保存链路与后端服务读取配置的事实来源，重构须保持默认值、兼容迁移与校验语义一致。

package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestMaskAndRestoreAllSecrets 覆盖全部四类敏感字段（LLM Key / Auth Token / ComicVine / MAL）的脱敏与回填。
func TestMaskAndRestoreAllSecrets(t *testing.T) {
	var cfg Config
	cfg.LLM.APIKey = "llm-real"
	cfg.Server.Auth.Token = "auth-real"
	cfg.Scrapers.ComicVineAPIKey = "cv-real"
	cfg.Scrapers.MALClientID = "mal-real"

	masked := MaskSecrets(cfg)
	for name, got := range map[string]string{
		"llm":       masked.LLM.APIKey,
		"auth":      masked.Server.Auth.Token,
		"comicvine": masked.Scrapers.ComicVineAPIKey,
		"mal":       masked.Scrapers.MALClientID,
	} {
		if got != SecretMask {
			t.Fatalf("%s secret not masked: %q", name, got)
		}
	}
	// 不得修改入参。
	if cfg.Scrapers.ComicVineAPIKey != "cv-real" || cfg.Scrapers.MALClientID != "mal-real" {
		t.Fatalf("MaskSecrets mutated its input")
	}

	// 原样回传占位符 -> 用当前真实值回填。
	unchanged := masked
	RestoreMaskedSecrets(&unchanged, cfg)
	if unchanged.Scrapers.ComicVineAPIKey != "cv-real" || unchanged.Scrapers.MALClientID != "mal-real" {
		t.Fatalf("masked scraper secrets not restored: %+v", unchanged.Scrapers)
	}

	// 用户填入新值 -> 保留新值。
	updated := masked
	updated.Scrapers.ComicVineAPIKey = "cv-new"
	RestoreMaskedSecrets(&updated, cfg)
	if updated.Scrapers.ComicVineAPIKey != "cv-new" {
		t.Fatalf("new comicvine key should be kept, got %q", updated.Scrapers.ComicVineAPIKey)
	}

	// 未设置的密钥脱敏后仍为空。
	empty := MaskSecrets(Config{})
	if empty.Scrapers.ComicVineAPIKey != "" || empty.Scrapers.MALClientID != "" {
		t.Fatalf("empty scraper secrets should not become placeholder")
	}

	// nil incoming 不应 panic。
	RestoreMaskedSecrets(nil, cfg)
}

// TestNormalizeConfigDefaultsAndClamps 覆盖端口/路径/缓存/扫描器/日志级别的默认与归一化。
func TestNormalizeConfigDefaultsAndClamps(t *testing.T) {
	cfg := &Config{}
	cfg.Logging.Level = "WARN" // 大小写归一化
	cfg.Cache.PageDiskCacheMaxBytes = 0
	NormalizeConfig(cfg)

	if cfg.Server.Port != 8080 {
		t.Fatalf("port default = %d, want 8080", cfg.Server.Port)
	}
	if cfg.Database.Path != "./data/manga.db" {
		t.Fatalf("database path default = %q", cfg.Database.Path)
	}
	if cfg.Cache.Dir != "./data/cache" {
		t.Fatalf("cache dir default = %q", cfg.Cache.Dir)
	}
	if cfg.Cache.PageDiskCacheMaxBytes != DefaultPageDiskCacheMaxBytes {
		t.Fatalf("zero page cache bytes should default to %d, got %d", DefaultPageDiskCacheMaxBytes, cfg.Cache.PageDiskCacheMaxBytes)
	}
	if cfg.Scanner.ThumbnailFormat != "webp" {
		t.Fatalf("thumbnail default = %q", cfg.Scanner.ThumbnailFormat)
	}
	if cfg.Scanner.ArchivePoolSize != 5 {
		t.Fatalf("archive pool default = %d, want 5", cfg.Scanner.ArchivePoolSize)
	}
	if cfg.Scanner.MaxAiConcurrency != 3 {
		t.Fatalf("max ai concurrency default = %d, want 3", cfg.Scanner.MaxAiConcurrency)
	}
	if cfg.Logging.Level != LogLevelWarn {
		t.Fatalf("log level should lowercase-normalize to warn, got %q", cfg.Logging.Level)
	}

	// 负的 page cache 表示“不限”，保持负值不被归一化。
	neg := &Config{}
	neg.Cache.PageDiskCacheMaxBytes = -1
	NormalizeConfig(neg)
	if neg.Cache.PageDiskCacheMaxBytes != -1 {
		t.Fatalf("negative page cache bytes should be preserved as unlimited, got %d", neg.Cache.PageDiskCacheMaxBytes)
	}

	// 非法日志级别回退 info。
	bad := &Config{}
	bad.Logging.Level = "verbose"
	NormalizeConfig(bad)
	if bad.Logging.Level != LogLevelInfo {
		t.Fatalf("invalid log level should fall back to info, got %q", bad.Logging.Level)
	}
}

// TestNormalizeConfigKOReaderBasePathAndMatchMode 覆盖 KOReader 基路径与匹配模式归一化。
func TestNormalizeConfigKOReaderBasePathAndMatchMode(t *testing.T) {
	basePathCases := []struct {
		in   string
		want string
	}{
		{"", "/koreader"},
		{"koreader", "/koreader"},
		{"/koreader/", "/koreader"},
		{"  /sync/ko  ", "/sync/ko"},
	}
	for _, tc := range basePathCases {
		cfg := &Config{}
		cfg.KOReader.BasePath = tc.in
		NormalizeConfig(cfg)
		if cfg.KOReader.BasePath != tc.want {
			t.Fatalf("BasePath(%q) = %q, want %q", tc.in, cfg.KOReader.BasePath, tc.want)
		}
	}

	matchCases := []struct {
		in   string
		want string
	}{
		{"", KOReaderMatchModeBinaryHash},
		{"FILE_PATH", KOReaderMatchModeFilePath},
		{"garbage", KOReaderMatchModeBinaryHash},
	}
	for _, tc := range matchCases {
		cfg := &Config{}
		cfg.KOReader.MatchMode = tc.in
		NormalizeConfig(cfg)
		if cfg.KOReader.MatchMode != tc.want {
			t.Fatalf("MatchMode(%q) = %q, want %q", tc.in, cfg.KOReader.MatchMode, tc.want)
		}
	}
}

// TestNormalizeLLMConfigProviders 覆盖 provider 分支：ollama 默认、openai 推导 api_mode/request_path、openai-legacy 迁移。
func TestNormalizeLLMConfigProviders(t *testing.T) {
	// 空 provider -> ollama 默认，端点为 base，清空 openai 专属字段。
	c1 := &Config{}
	c1.LLM.RequestPath = "/leftover"
	c1.LLM.APIMode = "leftover"
	NormalizeConfig(c1)
	if c1.LLM.Provider != "ollama" {
		t.Fatalf("empty provider should default to ollama, got %q", c1.LLM.Provider)
	}
	if c1.LLM.BaseURL != "http://localhost:11434" {
		t.Fatalf("ollama base default = %q", c1.LLM.BaseURL)
	}
	if c1.LLM.APIMode != "" || c1.LLM.RequestPath != "" {
		t.Fatalf("non-openai provider should clear api_mode/request_path, got mode=%q path=%q", c1.LLM.APIMode, c1.LLM.RequestPath)
	}
	if c1.LLM.Endpoint != "http://localhost:11434" {
		t.Fatalf("ollama endpoint = %q", c1.LLM.Endpoint)
	}
	if c1.LLM.Timeout != 120 {
		t.Fatalf("zero timeout should default to 120, got %d", c1.LLM.Timeout)
	}

	// openai + request_path 含 chat/completions -> 推导 chat_completions。
	c2 := &Config{}
	c2.LLM.Provider = "openai"
	c2.LLM.BaseURL = "https://api.openai.com"
	c2.LLM.RequestPath = "/v1/chat/completions"
	NormalizeConfig(c2)
	if c2.LLM.APIMode != "chat_completions" {
		t.Fatalf("api_mode inferred = %q, want chat_completions", c2.LLM.APIMode)
	}
	if c2.LLM.Endpoint != "https://api.openai.com/v1/chat/completions" {
		t.Fatalf("openai endpoint = %q", c2.LLM.Endpoint)
	}

	// openai 无 request_path/api_mode -> 默认 responses。
	c3 := &Config{}
	c3.LLM.Provider = "openai"
	c3.LLM.BaseURL = "https://api.openai.com"
	NormalizeConfig(c3)
	if c3.LLM.APIMode != "responses" {
		t.Fatalf("default openai api_mode = %q, want responses", c3.LLM.APIMode)
	}
	if c3.LLM.RequestPath != "/v1/responses" {
		t.Fatalf("default openai request_path = %q, want /v1/responses", c3.LLM.RequestPath)
	}

	// openai-legacy -> 迁移为 openai + chat_completions。
	c4 := &Config{}
	c4.LLM.Provider = "openai-legacy"
	c4.LLM.BaseURL = "https://api.openai.com"
	NormalizeConfig(c4)
	if c4.LLM.Provider != "openai" || c4.LLM.APIMode != "chat_completions" {
		t.Fatalf("openai-legacy migration failed: provider=%q mode=%q", c4.LLM.Provider, c4.LLM.APIMode)
	}
	if c4.LLM.RequestPath != "/v1/chat/completions" {
		t.Fatalf("openai-legacy request_path = %q", c4.LLM.RequestPath)
	}

	// 负超时归一化为 120。
	c5 := &Config{}
	c5.LLM.Timeout = -3
	NormalizeConfig(c5)
	if c5.LLM.Timeout != 120 {
		t.Fatalf("negative timeout should default to 120, got %d", c5.LLM.Timeout)
	}
}

func TestSplitEndpoint(t *testing.T) {
	cases := []struct {
		in       string
		wantBase string
		wantPath string
	}{
		{"https://api.openai.com/v1/responses", "https://api.openai.com", "/v1/responses"},
		{"http://host:11434/api/gen?stream=1", "http://host:11434", "/api/gen?stream=1"},
		{"", "", ""},
		{"localhost:11434", "localhost:11434", ""}, // 无 host，原样返回
	}
	for _, tc := range cases {
		base, path := splitEndpoint(tc.in)
		if base != tc.wantBase || path != tc.wantPath {
			t.Fatalf("splitEndpoint(%q) = %q,%q; want %q,%q", tc.in, base, path, tc.wantBase, tc.wantPath)
		}
	}
}

func TestInferAPIModeAndDefaultRequestPath(t *testing.T) {
	if got := inferAPIModeFromRequestPath("/v1/chat/completions"); got != "chat_completions" {
		t.Fatalf("infer chat = %q", got)
	}
	if got := inferAPIModeFromRequestPath("/v1/responses"); got != "responses" {
		t.Fatalf("infer responses = %q", got)
	}
	if got := inferAPIModeFromRequestPath("/unknown"); got != "responses" {
		t.Fatalf("infer default = %q, want responses", got)
	}
	if got := defaultRequestPath("chat_completions"); got != "/v1/chat/completions" {
		t.Fatalf("default path chat = %q", got)
	}
	if got := defaultRequestPath("responses"); got != "/v1/responses" {
		t.Fatalf("default path responses = %q", got)
	}
	if got := defaultRequestPath(""); got != "/v1/responses" {
		t.Fatalf("default path empty = %q, want /v1/responses", got)
	}
}

func TestBuildLLMEndpoint(t *testing.T) {
	if got := BuildLLMEndpoint(nil); got != "" {
		t.Fatalf("nil cfg endpoint = %q, want empty", got)
	}
	// 空 base -> 空。
	if got := BuildLLMEndpoint(&Config{}); got != "" {
		t.Fatalf("empty base endpoint = %q, want empty", got)
	}
	// ollama：仅裁剪尾部斜杠。
	ollama := &Config{}
	ollama.LLM.Provider = "ollama"
	ollama.LLM.BaseURL = "http://localhost:11434/"
	if got := BuildLLMEndpoint(ollama); got != "http://localhost:11434" {
		t.Fatalf("ollama endpoint = %q", got)
	}
	// openai：base + request_path（无前导斜杠也能拼接）。
	openai := &Config{}
	openai.LLM.Provider = "openai"
	openai.LLM.BaseURL = "https://api.openai.com/"
	openai.LLM.RequestPath = "v1/responses"
	if got := BuildLLMEndpoint(openai); got != "https://api.openai.com/v1/responses" {
		t.Fatalf("openai endpoint = %q", got)
	}
}

// validBaseConfig 构造一份可通过 ValidateConfig 的配置，供正/负用例微调。
func validBaseConfig(t *testing.T) *Config {
	t.Helper()
	dir := t.TempDir()
	cfg := &Config{}
	cfg.Server.Port = 8080
	cfg.Server.Host = "0.0.0.0"
	cfg.Server.AllowedOrigins = []string{"http://*"}
	cfg.Database.Path = filepath.Join(dir, "manga.db")
	cfg.Cache.Dir = dir
	cfg.Logging.Level = LogLevelInfo
	cfg.Scanner.ScanProfile = ScanProfileMetadata
	cfg.Scanner.ArchivePoolSize = 5
	cfg.Scanner.MaxAiConcurrency = 3
	cfg.Scanner.ThumbnailFormat = "webp"
	cfg.Library.StorageProfile = StorageProfileAuto
	cfg.LLM.Provider = "ollama"
	cfg.LLM.BaseURL = "http://localhost:11434"
	cfg.LLM.Model = "qwen2.5"
	cfg.LLM.Timeout = 120
	cfg.KOReader.BasePath = "/koreader"
	cfg.KOReader.MatchMode = KOReaderMatchModeBinaryHash
	return cfg
}

func TestValidateConfigAcceptsValidConfig(t *testing.T) {
	cfg := validBaseConfig(t)
	result := ValidateConfig(cfg)
	if !result.Valid {
		t.Fatalf("expected valid config, got issues: %+v", result.Issues)
	}
}

func TestValidateConfigRejectsFieldByField(t *testing.T) {
	hasIssue := func(res ValidationResult, field string) bool {
		for _, i := range res.Issues {
			if i.Field == field {
				return true
			}
		}
		return false
	}
	cases := []struct {
		name   string
		mutate func(*Config)
		field  string
	}{
		{"port-too-low", func(c *Config) { c.Server.Port = 0 }, "server.port"},
		{"port-too-high", func(c *Config) { c.Server.Port = 70000 }, "server.port"},
		{"empty-origins", func(c *Config) { c.Server.AllowedOrigins = nil }, "server.allowed_origins"},
		{"bad-origin", func(c *Config) { c.Server.AllowedOrigins = []string{"example.com"} }, "server.allowed_origins"},
		{"empty-model", func(c *Config) { c.LLM.Model = "" }, "llm.model"},
		{"timeout-too-low", func(c *Config) { c.LLM.Timeout = 5 }, "llm.timeout"},
		{"timeout-too-high", func(c *Config) { c.LLM.Timeout = 900 }, "llm.timeout"},
		{"bad-thumbnail", func(c *Config) { c.Scanner.ThumbnailFormat = "gif" }, "scanner.thumbnail_format"},
		{"negative-workers", func(c *Config) { c.Scanner.Workers = -1 }, "scanner.workers"},
		{"bad-match-mode", func(c *Config) { c.KOReader.MatchMode = "weird" }, "koreader.match_mode"},
		{"bad-base-path", func(c *Config) { c.KOReader.BasePath = "koreader" }, "koreader.base_path"},
		{"bad-scan-profile", func(c *Config) { c.Scanner.ScanProfile = "turbo" }, "scanner.scan_profile"},
		{"openai-bad-api-mode", func(c *Config) {
			c.LLM.Provider = "openai"
			c.LLM.BaseURL = "https://api.openai.com"
			c.LLM.RequestPath = "/v1/responses"
			c.LLM.APIMode = "foo"
		}, "llm.api_mode"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validBaseConfig(t)
			tc.mutate(cfg)
			res := ValidateConfig(cfg)
			if res.Valid {
				t.Fatalf("expected invalid config for %s", tc.name)
			}
			if !hasIssue(res, tc.field) {
				t.Fatalf("expected issue for field %q, got %+v", tc.field, res.Issues)
			}
		})
	}
}

func TestValidateConfigNilIsInvalid(t *testing.T) {
	res := ValidateConfig(nil)
	if res.Valid || len(res.Issues) == 0 {
		t.Fatalf("nil config should be invalid with issues, got %+v", res)
	}
}

// TestNormalizeScanFormatsCSV 覆盖去重、过滤不支持格式、大小写与空回退。
func TestNormalizeScanFormatsCSV(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", DefaultScanFormatsCSV},
		{"   ", DefaultScanFormatsCSV},
		{"png,gif", DefaultScanFormatsCSV}, // 全部不支持 -> 默认
		{" CBZ, zip, cbz, png, rar ", "cbz,zip,rar"},
	}
	for _, tc := range cases {
		if got := NormalizeScanFormatsCSV(tc.in); got != tc.want {
			t.Fatalf("NormalizeScanFormatsCSV(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	if got := ParseScanFormats("cbz,,rar"); strings.Join(got, ",") != "cbz,rar" {
		t.Fatalf("ParseScanFormats dedupe/filter = %v", got)
	}
	if got := ParseScanFormats(""); strings.Join(got, ",") != DefaultScanFormatsCSV {
		t.Fatalf("ParseScanFormats empty = %v", got)
	}
}

func TestIsSupportedScanFormatAndExtension(t *testing.T) {
	if !IsSupportedScanFormat(".CBZ") {
		t.Fatalf("leading-dot uppercase CBZ should be supported")
	}
	if IsSupportedScanFormat("pdf") {
		t.Fatalf("pdf should not be a supported scan format")
	}
	if !IsSupportedArchiveExtension(".rar") {
		t.Fatalf(".rar extension should be supported")
	}
	if IsSupportedArchiveExtension(".pdf") {
		t.Fatalf(".pdf extension should not be supported")
	}
}

// TestNormalizeStorageIOPolicyProfiles 覆盖各存储画像的默认并发与低影响开关。
func TestNormalizeStorageIOPolicyProfiles(t *testing.T) {
	// HDD 外置：空策略 -> 并发全为 1，低影响开关全开。
	hdd := NormalizeStorageIOPolicy(StorageProfileHDDExternal, StorageIOPolicy{})
	if hdd.ScanConcurrency != 1 || hdd.ArchiveOpenConcurrency != 1 || hdd.CoverConcurrency != 1 || hdd.HashConcurrency != 1 {
		t.Fatalf("hdd default concurrency should be 1, got %+v", hdd)
	}
	if !hdd.PauseBackgroundWhenReading || !hdd.IdleOnlyHeavyTasks || !hdd.DisableSameDiskPageCache {
		t.Fatalf("hdd low-impact toggles should be on, got %+v", hdd)
	}

	// HDD 外置：用户显式并发保留，但低影响开关仍被强制打开，未设的其余并发填 1。
	hddCustom := NormalizeStorageIOPolicy(StorageProfileHDDExternal, StorageIOPolicy{ScanConcurrency: 4})
	if hddCustom.ScanConcurrency != 4 {
		t.Fatalf("explicit scan concurrency should be preserved, got %d", hddCustom.ScanConcurrency)
	}
	if hddCustom.CoverConcurrency != 1 {
		t.Fatalf("unset cover concurrency should default to 1, got %d", hddCustom.CoverConcurrency)
	}
	if !hddCustom.PauseBackgroundWhenReading {
		t.Fatalf("hdd should force low-impact toggles even with custom concurrency")
	}

	// SSD/auto：默认并发 0（不限），无低影响开关。
	ssd := NormalizeStorageIOPolicy(StorageProfileSSD, StorageIOPolicy{})
	if ssd.ScanConcurrency != 0 || ssd.PauseBackgroundWhenReading {
		t.Fatalf("ssd defaults should be unbounded without low-impact toggles, got %+v", ssd)
	}
}

func TestNormalizeStorageProfile(t *testing.T) {
	cases := map[string]string{
		"SSD":          StorageProfileSSD,
		" Network ":    StorageProfileNetwork,
		"hdd_external": StorageProfileHDDExternal,
		"custom":       StorageProfileCustom,
		"":             StorageProfileAuto,
		"nonsense":     StorageProfileAuto,
	}
	for in, want := range cases {
		if got := NormalizeStorageProfile(in); got != want {
			t.Fatalf("NormalizeStorageProfile(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestVolumeKeyAndSameVolume 覆盖卷标提取与同卷判定（跨平台性质断言）。
func TestVolumeKeyAndSameVolume(t *testing.T) {
	if VolumeKey("") != "" {
		t.Fatalf("empty path should have empty volume key")
	}
	if VolumeKey("   ") != "" {
		t.Fatalf("blank path should have empty volume key")
	}
	// 空卷标不算同卷。
	if SameVolume("", "/anything") {
		t.Fatalf("empty volume should never match")
	}
	// 同一父目录下的两个临时子目录必然同卷。
	dirA := t.TempDir()
	dirB := t.TempDir()
	if VolumeKey(dirA) == "" {
		t.Fatalf("temp dir should have non-empty volume key")
	}
	if !SameVolume(dirA, dirB) {
		t.Fatalf("sibling temp dirs should be on the same volume: %q vs %q", VolumeKey(dirA), VolumeKey(dirB))
	}
}

// TestResolveStoragePolicyFallsBackToLibraryDefault 当路径不落在任何策略内时使用库级默认画像。
func TestResolveStoragePolicyFallsBackToLibraryDefault(t *testing.T) {
	root := t.TempDir()
	cfg := Config{}
	cfg.Library.StorageProfile = StorageProfileSSD
	cfg.Library.StoragePolicies = []LibraryStoragePolicy{
		{Path: filepath.Join(root, "elsewhere"), StorageProfile: StorageProfileHDDExternal},
	}
	// 目标路径不在 elsewhere 之内 -> 回退库级 SSD。
	resolved := ResolveStoragePolicy(cfg, filepath.Join(root, "other", "book.cbz"))
	if resolved.StorageProfile != StorageProfileSSD {
		t.Fatalf("expected fallback to library default ssd, got %q", resolved.StorageProfile)
	}
	if resolved.VolumeKey == "" {
		t.Fatalf("resolved policy should carry a volume key")
	}
}
