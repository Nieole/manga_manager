// 业务说明：本文件是业务实现，属于元数据聚合链路，负责从本地规则、外部站点和 AI Provider 获取漫画标题、简介、人物、标签与关系信息。
// 它支撑系列详情、智能补全、关系图谱和搜索索引的内容质量。
// 维护时应关注 Provider 契约、失败回退、限流、提示词稳定性和人工审核数据不被覆盖。

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
	trimmed := strings.TrimSpace(value)
	if normalized, ok := statusAliases[strings.ToLower(trimmed)]; ok {
		return normalized
	}
	if normalized, ok := statusAliases[trimmed]; ok {
		return normalized
	}
	return "unknown"
}
