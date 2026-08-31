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
	"ongoing":              "ongoing",
	"on going":             "ongoing",
	"publishing":           "ongoing",
	"currently publishing": "ongoing",
	"releasing":            "ongoing",
	"serializing":          "ongoing",
	"serialization":        "ongoing",
	"serialized":           "ongoing",
	"completed":            "completed",
	"complete":             "completed",
	"finished":             "completed",
	"ended":                "completed",
	"hiatus":               "hiatus",
	"on hiatus":            "hiatus",
	"on hold":              "hiatus",
	"paused":               "hiatus",
	"cancelled":            "cancelled",
	"canceled":             "cancelled",
	"dropped":              "cancelled",
	"discontinued":         "cancelled",
	"abandoned":            "cancelled",
	"unknown":              "unknown",
	"连载中":                  "ongoing",
	"連載中":                  "ongoing",
	"连载":                   "ongoing",
	"連載":                   "ongoing",
	"已完结":                  "completed",
	"已完結":                  "completed",
	"完结":                   "completed",
	"完結":                   "completed",
	"休刊中":                  "hiatus",
	"休載中":                  "hiatus",
	"休刊":                   "hiatus",
	"有生之年":                 "hiatus",
	"已放弃":                  "cancelled",
	"已放棄":                  "cancelled",
	"已取消":                  "cancelled",
	"未知":                   "unknown",
}

// statusAliasKey 把一种写法折成别名表的键：小写、去首尾空白，并把 -、_ 与连续空白都折成单个空格。
// 归一放在这里而不是往表里堆写法，是因为 on-hiatus / on_hiatus / On Hiatus 是同一个词。
func statusAliasKey(value string) string {
	lowered := strings.ToLower(strings.TrimSpace(value))
	spaced := strings.Map(func(r rune) rune {
		if r == '-' || r == '_' {
			return ' '
		}
		return r
	}, lowered)
	return strings.Join(strings.Fields(spaced), " ")
}

// NormalizeStatusCodeOptional 把一种状态写法折成内部编码，ok=false 表示「没给」或「不认识」。
//
// 这个区分是必要的：数据源交出结果时把它折成一个值，会让不提供状态的数据源
// 每次刮削都生成一条「status → unknown」的提案去覆盖系列上已有的正确状态。
// 数据源侧用 sourceStatusCode，写入端用 StatusCodeOrUnknown。
func NormalizeStatusCodeOptional(value string) (string, bool) {
	code, ok := statusAliases[statusAliasKey(value)]
	return code, ok
}

// StatusCodeOrUnknown 把一个**已经决定要写进系列**的状态折成合法编码，认不出就落 "unknown"。
// 只用在写入端：那里调用方已经判定这个字段要写，留空没有意义，落个野值更糟。
// 数据源交出结果时不要用它——那时「认不出」的正确表达是留空。
func StatusCodeOrUnknown(value string) string {
	if code, ok := NormalizeStatusCodeOptional(value); ok {
		return code
	}
	return "unknown"
}

// sourceStatusCode 是数据源侧的状态出口：只报数据源确实给了、且我们认得的状态。
// 没给、认不出、以及数据源自己说 unknown（提示词把 unknown 列为可选值，模型常照填）
// 三种都留空——它们都是「数据源不知道」，不是一个能覆盖系列现有状态的事实。
func sourceStatusCode(value string) string {
	code, ok := NormalizeStatusCodeOptional(value)
	if !ok || code == "unknown" {
		return ""
	}
	return code
}
