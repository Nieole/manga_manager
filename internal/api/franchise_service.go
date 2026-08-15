package api

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"manga-manager/internal/database"
	"net/http"
	"sort"
	"strconv"
)

// scheduleFranchiseRebuild 委托给 franchiseRebuilder（合并式调度已抽入 franchise_rebuilder.go）。
func (c *Controller) scheduleFranchiseRebuild() {
	c.franchiseRebuilder.schedule()
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
	//
	// 遍历顺序必须确定：Go 的 map 迭代是随机的，而下面用 comp 的最小 id 做稳定键、
	// 用它对应的系列名做合集名。不排序的话，同一批关系每次重建都可能算出不同的
	// 起点、不同的名字——用户会看到合集名无缘无故变来变去。
	nodes := make([]int64, 0, len(adj))
	for node := range adj {
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i] < nodes[j] })

	visited := make(map[int64]bool)
	var components [][]int64

	for _, node := range nodes {
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
				// comp[0] 是 BFS 的起点，而外层是按 id 升序遍历并标记 visited 的，
				// 所以起点必然是这个连通分量里最小的 id——任何更小的成员都会更早被访问、
				// 自己去当起点。下面直接拿 comp[0] 当稳定键，不必再排一次。
				components = append(components, comp)
			}
		}
	}

	// 3. 一次批量取每个连通分量代表系列的名称，避免逐分量单独 GetSeries 造成 N+1。
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

	// 4. 按稳定自然键 upsert + 成员差集，整体入事务；不得改回「整体 DELETE 再全部重建」——
	// 合集 id 是对外暴露的：Mihon 的 /mihon/collections/{id}/series 与 OPDS 的
	// /opds/collections/{id} 都按它取数，重建会让所有 franchise 合集换一遍 id，
	// 客户端书库里记下的条目与用户站内收藏的合集链接会集体失效。
	//
	// 键取连通分量的最小系列 id。它在「有更大 id 的系列加入」「关系类型变化」这些常见改动下
	// 都不变；只有当一个 id 更小的系列并进来时才会换键——那种情况下这个合集的身份本身
	// 也确实变了（两个 franchise 合并），换 id 是诚实的。
	keys := make([]string, 0, len(components))
	err = c.store.ExecTx(ctx, func(q *database.Queries) error {
		for _, comp := range components {
			key := strconv.FormatInt(comp[0], 10)
			keys = append(keys, key)

			name := "Franchise #" + key
			if n, ok := nameByID[comp[0]]; ok && n != "" {
				name = n + " Franchise"
			}
			collection, err := q.UpsertFranchiseCollection(ctx, database.UpsertFranchiseCollectionParams{
				Name:        name,
				Description: sql.NullString{String: "Auto-generated franchise collection based on series relations.", Valid: true},
				SourceKey:   key,
			})
			if err != nil {
				return err
			}

			// 成员差集：只增删真正变化的那几行。整体重建会让 collection_series.added_at
			// 全部刷新，「加入时间」这个字段就永久失去意义了。
			existing, err := q.ListCollectionSeriesIDs(ctx, collection.ID)
			if err != nil {
				return err
			}
			want := make(map[int64]struct{}, len(comp))
			for _, seriesID := range comp {
				want[seriesID] = struct{}{}
			}
			have := make(map[int64]struct{}, len(existing))
			for _, seriesID := range existing {
				have[seriesID] = struct{}{}
			}
			membersChanged := false
			for _, seriesID := range comp {
				if _, ok := have[seriesID]; ok {
					continue
				}
				if _, err := q.AddSeriesToCollection(ctx, database.AddSeriesToCollectionParams{
					CollectionID: collection.ID,
					SeriesID:     seriesID,
				}); err != nil {
					return err
				}
				membersChanged = true
			}
			for _, seriesID := range existing {
				if _, ok := want[seriesID]; ok {
					continue
				}
				if _, err := q.RemoveSeriesFromCollection(ctx, database.RemoveSeriesFromCollectionParams{
					CollectionID: collection.ID,
					SeriesID:     seriesID,
				}); err != nil {
					return err
				}
				membersChanged = true
			}
			// 成员真的变了才顶 updated_at，与站内其他三处成员变动的写法一致
			// （合集详情、合集视图、AI 分组落地都在写完成员后 TouchCollection）。
			// 旧实现整体删行重建，行是新建的、updated_at 天然是新的；换成差集之后
			// 若不补这一下，franchise 合集就能增删成员而 updated_at 原地不动
			// ——而它是对外可见的（OPDS 的 <updated> 与 Mihon 的 updated_at 都读它）。
			if membersChanged {
				if err := q.TouchCollection(ctx, collection.ID); err != nil {
					return err
				}
			}
		}

		// 清掉不再对应任何连通分量的 franchise 合集。source_key 为空的旧行（升级前建的）
		// 也算过期——它们会被上面的 upsert 用新键重建一遍。
		// keys 为空必须走「全删」而不是 DeleteStaleFranchiseCollections：
		// sqlc 对空 slice 生成的是 `NOT IN (NULL)`，那个条件对任何值都不成立，
		// 于是带键的行一条都删不掉——最后一条系列关系被删除后，界面上会永远挂着
		// 一堆没有任何依据的 franchise 合集。这个分支是承重的，不是防御性冗余。
		if len(keys) == 0 {
			_, err := q.DeleteAllFranchiseCollections(ctx)
			return err
		}
		_, err := q.DeleteStaleFranchiseCollections(ctx, keys)
		return err
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
