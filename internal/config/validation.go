// 业务说明：本文件是业务实现，属于运行时配置管理层，负责读取、归一化和持久化漫画库、扫描、元数据、AI 和服务端选项。
// 它是后端各服务共享配置的来源，影响扫描路径、外部库、图片缓存和前端设置页展示。
// 维护时应避免直接修改配置副本，新增字段需要兼顾默认值、兼容迁移和前端表单含义。

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
	if strings.ContainsAny(strings.TrimSpace(cfg.Server.Host), "/?#") {
		issues = append(issues, ValidationIssue{Field: "server.host", Message: "监听地址不能包含 URL 路径、查询或片段。", Severity: "error"})
	}
	if len(cfg.Server.AllowedOrigins) == 0 {
		issues = append(issues, ValidationIssue{Field: "server.allowed_origins", Message: "CORS 允许来源不能为空。", Severity: "error"})
	}
	for _, origin := range cfg.Server.AllowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin == "*" || strings.HasPrefix(origin, "http://") || strings.HasPrefix(origin, "https://") {
			continue
		}
		issues = append(issues, ValidationIssue{Field: "server.allowed_origins", Message: "CORS 来源必须是 http(s) URL 或通配符。", Severity: "error"})
		break
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

	switch strings.ToLower(strings.TrimSpace(cfg.Logging.Level)) {
	case LogLevelDebug, LogLevelInfo, LogLevelWarn, LogLevelError:
	default:
		issues = append(issues, ValidationIssue{Field: "logging.level", Message: "日志级别必须是 debug、info、warn 或 error。", Severity: "error"})
	}

	if cfg.Scanner.Workers < 0 {
		issues = append(issues, ValidationIssue{Field: "scanner.workers", Message: "工作协程数不能小于 0。", Severity: "error"})
	}
	if !IsSupportedScanProfile(cfg.Scanner.ScanProfile) {
		issues = append(issues, ValidationIssue{Field: "scanner.scan_profile", Message: "扫描等级必须是 fast_scan、metadata_scan、identity_scan 或 repair_scan。", Severity: "error"})
	}
	if cfg.Scanner.ArchivePoolSize < 1 {
		issues = append(issues, ValidationIssue{Field: "scanner.archive_pool_size", Message: "归档句柄池大小至少为 1。", Severity: "error"})
	}
	if cfg.Scanner.MaxAiConcurrency < 1 {
		issues = append(issues, ValidationIssue{Field: "scanner.max_ai_concurrency", Message: "AI 并发数至少为 1。", Severity: "error"})
	}

	if !isSupportedStorageProfile(cfg.Library.StorageProfile) {
		issues = append(issues, ValidationIssue{Field: "library.storage_profile", Message: "存储介质策略必须是 auto、ssd、hdd_external、network 或 custom。", Severity: "error"})
	}
	validateIOPolicy := func(prefix string, policy StorageIOPolicy) {
		if policy.ScanConcurrency < 0 {
			issues = append(issues, ValidationIssue{Field: prefix + ".scan_concurrency", Message: "扫描并发不能小于 0。", Severity: "error"})
		}
		if policy.ArchiveOpenConcurrency < 0 {
			issues = append(issues, ValidationIssue{Field: prefix + ".archive_open_concurrency", Message: "归档打开并发不能小于 0。", Severity: "error"})
		}
		if policy.CoverConcurrency < 0 {
			issues = append(issues, ValidationIssue{Field: prefix + ".cover_concurrency", Message: "封面生成并发不能小于 0。", Severity: "error"})
		}
		if policy.HashConcurrency < 0 {
			issues = append(issues, ValidationIssue{Field: prefix + ".hash_concurrency", Message: "Hash 并发不能小于 0。", Severity: "error"})
		}
	}
	validateIOPolicy("library.io_policy", cfg.Library.IOPolicy)
	for i, policy := range cfg.Library.StoragePolicies {
		if strings.TrimSpace(policy.Path) == "" {
			issues = append(issues, ValidationIssue{Field: fmt.Sprintf("library.storage_policies[%d].path", i), Message: "资源库策略路径不能为空。", Severity: "error"})
		}
		if !isSupportedStorageProfile(policy.StorageProfile) {
			issues = append(issues, ValidationIssue{Field: fmt.Sprintf("library.storage_policies[%d].storage_profile", i), Message: "资源库策略必须是 auto、ssd、hdd_external、network 或 custom。", Severity: "error"})
		}
		validateIOPolicy(fmt.Sprintf("library.storage_policies[%d].io_policy", i), policy.IOPolicy)
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
	switch strings.TrimSpace(cfg.KOReader.MatchMode) {
	case KOReaderMatchModeBinaryHash, KOReaderMatchModeFilePath:
	default:
		issues = append(issues, ValidationIssue{Field: "koreader.match_mode", Message: "匹配模式必须是 binary_hash 或 file_path。", Severity: "error"})
	}

	return ValidationResult{
		Valid:  len(issues) == 0,
		Issues: issues,
	}
}

func isSupportedStorageProfile(profile string) bool {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "", StorageProfileAuto, StorageProfileSSD, StorageProfileHDDExternal, StorageProfileNetwork, StorageProfileCustom:
		return true
	default:
		return false
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
