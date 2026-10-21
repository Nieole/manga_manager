package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"manga-manager/internal/database"
	"net/http"
	"strconv"

	"manga-manager/internal/relocation"
	"manga-manager/internal/runhandle"
	"manga-manager/internal/task"
)

type RelocationPlan = relocation.RelocationPlan
type RelocationDetail = relocation.RelocationDetail
type RelocationDetails = relocation.RelocationDetails
type RelocationPolicy = relocation.RelocationPolicy
type RelocationSourceRoots = relocation.RelocationSourceRoots

func (c *Controller) relocationSourceRoots(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "libraryId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid library ID")
		return
	}
	roots, err := c.relocation.SourceRoots(r.Context(), id)
	if err != nil {
		writeRelocationError(w, err)
		return
	}
	jsonResponse(w, http.StatusOK, roots)
}

type RelocationOperation = relocation.RelocationOperation

func (c *Controller) commitRelocationPlan(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "planId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid plan ID")
		return
	}
	var request struct {
		AcceptAnomalies bool `json:"accept_anomalies"`
	}
	if err = json.NewDecoder(r.Body).Decode(&request); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	op, created, err := c.relocation.Prepare(r.Context(), id, request.AcceptAnomalies)
	if err != nil {
		writeRelocationError(w, err)
		return
	}
	if created {
		lib, loadErr := c.store.GetLibrary(r.Context(), op.LibraryID)
		if loadErr != nil {
			_ = c.relocation.FailUnstarted(context.WithoutCancel(r.Context()), op.ID, "relocation_commit_failed")
			writeRelocationError(w, loadErr)
			return
		}
		operationID := op.ID
		launched, startErr := c.taskEngine.start(libraryTask("relocation_commit", op.LibraryID, variantSole), task.TriggerManual, RunSpec{
			Key: fmt.Sprintf("relocation_commit_%d", op.LibraryID), ScopeName: lib.Name, CanCancel: true,
			StartCode: "task.msg.relocation.committing", CompleteCode: "task.msg.relocation.completed", FailCode: "task.msg.relocation.commit_failed", CancelCode: "task.msg.relocation.commit_cancelled",
			Labels: map[string]string{"plan_id": strconv.FormatInt(id, 10), "operation_id": strconv.FormatInt(op.ID, 10)},
		}, func(ctx context.Context, handle *runhandle.Handle) (RunResult, error) {
			return RunResult{}, c.relocation.Commit(ctx, operationID, handle)
		})
		if startErr != nil {
			_ = c.relocation.FailUnstarted(context.WithoutCancel(r.Context()), op.ID, "relocation_conflict")
			writeRelocationError(w, startErr)
			return
		}
		if launched.Coalesced {
			_ = c.relocation.FailUnstarted(context.WithoutCancel(r.Context()), op.ID, "relocation_conflict")
			jsonResponse(w, http.StatusConflict, map[string]string{"code": "relocation_conflict", "error": "A relocation is already queued"})
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

func (c *Controller) getRelocationOperation(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "operationId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid operation ID")
		return
	}
	op, err := c.relocation.Operation(r.Context(), id)
	if err != nil {
		writeRelocationError(w, err)
		return
	}
	jsonResponse(w, http.StatusOK, op)
}

func (c *Controller) relocationEffects(ctx context.Context, op RelocationOperation) error {
	c.purgeReadingPathCaches()
	c.invalidateDashboardStatsCache("library_relocated")
	c.external.InvalidateLibrary(op.LibraryID)
	lib, err := c.store.GetLibrary(ctx, op.LibraryID)
	if err != nil {
		return err
	}
	if c.watcher != nil {
		c.watcher.UnwatchLibrary(op.SourceRoot)
	}
	if lib.ScanMode == "watch" {
		if err := c.requireAutomaticProcessing(ctx, lib.ID); errors.Is(err, database.ErrLibraryProtected) {
			return nil
		} else if err != nil {
			return err
		}
		if c.watcherInitErr != nil {
			return c.watcherInitErr
		}
		if c.watcher == nil {
			return errors.New("file watcher unavailable")
		}
		return c.watcher.WatchLibrary(lib.ID, lib.Path, lib.ScanFormats)
	}
	return nil
}

