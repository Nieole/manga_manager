// 业务说明：本文件是业务实现，属于后端 HTTP API 层，负责把前端请求转换为数据库、扫描器、图片处理和元数据服务调用。
// 它承载资料库浏览、阅读器取页、系列维护、任务进度、系统设置和静态资源缓存等对外业务契约。
// 维护时应重点关注请求参数校验、错误语义、缓存头、并发任务状态和前后端字段兼容性。

package api

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"manga-manager/internal/database"
	"net/http"
)

// scheduleFranchiseRebuild 合并并发的 franchise 重建请求：已有重建在跑时只置 pending，
// 否则起一个后台任务循环重建直到无待处理请求。经 runBackground 登记到 backgroundWG，
// 关闭流程会等待其结束（此前是脱离生命周期、用 context.Background() 的 fire-and-forget goroutine）。
func (c *Controller) scheduleFranchiseRebuild() {
	c.franchiseRebuildMu.Lock()
	if c.franchiseRebuildRunning {
		c.franchiseRebuildPending = true
		c.franchiseRebuildMu.Unlock()
		return
	}
	c.franchiseRebuildRunning = true
	c.franchiseRebuildMu.Unlock()

	c.runBackground(func() {
		for {
			if err := c.RebuildFranchiseCollections(context.Background()); err != nil {
				slog.Error("Franchise rebuild failed", "error", err)
			}
			c.franchiseRebuildMu.Lock()
			if c.franchiseRebuildPending {
				c.franchiseRebuildPending = false
				c.franchiseRebuildMu.Unlock()
				continue
			}
			c.franchiseRebuildRunning = false
			c.franchiseRebuildMu.Unlock()
			return
		}
	})
}

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

	// 3. 一次批量取每个连通分量代表系列的名称，消除此前逐分量 GetSeries 的 N+1。
	firstIDs := make([]int64, 0, len(components))
	for _, comp := range components {
		firstIDs = append(firstIDs, comp[0])
	}
	nameByID := make(map[int64]string, len(firstIDs))
	if len(firstIDs) > 0 {
		rows, err := c.store.GetSeriesNamesByIDs(ctx, firstIDs)
		if err != nil {
			return fmt.Errorf("failed to batch-load series names: %w", err)
		}
		for _, row := range rows {
			nameByID[row.ID] = row.Name
		}
	}

	// 4. 删旧建新整体入事务：先删后建原子化，避免中途失败/并发交错留下“已删光/半重建”的
	//    不一致状态；错误直接返回以触发回滚（此前吞错并 continue）。
	err = c.store.ExecTx(ctx, func(q *database.Queries) error {
		if err := q.DeleteFranchiseCollections(ctx); err != nil {
			return err
		}
		for i, comp := range components {
			name := fmt.Sprintf("Franchise #%d", i+1)
			if n, ok := nameByID[comp[0]]; ok && n != "" {
				name = n + " Franchise"
			}
			created, err := q.CreateCollection(ctx, database.CreateCollectionParams{
				Name:        name,
				Description: sql.NullString{String: "Auto-generated franchise collection based on series relations.", Valid: true},
				SourceType:  "system_franchise",
			})
			if err != nil {
				return err
			}
			for _, seriesID := range comp {
				if err := q.AddSeriesToCollection(ctx, database.AddSeriesToCollectionParams{
					CollectionID: created.ID,
					SeriesID:     seriesID,
				}); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		slog.Error("Failed to rebuild franchise collections", "error", err)
		return err
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
