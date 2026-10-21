package relocation

import (
	"context"
	"crypto/sha256"
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
	if err = s.dbtx().QueryRowContext(ctx, `SELECT path FROM libraries WHERE id=?`, plan.LibraryID).Scan(&plan.LibraryRoot); err != nil {
		return err
	}
	checkingCode, checkedCode := "task.msg.relocation.checking", "task.msg.relocation.checked"
	if plan.Purpose == "restoration" {
		checkingCode, checkedCode = "task.msg.restoration.checking", "task.msg.restoration.checked"
	}
	plan.SeriesCount, plan.BookCount, plan.BlockerCount, plan.AnomalyCount, plan.PolicyCount = 0, 0, 0, 0, 0
	plan.AnomalyCounts = map[string]int{}
	plan.Samples = []RelocationDetail{}
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
	if !s.transient {
		handle.Phase("roots", checkingCode, nil)
	}
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
	libs, err := s.libraryPaths(ctx)
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
	if err = s.dbtx().QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM series WHERE library_id=?),(SELECT COUNT(*) FROM books WHERE library_id=?)`, plan.LibraryID, plan.LibraryID).Scan(&plan.SeriesCount, &plan.BookCount); err != nil {
		return err
	}
	seen := map[string]recordRef{}
	occupied, err := s.occupancy(ctx)
	if err != nil {
		return err
	}
	facts := sha256.New()
	_, _ = fmt.Fprintf(facts, "%d\n%s\n%s\n%s\n", plan.LibraryID, plan.LibraryRoot, plan.SourceRoot, plan.TargetRoot)
	var scanMode, scanFormats string
	var scanInterval int64
	if err = s.dbtx().QueryRowContext(ctx, `SELECT scan_mode,scan_interval,scan_formats FROM libraries WHERE id=?`, plan.LibraryID).Scan(&scanMode, &scanInterval, &scanFormats); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(facts, "automatic:%s:%d:%s\n", scanMode, scanInterval, scanFormats)
	var protectionOperation int64
	if err = s.dbtx().QueryRowContext(ctx, `SELECT COALESCE((SELECT operation_id FROM library_relocation_protection WHERE library_id=?),0)`, plan.LibraryID).Scan(&protectionOperation); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(facts, "protection:%d\n", protectionOperation)
	err = s.walkRecords(ctx, plan.LibraryID, "series", func(item storedRecord) error {
		if err = handle.Checkpoint(ctx); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(facts, "series:%d:%s:%s\n", item.ID, item.Path, item.ChangedAt)
		if _, err = s.mapRecord(ctx, &plan, "series", item.ID, item.Path, 0, seen, occupied); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !s.transient {
		handle.Phase("files", checkingCode, nil)
	}
	processed := 0
	err = s.walkRecords(ctx, plan.LibraryID, "book", func(item storedRecord) error {
		if err = handle.Checkpoint(ctx); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(facts, "book:%d:%s:%d:%s\n", item.ID, item.Path, item.Size, item.ChangedAt)
		mapped, mapErr := s.mapRecord(ctx, &plan, "book", item.ID, item.Path, item.Size, seen, occupied)
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
		processed++
		if !s.transient {
			handle.Advance(processed, plan.BookCount, checkingCode, nil)
		}
		if processed%100 == 0 {
			return s.save(ctx, plan)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !s.transient {
		handle.Phase("policies", checkingCode, nil)
	}
	cfg := s.Config.Snapshot()
	if err = s.checkPolicies(ctx, &plan, cfg); err != nil {
		return err
	}
	policies, err := json.Marshal(policyFacts(cfg))
	if err != nil {
		return err
	}
	plan.PoliciesJSON = string(policies)
	_, _ = facts.Write(policies)
	plan.FactsDigest = hex.EncodeToString(facts.Sum(nil))
	plan.Status = "checked"
	if !s.transient {
		handle.Phase("checked", checkedCode, nil)
	}
	slog.InfoContext(ctx, "Library relocation preview checked", "plan_id", plan.ID, "books", plan.BookCount, "blockers", plan.BlockerCount, "anomalies", plan.AnomalyCount)
	return nil
}

type storedRecord struct {
	ID        int64
	Path      string
	Size      int64
	ChangedAt string
}

func (s *Service) walkRecords(ctx context.Context, libraryID int64, kind string, visit func(storedRecord) error) error {
	table, size := "series", "0"
	if kind == "book" {
		table, size = "books", "size"
	}
	var last int64
	for {
		query := fmt.Sprintf("SELECT id,path,%s,CAST(updated_at AS TEXT) FROM %s WHERE library_id=? AND id>? ORDER BY id LIMIT 100", size, table)
		rows, err := s.dbtx().QueryContext(ctx, query, libraryID, last)
		if err != nil {
			return err
		}
		batch := make([]storedRecord, 0, 100)
		for rows.Next() {
			var item storedRecord
			if err = rows.Scan(&item.ID, &item.Path, &item.Size, &item.ChangedAt); err != nil {
				rows.Close()
				return err
			}
			batch = append(batch, item)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(batch) == 0 {
			return nil
		}
		for _, item := range batch {
			if err = visit(item); err != nil {
				return err
			}
			last = item.ID
		}
	}
}

type recordRef struct {
	kind string
	id   int64
}

func (s *Service) mapRecord(ctx context.Context, plan *RelocationPlan, kind string, id int64, path string, size int64, seen map[string]recordRef, occupied map[string][]int64) (string, error) {
	mapped, err := MapPath(plan.SourceRoot, plan.TargetRoot, path)
	if err != nil {
		return "", s.add(ctx, plan, RelocationDetail{Kind: "blocker", RecordType: kind, RecordID: id, SourcePath: path, Reason: "outside_source_root"})
	}
	detail := RelocationDetail{Kind: "mapping", RecordType: kind, RecordID: id, SourcePath: path, TargetPath: mapped, ExpectedSize: size}
	if err = s.add(ctx, plan, detail); err != nil {
		return "", err
	}
	canonical := pathKey(mapped)
	if previous, ok := seen[canonical]; ok && (previous.id != id || previous.kind != kind) {
		detail.Kind, detail.Reason = "blocker", "ambiguous_mapping"
		detail.ConflictRecordID, detail.ConflictRecordType = previous.id, previous.kind
		if err = s.add(ctx, plan, detail); err != nil {
			return "", err
		}
	}
	seen[canonical] = recordRef{kind: kind, id: id}
	for _, occupiedKind := range []string{"series", "book"} {
		for _, occupiedID := range occupied[occupiedKind+":"+canonical] {
			if occupiedID != id || occupiedKind != kind {
				detail.Kind, detail.Reason = "blocker", "record_occupied"
				detail.ConflictRecordType, detail.ConflictRecordID = occupiedKind, occupiedID
				if err = s.add(ctx, plan, detail); err != nil {
					return "", err
				}
				break
			}
		}
	}
	for _, libraryID := range occupied["library:"+pathKey(mapped)] {
		if libraryID != plan.LibraryID {
			detail.Kind, detail.Reason = "blocker", "library_occupied"
			detail.ConflictRecordType, detail.ConflictRecordID = "library", libraryID
			if err = s.add(ctx, plan, detail); err != nil {
				return "", err
			}
			break
		}
	}
	return mapped, nil
}

func (s *Service) occupancy(ctx context.Context) (map[string][]int64, error) {
	rows, err := s.dbtx().QueryContext(ctx, `SELECT 'series',id,path FROM series UNION ALL SELECT 'book',id,path FROM books UNION ALL SELECT 'library',id,path FROM libraries`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string][]int64{}
	for rows.Next() {
		var kind, path string
		var id int64
		if err = rows.Scan(&kind, &id, &path); err != nil {
			return nil, err
		}
		key := kind + ":" + pathKey(path)
		result[key] = append(result[key], id)
	}
	return result, rows.Err()
}
