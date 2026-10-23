package proposal

import (
	"context"
	"manga-manager/internal/database"
	"testing"
)

func provenanceByField(t *testing.T, store database.Store, seriesID int64) map[string]database.SeriesMetadataProvenance {
	t.Helper()
	rows, err := store.GetSeriesMetadataProvenance(context.Background(), seriesID)
	if err != nil {
		t.Fatalf("GetSeriesMetadataProvenance: %v", err)
	}
	byField := make(map[string]database.SeriesMetadataProvenance, len(rows))
	for _, row := range rows {
		byField[row.FieldName] = row
	}
	return byField
}
