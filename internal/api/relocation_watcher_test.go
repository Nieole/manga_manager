package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/scanner"
)

func TestRelocationKeepsIndependentNestedLibraryWatching(t *testing.T) {
	for _, location := range []string{"source", "target"} {
		for _, mode := range []string{"none", "watch"} {
			t.Run(location+"/"+mode, func(t *testing.T) {
				c, store, _, root := newTestController(t)
				lib, _, book := seedBookFixture(t, store, root, "source", "series", "book.cbz", 1)
				seedBookArchivePages(t, c, book, png1x1)
				if runtime.GOOS == "windows" {
					lib.Path = strings.Replace(filepath.ToSlash(lib.Path), "/source", "/SOURCE", 1)
				}
				target := filepath.Join(root, "target")
				if err := os.MkdirAll(filepath.Join(target, "series"), 0o755); err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(book.Path)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(target, "series", "book.cbz"), data, 0o644); err != nil {
					t.Fatal(err)
				}
				if _, err = store.UpdateLibrary(context.Background(), database.UpdateLibraryParams{ID: lib.ID, Name: lib.Name, Path: lib.Path, ScanMode: mode, ScanInterval: 60, ScanFormats: lib.ScanFormats}); err != nil {
					t.Fatal(err)
				}
				nestedPath := filepath.Join(root, location, "independent")
				if err = os.MkdirAll(nestedPath, 0o755); err != nil {
					t.Fatal(err)
				}
				nested, err := store.CreateLibrary(context.Background(), database.CreateLibraryParams{Name: "independent", Path: nestedPath, ScanMode: "watch", ScanInterval: 60})
				if err != nil {
					t.Fatal(err)
				}
				c.watcher.Stop()
				scans := make(chan int64, 8)
				c.watcher, err = scanner.NewFileWatcher(scanner.WatcherHooks{
					ScanLibrary:        func(_ context.Context, id int64) error { scans <- id; return nil },
					CleanupLibrary:     func(context.Context, int64) error { return nil },
					LibraryScanRunning: func(int64) bool { return false },
				})
				if err != nil {
					t.Fatal(err)
				}
				if mode == "watch" {
					if err = c.watcher.WatchLibrary(lib.ID, lib.Path, ""); err != nil {
						t.Fatal(err)
					}
				}
				if err = c.watcher.WatchLibrary(nested.ID, nested.Path, ""); err != nil {
					t.Fatal(err)
				}
				// Replace the watcher at its application owner as well as the HTTP fixture reference.
				c.runtime.Watcher = c.watcher
				c.watcher.Start(func(string) {})
				body, _ := json.Marshal(map[string]string{"source_root": filepath.Clean(strings.Replace(lib.Path, "/SOURCE", "/source", 1)), "target_root": target})
				rec := httptest.NewRecorder()
				c.createRelocationPlan(rec, requestWithRouteParam(http.MethodPost, "/api/libraries/1/relocation-plans", body, "libraryId", strconv.FormatInt(lib.ID, 10)))
				var started RelocationPlan
				if err = json.Unmarshal(rec.Body.Bytes(), &started); err != nil || rec.Code != http.StatusAccepted {
					t.Fatalf("check explicit native source root: %d %s %v", rec.Code, rec.Body.String(), err)
				}
				plan := awaitRelocationPlan(t, c, started.ID)
				if plan.BlockerCount != 0 || plan.AnomalyCount != 0 {
					t.Fatalf("independent nested library prevented relocation: %+v", plan)
				}
				op := submitRelocation(t, c, plan.ID)
				if got := awaitRelocationOperation(t, c, op.ID); got.Status != "completed" {
					t.Fatalf("relocation: %+v", got)
				}
				if err = os.WriteFile(filepath.Join(nestedPath, "new.cbz"), data, 0o644); err != nil {
					t.Fatal(err)
				}
				deadline := time.After(10 * time.Second)
				var settled <-chan time.Time
				for {
					select {
					case id := <-scans:
						if location == "source" && id == lib.ID {
							t.Fatal("relocated library still receives events from its old root")
						}
						if id == nested.ID {
							settled = time.After(time.Second)
						}
					case <-settled:
						return
					case <-deadline:
						t.Fatal("independent nested library stopped receiving filesystem changes after relocation")
					}
				}
			})
		}
	}
}
