package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"manga-manager/internal/relocation"
	"manga-manager/internal/runhandle"
	"manga-manager/internal/task"
)

type RelocationPlan = relocation.RelocationPlan
type RelocationDetail = relocation.RelocationDetail
type RelocationDetails = relocation.RelocationDetails

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
		conflicts, conflictErr := c.libraryAccessConflicts(ctx, id)
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

func (c *Controller) reconcileRelocationPlan(ctx context.Context, plan RelocationPlan) RelocationPlan {
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
			if err := c.relocation.SetStatus(ctx, plan.ID, status); err == nil {
				plan.Status = status
			}
		}
	}
	return plan
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
	jsonResponse(w, http.StatusOK, c.reconcileRelocationPlan(r.Context(), plan))
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
	jsonResponse(w, http.StatusOK, c.reconcileRelocationPlan(r.Context(), plan))
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
