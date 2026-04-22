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
