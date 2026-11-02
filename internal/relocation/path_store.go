package relocation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"manga-manager/internal/config"
	"manga-manager/internal/runhandle"
)

// commitPaths owns the SQLite write reservation, record revalidation and atomic path publication.
// Configuration coordination and postcommit effects stay with the durable process.
func (s *Service) commitPaths(ctx context.Context, plan RelocationPlan, op *RelocationOperation, cfg config.Config, handle *runhandle.Handle, verify func() error) (int, error) {
	total := plan.SeriesCount + plan.BookCount
	inspection := &Service{Store: s.Store, persistence: s.persistence, Config: config.NewManager(&cfg), transient: true, recordsOnly: true}
	tx, e := s.persistence.db.BeginTx(ctx, nil)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `UPDATE relocation_plans SET status=status WHERE id=? AND status='checked'`, plan.ID); e != nil {
		return 0, e
	}
	inspection.db = tx
	var expectedRecords string
	if e = tx.QueryRowContext(ctx, `SELECT digest FROM relocation_record_facts WHERE plan_id=?`, plan.ID).Scan(&expectedRecords); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return 0, ErrStalePlan
		}
		return 0, e
	}
	if e = inspection.Check(ctx, plan.ID, handle, nil); e != nil {
		return 0, e
	}
	checked := inspection.evaluated
	if inspection.recordsDigest != expectedRecords || checked.BlockerCount != 0 || (plan.AnomalyCount != 0 && !op.AcceptAnomalies) {
		return 0, ErrStalePlan
	}
	processed := 0
	for _, kind := range []string{"series", "book"} {
		table := "series"
		if kind == "book" {
			table = "books"
		}
		e = inspection.records().walk(ctx, op.LibraryID, kind, func(item storedRecord) error {
			mapped, mapErr := MapPath(op.SourceRoot, op.TargetRoot, item.Path)
			if mapErr != nil {
				return mapErr
			}
			_, mapErr = tx.ExecContext(ctx, fmt.Sprintf("UPDATE %s SET path=? WHERE id=?", table), mapped, item.ID)
			if mapErr == nil {
				processed++
				if processed%100 == 0 || processed == total {
					handle.AdvanceLive(processed, total)
				}
			}
			return mapErr
		})
		if e != nil {
			return 0, e
		}
	}
	if op.Protected {
		if _, e = tx.ExecContext(ctx, `INSERT INTO library_relocation_protection(library_id,operation_id,plan_id,facts_digest,anomaly_count,scan_mode,scan_interval,scan_formats) SELECT id,?,?,?,?,scan_mode,scan_interval,scan_formats FROM libraries WHERE id=? ON CONFLICT(library_id) DO UPDATE SET operation_id=excluded.operation_id,plan_id=excluded.plan_id,facts_digest=excluded.facts_digest,anomaly_count=excluded.anomaly_count`, op.ID, plan.ID, plan.FactsDigest, plan.AnomalyCount, op.LibraryID); e != nil {
			return 0, e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE libraries SET scan_mode='none' WHERE id=?`, op.LibraryID); e != nil {
			return 0, e
		}
	}
	if _, e = tx.ExecContext(ctx, `UPDATE libraries SET path=? WHERE id=? AND path=?`, op.TargetRoot, op.LibraryID, plan.LibraryRoot); e != nil {
		return 0, e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE relocation_plans SET status='consumed' WHERE id=? AND status='checked'`, plan.ID); e != nil {
		return 0, e
	}
	op.Status = "paths_committed"
	data, _ := json.Marshal(*op)
	if _, e = tx.ExecContext(ctx, `UPDATE relocation_operations SET status=?,result=? WHERE id=?`, op.Status, data, op.ID); e != nil {
		return 0, e
	}
	if e = verify(); e != nil {
		return 0, e
	}
	if e = tx.Commit(); e != nil {
		return 0, e
	}

	return processed, nil
}
