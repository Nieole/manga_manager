package api

import "manga-manager/internal/application/seriesediting"

type bulkEditSeriesResponse struct {
	Updated          int     `json:"updated"`
	Committed        bool    `json:"committed"`
	SeriesIDs        []int64 `json:"committed_series_ids"`
	RefreshFailedIDs []int64 `json:"refresh_failed_series_ids"`
	Error            string  `json:"error,omitempty"`
}

func selectionResponse(result seriesediting.SelectionResult) bulkEditSeriesResponse {
	return bulkEditSeriesResponse{Updated: result.Updated, Committed: result.Committed, SeriesIDs: result.SeriesIDs, RefreshFailedIDs: result.RefreshFailedIDs}
}
