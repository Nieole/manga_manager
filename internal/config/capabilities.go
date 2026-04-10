package config

import "strings"

var SupportedScanFormats = []string{"zip", "cbz", "rar", "cbr"}

const DefaultScanInterval = 60

const DefaultScanFormatsCSV = "zip,cbz,rar,cbr"

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
