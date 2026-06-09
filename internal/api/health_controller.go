// 业务说明：本文件是业务实现，属于后端 HTTP API 层，负责把前端请求转换为数据库、扫描器、图片处理和元数据服务调用。
// 它承载资料库浏览、阅读器取页、系列维护、任务进度、系统设置和静态资源缓存等对外业务契约。
// 维护时应重点关注请求参数校验、错误语义、缓存头、并发任务状态和前后端字段兼容性。

package api

import (
	"net/http"
	"strconv"
	"strings"

	"manga-manager/internal/database"
)

func (c *Controller) getHealthReport(w http.ResponseWriter, r *http.Request) {
	libraryID, err := parseOptionalInt64(r.URL.Query().Get("library_id"))
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid library ID")
		return
	}
	limit, err := parseOptionalInt(r.URL.Query().Get("limit"), 50)
	if err != nil || limit <= 0 {
		jsonError(w, http.StatusBadRequest, "Invalid limit")
		return
	}
	report, err := c.store.GetHealthReport(r.Context(), database.HealthIssueFilters{
		LibraryID:    libraryID,
		Type:         strings.TrimSpace(r.URL.Query().Get("type")),
		Limit:        limit,
		SkipKOReader: !c.currentConfig().KOReader.Enabled,
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to build health report")
		return
	}
	jsonResponse(w, http.StatusOK, report)
}

func parseOptionalInt64(value string) (int64, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	return strconv.ParseInt(value, 10, 64)
}

func parseOptionalInt(value string, fallback int) (int, error) {
	if strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	return strconv.Atoi(value)
}
