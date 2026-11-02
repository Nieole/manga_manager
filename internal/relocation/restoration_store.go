package relocation

import (
	"context"
	"encoding/json"
	"manga-manager/internal/config"
	"manga-manager/internal/runhandle"
)

// persistRestorationCheck commits the checked protection generation before effects run.
func (s *Service) persistRestorationCheck(ctx context.Context, op RelocationOperation, cfg config.Config, handle *runhandle.Handle, verify func() error) error {
	tx, e := s.persistence.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	// Take SQLite's write reservation before reading facts. The protection stays
	// present until watcher registration and the durable completion agree.
	if _, e = tx.ExecContext(ctx, `UPDATE libraries SET id=id WHERE id=?`, op.LibraryID); e != nil {
		return e
	}
	inspection := &Service{Store: s.Store, persistence: s.persistence, Config: config.NewManager(&cfg), Files: s.Files, transient: true, db: tx}
	plan, e := inspection.Get(ctx, op.PlanID)
	if e != nil {
		return e
	}
	if plan.Purpose != "restoration" || plan.Status != "checked" || plan.ProtectionID != op.ProtectionID {
		return ErrPlanNotReady
	}
	var generation int64
	if e = tx.QueryRowContext(ctx, `SELECT operation_id FROM library_relocation_protection WHERE library_id=?`, op.LibraryID).Scan(&generation); e != nil {
		return e
	}
	if generation != op.ProtectionID {
		return ErrStalePlan
	}
	if e = inspection.Check(ctx, plan.ID, handle, nil); e != nil {
		return e
	}
	checked := inspection.evaluated
	if checked.LibraryRoot != plan.LibraryRoot || checked.SourceRoot != checked.LibraryRoot || checked.TargetRoot != checked.LibraryRoot || checked.FactsDigest != plan.FactsDigest || checked.BlockerCount != 0 || (checked.AnomalyCount != 0 && !op.AcceptAnomalies) {
		return ErrStalePlan
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE relocation_plans SET status='consumed' WHERE id=? AND status='checked'`, plan.ID); e != nil {
		return e
	}
	op.Status, op.Code = "paths_committed", ""
	data, e := json.Marshal(op)
	if e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE relocation_operations SET status=?,result=? WHERE id=?`, op.Status, data, op.ID); e != nil {
		return e
	}
	if e = verify(); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}

	return nil
}

// releaseRestoration atomically restores settings, removes the exact protection and completes history.
func (s *Service) releaseRestoration(ctx context.Context, op RelocationOperation) error {
	tx, err := s.persistence.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	settings, err := tx.ExecContext(ctx, `UPDATE libraries SET scan_mode=?,scan_interval=?,scan_formats=? WHERE id=? AND path=? AND scan_mode='none' AND EXISTS (SELECT 1 FROM library_relocation_protection WHERE library_id=? AND operation_id=?)`, op.ScanMode, op.ScanInterval, op.ScanFormats, op.LibraryID, op.TargetRoot, op.LibraryID, op.ProtectionID)
	if err != nil {
		return err
	}
	changed, err := settings.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrStalePlan
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM library_relocation_protection WHERE library_id=? AND operation_id=? AND EXISTS (SELECT 1 FROM libraries WHERE id=? AND path=? AND scan_mode=? AND scan_interval=? AND scan_formats=?)`, op.LibraryID, op.ProtectionID, op.LibraryID, op.TargetRoot, op.ScanMode, op.ScanInterval, op.ScanFormats)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrStalePlan
	}
	op.Status, op.Code, op.Protected = "completed", "restoration_completed", false
	data, err := json.Marshal(op)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE relocation_operations SET status=?,result=? WHERE id=?`, op.Status, data, op.ID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}

	return nil
}
