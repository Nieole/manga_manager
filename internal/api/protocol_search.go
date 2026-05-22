package api

import (
	"context"
	"strconv"
	"strings"

	"manga-manager/internal/database"
)

func (c *Controller) searchProtocolSeries(ctx context.Context, query string, page, limit int) ([]database.ProtocolSeriesRow, int, bool, error) {
	query = strings.TrimSpace(query)
	if query == "" || c.engine == nil {
		return nil, 0, false, nil
	}
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 30
	}

	result, err := c.engine.SearchWithOffset(query, "series", limit, (page-1)*limit)
	if err != nil {
		return nil, 0, true, err
	}

	ids := make([]int64, 0, len(result.Hits))
	for _, hit := range result.Hits {
		id, ok := protocolSeriesID(hit.ID)
		if ok {
			ids = append(ids, id)
		}
	}

	rows, err := c.store.ListProtocolSeriesByIDs(ctx, ids)
	if err != nil {
		return nil, 0, true, err
	}
	return rows, int(result.Total), true, nil
}

func protocolSeriesID(raw string) (int64, bool) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "s_") {
		return 0, false
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(raw, "s_"), 10, 64)
	return id, err == nil && id > 0
}
