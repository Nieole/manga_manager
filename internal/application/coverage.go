package application

import (
	"context"
	"manga-manager/internal/database"
	"manga-manager/internal/relocation"
	"manga-manager/internal/task"
	"path/filepath"
)

// LibraryRoots reads the same current library roots used by relocation coordination.
type LibraryRoots interface {
	ListLibraries(context.Context) ([]database.Library, error)
}

// PathsIntersect reports shared directory coverage, including resolvable aliases.
func PathsIntersect(a, b string) bool {
	if pathsIntersectLiteral(a, b) {
		return true
	}
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		ra = a
	}
	rb, err := filepath.EvalSymlinks(b)
	if err != nil {
		rb = b
	}
	return pathsIntersectLiteral(ra, rb)
}
func pathsIntersectLiteral(a, b string) bool {
	_, inside := relocation.MapPath(a, a, b)
	_, contains := relocation.MapPath(b, b, a)
	return inside == nil || contains == nil
}

// CoverPaths declares every current library touched by retained paths. Revalidation
// prevents admission after a root change invalidates the captured coverage.
func CoverPaths(ctx context.Context, store LibraryRoots, owner int64, paths []string) ([]int64, func() error, error) {
	libs, err := store.ListLibraries(ctx)
	if err != nil {
		return nil, nil, err
	}
	ids := []int64{owner}
	roots := make(map[int64]string, len(libs))
	for _, lib := range libs {
		roots[lib.ID] = lib.Path
		if lib.ID == owner {
			continue
		}
		for _, path := range paths {
			if PathsIntersect(lib.Path, path) {
				ids = append(ids, lib.ID)
				break
			}
		}
	}
	return ids, func() error {
		current, err := store.ListLibraries(context.WithoutCancel(ctx))
		if err != nil {
			return err
		}
		if len(current) != len(roots) {
			return task.ErrLibraryUnavailable
		}
		for _, lib := range current {
			if root, ok := roots[lib.ID]; !ok || root != lib.Path {
				return task.ErrLibraryUnavailable
			}
		}
		return nil
	}, nil
}
