package api

import (
	"net/http"
)

// RebuildFranchiseCollections synchronizes relation-derived collections through the organizing use case.
func (c *organizationHTTP) rebuildFranchiseCollectionsHandler(w http.ResponseWriter, r *http.Request) {
	if err := c.relations.RetryFranchiseRepair(r.Context()); err != nil {
		c.publish("refresh")
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	c.publish("refresh")
	jsonResponse(w, http.StatusOK, map[string]string{"status": "success"})
}

func (c *organizationHTTP) getFranchiseState(w http.ResponseWriter, r *http.Request) {
	state, err := c.relations.FranchiseState(r.Context())
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to read franchise repair state")
		return
	}
	jsonResponse(w, http.StatusOK, state)
}
