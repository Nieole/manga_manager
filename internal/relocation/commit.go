package relocation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/runhandle"
)

// RelocationOperation survives run history pruning and records the committed boundary.
type RelocationOperation struct {
	ID              int64     `json:"id"`
	PlanID          int64     `json:"plan_id"`
	LibraryID       int64     `json:"library_id"`
	RunID           int64     `json:"run_id"`
	Status          string    `json:"status"`
	SourceRoot      string    `json:"source_root"`
	TargetRoot      string    `json:"target_root"`
	SeriesCount     int       `json:"series_count"`
	BookCount       int       `json:"book_count"`
	Code            string    `json:"code"`
	UpdatedAt       time.Time `json:"updated_at"`
	AcceptAnomalies bool      `json:"accept_anomalies"`
	Protected       bool      `json:"protected"`
}

var ErrPlanNotReady = errors.New("relocation_plan_not_ready")
var ErrStalePlan = errors.New("relocation_stale_plan")

type commitAccess struct {
	mu       sync.Mutex
	releases map[int64]func()
}

func (s *Service) Operation(ctx context.Context, id int64) (RelocationOperation, error) {
	var op RelocationOperation
	var data string
	var runID int64
	var status string
	err := s.Store.DB().QueryRowContext(ctx, `SELECT run_id,status,result FROM relocation_operations WHERE id=?`, id).Scan(&runID, &status, &data)
	if err != nil {
		return op, err
	}
	if err = json.Unmarshal([]byte(data), &op); err != nil {
		return op, err
	}
	op.ID, op.RunID, op.Status = id, runID, status
	return op, err
}

// Prepare returns the same durable operation for every confirmation of a plan.
func (s *Service) Prepare(ctx context.Context, planID int64, accept ...bool) (RelocationOperation, bool, error) {
	plan, err := s.Get(ctx, planID)
	if err != nil {
		return RelocationOperation{}, false, err
	}
	var existing int64
	err = s.Store.DB().QueryRowContext(ctx, `SELECT id FROM relocation_operations WHERE plan_id=?`, planID).Scan(&existing)
	if err == nil {
		op, e := s.Operation(ctx, existing)
		return op, false, e
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return RelocationOperation{}, false, err
	}
	accepted := len(accept) > 0 && accept[0]
	if plan.AnomalyCount != 0 && !accepted {
		return RelocationOperation{}, false, ErrAnomaliesRequireConfirmation
	}
	if plan.Status != "checked" || plan.BlockerCount != 0 {
		return RelocationOperation{}, false, ErrPlanNotReady
	}
	op := RelocationOperation{PlanID: plan.ID, LibraryID: plan.LibraryID, Status: "pending", SourceRoot: plan.SourceRoot, TargetRoot: plan.TargetRoot, SeriesCount: plan.SeriesCount, BookCount: plan.BookCount, UpdatedAt: time.Now().UTC()}
	op.AcceptAnomalies = accepted
	op.Protected = plan.AnomalyCount != 0
	protection, err := s.Store.LibraryRelocationProtection(ctx, plan.LibraryID)
	if err != nil {
		return op, false, err
	}
	op.Protected = op.Protected || protection != nil
	data, err := json.Marshal(op)
	if err != nil {
		return op, false, err
	}
	result, err := s.Store.DB().ExecContext(ctx, `INSERT INTO relocation_operations(plan_id,library_id,status,result) VALUES(?,?,?,?) ON CONFLICT(plan_id) DO NOTHING`, planID, plan.LibraryID, op.Status, data)
	if err != nil {
		return op, false, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return op, false, err
	}
	if err = s.Store.DB().QueryRowContext(ctx, `SELECT id FROM relocation_operations WHERE plan_id=?`, planID).Scan(&existing); err != nil {
		return op, false, err
	}
	op.ID = existing
	if n == 0 {
		existingOp, e := s.Operation(ctx, existing)
		return existingOp, false, e
	}
	if err = s.saveOperation(ctx, op); err != nil {
		return op, false, err
	}
	return op, n == 1, nil
}

func (s *Service) BindOperationRun(ctx context.Context, id, runID int64) error {
	_, err := s.Store.DB().ExecContext(ctx, `UPDATE relocation_operations SET run_id=? WHERE id=? AND run_id=0`, runID, id)
	return err
}

// FailUnstarted records admission failure without changing a committed result.
func (s *Service) FailUnstarted(ctx context.Context, id int64, code string) error {
	op, err := s.Operation(ctx, id)
	if err != nil {
		return err
	}
	if op.Status != "pending" {
		return nil
	}
	op.Status = "failed"
	op.Code = code
	return s.saveOperation(ctx, op)
}

