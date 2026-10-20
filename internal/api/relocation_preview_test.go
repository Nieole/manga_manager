package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/relocation"
)

func TestRelocationPreviewPreservesRecordsAndCanBeReadAfterReopen(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, series, book := seedBookFixture(t, store, root, "旧资料库", "漫画 空格", "第一卷.cbz", 10)
	target := filepath.Join(root, "目标盘")
	if err := os.MkdirAll(filepath.Join(target, "漫画 空格"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "漫画 空格", "第一卷.cbz"), make([]byte, 1024), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(lib.Path); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"target_root": target})
	req := requestWithRouteParam(http.MethodPost, "/api/libraries/1/relocation-plans", body, "libraryId", strconv.FormatInt(lib.ID, 10))
	rec := httptest.NewRecorder()
	c.createRelocationPlan(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("start preview: %d %s", rec.Code, rec.Body.String())
	}
	var started RelocationPlan
	if err := json.Unmarshal(rec.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	plan := awaitRelocationPlan(t, c, started.ID)
	if plan.Status != "checked" || plan.BlockerCount != 0 || plan.AnomalyCount != 0 || plan.SeriesCount != 1 || plan.BookCount != 1 {
		t.Fatalf("preview result: %+v", plan)
	}
	if plan.SourceRoot != lib.Path || plan.TargetRoot != target || plan.RunID == 0 {
		t.Fatalf("preview roots and run: %+v", plan)
	}
	gotLib, _ := store.GetLibrary(context.Background(), lib.ID)
	gotBook, _ := store.GetBook(context.Background(), book.ID)
	gotSeries, _ := store.GetSeries(context.Background(), series.ID)
	if gotLib.Path != lib.Path || gotBook.Path != book.Path || gotSeries.Path != series.Path {
		t.Fatalf("read-only preview changed paths: %s %s %s", gotLib.Path, gotSeries.Path, gotBook.Path)
	}
	reopened := restartController(t, c, store, root)
	latestReq := requestWithRouteParam(http.MethodGet, "/api/libraries/1/relocation-plans/latest", nil, "libraryId", strconv.FormatInt(lib.ID, 10))
	latestRec := httptest.NewRecorder()
	reopened.latestRelocationPlan(latestRec, latestReq)
	var latest RelocationPlan
	if err := json.Unmarshal(latestRec.Body.Bytes(), &latest); err != nil || latest.ID != started.ID || latest.Status != "checked" {
		t.Fatalf("reopened preview: %d %s (%v)", latestRec.Code, latestRec.Body.String(), err)
	}
}

func startRelocationPreview(t *testing.T, c *Controller, libraryID int64, target string) RelocationPlan {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"target_root": target})
	req := requestWithRouteParam(http.MethodPost, "/api/libraries/1/relocation-plans", body, "libraryId", strconv.FormatInt(libraryID, 10))
	rec := httptest.NewRecorder()
	c.createRelocationPlan(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("start preview: %d %s", rec.Code, rec.Body.String())
	}
	var plan RelocationPlan
	if err := json.Unmarshal(rec.Body.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	return awaitRelocationPlan(t, c, plan.ID)
}

func previewDetails(t *testing.T, c *Controller, id int64, query string) RelocationDetails {
	t.Helper()
	req := requestWithRouteParam(http.MethodGet, "/api/relocation-plans/1/details?"+query, nil, "planId", strconv.FormatInt(id, 10))
	rec := httptest.NewRecorder()
	c.relocationDetails(rec, req)
	var page RelocationDetails
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("preview details: %d %s (%v)", rec.Code, rec.Body.String(), err)
	}
	return page
}