func (c *Controller) createRelocationPlan(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "libraryId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid library ID")
		return
	}
	var request struct {
		SourceRoot string `json:"source_root"`
		TargetRoot string `json:"target_root"`
	}
	if err = json.NewDecoder(r.Body).Decode(&request); err != nil || request.TargetRoot == "" {
		jsonError(w, http.StatusBadRequest, "Target root required")
		return
	}
	plan, err := c.relocation.Create(r.Context(), id, request.SourceRoot, request.TargetRoot)
	if err != nil {
		writeRelocationError(w, err)
		return
	}
	launched, err := c.taskEngine.start(libraryTask("relocation_check", id, variantSole), task.TriggerManual, RunSpec{
		Key: fmt.Sprintf("relocation_check_%d", id), ScopeName: plan.LibraryName, CanCancel: true,
		StartCode: "task.msg.relocation.checking", CompleteCode: "task.msg.relocation.checked",
		CancelCode: "task.msg.relocation.cancelled", FailCode: "task.msg.relocation.failed",
		Labels: map[string]string{"plan_id": strconv.FormatInt(plan.ID, 10)},
	}, func(ctx context.Context, handle *runhandle.Handle) (RunResult, error) {
		conflicts, conflictErr := c.relocationCheckConflicts(ctx, plan)
		if conflictErr != nil {
			return RunResult{}, conflictErr
		}
		return RunResult{}, c.relocation.Check(ctx, plan.ID, handle, conflicts)
	})
	if err != nil {
		_ = c.relocation.SetStatus(context.WithoutCancel(r.Context()), plan.ID, "failed")
		writeRelocationError(w, err)
		return
	}
	if launched.Coalesced {
		_ = c.relocation.SetStatus(context.WithoutCancel(r.Context()), plan.ID, "cancelled")
		jsonResponse(w, http.StatusConflict, map[string]string{"code": "relocation_check_queued", "error": "A relocation check is already queued"})
		return
	}
	if err = c.relocation.SetRun(context.WithoutCancel(r.Context()), plan.ID, launched.Run.ID); err != nil {
		writeRelocationError(w, err)
		return
	}
	plan.RunID = launched.Run.ID
	jsonResponse(w, http.StatusAccepted, plan)
}

func writeRelocationError(w http.ResponseWriter, err error) {
	if errors.Is(err, relocation.ErrAnomaliesRequireConfirmation) {
		jsonResponse(w, http.StatusConflict, map[string]string{"code": "relocation_anomalies_require_confirmation", "error": "Explicitly accept the anomalies in this plan"})
		return
	}
	if errors.Is(err, relocation.ErrPlanNotReady) {
		jsonResponse(w, http.StatusConflict, map[string]string{"code": "relocation_plan_not_ready", "error": "Recheck this relocation plan before confirming"})
		return
	}
	if errors.Is(err, sql.ErrNoRows) {
		jsonError(w, http.StatusNotFound, "Relocation plan or library not found")
		return
	}
	if errors.Is(err, errLibraryUnavailable) {
		writeLibraryUnavailable(w)
		return
	}
	jsonResponse(w, http.StatusInternalServerError, map[string]string{"code": "relocation_persistence_failed", "error": "Unable to read or persist relocation plan"})
}

func (c *Controller) reconcileRelocationPlan(ctx context.Context, plan RelocationPlan) (RelocationPlan, error) {
	if (plan.Status == "pending" || plan.Status == "checking") && plan.RunID != 0 {
		run, loadErr := c.taskEngine.runStore.LoadRun(ctx, plan.RunID)
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
			if err := c.relocation.SetStatus(ctx, plan.ID, status); err != nil {
				return plan, err
			}
			return c.relocation.Get(ctx, plan.ID)
		}
	}
	return plan, nil
}

func (c *Controller) getRelocationPlan(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "planId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid plan ID")
		return
	}
	plan, err := c.relocation.Get(r.Context(), id)
	if err != nil {
		writeRelocationError(w, err)
		return
	}
	plan, err = c.reconcileRelocationPlan(r.Context(), plan)
	if err != nil {
		writeRelocationError(w, err)
		return
	}
	jsonResponse(w, http.StatusOK, plan)
}

func (c *Controller) latestRelocationPlan(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "libraryId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid library ID")
		return
	}
	plan, err := c.relocation.Latest(r.Context(), id)
	if err != nil {
		writeRelocationError(w, err)
		return
	}
	plan, err = c.reconcileRelocationPlan(r.Context(), plan)
	if err != nil {
		writeRelocationError(w, err)
		return
	}
	jsonResponse(w, http.StatusOK, plan)
}

func (c *Controller) relocationDetails(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "planId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid plan ID")
		return
	}
	kind := r.URL.Query().Get("kind")
	if kind != "" && kind != "blocker" && kind != "anomaly" && kind != "mapping" && kind != "policy" {
		jsonError(w, http.StatusBadRequest, "Invalid detail category")
		return
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 50
	}
	page, err := c.relocation.Details(r.Context(), id, kind, offset, limit)
	if err != nil {
		writeRelocationError(w, err)
		return
	}
	jsonResponse(w, http.StatusOK, page)
}
