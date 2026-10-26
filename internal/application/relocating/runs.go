package relocating

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"manga-manager/internal/relocation"
	"manga-manager/internal/runhandle"
	"manga-manager/internal/task"
	"strconv"
)

// ErrCheckQueued means an existing queued check absorbed this launch.
var ErrCheckQueued = errors.New("relocation_check_queued")

// ErrConflict means confirmation cannot acquire its related work boundary.
var ErrConflict = errors.New("relocation_conflict")

// ErrRestorationCheckNotFound rejects a check belonging to another library.
var ErrRestorationCheckNotFound = errors.New("restoration_check_not_found")

// Preview creates and runs one checked proposal without moving any files.
func (c *Service) Preview(ctx context.Context, libraryID int64, source, target string) (relocation.RelocationPlan, error) {
	plan, err := c.domain.Create(ctx, libraryID, source, target)
	if err != nil {
		return plan, err
	}
	return c.startCheck(ctx, plan)
}

// CheckRestoration inspects the currently protected records without releasing protection.
func (c *Service) CheckRestoration(ctx context.Context, libraryID int64) (relocation.RelocationPlan, error) {
	plan, err := c.domain.CreateRestoration(ctx, libraryID)
	if err != nil {
		return plan, err
	}
	return c.startCheck(ctx, plan)
}

func (c *Service) startCheck(ctx context.Context, plan relocation.RelocationPlan) (relocation.RelocationPlan, error) {
	restore := plan.Purpose == "restoration"
	spec := task.RunSpec{Identity: task.Identity{Type: "relocation_check", Scope: task.ScopeLibrary, ScopeID: plan.LibraryID}, LibraryIDs: []int64{plan.LibraryID}, Trigger: task.TriggerManual,
		Key: fmt.Sprintf("relocation_check_%d", plan.LibraryID), ScopeName: plan.LibraryName, CanCancel: true,
		StartCode: "task.msg.relocation.checking", CompleteCode: "task.msg.relocation.checked", CancelCode: "task.msg.relocation.cancelled", FailCode: "task.msg.relocation.failed", Labels: map[string]string{"plan_id": strconv.FormatInt(plan.ID, 10)}}
	if restore {
		spec.StartCode = "task.msg.restoration.checking"
		spec.CompleteCode = "task.msg.restoration.checked"
		spec.Labels["purpose"] = "restoration"
	}
	launched, err := c.runs.Start(ctx, spec, func(runCtx context.Context, handle *runhandle.Handle) (task.Result, error) {
		conflicts, workErr := c.checkConflicts(runCtx, plan)
		if workErr == nil {
			workErr = c.domain.Check(runCtx, plan.ID, handle, conflicts)
		}
		if restore {
			workErr = restorationTaskError(runCtx, workErr)
		}
		return task.Result{}, workErr
	})
	if err != nil || launched.Coalesced {
		status := "failed"
		if launched.Coalesced && !restore {
			status = "cancelled"
		}
		_ = c.domain.SetStatus(context.WithoutCancel(ctx), plan.ID, status)
		if err != nil {
			return plan, err
		}
		return plan, ErrCheckQueued
	}
	if err = c.domain.SetRun(context.WithoutCancel(ctx), plan.ID, launched.Run.ID); err != nil {
		return plan, err
	}
	plan.RunID = launched.Run.ID
	return plan, nil
}

// Confirm starts one durable relocation operation after explicit anomaly confirmation.
func (c *Service) Confirm(ctx context.Context, planID int64, acceptAnomalies bool) (relocation.RelocationOperation, error) {
	op, created, err := c.domain.Prepare(ctx, planID, acceptAnomalies)
	if err != nil {
		return op, err
	}
	if created {
		lib, loadErr := c.store.GetLibrary(ctx, op.LibraryID)
		if loadErr != nil {
			_ = c.domain.FailUnstarted(context.WithoutCancel(ctx), op.ID, "relocation_commit_failed")
			return op, loadErr
		}
		if err = c.startCommit(ctx, op, lib.Name); err != nil {
			return op, err
		}
	}
	return c.domain.Operation(ctx, op.ID)
}

