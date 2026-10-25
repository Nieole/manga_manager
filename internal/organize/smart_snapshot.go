package organize

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"manga-manager/internal/database"
)

var ErrSmartSnapshotEmpty = errors.New("Smart collection has no members to snapshot")

type SmartSnapshotRequest struct {
	Name, Description string
	Limit             int
}
type SmartSnapshotResult struct {
	ID                 int64
	Name               string
	SeriesCount, Total int
	Truncated          bool
}
type SmartSnapshotPreview struct {
	FilterID                                          int64
	Name, Description                                 string
	Items                                             []database.SearchSeriesPagedRow
	Total, PreviewLimit, SnapshotLimit, SnapshotCount int
	Truncated, NameConflict                           bool
	Filter                                            SmartFilter
}

func snapshotLimit(value int) int {
	if value <= 0 || value > MaxCollectionBatchSize {
		return MaxCollectionBatchSize
	}
	return value
}
func snapshotDetails(filter SmartFilter, req SmartSnapshotRequest) (string, string) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = strings.TrimSpace(filter.Name)
	}
	desc := strings.TrimSpace(req.Description)
	if desc == "" {
		desc = "Snapshot from smart collection: " + filter.Name
	}
	return name, desc
}

// PreviewSnapshot reports advisory safety signals; Snapshot rechecks against its transaction.
func (s *SmartCollections) PreviewSnapshot(ctx context.Context, id, userID int64, req SmartSnapshotRequest, previewLimit int) (SmartSnapshotPreview, error) {
	filter, err := s.Get(ctx, id)
	if err != nil {
		return SmartSnapshotPreview{}, err
	}
	if previewLimit <= 0 {
		previewLimit = 12
	}
	if previewLimit > 50 {
		previewLimit = 50
	}
	limit := snapshotLimit(req.Limit)
	name, desc := snapshotDetails(filter, req)
	rows, total, err := s.store.SearchSmartCollectionSeries(ctx, smartCollectionFilterFrom(filter, userID), previewLimit, 0)
	if err != nil {
		return SmartSnapshotPreview{}, err
	}
	conflict, err := CollectionNameExists(ctx, s.store, name)
	if err != nil {
		return SmartSnapshotPreview{}, err
	}
	count := min(total, limit)
	return SmartSnapshotPreview{FilterID: id, Name: name, Description: desc, Items: rows, Total: total, PreviewLimit: previewLimit, SnapshotLimit: limit, SnapshotCount: count, Truncated: total > limit, NameConflict: conflict, Filter: filter}, nil
}

// Snapshot reads conditions and members under the same write transaction as creation.
// The resulting identity is owned by manual collection use cases and is never resynchronized.
func (s *SmartCollections) Snapshot(ctx context.Context, id, userID int64, req SmartSnapshotRequest) (SmartSnapshotResult, error) {
	var result SmartSnapshotResult
	err := s.store.ExecTx(ctx, func(q *database.Queries) error {
		raw, err := q.GetSmartFilterByID(ctx, id)
		if err != nil {
			return err
		}
		filter := smartFilterFromDB(raw)
		name, desc := snapshotDetails(filter, req)
		rows, total, err := q.SearchSmartCollectionSeries(ctx, smartCollectionFilterFrom(filter, userID), snapshotLimit(req.Limit), 0)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return ErrSmartSnapshotEmpty
		}
		if err := CheckCollectionName(ctx, q, name); err != nil {
			return err
		}
		created, err := q.CreateCollection(ctx, database.CreateCollectionParams{Name: name, Description: description(desc), SourceType: "smart_snapshot", SourceReviewID: sql.NullInt64{}})
		if err != nil {
			return err
		}
		for _, row := range rows {
			if _, err := q.AddSeriesToCollection(ctx, database.AddSeriesToCollectionParams{CollectionID: created.ID, SeriesID: row.ID}); err != nil {
				return err
			}
		}
		if err := q.TouchCollection(ctx, created.ID); err != nil {
			return err
		}
		result = SmartSnapshotResult{ID: created.ID, Name: name, SeriesCount: len(rows), Total: total, Truncated: total > len(rows)}
		return nil
	})
	if err != nil {
		return SmartSnapshotResult{}, err
	}
	return result, nil
}
