package api

import (
	"context"
	"net/http"
)

// RebuildFranchiseCollections synchronizes relation-derived collections through the organizing use case.
func (c *Controller) RebuildFranchiseCollections(ctx context.Context) error {
	return c.relations.Rebuild(ctx)
}
func (c *Controller) rebuildFranchiseCollectionsHandler(w http.ResponseWriter, r *http.Request) {
	if err := c.relations.RetryFranchiseRepair(r.Context()); err != nil {
		c.PublishEvent("refresh")
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	c.PublishEvent("refresh")
	jsonResponse(w, http.StatusOK, map[string]string{"status": "success"})
}

func (c *Controller) getFranchiseState(w http.ResponseWriter, r *http.Request) {
	state, err := c.relations.FranchiseState(r.Context())
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to read franchise repair state")
		return
	}
	jsonResponse(w, http.StatusOK, state)
}
