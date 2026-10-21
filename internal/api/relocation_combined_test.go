package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/scanner"
)

func TestRelocationCombinedLegacyRepairProtectionAndPolicyRecovery(t *testing.T) {
	for _, runtimeFailure := range []bool{false, true} {
		t.Run("runtime_failure="+strconv.FormatBool(runtimeFailure), func(t *testing.T) {
			ctx := context.Background()
			c, store, _, root := newTestController(t)
			lib, series, readable := seedBookFixture(t, store, root, "old", "series", "present.cbz", 1)
			seedBookArchivePages(t, c, readable, png1x1)
			archive, err := os.ReadFile(readable.Path)
			if err != nil {
				t.Fatal(err)
			}
			missing, err := store.CreateBook(ctx, database.CreateBookParams{LibraryID: lib.ID, SeriesID: series.ID, Name: "missing.cbz", Path: filepath.Join(series.Path, "missing.cbz"), Size: 1024, FileModifiedAt: time.Now(), PageCount: 1})
			if err != nil {
				t.Fatal(err)
			}
			missingSeries, err := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib.ID, Name: "absent-series", Path: filepath.Join(lib.Path, "absent-series")})
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range []int64{101, 202} {
				if _, err = store.DB().Exec(`INSERT INTO users(id,username,role) VALUES(?,?,'regular')`, id, strconv.FormatInt(id, 10)); err != nil {
					t.Fatal(err)
				}
				if err = store.SetUserBookProgress(ctx, id, missing.ID, id, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = store.DB().Exec(`INSERT INTO collections(id,name) VALUES(100,'keep'); INSERT INTO collection_series(collection_id,series_id) VALUES(100,?)`, missingSeries.ID); err != nil {
				t.Fatal(err)
			}
			if _, err = store.DB().Exec(`UPDATE books SET quick_hash='stable-quick',path_fingerprint='stable-relative' WHERE id=?`, missing.ID); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(root, "new")
			if err = os.Rename(lib.Path, target); err != nil {
				t.Fatal(err)
			}
			// Reproduce an earlier ordinary root edit without rewriting child rows.
			if _, err = store.DB().Exec(`UPDATE libraries SET path=?,scan_mode='interval',scan_interval=17,scan_formats='cbz' WHERE id=?`, target, lib.ID); err != nil {
				t.Fatal(err)
			}
			cfg := c.currentConfig()
			cfg.Library.StoragePolicies = []config.LibraryStoragePolicy{
				{Path: root, StorageProfile: "ssd"},
				{Path: lib.Path, StorageProfile: "hdd_external"},
				{Path: series.Path, StorageProfile: "custom", IOPolicy: config.StorageIOPolicy{ArchiveOpenConcurrency: 3}},
			}
			if err = c.persistConfig(&cfg); err != nil {
				t.Fatal(err)
			}
			hintsRec := httptest.NewRecorder()
			c.relocationSourceRoots(hintsRec, requestWithRouteParam(http.MethodGet, "/sources", nil, "libraryId", strconv.FormatInt(lib.ID, 10)))
			var hints RelocationSourceRoots
			if err = json.Unmarshal(hintsRec.Body.Bytes(), &hints); err != nil || hintsRec.Code != http.StatusOK || !hints.RecordsOutsideCurrentRoot {
				t.Fatalf("legacy repair detection: %d %s %v", hintsRec.Code, hintsRec.Body.String(), err)
			}
			plan := startRepairPreview(t, c, lib.ID, lib.Path, target)
			if plan.LibraryRoot != target || plan.SourceRoot != lib.Path || plan.TargetRoot != target || plan.BlockerCount != 0 || plan.AnomalyCount != 1 || plan.PolicyCount != 2 {
				t.Fatalf("combined preview: %+v", plan)
			}
			cfg = c.currentConfig()
			cfg.Tasks.RunSlots = 7
			if err = c.persistConfig(&cfg); err != nil {
				t.Fatal(err)
			}
			if runtimeFailure {
				c.relocation.ApplyConfig = func(*config.Config) error { return errors.New("runtime unavailable") }
			}
			op := acceptRelocationAnomalies(t, c, plan.ID)
			wantStatus := "completed"
			if runtimeFailure {
				wantStatus = "paths_committed"
			}
			if op.Status != wantStatus || !op.Protected {
				t.Fatalf("combined commit: %+v", op)
			}
			readPage := func(current *Controller, want int) {
				t.Helper()
				rec := httptest.NewRecorder()
				current.servePageImage(rec, requestWithRouteParams(http.MethodGet, "/page", nil, map[string]string{"bookId": strconv.FormatInt(readable.ID, 10), "pageNumber": "1"}))
				if rec.Code != want || want == http.StatusOK && !bytes.Equal(rec.Body.Bytes(), png1x1) {
					t.Fatalf("combined page: %d %s", rec.Code, rec.Body.String())
				}
			}
			if runtimeFailure {
				readPage(c, http.StatusServiceUnavailable)
			} else {
				readPage(c, http.StatusOK)
			}
			c.Close()
			if _, err = store.DB().Exec(`DELETE FROM runs`); err != nil {
				t.Fatal(err)
			}
			if err = store.Close(); err != nil {
				t.Fatal(err)
			}
			freshStore, err := database.NewStore(filepath.Join(root, "test.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = freshStore.Close() })
			disk, err := config.LoadConfigFile(c.configPath)
			if err != nil {
				t.Fatal(err)
			}
			manager := config.NewManager(disk)
			fresh := newControllerCore(freshStore, scanner.NewScanner(freshStore, manager), manager, c.configPath, defaultControllerCacheSizes())
			t.Cleanup(fresh.Close)
			op = awaitRelocationOperation(t, fresh, op.ID)
			if op.Status != "completed" || !op.Protected {
				t.Fatalf("fresh combined recovery: %+v", op)
			}
			readPage(fresh, http.StatusOK)
			// A separately constructed Scanner must honor durable protection too.
			if err = scanner.NewScanner(freshStore, manager).CleanupLibrary(ctx, lib.ID); !errors.Is(err, database.ErrLibraryProtected) {
				t.Fatalf("direct cleanup: %v", err)
			}
			gotLibrary, err := freshStore.GetLibrary(ctx, lib.ID)
			if err != nil || gotLibrary.Path != target || gotLibrary.ScanMode != "none" {
				t.Fatalf("protected library: %+v %v", gotLibrary, err)
			}
			gotBook, err := freshStore.GetBook(ctx, missing.ID)
			if err != nil || gotBook.SeriesID != series.ID || gotBook.Path != filepath.Join(target, "series", "missing.cbz") || !gotBook.QuickHash.Valid || gotBook.QuickHash.String != "stable-quick" || !gotBook.PathFingerprint.Valid || gotBook.PathFingerprint.String != "stable-relative" {
				t.Fatalf("protected book: %+v %v", gotBook, err)
			}
			gotSeries, err := freshStore.GetSeries(ctx, missingSeries.ID)
			if err != nil || gotSeries.Path != filepath.Join(target, "absent-series") {
				t.Fatalf("protected series: %+v %v", gotSeries, err)
			}
			members, err := freshStore.ListCollectionSeriesIDs(ctx, 100)
			if err != nil || len(members) != 1 || members[0] != missingSeries.ID {
				t.Fatalf("collection membership: %v %v", members, err)
			}
			for _, id := range []int64{101, 202} {
				progress, found, err := freshStore.GetUserBookProgress(ctx, id, missing.ID)
				if err != nil || !found || progress.LastReadPage.Int64 != id {
					t.Fatalf("independent progress: %+v %v", progress, err)
				}
			}
			protection, err := freshStore.LibraryRelocationProtection(ctx, lib.ID)
			if err != nil || protection == nil || protection.ScanMode != "interval" || protection.ScanInterval != 17 || protection.ScanFormats != "cbz" {
				t.Fatalf("original automatic settings: %+v %v", protection, err)
			}
			disk, err = config.LoadConfigFile(c.configPath)
			if err != nil {
				t.Fatal(err)
			}
			for _, snapshot := range []config.Config{*disk, fresh.currentConfig()} {
				if snapshot.Tasks.RunSlots != 7 || len(snapshot.Library.StoragePolicies) != 3 || config.ResolveStoragePolicy(snapshot, lib.Path).StorageProfile != "ssd" || config.ResolveStoragePolicy(snapshot, target).StorageProfile != "hdd_external" || config.ResolveStoragePolicy(snapshot, filepath.Join(target, "series")).IOPolicy.ArchiveOpenConcurrency != 3 {
					t.Fatalf("combined persisted/effective settings: %+v", snapshot.Library.StoragePolicies)
				}
			}
			after, err := os.ReadFile(filepath.Join(target, "series", "present.cbz"))
			if err != nil || !bytes.Equal(archive, after) {
				t.Fatalf("archive bytes changed: %v", err)
			}
			if _, err = os.Stat(lib.Path); !os.IsNotExist(err) {
				t.Fatalf("old source unexpectedly recreated: %v", err)
			}
		})
	}
}
