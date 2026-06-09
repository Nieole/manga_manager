// 业务说明：本文件是业务实现，属于运行时配置管理层，负责读取、归一化和持久化漫画库、扫描、元数据、AI 和服务端选项。
// 它是后端各服务共享配置的来源，影响扫描路径、外部库、图片缓存和前端设置页展示。
// 维护时应避免直接修改配置副本，新增字段需要兼顾默认值、兼容迁移和前端表单含义。

package config

import "strings"

var SupportedScanFormats = []string{"zip", "cbz", "rar", "cbr"}

var SupportedScanProfiles = []string{ScanProfileFast, ScanProfileMetadata, ScanProfileIdentity, ScanProfileRepair}

const DefaultScanInterval = 60

const DefaultScanFormatsCSV = "zip,cbz,rar,cbr"

const (
	ScanProfileFast     = "fast_scan"
	ScanProfileMetadata = "metadata_scan"
	ScanProfileIdentity = "identity_scan"
	ScanProfileRepair   = "repair_scan"
)

var SupportedLogLevels = []string{LogLevelDebug, LogLevelInfo, LogLevelWarn, LogLevelError}

func NormalizeScanFormatsCSV(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return DefaultScanFormatsCSV
	}

	seen := make(map[string]struct{}, len(SupportedScanFormats))
	result := make([]string, 0, len(SupportedScanFormats))
	for _, item := range strings.Split(raw, ",") {
		format := strings.ToLower(strings.TrimSpace(item))
		if format == "" {
			continue
		}
		if !IsSupportedScanFormat(format) {
			continue
		}
		if _, ok := seen[format]; ok {
			continue
		}
		seen[format] = struct{}{}
		result = append(result, format)
	}

	if len(result) == 0 {
		return DefaultScanFormatsCSV
	}
	return strings.Join(result, ",")
}

func ParseScanFormats(raw string) []string {
	return strings.Split(NormalizeScanFormatsCSV(raw), ",")
}

func IsSupportedScanFormat(format string) bool {
	format = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(format, ".")))
	for _, supported := range SupportedScanFormats {
		if format == supported {
			return true
		}
	}
	return false
}

func IsSupportedArchiveExtension(ext string) bool {
	return IsSupportedScanFormat(strings.TrimPrefix(ext, "."))
}

func NormalizeScanProfile(raw string) string {
	profile := strings.ToLower(strings.TrimSpace(raw))
	for _, supported := range SupportedScanProfiles {
		if profile == supported {
			return profile
		}
	}
	return ScanProfileMetadata
}

func IsSupportedScanProfile(profile string) bool {
	profile = strings.ToLower(strings.TrimSpace(profile))
	for _, supported := range SupportedScanProfiles {
		if profile == supported {
			return true
		}
	}
	return false
}
