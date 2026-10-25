package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"manga-manager/internal/application/relocating"
	"manga-manager/internal/relocation"
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
		jsonError(w, 400, "Invalid plan ID")
		return
	}
	var request struct {
		AcceptAnomalies bool `json:"accept_anomalies"`
	}
	if err = json.NewDecoder(r.Body).Decode(&request); err != nil {
		jsonError(w, 400, "Invalid request payload")
		return
	}
	op, err := c.moves.Confirm(r.Context(), id, request.AcceptAnomalies)
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

func (c *Controller) createRelocationPlan(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "libraryId")
	if err != nil {
		jsonError(w, 400, "Invalid library ID")
		return
	}
	var request struct {
		SourceRoot string `json:"source_root"`
		TargetRoot string `json:"target_root"`
	}
	if err = json.NewDecoder(r.Body).Decode(&request); err != nil || request.TargetRoot == "" {
		jsonError(w, 400, "Target root required")
		return
	}
	plan, err := c.moves.Preview(r.Context(), id, request.SourceRoot, request.TargetRoot)
	if err != nil {
		writeRelocationError(w, err)
		return
	}
	jsonResponse(w, http.StatusAccepted, plan)
}

func writeRelocationError(w http.ResponseWriter, err error) {
	if errors.Is(err, relocating.ErrCheckQueued) {
		jsonResponse(w, 409, map[string]string{"code": "relocation_check_queued", "error": "A relocation check is already queued"})
		return
	}
	if errors.Is(err, relocating.ErrConflict) {
		jsonResponse(w, 409, map[string]string{"code": "relocation_conflict", "error": "Related library work or access must finish before confirmation"})
		return
	}
	if errors.Is(err, relocating.ErrRestorationCheckNotFound) {
		jsonError(w, 404, "Check not found")
		return
	}

	if errors.Is(err, relocation.ErrStalePlan) {
		jsonResponse(w, http.StatusConflict, map[string]string{"code": "relocation_stale_plan", "error": "Facts changed. Recheck before confirming."})
		return
	}
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
	return c.moves.ReconcilePlan(ctx, plan)
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
