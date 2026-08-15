package config

import (
	"path/filepath"
	"strings"
)

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

// ScanFormatSet 是某个资料库生效的归档扩展名集合，用于发现阶段过滤。
//
// **零值刻意 fail-open**（等价于全部支持格式）：这是用户偏好过滤器，不是安全闸门。
// 任何忘记初始化的调用方应当退化成「多扫了几个文件」，而不是「一本都扫不到、整个库看起来空了」。
type ScanFormatSet struct {
	exts map[string]struct{} // key 形如 ".cbz"
}

// NewScanFormatSet 由库的 scan_formats CSV 构造集合。
// 空值与全非法值经 ParseScanFormats 回落到全部支持格式，与既有口径一致。
func NewScanFormatSet(raw string) ScanFormatSet {
	formats := ParseScanFormats(raw)
	exts := make(map[string]struct{}, len(formats))
	for _, f := range formats {
		exts["."+strings.ToLower(strings.TrimPrefix(strings.TrimSpace(f), "."))] = struct{}{}
	}
	return ScanFormatSet{exts: exts}
}

// Matches 报告该路径的扩展名是否在本集合内。零值集合退化为全局支持格式（见类型注释）。
func (s ScanFormatSet) Matches(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if s.exts == nil {
		return IsSupportedArchiveExtension(ext)
	}
	_, ok := s.exts[ext]
	return ok
}

// Equal 按**集合**比较两个格式集。
// 不能比较 CSV 字符串：NormalizeScanFormatsCSV 保留输入顺序，"cbz,zip" 与 "zip,cbz"
// 是同一集合却是两个不同的串，按串比较会把纯换序误判成配置变更。
func (s ScanFormatSet) Equal(other ScanFormatSet) bool {
	if len(s.exts) != len(other.exts) {
		return false
	}
	for ext := range s.exts {
		if _, ok := other.exts[ext]; !ok {
			return false
		}
	}
	return true
}
