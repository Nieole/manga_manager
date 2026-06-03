package api

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"manga-manager/internal/database"
)

// RebuildFranchiseCollections reads all series relations, computes connected components,
// and automatically creates or updates collections for each franchise.
func (c *Controller) RebuildFranchiseCollections(ctx context.Context) error {
	relations, err := c.store.GetAllSeriesRelations(ctx)
	if err != nil {
		return fmt.Errorf("failed to get all series relations: %w", err)
	}

	// 1. Build adjacency list for undirected graph (since franchise includes all connections)
	adj := make(map[int64][]int64)
	for _, r := range relations {
		adj[r.SourceSeriesID] = append(adj[r.SourceSeriesID], r.TargetSeriesID)
		adj[r.TargetSeriesID] = append(adj[r.TargetSeriesID], r.SourceSeriesID)
	}

	// 2. Find connected components
	visited := make(map[int64]bool)
	var components [][]int64

	for node := range adj {
		if !visited[node] {
			var comp []int64
			queue := []int64{node}
			visited[node] = true

			for len(queue) > 0 {
				curr := queue[0]
				queue = queue[1:]
				comp = append(comp, curr)

				for _, neighbor := range adj[curr] {
					if !visited[neighbor] {
						visited[neighbor] = true
						queue = append(queue, neighbor)
					}
				}
			}
			// Only care about franchises with at least 2 series
			if len(comp) > 1 {
				components = append(components, comp)
			}
		}
	}

	// 3. Delete existing franchise collections
	if err := c.store.DeleteFranchiseCollections(ctx); err != nil {
		slog.Error("Failed to delete existing franchise collections", "error", err)
		return err
	}

	// 4. Create new collections for each component
	for i, comp := range components {
		// Just a simple name for now, e.g. "Franchise #1".
		// Better approach: fetch series names and pick the oldest or most connected.
		// For simplicity, we just use the first node's name if available, or just generic.
		
		var name string
		if series, err := c.store.GetSeries(ctx, comp[0]); err == nil {
			name = series.Name + " Franchise"
		} else {
			name = fmt.Sprintf("Franchise #%d", i+1)
		}

		collID, err := c.store.CreateCollection(ctx, database.CreateCollectionParams{
			Name:        name,
			Description: sql.NullString{String: "Auto-generated franchise collection based on series relations.", Valid: true},
			SourceType:  "system_franchise",
		})
		if err != nil {
			slog.Error("Failed to create franchise collection", "name", name, "error", err)
			continue
		}

		for _, seriesID := range comp {
			_ = c.store.AddSeriesToCollection(ctx, database.AddSeriesToCollectionParams{
				CollectionID: collID.ID,
				SeriesID:     seriesID,
			})
		}
	}

	slog.Info("Successfully rebuilt franchise collections", "franchise_count", len(components))
	return nil
}

func (c *Controller) rebuildFranchiseCollectionsHandler(w http.ResponseWriter, r *http.Request) {
	err := c.RebuildFranchiseCollections(r.Context())
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "success"})
}
