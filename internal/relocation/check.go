package relocation

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"manga-manager/internal/diskwork"
	"manga-manager/internal/runhandle"
	"manga-manager/internal/storageio"
	"manga-manager/internal/task"
)

// MapPath preserves the native relative path and rejects paths outside source.
// Neither source nor series directories need to exist on the host.
func MapPath(source, target, path string) (string, error) {
	if !filepath.IsAbs(source) || !filepath.IsAbs(target) || !filepath.IsAbs(path) {
		return "", errors.New("absolute_path_required")
	}
	rel, err := filepath.Rel(filepath.Clean(source), filepath.Clean(path))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", errors.New("outside_source_root")
	}
	return filepath.Join(target, rel), nil
}

func pathKey(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}

// Check persists the checked facts without changing library records or config.
// The adapter supplies conflicts observed with this check's own run excluded.
func (s *Service) Check(ctx context.Context, id int64, handle *runhandle.Handle, conflicts []task.LibraryConflict) (err error) {
	plan, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	plan.Status = "checking"
	if err = s.save(ctx, plan); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			plan.Status = "failed"
			if errors.Is(err, context.Canceled) {
				plan.Status = "cancelled"
			}
		}
		if saveErr := s.save(context.WithoutCancel(ctx), plan); saveErr != nil && err == nil {
			err = saveErr
		}
	}()
	for _, conflict := range conflicts {
		if err = s.add(ctx, &plan, RelocationDetail{Kind: "blocker", RecordType: "work", Reason: conflict.Reason,
			RunID: conflict.RunID, RunStatus: string(conflict.Status), CanCancel: conflict.CanCancel}); err != nil {
			return err
		}
	}
	handle.Phase("roots", "task.msg.relocation.checking", nil)
	validTarget := true
	if !filepath.IsAbs(plan.SourceRoot) || !filepath.IsAbs(plan.TargetRoot) {
		validTarget = false
		err = s.add(ctx, &plan, RelocationDetail{Kind: "blocker", RecordType: "library", Reason: "absolute_path_required", SourcePath: plan.SourceRoot, TargetPath: plan.TargetRoot})
		if err != nil {
			return err
		}
	} else {
		var rootErr error
		err = handle.Disk(ctx, diskwork.Work{Kind: storageio.WorkKindScanFast, Path: plan.TargetRoot}, func() error {
			info, statErr := s.Files.Stat(plan.TargetRoot)
			if statErr != nil {
				rootErr = statErr
			} else if !info.IsDir() {
				rootErr = errors.New("not a directory")
			} else {
				f, openErr := s.Files.Open(plan.TargetRoot)
				rootErr = openErr
				if openErr == nil {
					rootErr = f.Close()
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		if rootErr != nil {
			validTarget = false
			if err = s.add(ctx, &plan, RelocationDetail{Kind: "blocker", RecordType: "library", TargetPath: plan.TargetRoot, Reason: "invalid_target_root"}); err != nil {
				return err
			}
		}
	}
	libs, err := s.Store.ListLibraries(ctx)
	if err != nil {
		return err
	}
	for _, lib := range libs {
		if lib.ID != plan.LibraryID && pathKey(lib.Path) == pathKey(plan.TargetRoot) {
			if err = s.add(ctx, &plan, RelocationDetail{Kind: "blocker", RecordType: "library", RecordID: lib.ID, TargetPath: lib.Path, Reason: "library_occupied"}); err != nil {
				return err
			}
		}
	}
	series, err := s.Store.ListSeriesByLibraryLite(ctx, plan.LibraryID)
	if err != nil {
		return err
	}
	books, err := s.Store.ListBooksByLibrary(ctx, plan.LibraryID)
	if err != nil {
		return err
	}
	plan.SeriesCount, plan.BookCount = len(series), len(books)
	seen := map[string]int64{}
	facts := sha256.New()
	_, _ = fmt.Fprintf(facts, "%d\n%s\n%s\n%s\n", plan.LibraryID, plan.LibraryRoot, plan.SourceRoot, plan.TargetRoot)
	for _, item := range series {
		if err = handle.Checkpoint(ctx); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(facts, "series:%d:%s:%s\n", item.ID, item.Path, item.UpdatedAt)
		if _, err = s.mapRecord(ctx, &plan, "series", item.ID, item.Path, 0, seen); err != nil {
			return err
		}
	}
	handle.Phase("files", "task.msg.relocation.checking", nil)
	for i, item := range books {
		if err = handle.Checkpoint(ctx); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(facts, "book:%d:%s:%d:%s\n", item.ID, item.Path, item.Size, item.FileModifiedAt)
		mapped, mapErr := s.mapRecord(ctx, &plan, "book", item.ID, item.Path, item.Size, seen)
		if mapErr != nil {
			return mapErr
		}
		if mapped != "" && validTarget {
			detail := RelocationDetail{Kind: "anomaly", RecordType: "book", RecordID: item.ID, SourcePath: item.Path, TargetPath: mapped, ExpectedSize: item.Size}
			err = handle.Disk(ctx, diskwork.Work{Kind: storageio.WorkKindScanFast, Path: mapped}, func() error {
				info, statErr := s.Files.Stat(mapped)
				switch {
				case errors.Is(statErr, os.ErrNotExist):
					detail.Reason = "missing"
				case statErr != nil:
					detail.Reason = "unreadable"
				case !info.Mode().IsRegular():
					detail.Reason = "not_regular"
				default:
					detail.ActualSize, detail.ModifiedNS = info.Size(), info.ModTime().UnixNano()
					f, openErr := s.Files.Open(mapped)
					if openErr != nil {
						detail.Reason = "unreadable"
					} else if closeErr := f.Close(); closeErr != nil {
						detail.Reason = "unreadable"
					} else if info.Size() != item.Size {
						detail.Reason = "size_mismatch"
					}
				}
				return nil
			})
			if err != nil {
				return err
			}
			encoded, _ := json.Marshal(detail)
			_, _ = facts.Write(encoded)
			if detail.Reason != "" {
				if err = s.add(ctx, &plan, detail); err != nil {
					return err
				}
			}
		}
		handle.Advance(i+1, len(books), "task.msg.relocation.checking", nil)
	}
	handle.Phase("policies", "task.msg.relocation.checking", nil)
	cfg := s.Config.Snapshot()
	if err = s.checkPolicies(ctx, &plan, cfg); err != nil {
		return err
	}
	policies, err := json.Marshal(cfg.Library.StoragePolicies)
	if err != nil {
		return err
	}
	plan.PoliciesJSON = string(policies)
	_, _ = facts.Write(policies)
	plan.FactsDigest = hex.EncodeToString(facts.Sum(nil))
	plan.Status = "checked"
	handle.Phase("checked", "task.msg.relocation.checked", nil)
	slog.InfoContext(ctx, "Library relocation preview checked", "plan_id", plan.ID, "books", plan.BookCount, "blockers", plan.BlockerCount, "anomalies", plan.AnomalyCount)
	return nil
}

func (s *Service) mapRecord(ctx context.Context, plan *RelocationPlan, kind string, id int64, path string, size int64, seen map[string]int64) (string, error) {
	mapped, err := MapPath(plan.SourceRoot, plan.TargetRoot, path)
	if err != nil {
		return "", s.add(ctx, plan, RelocationDetail{Kind: "blocker", RecordType: kind, RecordID: id, SourcePath: path, Reason: "outside_source_root"})
	}
	detail := RelocationDetail{Kind: "mapping", RecordType: kind, RecordID: id, SourcePath: path, TargetPath: mapped, ExpectedSize: size}
	if err = s.add(ctx, plan, detail); err != nil {
		return "", err
	}
	canonical := kind + ":" + pathKey(mapped)
	if previous, ok := seen[canonical]; ok && previous != id {
		detail.Kind, detail.Reason = "blocker", "ambiguous_mapping"
		if err = s.add(ctx, plan, detail); err != nil {
			return "", err
		}
	}
	seen[canonical] = id
	table := "series"
	if kind == "book" {
		table = "books"
	}
	expression, key := "rtrim(path,'/')", strings.TrimRight(filepath.Clean(mapped), "/")
	if runtime.GOOS == "windows" {
		expression = `lower(replace(rtrim(path,'/\'),'\','/'))`
		key = strings.ToLower(strings.TrimRight(filepath.ToSlash(filepath.Clean(mapped)), "/"))
	}
	var occupiedID int64
	query := fmt.Sprintf("SELECT id FROM %s WHERE %s=? AND id<>? LIMIT 1", table, expression)
	conflictErr := s.Store.DB().QueryRowContext(ctx, query, key, id).Scan(&occupiedID)
	if conflictErr == nil {
		detail.Kind, detail.Reason = "blocker", "record_occupied"
		if err = s.add(ctx, plan, detail); err != nil {
			return "", err
		}
	} else if !errors.Is(conflictErr, sql.ErrNoRows) {
		return "", conflictErr
	}
	return mapped, nil
}