func (s *Service) saveOperation(ctx context.Context, op RelocationOperation) error {
	op.UpdatedAt = time.Now().UTC()
	data, err := json.Marshal(op)
	if err != nil {
		return err
	}
	_, err = s.Store.DB().ExecContext(ctx, `UPDATE relocation_operations SET status=?,result=? WHERE id=?`, op.Status, data, op.ID)
	return err
}

// Commit keeps exclusive access through database commit and derived effects.
// Failed effects retain the reservation until recovery finishes.
func (s *Service) Commit(ctx context.Context, id int64, handle *runhandle.Handle) error {
	op, err := s.Operation(ctx, id)
	if err != nil {
		return err
	}
	if op.Status != "pending" {
		return nil
	}
	release, err := s.acquireOperation(ctx, op)
	if err != nil {
		op.Status = "failed"
		op.Code = "relocation_conflict"
		_ = s.saveOperation(context.WithoutCancel(ctx), op)
		return err
	}
	keep := false
	defer func() {
		if !keep {
			release()
		}
	}()
	handle.Phase("revalidate", "task.msg.relocation.committing", nil)
	err = s.withCommitConfig(func(cfg config.Config, session *config.UpdateSession) error {
		plan, e := s.Get(ctx, op.PlanID)
		if e != nil {
			return e
		}
		if plan.Status != "checked" {
			return ErrPlanNotReady
		}
		inspection := &Service{Store: s.Store, Config: config.NewManager(&cfg), Files: s.Files, transient: true}
		if e = inspection.Check(ctx, plan.ID, handle, nil); e != nil {
			return e
		}
		if inspection.evaluated.FactsDigest != plan.FactsDigest || inspection.evaluated.BlockerCount != 0 {
			return ErrStalePlan
		}
		if e = handle.DisableCancellation(); e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		handle.Phase("commit", "task.msg.relocation.committing", nil)
		nextConfig, e := s.preparePolicies(ctx, &op, cfg, session)
		if e != nil {
			return e
		}
		tx, e := s.Store.DB().BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		if _, e = tx.ExecContext(ctx, `UPDATE relocation_plans SET status=status WHERE id=? AND status='checked'`, plan.ID); e != nil {
			return e
		}
		inspection.db = tx
		if e = inspection.Check(ctx, plan.ID, handle, nil); e != nil {
			return e
		}
		checked := inspection.evaluated
		if checked.FactsDigest != plan.FactsDigest || checked.BlockerCount != 0 || (checked.AnomalyCount != 0 && !op.AcceptAnomalies) {
			return ErrStalePlan
		}
		for _, kind := range []string{"series", "book"} {
			table := "series"
			if kind == "book" {
				table = "books"
			}
			e = inspection.walkRecords(ctx, op.LibraryID, kind, func(item storedRecord) error {
				mapped, mapErr := MapPath(op.SourceRoot, op.TargetRoot, item.Path)
				if mapErr != nil {
					return mapErr
				}
				_, mapErr = tx.ExecContext(ctx, fmt.Sprintf("UPDATE %s SET path=? WHERE id=?", table), mapped, item.ID)
				return mapErr
			})
			if e != nil {
				return e
			}
		}
		if op.Protected {
			if _, e = tx.ExecContext(ctx, `INSERT INTO library_relocation_protection(library_id,operation_id,plan_id,facts_digest,anomaly_count,scan_mode,scan_interval,scan_formats) SELECT id,?,?,?,?,scan_mode,scan_interval,scan_formats FROM libraries WHERE id=? ON CONFLICT(library_id) DO UPDATE SET operation_id=excluded.operation_id,plan_id=excluded.plan_id,facts_digest=excluded.facts_digest,anomaly_count=excluded.anomaly_count`, op.ID, plan.ID, plan.FactsDigest, plan.AnomalyCount, op.LibraryID); e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, `UPDATE libraries SET scan_mode='none' WHERE id=?`, op.LibraryID); e != nil {
				return e
			}
		}
		if _, e = tx.ExecContext(ctx, `UPDATE libraries SET path=? WHERE id=? AND path=?`, op.TargetRoot, op.LibraryID, plan.LibraryRoot); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE relocation_plans SET status='consumed' WHERE id=? AND status='checked'`, plan.ID); e != nil {
			return e
		}
		op.Status = "paths_committed"
		data, _ := json.Marshal(op)
		if _, e = tx.ExecContext(ctx, `UPDATE relocation_operations SET status=?,result=? WHERE id=?`, op.Status, data, op.ID); e != nil {
			return e
		}
		if session != nil {
			if e = session.Verify(); e != nil {
				return e
			}
		}
		if e = tx.Commit(); e != nil {
			return e
		}
		keep = true
		s.access.mu.Lock()
		s.access.releases[op.ID] = release
		s.access.mu.Unlock()
		if nextConfig != nil {
			if e = session.Publish(*nextConfig); e != nil {
				return e
			}
		}
		handle.Phase("refresh", "task.msg.relocation.committing", nil)
		if session != nil {
			return s.finish(context.WithoutCancel(ctx), op, session.Verify)
		}
		return s.finish(context.WithoutCancel(ctx), op)
	})
	if err != nil {
		persisted, loadErr := s.Operation(context.WithoutCancel(ctx), op.ID)
		if loadErr == nil {
			op = persisted
		}
		if op.Status == "paths_committed" || op.Status == "prepared" {
			keep = true
			s.access.mu.Lock()
			s.access.releases[op.ID] = release
			s.access.mu.Unlock()
			if op.Status == "prepared" && loadErr == nil {
				if recoveryErr := s.reconcilePolicies(context.WithoutCancel(ctx), op, nil); recoveryErr == nil {
					op.Status = "failed"
					op.Code = "relocation_not_committed"
					if saveErr := s.saveOperation(context.WithoutCancel(ctx), op); saveErr == nil {
						s.releaseOperation(op.ID)
						return err
					}
				}
			}
			op.Code = "relocation_recovering"
			_ = s.saveOperation(context.WithoutCancel(ctx), op)
			return err
		}
		op.Status = "failed"
		op.Code = "relocation_commit_failed"
		if errors.Is(err, context.Canceled) {
			op.Status = "cancelled"
			op.Code = "relocation_not_committed"
		}
		if errors.Is(err, ErrStalePlan) {
			op.Code = "relocation_stale_plan"
		}
		_ = s.saveOperation(context.WithoutCancel(ctx), op)
		return err
	}
	return nil
}

func (s *Service) acquireOperation(ctx context.Context, op RelocationOperation) (func(), error) {
	if s.AcquireOperation != nil {
		return s.AcquireOperation(ctx, op)
	}
	return s.Acquire(ctx, op.LibraryID)
}

func (s *Service) finish(ctx context.Context, op RelocationOperation, verify ...func() error) error {
	if err := s.Effects(ctx, op); err != nil {
		op.Code = "relocation_recovering"
		_ = s.saveOperation(ctx, op)
		return err
	}
	if len(verify) != 0 {
		if err := verify[0](); err != nil {
			return err
		}
	}
	op.Status = "completed"
	op.Code = "relocation_completed"
	if err := s.saveOperation(ctx, op); err != nil {
		return err
	}
	s.releaseOperation(op.ID)
	return nil
}

func (s *Service) releaseOperation(id int64) {
	s.access.mu.Lock()
	release := s.access.releases[id]
	delete(s.access.releases, id)
	s.access.mu.Unlock()
	if release != nil {
		release()
	}
}

// Recover reserves every unfinished committed library before replaying effects.
// It never reapplies path mappings or restarts an uncommitted confirmation.
func (s *Service) Recover(ctx context.Context) error {
	if err := s.Initialize(ctx); err != nil {
		return err
	}
	rows, err := s.Store.DB().QueryContext(ctx, `SELECT id FROM relocation_operations WHERE status IN ('pending','prepared','paths_committed') ORDER BY id`)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var committed []RelocationOperation
	for _, id := range ids {
		op, e := s.Operation(ctx, id)
		if e != nil {
			return e
		}
		if op.Status == "pending" {
			op.Status = "interrupted"
			op.Code = "relocation_not_committed"
			if e = s.saveOperation(ctx, op); e != nil {
				return e
			}
			continue
		}
		s.access.mu.Lock()
		_, owned := s.access.releases[id]
		s.access.mu.Unlock()
		if !owned {
			release, e := s.acquireOperation(ctx, op)
			if e != nil {
				return e
			}
			s.access.mu.Lock()
			s.access.releases[id] = release
			s.access.mu.Unlock()
		}
		committed = append(committed, op)
	}
	for _, op := range committed {
		if e := s.reconcilePolicies(ctx, op, func(verify func() error) error {
			if op.Status == "prepared" {
				return nil
			}
			return s.finish(ctx, op, verify)
		}); e != nil {
			op.Code = "relocation_recovering"
			if errors.Is(e, errPolicyRecoveryConflict) {
				op.Code = "relocation_policy_recovery_conflict"
			}
			_ = s.saveOperation(ctx, op)
			continue
		}
		if op.Status == "prepared" {
			op.Status, op.Code = "interrupted", "relocation_not_committed"
			if e := s.saveOperation(ctx, op); e == nil {
				s.releaseOperation(op.ID)
			}
			continue
		}
	}
	return nil
}
