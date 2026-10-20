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
	ID          int64     `json:"id"`
	PlanID      int64     `json:"plan_id"`
	LibraryID   int64     `json:"library_id"`
	RunID       int64     `json:"run_id"`
	Status      string    `json:"status"`
	SourceRoot  string    `json:"source_root"`
	TargetRoot  string    `json:"target_root"`
	SeriesCount int       `json:"series_count"`
	BookCount   int       `json:"book_count"`
	Code        string    `json:"code"`
	UpdatedAt   time.Time `json:"updated_at"`
}

var ErrPlanNotReady = errors.New("relocation_plan_not_ready")

type commitAccess struct {
	mu       sync.Mutex
	releases map[int64]func()
}

func (s *Service) Operation(ctx context.Context, id int64) (RelocationOperation, error) {
	var op RelocationOperation
	var data string
	err := s.Store.DB().QueryRowContext(ctx, `SELECT id,plan_id,library_id,run_id,status,result FROM relocation_operations WHERE id=?`, id).Scan(&op.ID, &op.PlanID, &op.LibraryID, &op.RunID, &op.Status, &data)
	if err != nil {
		return op, err
	}
	if err = json.Unmarshal([]byte(data), &op); err != nil {
		return op, err
	}
	err = s.Store.DB().QueryRowContext(ctx, `SELECT run_id,status FROM relocation_operations WHERE id=?`, id).Scan(&op.RunID, &op.Status)
	return op, err
}

// Prepare returns the same durable operation for every confirmation of a plan.
func (s *Service) Prepare(ctx context.Context, planID int64) (RelocationOperation, bool, error) {
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
	if plan.Status != "checked" || plan.BlockerCount != 0 || plan.AnomalyCount != 0 || plan.PolicyCount != 0 {
		return RelocationOperation{}, false, ErrPlanNotReady
	}
	op := RelocationOperation{PlanID: plan.ID, LibraryID: plan.LibraryID, Status: "pending", SourceRoot: plan.SourceRoot, TargetRoot: plan.TargetRoot, SeriesCount: plan.SeriesCount, BookCount: plan.BookCount, UpdatedAt: time.Now().UTC()}
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
	if err = s.saveOperation(ctx, op); err != nil {
		return op, false, err
	}
	return op, n == 1, nil
}

func (s *Service) BindOperationRun(ctx context.Context, id, runID int64) error {
	_, err := s.Store.DB().ExecContext(ctx, `UPDATE relocation_operations SET run_id=? WHERE id=? AND run_id=0`, runID, id)
	return err
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
	release, err := s.Acquire(ctx, op.LibraryID)
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
	err = s.Config.WithSnapshot(func(cfg config.Config) error {
		plan, e := s.Get(ctx, op.PlanID)
		if e != nil {
			return e
		}
		if plan.Status != "checked" {
			return ErrPlanNotReady
		}
		tx, e := s.Store.DB().BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		for _, kind := range []string{"series", "book"} {
			table := "series"
			if kind == "book" {
				table = "books"
			}
			e = s.walkRecords(ctx, op.LibraryID, kind, func(item storedRecord) error {
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
		return tx.Commit()
	})
	if err != nil {
		op.Status = "failed"
		op.Code = "relocation_commit_failed"
		_ = s.saveOperation(context.WithoutCancel(ctx), op)
		return err
	}
	keep = true
	s.access.mu.Lock()
	s.access.releases[op.ID] = release
	s.access.mu.Unlock()
	handle.Phase("refresh", "task.msg.relocation.committing", nil)
	return s.finish(context.WithoutCancel(ctx), op)
}

func (s *Service) finish(ctx context.Context, op RelocationOperation) error {
	if err := s.Effects(ctx, op); err != nil {
		op.Code = "relocation_recovering"
		_ = s.saveOperation(ctx, op)
		return err
	}
	op.Status = "completed"
	op.Code = "relocation_completed"
	if err := s.saveOperation(ctx, op); err != nil {
		return err
	}
	s.access.mu.Lock()
	release := s.access.releases[op.ID]
	delete(s.access.releases, op.ID)
	s.access.mu.Unlock()
	if release != nil {
		release()
	}
	return nil
}
