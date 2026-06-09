// 业务说明：本文件是业务实现，属于后端 HTTP API 层，负责把前端请求转换为数据库、扫描器、图片处理和元数据服务调用。
// 它承载资料库浏览、阅读器取页、系列维护、任务进度、系统设置和静态资源缓存等对外业务契约。
// 维护时应重点关注请求参数校验、错误语义、缓存头、并发任务状态和前后端字段兼容性。

package api

import (
	"context"
	"strings"

	"manga-manager/internal/database"
)

func (c *Controller) searchProtocolSeries(ctx context.Context, query string, page, limit int) ([]database.ProtocolSeriesRow, int, bool, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, 0, false, nil
	}
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 30
	}

	rows, total, err := c.store.SearchProtocolSeries(ctx, query, int32(limit), int32((page-1)*limit))
	return rows, total, true, err
}
