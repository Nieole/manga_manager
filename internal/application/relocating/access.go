package relocating

import (
	"context"
	"sort"
	"sync"

	"manga-manager/internal/logger"
	"manga-manager/internal/relocation"
	"manga-manager/internal/task"
)

type relocationTopology struct {
	mu    sync.Mutex
	roots map[int64][2]string
}

func (c *Service) checkConflicts(ctx context.Context, plan relocation.RelocationPlan) ([]task.LibraryConflict, error) {
	conflicts, err := c.libraryConflicts(ctx, plan.LibraryID)
	if err != nil {
		return nil, err
	}
	libs, err := c.store.ListLibraries(ctx)
	if err != nil {
		return nil, err
	}
	affected := map[int64]bool{}
	err = c.domain.VisitRecordPaths(ctx, plan.LibraryID, func(path string) error {
		mapped, mapErr := relocation.MapPath(plan.SourceRoot, plan.TargetRoot, path)
		if mapErr != nil {
			return nil
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
		return nil
	})
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
		other, loadErr := c.libraryConflicts(ctx, lib.ID)
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
func (c *Service) relocationRootConflicts(path string) bool {
	for _, roots := range c.topology.roots {
		for _, root := range roots {
			if relocationPathsIntersect(root, path) {
				return true
			}
		}
	}
	return false
}

// acquireOperation includes other library work whose directory coverage
// intersects mapped records. Disjoint nested libraries retain their access.
func (c *Service) acquireOperation(ctx context.Context, op relocation.RelocationOperation) (func(), error) {
	c.topology.mu.Lock()
	defer c.topology.mu.Unlock()
	release, err := c.acquireExclusive(ctx, op.LibraryID)
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
	affected := map[int64]bool{}
	err = c.domain.VisitMappingPaths(ctx, op.PlanID, func(source, target string) error {
		for _, lib := range libs {
			if lib.ID == op.LibraryID {
				continue
			}
			for _, candidate := range []string{source, target} {
				if relocationPathsIntersect(lib.Path, candidate) {
					affected[lib.ID] = true
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	ids := make([]int64, 0, len(affected))
	for id := range affected {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		release, err = c.acquireExclusive(ctx, id)
		if err != nil {
			return nil, err
		}
		releases = append(releases, release)
	}
	ok = true
	if c.topology.roots == nil {
		c.topology.roots = map[int64][2]string{}
	}
	c.topology.roots[op.ID] = [2]string{op.SourceRoot, op.TargetRoot}
	return func() {
		c.topology.mu.Lock()
		defer c.topology.mu.Unlock()
		delete(c.topology.roots, op.ID)
		releaseAll()
	}, nil
}

func relocationPathsIntersect(a, b string) bool {
	_, inside := relocation.MapPath(a, a, b)
	_, contains := relocation.MapPath(b, b, a)
	return inside == nil || contains == nil
}

// GuardRoot holds the topology boundary while a library root is created or changed.
func (c *Service) GuardRoot(path string) (func(), error) {
	c.topology.mu.Lock()
	if c.relocationRootConflicts(path) {
		c.topology.mu.Unlock()
		return nil, task.ErrLibraryUnavailable
	}
	return c.topology.mu.Unlock, nil
}

func (c *Service) acquireExclusive(ctx context.Context, id int64) (func(), error) {
	return c.engine.AcquireLibraryExclusiveForRun(ctx, id, logger.RunIDFrom(ctx))
}
func (c *Service) libraryConflicts(ctx context.Context, id int64) ([]task.LibraryConflict, error) {
	return c.engine.LibraryConflicts(ctx, id, logger.RunIDFrom(ctx))
}
