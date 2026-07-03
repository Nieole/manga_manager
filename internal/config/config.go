// 业务说明：本文件是业务实现，属于运行时配置管理层，负责读取、归一化和持久化漫画库、扫描、元数据、AI 和服务端选项。
// 它是后端各服务共享配置的来源，影响扫描路径、外部库、图片缓存和前端设置页展示。
// 维护时应避免直接修改配置副本，新增字段需要兼顾默认值、兼容迁移和前端表单含义。

package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server struct {
		Host           string   `yaml:"host" json:"host"`
		Port           int      `yaml:"port" json:"port"`
		AllowedOrigins []string `yaml:"allowed_origins" json:"allowed_origins"`
		// Auth 是可选的管理 API 令牌鉴权。默认关闭（Enabled=false），此时行为与历史版本
		// 完全一致（无鉴权）；启用后，管理端点要求携带匹配 Token 的令牌，阅读协议
		// （OPDS/Mihon/KOReader）仍走各自的鉴权模型。Token 为敏感字段，回显前端时会被脱敏。
		Auth struct {
			Enabled bool   `yaml:"enabled" json:"enabled"`
			Token   string `yaml:"token" json:"token"`
		} `yaml:"auth" json:"auth"`
	} `yaml:"server" json:"server"`
	Database struct {
		Path string `yaml:"path" json:"path"`
	} `yaml:"database" json:"database"`
	Library struct {
		Paths           []string               `yaml:"paths" json:"paths"`
		StorageProfile  string                 `yaml:"storage_profile" json:"storage_profile"`
		IOPolicy        StorageIOPolicy        `yaml:"io_policy" json:"io_policy"`
		StoragePolicies []LibraryStoragePolicy `yaml:"storage_policies" json:"storage_policies"`
	} `yaml:"library" json:"library"`
	Cache struct {
		Dir                  string `yaml:"dir" json:"dir"`
		PageDiskCacheEnabled bool   `yaml:"page_disk_cache_enabled" json:"page_disk_cache_enabled"`
	} `yaml:"cache" json:"cache"`
	Logging struct {
		Level string `yaml:"level" json:"level"`
	} `yaml:"logging" json:"logging"`
	Scanner struct {
		Workers          int    `yaml:"workers" json:"workers"`
		ScanProfile      string `yaml:"scan_profile" json:"scan_profile"`
		ThumbnailFormat  string `yaml:"thumbnail_format" json:"thumbnail_format"`
		Waifu2xPath      string `yaml:"waifu2x_path" json:"waifu2x_path"`
		RealCuganPath    string `yaml:"realcugan_path" json:"realcugan_path"`
		ArchivePoolSize  int    `yaml:"archive_pool_size" json:"archive_pool_size"`
		MaxAiConcurrency int    `yaml:"max_ai_concurrency" json:"max_ai_concurrency"`
	} `yaml:"scanner" json:"scanner"`
	Ollama struct {
		Endpoint string `yaml:"endpoint" json:"endpoint"`
		Model    string `yaml:"model" json:"model"`
	} `yaml:"ollama" json:"ollama"` // Deprecated: Use LLM instead

	LLM struct {
		Provider    string `yaml:"provider" json:"provider"`         // e.g. "ollama", "openai"
		APIMode     string `yaml:"api_mode" json:"api_mode"`         // "responses" or "chat_completions"
		BaseURL     string `yaml:"base_url" json:"base_url"`         // e.g. "http://localhost:11434" or "https://api.openai.com"
		RequestPath string `yaml:"request_path" json:"request_path"` // e.g. "/v1/responses"
		Endpoint    string `yaml:"endpoint" json:"endpoint"`         // Deprecated: kept for backwards compatibility
		Model       string `yaml:"model" json:"model"`               // e.g. "qwen2.5" or "gpt-4o"
		APIKey      string `yaml:"api_key" json:"api_key"`           // Optional API Key for OpenAI/DeepSeek
		Timeout     int    `yaml:"timeout" json:"timeout"`           // 请求超时时间（秒），默认 120
	} `yaml:"llm" json:"llm"`
	Protocols struct {
		OPDS struct {
			Enabled bool `yaml:"enabled" json:"enabled"`
		} `yaml:"opds" json:"opds"`
		Mihon struct {
			Enabled bool `yaml:"enabled" json:"enabled"`
		} `yaml:"mihon" json:"mihon"`
	} `yaml:"protocols" json:"protocols"`
	KOReader struct {
		Enabled             bool   `yaml:"enabled" json:"enabled"`
		BasePath            string `yaml:"base_path" json:"base_path"`
		AllowRegistration   bool   `yaml:"allow_registration" json:"allow_registration"`
		MatchMode           string `yaml:"match_mode" json:"match_mode"`
		PathIgnoreExtension bool   `yaml:"path_ignore_extension" json:"path_ignore_extension"`
	} `yaml:"koreader" json:"koreader"`
}

const (
	KOReaderMatchModeBinaryHash = "binary_hash"
	KOReaderMatchModeFilePath   = "file_path"
	KOReaderPathMatchDepth      = 2
	LogLevelDebug               = "debug"
	LogLevelInfo                = "info"
	LogLevelWarn                = "warn"
	LogLevelError               = "error"
)

