package metadata

import (
	"context"
	"strings"
)

type localeContextKey struct{}

func normalizeLocale(locale string) string {
	locale = strings.TrimSpace(strings.ToLower(locale))
	switch {
	case strings.HasPrefix(locale, "en"):
		return "en-US"
	default:
		return "zh-CN"
	}
}

func WithLocale(ctx context.Context, locale string) context.Context {
	return context.WithValue(ctx, localeContextKey{}, normalizeLocale(locale))
}

func LocaleFromContext(ctx context.Context) string {
	if ctx == nil {
		return "zh-CN"
	}
	if value, ok := ctx.Value(localeContextKey{}).(string); ok && value != "" {
		return normalizeLocale(value)
	}
	return "zh-CN"
}

var statusAliases = map[string]string{
	"ongoing":     "ongoing",
	"publishing":  "ongoing",
	"serializing": "ongoing",
	"completed":   "completed",
	"complete":    "completed",
	"finished":    "completed",
	"hiatus":      "hiatus",
	"paused":      "hiatus",
	"cancelled":   "cancelled",
	"canceled":    "cancelled",
	"dropped":     "cancelled",
	"unknown":     "unknown",
	"连载中":         "ongoing",
	"已完结":         "completed",
	"休刊中":         "hiatus",
	"已放弃":         "cancelled",
	"已取消":         "cancelled",
	"有生之年":        "hiatus",
	"未知":          "unknown",
	"":            "unknown",
}

func NormalizeStatusCode(value string) string {
	code, _ := NormalizeStatusCodeOptional(value)
	if code == "" {
		return "unknown"
	}
	return code
}

// NormalizeStatusCodeOptional 与 NormalizeStatusCode 同义，但把「数据源根本没给状态」
// 与「给了但我们不认识」都如实报成 ok=false，而不是一律折成 "unknown"。
//
// 这个区分是必要的：入队待审字段时，把空值折成 "unknown" 会让不提供状态的数据源
// （Comic Vine 恒不返回、AniList/MAL 部分条目、LLM 未识别）每次刮削都生成一条
// 「status → unknown」的提案，去覆盖系列上已有的正确状态；用户还得手动逐条驳回。
func NormalizeStatusCodeOptional(value string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", false
	}
	if normalized, ok := statusAliases[strings.ToLower(trimmed)]; ok {
		return normalized, true
	}
	if normalized, ok := statusAliases[trimmed]; ok {
		return normalized, true
	}
	return "", false
}
