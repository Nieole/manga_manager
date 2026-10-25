package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"manga-manager/internal/relocation"
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
	plan, err := c.moves.CheckRestoration(r.Context(), id)
	if err != nil {
		writeRelocationError(w, err)
		return
	}
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
	op, err := c.moves.Restore(r.Context(), id, request)
	if err != nil {
		writeRelocationError(w, err)
		return
	}
	jsonResponse(w, http.StatusAccepted, op)
}
