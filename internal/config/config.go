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
		Workers         int    `yaml:"workers" json:"workers"`
		ThumbnailFormat string `yaml:"thumbnail_format" json:"thumbnail_format"`
		Waifu2xPath     string `yaml:"waifu2x_path" json:"waifu2x_path"`
	} `yaml:"scanner" json:"scanner"`
	Ollama struct {
		Endpoint string `yaml:"endpoint" json:"endpoint"`
		Model    string `yaml:"model" json:"model"`
	} `yaml:"ollama" json:"ollama"`
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
