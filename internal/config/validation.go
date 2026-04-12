package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ValidationIssue struct {
	Field    string `json:"field"`
	Message  string `json:"message"`
	Severity string `json:"severity"`
}

type ValidationResult struct {
	Valid  bool              `json:"valid"`
	Issues []ValidationIssue `json:"issues"`
}

func ValidateConfig(cfg *Config) ValidationResult {
	issues := make([]ValidationIssue, 0)
	if cfg == nil {
		return ValidationResult{
			Valid: false,
			Issues: []ValidationIssue{
				{Field: "config", Message: "配置不能为空。", Severity: "error"},
			},
		}
	}

	if cfg.Server.Port < 1 || cfg.Server.Port > 65535 {
		issues = append(issues, ValidationIssue{Field: "server.port", Message: "端口必须在 1 到 65535 之间。", Severity: "error"})
	}

	if strings.TrimSpace(cfg.Database.Path) == "" {
		issues = append(issues, ValidationIssue{Field: "database.path", Message: "数据库路径不能为空。", Severity: "error"})
	} else if err := checkParentDir(cfg.Database.Path); err != nil {
		issues = append(issues, ValidationIssue{Field: "database.path", Message: err.Error(), Severity: "error"})
	}

	if strings.TrimSpace(cfg.Cache.Dir) == "" {
		issues = append(issues, ValidationIssue{Field: "cache.dir", Message: "缓存目录不能为空。", Severity: "error"})
	} else if err := checkDir(cfg.Cache.Dir); err != nil {
		issues = append(issues, ValidationIssue{Field: "cache.dir", Message: err.Error(), Severity: "error"})
	}

	if cfg.Scanner.Workers < 0 {
		issues = append(issues, ValidationIssue{Field: "scanner.workers", Message: "工作协程数不能小于 0。", Severity: "error"})
	}
	if cfg.Scanner.ArchivePoolSize < 1 {
		issues = append(issues, ValidationIssue{Field: "scanner.archive_pool_size", Message: "归档句柄池大小至少为 1。", Severity: "error"})
	}
	if cfg.Scanner.MaxAiConcurrency < 1 {
		issues = append(issues, ValidationIssue{Field: "scanner.max_ai_concurrency", Message: "AI 并发数至少为 1。", Severity: "error"})
	}

	format := strings.ToLower(strings.TrimSpace(cfg.Scanner.ThumbnailFormat))
	switch format {
	case "webp", "avif", "jpg", "jpeg":
	default:
		issues = append(issues, ValidationIssue{Field: "scanner.thumbnail_format", Message: "缩略图格式仅支持 webp、avif、jpg。", Severity: "error"})
	}

	for _, item := range []struct {
		field string
		value string
	}{
		{field: "scanner.waifu2x_path", value: cfg.Scanner.Waifu2xPath},
		{field: "scanner.realcugan_path", value: cfg.Scanner.RealCuganPath},
	} {
		if strings.TrimSpace(item.value) == "" {
			continue
		}
		if info, err := os.Stat(item.value); err != nil {
			issues = append(issues, ValidationIssue{Field: item.field, Message: "指定的可执行文件不存在或不可访问。", Severity: "error"})
		} else if info.IsDir() {
			issues = append(issues, ValidationIssue{Field: item.field, Message: "这里需要填写可执行文件路径，而不是目录。", Severity: "error"})
		}
	}

	provider := strings.ToLower(strings.TrimSpace(cfg.LLM.Provider))
	switch provider {
	case "", "ollama", "openai":
	default:
		issues = append(issues, ValidationIssue{Field: "llm.provider", Message: "当前仅支持 ollama 和 openai 兼容协议。", Severity: "error"})
	}

	if strings.TrimSpace(cfg.LLM.Model) == "" {
		issues = append(issues, ValidationIssue{Field: "llm.model", Message: "模型名不能为空。", Severity: "error"})
	}
	if cfg.LLM.Timeout < 10 || cfg.LLM.Timeout > 600 {
		issues = append(issues, ValidationIssue{Field: "llm.timeout", Message: "超时时间建议在 10 到 600 秒之间。", Severity: "error"})
	}

	if provider == "openai" {
		if strings.TrimSpace(cfg.LLM.BaseURL) == "" {
			issues = append(issues, ValidationIssue{Field: "llm.base_url", Message: "OpenAI 兼容模式需要 Base URL。", Severity: "error"})
		}
		if apiMode := strings.TrimSpace(cfg.LLM.APIMode); apiMode != "responses" && apiMode != "chat_completions" {
			issues = append(issues, ValidationIssue{Field: "llm.api_mode", Message: "API 模式必须是 responses 或 chat_completions。", Severity: "error"})
		}
		if strings.TrimSpace(cfg.LLM.RequestPath) == "" {
			issues = append(issues, ValidationIssue{Field: "llm.request_path", Message: "OpenAI 兼容模式需要请求路径。", Severity: "error"})
		}
	} else if strings.TrimSpace(cfg.LLM.BaseURL) == "" {
		issues = append(issues, ValidationIssue{Field: "llm.base_url", Message: "Ollama 地址不能为空。", Severity: "error"})
	}

	if basePath := strings.TrimSpace(cfg.KOReader.BasePath); basePath == "" {
		issues = append(issues, ValidationIssue{Field: "koreader.base_path", Message: "KOReader 同步路径不能为空。", Severity: "error"})
	} else if !strings.HasPrefix(basePath, "/") {
		issues = append(issues, ValidationIssue{Field: "koreader.base_path", Message: "KOReader 同步路径必须以 / 开头。", Severity: "error"})
	}

	return ValidationResult{
		Valid:  len(issues) == 0,
		Issues: issues,
	}
}

func checkParentDir(filePath string) error {
	dir := filepath.Dir(strings.TrimSpace(filePath))
	if dir == "" || dir == "." {
		return nil
	}
	return checkDir(dir)
}

func checkDir(dir string) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return fmt.Errorf("目录不能为空。")
	}

	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			parent := filepath.Dir(dir)
			if parent == "" || parent == "." {
				return nil
			}
			parentInfo, parentErr := os.Stat(parent)
			if parentErr != nil {
				return fmt.Errorf("目录的父路径不存在或不可访问：%w", parentErr)
			}
			if !parentInfo.IsDir() {
				return fmt.Errorf("目录的父路径不是目录。")
			}
			return nil
		}
		return fmt.Errorf("目录不可访问：%w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("该路径不是目录。")
	}
	return nil
}