// SecretMask 是回显给前端的敏感字段占位符。前端把它原样存进只写输入框（如 <input type=password>），
// 保存时若字段仍等于该占位符，后端据此保留原值（见 RestoreMaskedSecrets），从而既不向客户端泄露
// 明文密钥，又不会因前端回传占位符而把真实密钥覆盖掉。占位符本身不是任何合法密钥。
const SecretMask = "__mm_secret_unchanged__"

// MaskSecrets 返回 cfg 的副本，将敏感字段（LLM APIKey、Server.Auth.Token）替换为占位符。
// 仅当字段非空时替换，空值保持为空，便于前端区分“已设置”与“未设置”。
func MaskSecrets(cfg Config) Config {
	if cfg.LLM.APIKey != "" {
		cfg.LLM.APIKey = SecretMask
	}
	if cfg.Server.Auth.Token != "" {
		cfg.Server.Auth.Token = SecretMask
	}
	return cfg
}

// RestoreMaskedSecrets 把 incoming 中仍为占位符的敏感字段用 current 的真实值回填。
// 前端保存整份配置时会把未改动的密钥以占位符形式回传，此处据此避免真实密钥被占位符覆盖。
func RestoreMaskedSecrets(incoming *Config, current Config) {
	if incoming == nil {
		return
	}
	if incoming.LLM.APIKey == SecretMask {
		incoming.LLM.APIKey = current.LLM.APIKey
	}
	if incoming.Server.Auth.Token == SecretMask {
		incoming.Server.Auth.Token = current.Server.Auth.Token
	}
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return createDefaultConfig(path)
		}
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Backwards compatibility layer
	if cfg.LLM.Provider == "" && cfg.Ollama.Endpoint != "" {
		cfg.LLM.Provider = "ollama"
		cfg.LLM.BaseURL = cfg.Ollama.Endpoint
		cfg.LLM.Model = cfg.Ollama.Model
	}
	// Defaults if LLM is entirely absent
	if cfg.LLM.Provider == "" {
		cfg.LLM.Provider = "ollama"
		cfg.LLM.BaseURL = "http://localhost:11434"
		cfg.LLM.Model = "qwen2.5"
	}
	NormalizeConfig(&cfg)

	return &cfg, nil
}

func createDefaultConfig(path string) (*Config, error) {
	cfg := &Config{}
	cfg.Server.Host = "0.0.0.0"
	cfg.Server.Port = 8080
	cfg.Server.AllowedOrigins = []string{"http://*", "https://*"}
	cfg.Database.Path = "./data/manga.db"
	cfg.Library.Paths = []string{}
	cfg.Library.StorageProfile = StorageProfileAuto
	cfg.Cache.Dir = "./data/cache"
	cfg.Cache.PageDiskCacheEnabled = false
	cfg.Logging.Level = LogLevelInfo
	cfg.Scanner.Workers = 0 // 0 表示自动使用 runtime.NumCPU() * 2
	cfg.Scanner.ScanProfile = ScanProfileMetadata
	cfg.Scanner.ThumbnailFormat = "webp" // 支持 webp, jpg, avif
	cfg.Scanner.Waifu2xPath = ""
	cfg.Scanner.RealCuganPath = ""
	cfg.Scanner.ArchivePoolSize = 5  // 默认缓存 5 个打开的归档压缩包句柄
	cfg.Scanner.MaxAiConcurrency = 3 // 默认限制最多抛出 3 个外置 AI 渲染子进程

	cfg.LLM.Provider = "ollama"
	cfg.LLM.BaseURL = "http://localhost:11434"
	cfg.LLM.RequestPath = ""
	cfg.LLM.APIMode = ""
	cfg.LLM.Model = "qwen2.5"
	cfg.LLM.Timeout = 120
	cfg.Protocols.OPDS.Enabled = false
	cfg.Protocols.Mihon.Enabled = false
	cfg.KOReader.Enabled = false
	cfg.KOReader.BasePath = "/koreader"
	cfg.KOReader.AllowRegistration = false
	cfg.KOReader.MatchMode = KOReaderMatchModeBinaryHash
	cfg.KOReader.PathIgnoreExtension = false
	NormalizeConfig(cfg)

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll("./data", 0755); err != nil {
		return nil, err
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return nil, err
	}

	return cfg, nil
}

