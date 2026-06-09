// 业务说明：本文件是业务实现，属于后端 HTTP API 层，负责把前端请求转换为数据库、扫描器、图片处理和元数据服务调用。
// 它承载资料库浏览、阅读器取页、系列维护、任务进度、系统设置和静态资源缓存等对外业务契约。
// 维护时应重点关注请求参数校验、错误语义、缓存头、并发任务状态和前后端字段兼容性。

package api

import (
	"context"
	"net/http"
	"strings"

	"manga-manager/internal/metadata"
)

func requestLocale(r *http.Request) string {
	if r == nil {
		return "zh-CN"
	}
	if locale := strings.TrimSpace(r.Header.Get("X-App-Locale")); locale != "" {
		return locale
	}
	if locale := strings.TrimSpace(r.Header.Get("Accept-Language")); locale != "" {
		return locale
	}
	return "zh-CN"
}

func requestContextWithLocale(r *http.Request) context.Context {
	return metadata.WithLocale(r.Context(), requestLocale(r))
}
