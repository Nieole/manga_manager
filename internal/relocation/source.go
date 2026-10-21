package relocation

import (
	"context"
	"path/filepath"
)

// RelocationSourceRoots offers bounded path hints, never historical provenance.
// Candidates require explicit selection and do not inspect the source disk.
type RelocationSourceRoots struct {
	LibraryRoot               string   `json:"library_root"`
	RecordsOutsideCurrentRoot bool     `json:"records_outside_current_root"`
	Candidates                []string `json:"candidates"`
}

func (s *Service) SourceRoots(ctx context.Context, libraryID int64) (RelocationSourceRoots, error) {
	lib, err := s.Store.GetLibrary(ctx, libraryID)
	result := RelocationSourceRoots{LibraryRoot: lib.Path, Candidates: []string{}}
	if err != nil {
		return result, err
	}
	common := ""
	incompatible := false
	for _, kind := range []string{"series", "book"} {
		err = s.walkRecords(ctx, libraryID, kind, func(item storedRecord) error {
			if _, e := MapPath(lib.Path, lib.Path, item.Path); e != nil {
				result.RecordsOutsideCurrentRoot = true
			}
			path := filepath.Clean(item.Path)
			if !filepath.IsAbs(path) {
				incompatible = true
				return nil
			}
			if kind == "book" {
				path = filepath.Dir(path)
			}
			if common == "" {
				common = path
				return nil
			}
			for {
				if _, e := MapPath(common, common, path); e == nil {
					break
				}
				parent := filepath.Dir(common)
				if parent == common {
					incompatible = true
					break
				}
				common = parent
			}
			return nil
		})
		if err != nil {
			return result, err
		}
	}
	if incompatible || common == "" || !result.RecordsOutsideCurrentRoot {
		return result, nil
	}
	for len(result.Candidates) < 6 {
		result.Candidates = append(result.Candidates, common)
		parent := filepath.Dir(common)
		if parent == common {
			break
		}
		common = parent
	}
	return result, nil
}