func normalizeLLMConfig(cfg *Config) {
	if cfg == nil {
		return
	}

	provider := strings.ToLower(strings.TrimSpace(cfg.LLM.Provider))
	if provider == "" {
		provider = "ollama"
		cfg.LLM.Provider = provider
	}

	if cfg.LLM.BaseURL == "" && cfg.LLM.Endpoint != "" {
		cfg.LLM.BaseURL, cfg.LLM.RequestPath = splitEndpoint(cfg.LLM.Endpoint)
	}

	switch provider {
	case "openai-legacy":
		cfg.LLM.Provider = "openai"
		if cfg.LLM.APIMode == "" {
			cfg.LLM.APIMode = "chat_completions"
		}
	case "openai":
		if cfg.LLM.APIMode == "" {
			cfg.LLM.APIMode = inferAPIModeFromRequestPath(cfg.LLM.RequestPath)
		}
	default:
		cfg.LLM.APIMode = ""
		cfg.LLM.RequestPath = ""
	}

	if cfg.LLM.BaseURL == "" {
		if cfg.LLM.Provider == "openai" {
			cfg.LLM.BaseURL = "https://api.openai.com"
		} else {
			cfg.LLM.BaseURL = "http://localhost:11434"
		}
	}

	if cfg.LLM.Provider == "openai" && cfg.LLM.RequestPath == "" {
		cfg.LLM.RequestPath = defaultRequestPath(cfg.LLM.APIMode)
	}

	cfg.LLM.Endpoint = BuildLLMEndpoint(cfg)

	if cfg.LLM.Timeout <= 0 {
		cfg.LLM.Timeout = 120
	}
}

func NormalizeConfig(cfg *Config) {
	if cfg == nil {
		return
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	cfg.Server.Host = strings.TrimSpace(cfg.Server.Host)
	if cfg.Server.Host == "" {
		cfg.Server.Host = "0.0.0.0"
	}
	cfg.Server.AllowedOrigins = normalizeAllowedOrigins(cfg.Server.AllowedOrigins)
	if cfg.Database.Path == "" {
		cfg.Database.Path = "./data/manga.db"
	}
	NormalizeLibraryStorageConfig(cfg)
	if cfg.Cache.Dir == "" {
		cfg.Cache.Dir = "./data/cache"
	}
	level := strings.ToLower(strings.TrimSpace(cfg.Logging.Level))
	switch level {
	case LogLevelDebug, LogLevelInfo, LogLevelWarn, LogLevelError:
	default:
		level = LogLevelInfo
	}
	cfg.Logging.Level = level
	if cfg.Scanner.ThumbnailFormat == "" {
		cfg.Scanner.ThumbnailFormat = "webp"
	}
	cfg.Scanner.ScanProfile = NormalizeScanProfile(cfg.Scanner.ScanProfile)
	if cfg.Scanner.ArchivePoolSize == 0 {
		cfg.Scanner.ArchivePoolSize = 5
	}
	if cfg.Scanner.MaxAiConcurrency == 0 {
		cfg.Scanner.MaxAiConcurrency = 3
	}
	normalizeLLMConfig(cfg)
	basePath := strings.TrimSpace(cfg.KOReader.BasePath)
	if basePath == "" {
		basePath = "/koreader"
	}
	if !strings.HasPrefix(basePath, "/") {
		basePath = "/" + basePath
	}
	basePath = "/" + strings.Trim(strings.TrimSpace(basePath), "/")
	if basePath == "//" || basePath == "" {
		basePath = "/koreader"
	}
	cfg.KOReader.BasePath = basePath
	matchMode := strings.TrimSpace(strings.ToLower(cfg.KOReader.MatchMode))
	switch matchMode {
	case KOReaderMatchModeBinaryHash, KOReaderMatchModeFilePath:
	default:
		matchMode = KOReaderMatchModeBinaryHash
	}
	cfg.KOReader.MatchMode = matchMode
}

func normalizeAllowedOrigins(origins []string) []string {
	normalized := make([]string, 0, len(origins))
	seen := make(map[string]struct{}, len(origins))
	for _, origin := range origins {
		origin = strings.TrimSpace(origin)
		if origin == "" {
			continue
		}
		if _, exists := seen[origin]; exists {
			continue
		}
		seen[origin] = struct{}{}
		normalized = append(normalized, origin)
	}
	if len(normalized) == 0 {
		return []string{"http://*", "https://*"}
	}
	return normalized
}

func splitEndpoint(raw string) (string, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return raw, ""
	}

	base := fmt.Sprintf("%s://%s", parsed.Scheme, parsed.Host)
	requestPath := parsed.EscapedPath()
	if parsed.RawQuery != "" {
		requestPath += "?" + parsed.RawQuery
	}
	return base, requestPath
}

func inferAPIModeFromRequestPath(path string) string {
	path = strings.ToLower(strings.TrimSpace(path))
	switch {
	case strings.Contains(path, "chat/completions"):
		return "chat_completions"
	case strings.Contains(path, "responses"):
		return "responses"
	default:
		return "responses"
	}
}

func defaultRequestPath(apiMode string) string {
	if strings.EqualFold(apiMode, "chat_completions") {
		return "/v1/chat/completions"
	}
	return "/v1/responses"
}

func BuildLLMEndpoint(cfg *Config) string {
	if cfg == nil {
		return ""
	}

	baseURL := strings.TrimSpace(cfg.LLM.BaseURL)
	if baseURL == "" {
		return ""
	}
	if cfg.LLM.Provider != "openai" {
		return strings.TrimRight(baseURL, "/")
	}

	requestPath := strings.TrimSpace(cfg.LLM.RequestPath)
	if requestPath == "" {
		requestPath = defaultRequestPath(cfg.LLM.APIMode)
	}

	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(requestPath, "/")
}
