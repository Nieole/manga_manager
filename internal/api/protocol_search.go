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
