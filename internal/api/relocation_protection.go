package api

import (
	"manga-manager/internal/database"
	"net/http"
)

type LibraryRelocationProtection = database.LibraryRelocationProtection

func writeRelocationProtected(w http.ResponseWriter) {
	jsonResponse(w, http.StatusConflict, map[string]string{"code": "relocation_protected", "error": "Relocation protection preserves missing records. Use explicit restoration to enable automatic processing."})
}
