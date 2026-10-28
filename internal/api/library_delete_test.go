package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/runhandle"
	"manga-manager/internal/task"
)

func TestDeleteLibraryHTTPReportsRealCoverageWithoutCancellingWork(t *testing.T) {
	for _, kind := range []string{"local", "shared-local", "global"} {
		t.Run(kind, func(t *testing.T) {
			c, store, _, _ := newTestController(t)
			lib, _, _ := seedBookFixture(t, store, t.TempDir(), "Target", "Series", "book.cbz", 3)
			other, err := store.CreateLibrary(context.Background(), database.CreateLibraryParams{Name: "Other", Path: t.TempDir(), ScanMode: "none", ScanInterval: 60, ScanFormats: "cbz"})
			if err != nil {
				t.Fatal(err)
			}
			spec := task.RunSpec{Identity: task.Identity{Type: "deletion_occupier", Scope: task.ScopeLibrary, ScopeID: lib.ID}, LibraryIDs: []int64{lib.ID}, Trigger: task.TriggerManual, CanCancel: true, Key: "misleading-library-key"}
			if kind == "shared-local" {
				spec.LibraryIDs = []int64{lib.ID, other.ID}
			}
			if kind == "global" {
				spec.Identity.Scope = task.ScopeSystem
				spec.Identity.ScopeID = 0
				spec.LibraryIDs = []int64{task.AllLibraries}
			}
			finish := make(chan struct{})
			defer close(finish)
			launched, err := c.runtime.Runs.Start(context.Background(), spec, func(ctx context.Context, _ *runhandle.Handle) (task.Result, error) {
				select {
				case <-finish:
					return task.Result{}, nil
				case <-ctx.Done():
					return task.Result{}, ctx.Err()
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			rec := httptest.NewRecorder()
			c.deleteLibrary(rec, requestWithRouteParam(http.MethodDelete, "/api/libraries/x", nil, "libraryId", strconv.FormatInt(lib.ID, 10)))
			if rec.Code != http.StatusConflict {
				t.Fatalf("busy delete HTTP%d %s", rec.Code, rec.Body.String())
			}
			var result struct {
				Code      string                 `json:"code"`
				Deleted   bool                   `json:"deleted"`
				Conflicts []task.LibraryConflict `json:"conflicts"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Code != "library_busy" || result.Deleted || len(result.Conflicts) != 1 {
				t.Fatalf("conflict contract=%+v", result)
			}
			conflict := result.Conflicts[0]
			if conflict.RunID != launched.Run.ID || conflict.Scope != spec.Identity.Scope || conflict.CanCancelForLibrary != (kind == "local") {
				t.Fatalf("coverage contract=%+v", conflict)
			}
			run, err := c.runtime.Tasks.LoadRun(context.Background(), launched.Run.ID)
			if err != nil || run.Status != task.StatusRunning {
				t.Fatalf("delete cancelled occupied work: %+v %v", run, err)
			}
			if _, err := store.GetLibrary(context.Background(), lib.ID); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDeleteLibraryHTTPPreservesReadConflictAndMissingIdentity(t *testing.T) {
	c, store, _, _ := newTestController(t)
	lib, _, _ := seedBookFixture(t, store, t.TempDir(), "Target", "Series", "book.cbz", 3)
	release, err := c.taskEngine.engine.AcquireLibraryRead(lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	c.deleteLibrary(rec, requestWithRouteParam(http.MethodDelete, "/api/libraries/x", nil, "libraryId", strconv.FormatInt(lib.ID, 10)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("read conflict HTTP%d %s", rec.Code, rec.Body.String())
	}
	release()
	rec = httptest.NewRecorder()
	c.deleteLibrary(rec, requestWithRouteParam(http.MethodDelete, "/api/libraries/x", nil, "libraryId", strconv.FormatInt(lib.ID, 10)))
	if rec.Code != http.StatusOK {
		t.Fatalf("idle delete HTTP%d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	c.deleteLibrary(rec, requestWithRouteParam(http.MethodDelete, "/api/libraries/x", nil, "libraryId", strconv.FormatInt(lib.ID, 10)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("removed identity HTTP%d %s", rec.Code, rec.Body.String())
	}
}

func TestDeleteLibraryWaitsForActualBookDownloadClosure(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, _, book := seedBookFixture(t, store, root, "Download", "Series", "book.cbz", 1)
	payload := []byte("whole book bytes")
	if err := os.WriteFile(book.Path, payload, 0600); err != nil {
		t.Fatal(err)
	}
	entered, resume, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	gate := &downloadFlushGate{ResponseWriter: httptest.NewRecorder(), entered: entered, resume: resume}
	go func() {
		defer close(done)
		c.serveBookFile(gate, requestWithRouteParam(http.MethodGet, "/api/books/x/file", nil, "bookId", strconv.FormatInt(book.ID, 10)))
	}()
	defer func() {
		select {
		case <-resume:
		default:
			close(resume)
		}
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("download did not close")
		}
	}()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("download did not begin")
	}
	rec := httptest.NewRecorder()
	c.deleteLibrary(rec, requestWithRouteParam(http.MethodDelete, "/api/libraries/x", nil, "libraryId", strconv.FormatInt(lib.ID, 10)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("active download delete HTTP%d %s", rec.Code, rec.Body.String())
	}
	if _, err := store.GetLibrary(context.Background(), lib.ID); err != nil {
		t.Fatal(err)
	}
	close(resume)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("download did not close after response resumed")
	}
	rec = httptest.NewRecorder()
	c.deleteLibrary(rec, requestWithRouteParam(http.MethodDelete, "/api/libraries/x", nil, "libraryId", strconv.FormatInt(lib.ID, 10)))
	if rec.Code != http.StatusOK {
		t.Fatalf("closed download delete HTTP%d %s", rec.Code, rec.Body.String())
	}
	if data, err := os.ReadFile(book.Path); err != nil || !bytes.Equal(data, payload) {
		t.Fatalf("download bytes removed: %q %v", data, err)
	}
}
