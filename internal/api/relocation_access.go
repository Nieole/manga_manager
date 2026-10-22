package api

import (
	"context"
	"encoding/json"
	"sort"
	"sync"

	"manga-manager/internal/relocation"
	"manga-manager/internal/task"
)

type relocationTopology struct {
	mu    sync.Mutex
	roots map[int64][2]string
}

func (c *Controller) relocationCheckConflicts(ctx context.Context, plan RelocationPlan) ([]task.LibraryConflict, error) {
	conflicts, err := c.libraryAccessConflicts(ctx, plan.LibraryID)
	if err != nil {
		return nil, err
	}
	libs, err := c.store.ListLibraries(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := c.store.DB().QueryContext(ctx, `SELECT path FROM series WHERE library_id=? UNION ALL SELECT path FROM books WHERE library_id=?`, plan.LibraryID, plan.LibraryID)
	if err != nil {
		return nil, err
	}
	affected := map[int64]bool{}
	for rows.Next() {
		var path string
		if err = rows.Scan(&path); err != nil {
			rows.Close()
			return nil, err
		}
		mapped, mapErr := relocation.MapPath(plan.SourceRoot, plan.TargetRoot, path)
		if mapErr != nil {
			continue
		}
		for _, lib := range libs {
			if lib.ID == plan.LibraryID {
				continue
			}
			for _, candidate := range []string{path, mapped} {
				if relocationPathsIntersect(lib.Path, candidate) {
					affected[lib.ID] = true
				}
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	seen := map[int64]bool{}
	for _, conflict := range conflicts {
		if conflict.RunID != 0 {
			seen[conflict.RunID] = true
		}
	}
	for _, lib := range libs {
		if !affected[lib.ID] {
			continue
		}
		other, loadErr := c.libraryAccessConflicts(ctx, lib.ID)
		if loadErr != nil {
			return nil, loadErr
		}
		for _, conflict := range other {
			if conflict.RunID == 0 || !seen[conflict.RunID] {
				conflicts = append(conflicts, conflict)
				seen[conflict.RunID] = true
			}
		}
	}
	return conflicts, nil
}

// relocationRootConflicts is called while topology.mu protects a root change.
func (c *Controller) relocationRootConflicts(path string) bool {
	for _, roots := range c.relocationTopology.roots {
		for _, root := range roots {
			if relocationPathsIntersect(root, path) {
				return true
			}
		}
	}
	return false
}

// acquireRelocationAccess includes other library work whose directory coverage
// intersects mapped records. Disjoint nested libraries retain their access.
func (c *Controller) acquireRelocationAccess(ctx context.Context, op RelocationOperation) (func(), error) {
	c.relocationTopology.mu.Lock()
	defer c.relocationTopology.mu.Unlock()
	release, err := c.acquireLibraryExclusive(ctx, op.LibraryID)
	if err != nil {
		return nil, err
	}
	releases := []func(){release}
	var once sync.Once
	releaseAll := func() {
		once.Do(func() {
			for i := len(releases) - 1; i >= 0; i-- {
				releases[i]()
			}
		})
	}
	ok := false
	defer func() {
		if !ok {
			releaseAll()
		}
	}()
	libs, err := c.store.ListLibraries(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := c.store.DB().QueryContext(ctx, `SELECT detail FROM relocation_details WHERE plan_id=? AND kind='mapping'`, op.PlanID)
	if err != nil {
		return nil, err
	}
	affected := map[int64]bool{}
	for rows.Next() {
		var body string
		if err = rows.Scan(&body); err != nil {
			rows.Close()
			return nil, err
		}
		var detail relocation.RelocationDetail
		if err = json.Unmarshal([]byte(body), &detail); err != nil {
			rows.Close()
			return nil, err
		}
		for _, lib := range libs {
			if lib.ID == op.LibraryID {
				continue
			}
			for _, candidate := range []string{detail.SourcePath, detail.TargetPath} {
				if relocationPathsIntersect(lib.Path, candidate) {
					affected[lib.ID] = true
				}
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(affected))
	for id := range affected {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		release, err = c.acquireLibraryExclusive(ctx, id)
		if err != nil {
			return nil, err
		}
		releases = append(releases, release)
	}
	ok = true
	if c.relocationTopology.roots == nil {
		c.relocationTopology.roots = map[int64][2]string{}
	}
	c.relocationTopology.roots[op.ID] = [2]string{op.SourceRoot, op.TargetRoot}
	return func() {
		c.relocationTopology.mu.Lock()
		defer c.relocationTopology.mu.Unlock()
		delete(c.relocationTopology.roots, op.ID)
		releaseAll()
	}, nil
}

func relocationPathsIntersect(a, b string) bool {
	_, inside := relocation.MapPath(a, a, b)
	_, contains := relocation.MapPath(b, b, a)
	return inside == nil || contains == nil
}
