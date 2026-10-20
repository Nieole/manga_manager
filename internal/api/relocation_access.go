package api

import (
	"context"
	"encoding/json"
	"sort"
	"sync"

	"manga-manager/internal/relocation"
)

// acquireRelocationAccess includes other library work whose directory coverage
// intersects mapped records. Disjoint nested libraries retain their access.
func (c *Controller) acquireRelocationAccess(ctx context.Context, op RelocationOperation) (func(), error) {
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
				_, inside := relocation.MapPath(lib.Path, lib.Path, candidate)
				_, contains := relocation.MapPath(candidate, candidate, lib.Path)
				if inside == nil || contains == nil {
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
	return releaseAll, nil
}