// Restore preserves the administrator's explicit release/loss-risk decision.
func (c *Service) Restore(ctx context.Context, libraryID int64, request relocation.RestorationRequest) (relocation.RelocationOperation, error) {
	plan, err := c.domain.Get(ctx, request.PlanID)
	if err != nil {
		return relocation.RelocationOperation{}, err
	}
	if plan.LibraryID != libraryID {
		return relocation.RelocationOperation{}, ErrRestorationCheckNotFound
	}
	op, created, err := c.domain.PrepareRestoration(ctx, request)
	if err != nil {
		return op, err
	}
	if created {
		conflicts, conflictErr := c.checkConflicts(ctx, plan)
		if conflictErr != nil || len(conflicts) != 0 {
			_ = c.domain.FailUnstarted(context.WithoutCancel(ctx), op.ID, "relocation_conflict")
			if conflictErr != nil {
				return op, conflictErr
			}
			return op, ErrConflict
		}
		if err = c.startCommit(ctx, op, plan.LibraryName); err != nil {
			return op, err
		}
	}
	return c.domain.Operation(ctx, op.ID)
}

func (c *Service) startCommit(ctx context.Context, op relocation.RelocationOperation, libraryName string) error {
	restore := op.Purpose == "restoration"
	spec := task.RunSpec{Identity: task.Identity{Type: "relocation_commit", Scope: task.ScopeLibrary, ScopeID: op.LibraryID}, LibraryIDs: []int64{op.LibraryID}, Trigger: task.TriggerManual,
		Key: fmt.Sprintf("relocation_commit_%d", op.LibraryID), ScopeName: libraryName, CanCancel: true,
		StartCode: "task.msg.relocation.committing", CompleteCode: "task.msg.relocation.completed", FailCode: "task.msg.relocation.commit_failed", CancelCode: "task.msg.relocation.commit_cancelled",
		Labels: map[string]string{"plan_id": strconv.FormatInt(op.PlanID, 10), "operation_id": strconv.FormatInt(op.ID, 10)}}
	if restore {
		spec.StartCode = "task.msg.restoration.releasing"
		spec.CompleteCode = "task.msg.restoration.completed"
		spec.FailCode = "task.msg.restoration.failed"
		spec.Labels["purpose"] = "restoration"
	}
	launched, err := c.runs.Start(ctx, spec, func(runCtx context.Context, handle *runhandle.Handle) (task.Result, error) {
		workErr := c.domain.Commit(runCtx, op.ID, handle)
		if restore {
			workErr = restorationTaskError(runCtx, workErr)
		}
		return task.Result{}, workErr
	})
	if err != nil || launched.Coalesced {
		_ = c.domain.FailUnstarted(context.WithoutCancel(ctx), op.ID, "relocation_conflict")
		if err != nil {
			return err
		}
		return ErrConflict
	}
	return c.domain.BindOperationRun(context.WithoutCancel(ctx), op.ID, launched.Run.ID)
}

// ReconcilePlan observes durable run completion without waiting for the writer's lock.
func (c *Service) ReconcilePlan(ctx context.Context, plan relocation.RelocationPlan) (relocation.RelocationPlan, error) {
	if (plan.Status == "pending" || plan.Status == "checking") && plan.RunID != 0 {
		run, loadErr := c.runReader.LoadRun(ctx, plan.RunID)
		if errors.Is(loadErr, sql.ErrNoRows) || (loadErr == nil && run.Status.IsTerminal()) {
			status := "failed"
			switch run.Status {
			case task.StatusCancelled:
				status = "cancelled"
			case task.StatusInterrupted:
				status = "interrupted"
			}
			if errors.Is(loadErr, sql.ErrNoRows) {
				status = "interrupted"
			}
			if err := c.domain.SetStatus(ctx, plan.ID, status); err != nil {
				return plan, err
			}
			return c.domain.Get(ctx, plan.ID)
		}
	}
	return plan, nil
}

func restorationTaskError(ctx context.Context, err error) error {
	if err == nil || errors.Is(err, context.Canceled) {
		return err
	}
	slog.ErrorContext(ctx, "Protected library restoration failed", "error", err)
	return errors.New("restoration_failed")
}
