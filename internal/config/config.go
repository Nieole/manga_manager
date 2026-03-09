package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server struct {
		Port int `yaml:"port" json:"port"`
	} `yaml:"server" json:"server"`
	Database struct {
		Path string `yaml:"path" json:"path"`
	} `yaml:"database" json:"database"`
	Library struct {
		Paths []string `yaml:"paths" json:"paths"`
	} `yaml:"library" json:"library"`
	Cache struct {
		Dir string `yaml:"dir" json:"dir"`
	} `yaml:"cache" json:"cache"`
	Scanner struct {
		Workers          int    `yaml:"workers" json:"workers"`
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
		Provider string `yaml:"provider" json:"provider"` // e.g. "ollama", "openai"
		Endpoint string `yaml:"endpoint" json:"endpoint"` // e.g. "http://localhost:11434" or "https://api.openai.com/v1"
		Model    string `yaml:"model" json:"model"`       // e.g. "qwen2.5" or "gpt-4o"
		APIKey   string `yaml:"api_key" json:"api_key"`   // Optional API Key for OpenAI/DeepSeek
	} `yaml:"llm" json:"llm"`
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
		cfg.LLM.Endpoint = cfg.Ollama.Endpoint
		cfg.LLM.Model = cfg.Ollama.Model
	}
	// Defaults if LLM is entirely absent
	if cfg.LLM.Provider == "" {
		cfg.LLM.Provider = "ollama"
		cfg.LLM.Endpoint = "http://localhost:11434"
		cfg.LLM.Model = "qwen2.5"
	}

	return &cfg, nil
}

func createDefaultConfig(path string) (*Config, error) {
	cfg := &Config{}
	cfg.Server.Port = 8080
	cfg.Database.Path = "./data/manga.db"
	cfg.Library.Paths = []string{}
	cfg.Cache.Dir = "./data/cache"
	cfg.Scanner.Workers = 0              // 0 表示自动使用 runtime.NumCPU() * 2
	cfg.Scanner.ThumbnailFormat = "webp" // 支持 webp, jpg, avif
	cfg.Scanner.Waifu2xPath = ""
	cfg.Scanner.RealCuganPath = ""
	cfg.Scanner.ArchivePoolSize = 5  // 默认缓存 5 个打开的归档压缩包句柄
	cfg.Scanner.MaxAiConcurrency = 3 // 默认限制最多抛出 3 个外置 AI 渲染子进程

	cfg.LLM.Provider = "ollama"
	cfg.LLM.Endpoint = "http://localhost:11434"
	cfg.LLM.Model = "qwen2.5"

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
