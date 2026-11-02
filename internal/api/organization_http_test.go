package api

import (
	"github.com/go-chi/chi/v5"
	"manga-manager/internal/database"
	"manga-manager/internal/organize"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// The organization adapter can serve a committed collection without any Controller,
// application runtime, authentication store or task engine.
func TestOrganizationHTTPCollectionWithoutController(t *testing.T) {
	path := filepath.Join(t.TempDir(), "organization.db")
	if err := database.Migrate(path); err != nil {
		t.Fatal(err)
	}
	store, err := database.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	h := newOrganizationHTTP(OrganizationDependencies{Collections: organize.NewCollections(store)})
	router := chi.NewRouter()
	h.setupRoutes(router)
	created := httptest.NewRecorder()
	router.ServeHTTP(created, httptest.NewRequest(http.MethodPost, "/collections/", strings.NewReader(`{"name":"Standalone","description":"manual"}`)))
	if created.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	listed := httptest.NewRecorder()
	router.ServeHTTP(listed, httptest.NewRequest(http.MethodGet, "/collections/", nil))
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `Standalone`) {
		t.Fatalf("list: %d %s", listed.Code, listed.Body.String())
	}
}