func TestRelocationPreviewReportsExistingRecordsAndRejectsPrefixLookalikes(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, _, book := seedBookFixture(t, store, root, "source", "series", "book.cbz", 1)
	other, otherSeries, _ := seedBookFixture(t, store, root, "other", "occupied", "other.cbz", 1)
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateBook(context.Background(), database.CreateBookParams{LibraryID: other.ID, SeriesID: otherSeries.ID,
		Name: "collision.cbz", Path: filepath.Join(target, "series", "book.cbz"), Size: 1, FileModifiedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateSeries(context.Background(), database.CreateSeriesParams{LibraryID: lib.ID, Name: "lookalike", Path: lib.Path + "-other", NameInitial: "L"}); err != nil {
		t.Fatal(err)
	}
	plan := startRelocationPreview(t, c, lib.ID, target)
	page := previewDetails(t, c, plan.ID, "kind=blocker")
	reasons := map[string]bool{}
	for _, detail := range page.Items {
		reasons[detail.Reason] = true
	}
	if !reasons["record_occupied"] || !reasons["outside_source_root"] {
		t.Fatalf("missing conflict reasons: %+v", page.Items)
	}
	got, _ := store.GetBook(context.Background(), book.ID)
	if got.Path != book.Path {
		t.Fatal("blocked preview rewrote original book")
	}
}

type unreadableRelocationFiles struct {
	relocation.HostFiles
	path string
}

func (f unreadableRelocationFiles) Open(path string) (io.Closer, error) {
	if path == f.path {
		return nil, os.ErrPermission
	}
	return f.HostFiles.Open(path)
}

func TestRelocationPreviewClassifiesFileAnomaliesAndPagesPersistedDetails(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, series, _ := seedBookFixture(t, store, root, "source", "series", "missing.cbz", 1)
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(filepath.Join(target, "series", "directory.cbz"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"changed.cbz", "unreadable.cbz"} {
		if err := os.WriteFile(filepath.Join(target, "series", name), []byte("smaller"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"changed.cbz", "directory.cbz", "unreadable.cbz"} {
		if _, err := store.CreateBook(context.Background(), database.CreateBookParams{LibraryID: lib.ID, SeriesID: series.ID,
			Name: name, Path: filepath.Join(series.Path, name), Size: 1024, FileModifiedAt: time.Now(), Title: sql.NullString{}}); err != nil {
			t.Fatal(err)
		}
	}
	c.relocation.Files = unreadableRelocationFiles{path: filepath.Join(target, "series", "unreadable.cbz")}
	plan := startRelocationPreview(t, c, lib.ID, target)
	for _, reason := range []string{"missing", "size_mismatch", "not_regular", "unreadable"} {
		if plan.AnomalyCounts[reason] != 1 {
			t.Fatalf("anomaly %s count: %+v", reason, plan.AnomalyCounts)
		}
	}
	first := previewDetails(t, c, plan.ID, "kind=anomaly&limit=2")
	second := previewDetails(t, c, plan.ID, "kind=anomaly&limit=2&offset=2")
	if first.Total != 4 || len(first.Items) != 2 || len(second.Items) != 2 || first.Items[0].Ordinal == second.Items[0].Ordinal {
		t.Fatalf("paged anomalies: %+v %+v", first, second)
	}
}

func TestRelocationPreviewPoliciesShowConflictsAndOtherLibraryEffects(t *testing.T) {
	for _, scenario := range []string{"different_target", "nested_source", "nested_target", "same_target"} {
		t.Run(scenario, func(t *testing.T) {
			c, store, _, root := newTestController(t)
			lib, _, _ := seedBookFixture(t, store, root, "source", "series", "book.cbz", 1)
			target := filepath.Join(root, "target")
			if err := os.MkdirAll(target, 0o755); err != nil {
				t.Fatal(err)
			}
			cfg := c.config.Snapshot()
			cfg.Library.StoragePolicies = []config.LibraryStoragePolicy{{Path: lib.Path, StorageProfile: config.StorageProfileHDDExternal},
				{Path: root, StorageProfile: config.StorageProfileSSD}}
			if scenario == "different_target" || scenario == "same_target" {
				profile := config.StorageProfileSSD
				if scenario == "same_target" {
					profile = config.StorageProfileHDDExternal
				}
				cfg.Library.StoragePolicies = append(cfg.Library.StoragePolicies, config.LibraryStoragePolicy{Path: target, StorageProfile: profile})
			} else {
				parent := lib.Path
				if scenario == "nested_target" {
					parent = target
				}
				if _, err := store.CreateLibrary(context.Background(), database.CreateLibraryParams{Name: "nested", Path: filepath.Join(parent, "nested"), ScanMode: "none", ScanInterval: 60}); err != nil {
					t.Fatal(err)
				}
			}
			c.config.Replace(&cfg)
			before, _ := json.Marshal(c.config.Snapshot().Library.StoragePolicies)
			plan := startRelocationPreview(t, c, lib.ID, target)
			if plan.PolicyCount != 1 {
				t.Fatalf("expected explicit policy mapping: %+v", plan)
			}
			expected := "policy_affects_other_library"
			if scenario == "different_target" {
				expected = "policy_conflict"
			}
			page := previewDetails(t, c, plan.ID, "kind=blocker")
			found := false
			for _, detail := range page.Items {
				found = found || detail.Reason == expected
			}
			if scenario == "same_target" {
				if page.Total != 0 {
					t.Fatalf("identical target policy should merge: %+v", page)
				}
			} else if !found {
				t.Fatalf("expected %s: %+v", expected, page)
			}
			after, _ := json.Marshal(c.config.Snapshot().Library.StoragePolicies)
			if string(before) != string(after) {
				t.Fatal("policy preview changed live policies")
			}
		})
	}
}

func awaitRelocationPlan(t *testing.T, c *Controller, id int64) RelocationPlan {
	t.Helper()
	deadline := time.After(10 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		req := requestWithRouteParam(http.MethodGet, "/api/relocation-plans/1", nil, "planId", strconv.FormatInt(id, 10))
		rec := httptest.NewRecorder()
		c.getRelocationPlan(rec, req)
		var plan RelocationPlan
		if err := json.Unmarshal(rec.Body.Bytes(), &plan); err != nil {
			t.Fatalf("get preview: %d %s (%v)", rec.Code, rec.Body.String(), err)
		}
		if plan.Status != "pending" && plan.Status != "checking" {
			return plan
		}
		select {
		case <-deadline:
			t.Fatalf("preview did not finish: %+v", plan)
		case <-ticker.C:
		}
	}
}
