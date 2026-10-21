package api

import (
	"context"
	"manga-manager/internal/database"
	"net/http"
)

type LibraryRelocationProtection = database.LibraryRelocationProtection

func (c *Controller) requireAutomaticProcessing(ctx context.Context, libraryID int64) error {
	protection, err := c.store.LibraryRelocationProtection(ctx, libraryID)
	if err != nil {
		return err
	}
	if protection != nil {
		return database.ErrLibraryProtected
	}
	return nil
}

func writeRelocationProtected(w http.ResponseWriter) {
	jsonResponse(w, http.StatusConflict, map[string]string{"code": "relocation_protected", "error": "Relocation protection preserves missing records. Use explicit restoration to enable automatic processing."})
}
