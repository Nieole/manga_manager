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
	if err := c.relations.Rebuild(r.Context()); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "success"})
}
