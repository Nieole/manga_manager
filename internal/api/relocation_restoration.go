package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"manga-manager/internal/relocation"
	"manga-manager/internal/runhandle"
	"manga-manager/internal/task"
)

type RestorationRequest = relocation.RestorationRequest

type RelocationRestoration struct {
	Protection *LibraryRelocationProtection `json:"protection"`
	Accepted   *RelocationPlan              `json:"accepted"`
	Check      *RelocationPlan              `json:"check"`
}

func (c *Controller) getRelocationRestoration(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "libraryId")
	if err != nil {
		jsonError(w, 400, "Invalid library ID")
		return
	}
	protection, err := c.store.LibraryRelocationProtection(r.Context(), id)
	if err != nil {
		writeRelocationError(w, err)
		return
	}
	state := RelocationRestoration{Protection: protection}
	if protection != nil {
		accepted, e := c.relocation.Get(r.Context(), protection.PlanID)
		if e != nil {
			writeRelocationError(w, e)
			return
		}
		state.Accepted = &accepted
	}
	check, err := c.relocation.LatestPurpose(r.Context(), id, "restoration")
	if err == nil {
		check, err = c.reconcileRelocationPlan(r.Context(), check)
		if err != nil {
			writeRelocationError(w, err)
			return
		}
		state.Check = &check
	} else if !errors.Is(err, sql.ErrNoRows) {
		writeRelocationError(w, err)
		return
	}
	jsonResponse(w, http.StatusOK, state)
}

func (c *Controller) createRestorationCheck(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "libraryId")
	if err != nil {
		jsonError(w, 400, "Invalid library ID")
		return
	}
	plan, err := c.relocation.CreateRestoration(r.Context(), id)
	if err != nil {
		writeRelocationError(w, err)
		return
	}
	checkPlan := plan
	launched, err := c.taskEngine.start(libraryTask("relocation_check", id, variantSole), task.TriggerManual, RunSpec{
		Key: fmt.Sprintf("relocation_check_%d", id), ScopeName: plan.LibraryName, CanCancel: true,
		StartCode: "task.msg.restoration.checking", CompleteCode: "task.msg.restoration.checked", CancelCode: "task.msg.relocation.cancelled", FailCode: "task.msg.relocation.failed",
		Labels: map[string]string{"plan_id": strconv.FormatInt(plan.ID, 10), "purpose": "restoration"},
	}, func(ctx context.Context, handle *runhandle.Handle) (RunResult, error) {
		conflicts, e := c.relocationCheckConflicts(ctx, checkPlan)
		if e != nil {
			return RunResult{}, restorationTaskError(ctx, e)
		}
		return RunResult{}, restorationTaskError(ctx, c.relocation.Check(ctx, checkPlan.ID, handle, conflicts))
	})
	if err != nil || launched.Coalesced {
		_ = c.relocation.SetStatus(context.WithoutCancel(r.Context()), plan.ID, "failed")
		if err != nil {
			writeRelocationError(w, err)
		} else {
			jsonResponse(w, 409, map[string]string{"code": "relocation_check_queued", "error": "A check is already queued"})
		}
		return
	}
	if err = c.relocation.SetRun(context.WithoutCancel(r.Context()), plan.ID, launched.Run.ID); err != nil {
		writeRelocationError(w, err)
		return
	}
	plan.RunID = launched.Run.ID
	jsonResponse(w, http.StatusAccepted, plan)
}

func (c *Controller) releaseRelocationProtection(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "libraryId")
	if err != nil {
		jsonError(w, 400, "Invalid library ID")
		return
	}
	var request RestorationRequest
	if err = json.NewDecoder(r.Body).Decode(&request); err != nil {
		jsonError(w, 400, "Invalid request payload")
		return
	}
	plan, err := c.relocation.Get(r.Context(), request.PlanID)
	if err != nil {
		writeRelocationError(w, err)
		return
	}
	if plan.LibraryID != id {
		jsonError(w, 404, "Check not found")
		return
	}
	op, created, err := c.relocation.PrepareRestoration(r.Context(), request)
	if err != nil {
		writeRelocationError(w, err)
		return
	}
	if created {
		conflicts, conflictErr := c.relocationCheckConflicts(r.Context(), plan)
		if conflictErr != nil || len(conflicts) != 0 {
			_ = c.relocation.FailUnstarted(context.WithoutCancel(r.Context()), op.ID, "relocation_conflict")
			if conflictErr != nil {
				writeRelocationError(w, conflictErr)
			} else {
				jsonResponse(w, http.StatusConflict, map[string]string{"code": "relocation_conflict", "error": "Related library work or access must finish before release"})
			}
			return
		}
		operationID := op.ID
		launched, e := c.taskEngine.start(libraryTask("relocation_commit", id, variantSole), task.TriggerManual, RunSpec{
			Key: fmt.Sprintf("relocation_commit_%d", id), ScopeName: plan.LibraryName, CanCancel: true,
			StartCode: "task.msg.restoration.releasing", CompleteCode: "task.msg.restoration.completed", FailCode: "task.msg.restoration.failed", CancelCode: "task.msg.relocation.commit_cancelled",
			Labels: map[string]string{"plan_id": strconv.FormatInt(plan.ID, 10), "operation_id": strconv.FormatInt(op.ID, 10), "purpose": "restoration"},
		}, func(ctx context.Context, handle *runhandle.Handle) (RunResult, error) {
			return RunResult{}, restorationTaskError(ctx, c.relocation.Commit(ctx, operationID, handle))
		})
		if e != nil || launched.Coalesced {
			_ = c.relocation.FailUnstarted(context.WithoutCancel(r.Context()), op.ID, "relocation_conflict")
			if e != nil {
				writeRelocationError(w, e)
			} else {
				jsonResponse(w, 409, map[string]string{"code": "relocation_conflict", "error": "A release is already queued"})
			}
			return
		}
		if err = c.relocation.BindOperationRun(context.WithoutCancel(r.Context()), op.ID, launched.Run.ID); err != nil {
			writeRelocationError(w, err)
			return
		}
	}
	op, err = c.relocation.Operation(r.Context(), op.ID)
	if err != nil {
		writeRelocationError(w, err)
		return
	}
	jsonResponse(w, http.StatusAccepted, op)
}

func restorationTaskError(ctx context.Context, err error) error {
	if err == nil || errors.Is(err, context.Canceled) {
		return err
	}
	slog.ErrorContext(ctx, "Protected library restoration failed", "error", err)
	return errors.New("restoration_failed")
}

// Register the chosen watcher while protection and exclusive access still stop
// automatic dispatch. Completion clears protection only after this succeeds.
func (c *Controller) restorationEffects(ctx context.Context, op RelocationOperation) error {
	lib, err := c.store.GetLibrary(ctx, op.LibraryID)
	if err != nil {
		return err
	}
	if c.watcher != nil {
		c.watcher.UnwatchLibrary(lib.Path)
	}
	if op.ScanMode == "watch" {
		if c.watcherInitErr != nil {
			return c.watcherInitErr
		}
		if c.watcher == nil {
			return errors.New("file watcher unavailable")
		}
		if err = c.watcher.WatchLibrary(lib.ID, lib.Path, op.ScanFormats); err != nil {
			return err
		}
	}
	c.invalidateDashboardStatsCache("library_restored")
	return nil
}
