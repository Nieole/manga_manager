package relocation

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/runhandle"
)

// RelocationOperation survives run history pruning and records the committed boundary.
type RelocationOperation struct {
	Purpose         string    `json:"purpose,omitempty"`
	ProtectionID    int64     `json:"protection_id,omitempty"`
	ScanMode        string    `json:"scan_mode,omitempty"`
	ScanInterval    int64     `json:"scan_interval,omitempty"`
	ScanFormats     string    `json:"scan_formats,omitempty"`
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
	return s.operations().load(ctx, id)
}

// Prepare returns the same durable operation for every confirmation of a plan.
func (s *Service) Prepare(ctx context.Context, planID int64, accept ...bool) (RelocationOperation, bool, error) {
	return s.prepare(ctx, planID, nil, len(accept) > 0 && accept[0])
}

func (s *Service) prepare(ctx context.Context, planID int64, restore *RestorationRequest, accepted bool) (RelocationOperation, bool, error) {
	plan, err := s.Get(ctx, planID)
	if err != nil {
		return RelocationOperation{}, false, err
	}
	if (plan.Purpose == "restoration") != (restore != nil) {
		return RelocationOperation{}, false, ErrPlanNotReady
	}
	var existing int64
	err = s.operations().find(ctx, planID, &existing)
	if err == nil {
		op, e := s.Operation(ctx, existing)
		return op, false, e
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return RelocationOperation{}, false, err
	}
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
	if restore != nil {
		if protection == nil || protection.OperationID != plan.ProtectionID || !restore.ConfirmRelease {
			return op, false, ErrStalePlan
		}
		op.Purpose, op.ProtectionID = "restoration", plan.ProtectionID
		op.ScanMode, op.ScanInterval, op.ScanFormats = restore.ScanMode, restore.ScanInterval, restore.ScanFormats
	}
	return s.operations().create(ctx, op)
}

func (s *Service) BindOperationRun(ctx context.Context, id, runID int64) error {
	return s.operations().bind(ctx, id, runID)
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
	return s.operations().save(ctx, op)
}

// Commit keeps exclusive access through database commit and derived effects.
// Failed effects retain the reservation until recovery finishes.
func (s *Service) Commit(ctx context.Context, id int64, handle *runhandle.Handle) error {
	return s.commitObserved(ctx, id, handle, nil)
}

func (s *Service) commitObserved(ctx context.Context, id int64, handle *runhandle.Handle, committed func(RelocationOperation)) error {
	op, err := s.Operation(ctx, id)
	if err != nil {
		return err
	}
	if op.Status != "pending" {
		return nil
	}
	if op.Purpose == "restoration" {
		return s.commitRestoration(ctx, op, handle, committed)
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
		if e = handle.DisableCancellation(); e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		handle.Phase("commit", "task.msg.relocation.updating", nil)
		total := plan.SeriesCount + plan.BookCount
		handle.Advance(0, total, "task.msg.relocation.updating", nil)
		nextConfig, e := s.preparePolicies(ctx, &op, cfg, session)
		if e != nil {
			return e
		}
		// Register provisional progress before taking SQLite's write reservation,
		// so background sample persistence cannot contend with this transaction.
		handle.AdvanceLive(0, total)
		verify := func() error { return nil }
		if session != nil {
			verify = session.Verify
		}
		processed, e := s.commitPaths(ctx, plan, &op, cfg, handle, verify)
		if e != nil {
			return e
		}
		if committed != nil {
			committed(op)
		}
		handle.Advance(processed, total, "task.msg.relocation.updating", nil)
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
					if errors.Is(err, ErrStalePlan) {
						op.Code = "relocation_stale_plan"
					}
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
	if op.Purpose == "restoration" {
		return s.finishRestoration(ctx, op, verify...)
	}
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
	ids, err := s.operations().unfinished(ctx)
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
		if op.Purpose == "restoration" {
			if e := s.withRestorationConfig(func(_ config.Config, verify func() error) error {
				return s.finishRestoration(ctx, op, verify)
			}); e != nil {
				op.Code = "restoration_recovering"
				_ = s.saveOperation(ctx, op)
			}
			continue
		}
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
