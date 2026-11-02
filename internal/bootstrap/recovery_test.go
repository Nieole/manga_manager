package bootstrap_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"manga-manager/internal/bootstrap"
	"manga-manager/internal/database"
	"manga-manager/internal/runhandle"
	"manga-manager/internal/task"
	"manga-manager/internal/taskstore"
)

// Keep real SQLite restart evidence at the sole production composition seam. The
// scheduling boundary records admission but deliberately never executes the old
// body, modelling a process lost between persisted activation and body execution.
func TestOpenMarksPersistedRunningTasksInterruptedBeforeServingHTTP(t *testing.T) {
	store, cfg := fixture(t)
	engine := task.New(task.Config{Store: taskstore.New(store.DB()), RunBackground: func(func()) {}})
	launched, err := engine.Start(context.Background(), task.RunSpec{
		Identity: task.Identity{Type: "cleanup_library", Scope: task.ScopeLibrary, ScopeID: 55, Variant: task.VariantPrimary},
		Key:      "cleanup_library_55", Trigger: task.TriggerManual, Total: 1,
	}, func(context.Context, *runhandle.Handle) (task.Result, error) {
		t.Error("crashed process body must not execute")
		return task.Result{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if launched.Run.Status != task.StatusRunning {
		t.Fatalf("persisted status=%s", launched.Run.Status)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := database.NewStore(cfg.Snapshot().Database.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	process, err := bootstrap.Open(context.Background(), reopened, cfg, bootstrap.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	router := chi.NewRouter()
	process.HTTP.SetupRoutes(router)
	setup := httptest.NewRecorder()
	router.ServeHTTP(setup, httptest.NewRequest(http.MethodPost, "/api/auth/setup", bytes.NewBufferString(`{"username":"admin","password":"correct horse battery staple"}`)))
	if setup.Code != http.StatusOK || len(setup.Result().Cookies()) == 0 {
		t.Fatalf("setup=%d %s", setup.Code, setup.Body.String())
	}
	request := httptest.NewRequest(http.MethodGet, "/api/system/tasks?scope=library&scope_id=55", nil)
	request.AddCookie(setup.Result().Cookies()[0])
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, request)
	var runs []struct {
		RunID       int64  `json:"run_id"`
		Status      string `json:"status"`
		MessageCode string `json:"message_code"`
		Retryable   bool   `json:"retryable"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &runs) != nil {
		t.Fatalf("tasks=%d %s", rec.Code, rec.Body.String())
	}
	if len(runs) != 1 || runs[0].RunID != launched.Run.ID || runs[0].Status != "interrupted" || runs[0].MessageCode != "task.msg.control.interrupted" || !runs[0].Retryable {
		t.Fatalf("persisted history not recovered before HTTP opened: %s", rec.Body.String())
	}
}
