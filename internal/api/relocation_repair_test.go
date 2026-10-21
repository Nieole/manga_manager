package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"manga-manager/internal/database"
)

func TestRelocationRepairEditedRootRequiresExplicitSourceAndPreservesReading(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, series, book := seedBookFixture(t, store, root, "old", "deep/series", "book.cbz", 1)
	seedBookArchivePages(t, c, book, png1x1)
	ctx := context.Background()
	for _, userID := range []int64{101, 202} {
		if _, err := store.DB().Exec(`INSERT INTO users(id,username,role,display_name) VALUES (?,?,'regular','Reader')`, userID, strconv.FormatInt(userID, 10)); err != nil {
			t.Fatal(err)
		}
		if err := store.SetUserBookProgress(ctx, userID, book.ID, userID, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	target := filepath.Join(root, "new")
	if err := os.Rename(lib.Path, target); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`UPDATE libraries SET path=? WHERE id=?`, target, lib.ID); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	c.relocationSourceRoots(rec, requestWithRouteParam(http.MethodGet, "/api/libraries/1/relocation-source-roots", nil, "libraryId", strconv.FormatInt(lib.ID, 10)))
	var hints RelocationSourceRoots
	if err := json.Unmarshal(rec.Body.Bytes(), &hints); err != nil || rec.Code != http.StatusOK || !hints.RecordsOutsideCurrentRoot {
		t.Fatalf("hints: %d %s %v", rec.Code, rec.Body.String(), err)
	}
	if len(hints.Candidates) == 0 || len(hints.Candidates) > 6 {
		t.Fatalf("bounded candidates: %+v", hints)
	}
	defaultPlan := startRelocationPreview(t, c, lib.ID, target)
	if defaultPlan.BlockerCount == 0 {
		t.Fatal("current root must not silently replace historical source")
	}
	plan := startRepairPreview(t, c, lib.ID, lib.Path, target)
	if plan.BlockerCount != 0 || plan.AnomalyCount != 0 || plan.LibraryRoot != target || plan.SourceRoot != lib.Path {
		t.Fatalf("repair preview: %+v", plan)
	}
	op := submitRelocation(t, c, plan.ID)
	op = awaitRelocationOperation(t, c, op.ID)
	if op.Status != "completed" {
		t.Fatalf("repair: %+v", op)
	}
	again := submitRelocation(t, c, plan.ID)
	if again.ID != op.ID {
		t.Fatal("duplicate repair created another operation")
	}
	gotSeries, _ := store.GetSeries(ctx, series.ID)
	gotBook, _ := store.GetBook(ctx, book.ID)
	if gotSeries.Path != filepath.Join(target, "deep/series") || gotBook.Path != filepath.Join(target, "deep/series/book.cbz") {
		t.Fatalf("relative paths lost: %s %s", gotSeries.Path, gotBook.Path)
	}
	for _, userID := range []int64{101, 202} {
		progress, found, err := store.GetUserBookProgress(ctx, userID, book.ID)
		if err != nil || !found || progress.LastReadPage.Int64 != userID {
			t.Fatalf("progress: %+v %v", progress, err)
		}
	}
	page := httptest.NewRecorder()
	c.servePageImage(page, requestWithRouteParams(http.MethodGet, "/api/books/page/1/1", nil, map[string]string{"bookId": strconv.FormatInt(book.ID, 10), "pageNumber": "1"}))
	if page.Code != http.StatusOK || !bytes.Equal(page.Body.Bytes(), png1x1) {
		t.Fatalf("repaired page: %d %s", page.Code, page.Body.String())
	}
}

func TestRelocationRepairLooseRootBooksAndVirtualSeries(t *testing.T) {
	for _, virtual := range []bool{false, true} {
		t.Run(strconv.FormatBool(virtual), func(t *testing.T) {
			c, store, _, root := newTestController(t)
			lib, series, book := seedBookFixture(t, store, root, "old", "series", "book.cbz", 1)
			seedBookArchivePages(t, c, book, png1x1)
			loose := filepath.Join(lib.Path, "book.cbz")
			if err := os.Rename(book.Path, loose); err != nil {
				t.Fatal(err)
			}
			seriesPath := lib.Path
			if virtual {
				seriesPath = filepath.Join(lib.Path, "virtual-loose-book")
			}
			if _, err := store.DB().Exec(`UPDATE series SET path=? WHERE id=?`, seriesPath, series.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := store.DB().Exec(`UPDATE books SET path=? WHERE id=?`, loose, book.ID); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(root, "new")
			if err := os.Rename(lib.Path, target); err != nil {
				t.Fatal(err)
			}
			if _, err := store.DB().Exec(`UPDATE libraries SET path=? WHERE id=?`, target, lib.ID); err != nil {
				t.Fatal(err)
			}
			plan := startRepairPreview(t, c, lib.ID, lib.Path, target)
			if plan.BlockerCount != 0 || plan.AnomalyCount != 0 {
				t.Fatalf("loose/virtual mapping: %+v", plan)
			}
			op := submitRelocation(t, c, plan.ID)
			if got := awaitRelocationOperation(t, c, op.ID); got.Status != "completed" {
				t.Fatalf("operation: %+v", got)
			}
			got, _ := store.GetBook(context.Background(), book.ID)
			if got.Path != filepath.Join(target, "book.cbz") {
				t.Fatalf("loose book path: %s", got.Path)
			}
		})
	}
}

func TestRelocationRepairMixedAndPrefixLookalikeRecordsRemainUntouched(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		t.Run(strconv.FormatBool(mixed), func(t *testing.T) {
			c, store, _, root := newTestController(t)
			lib, series, book := seedBookFixture(t, store, root, "old", "series", "book.cbz", 1)
			seedBookArchivePages(t, c, book, png1x1)
			target := filepath.Join(root, "new")
			if err := os.Rename(lib.Path, target); err != nil {
				t.Fatal(err)
			}
			if _, err := store.DB().Exec(`UPDATE libraries SET path=? WHERE id=?`, target, lib.ID); err != nil {
				t.Fatal(err)
			}
			extraPath := lib.Path + "-other"
			if mixed {
				extraPath = filepath.Join(target, "already-new")
			}
			extra, err := store.CreateSeries(context.Background(), database.CreateSeriesParams{LibraryID: lib.ID, Name: "mixed", Path: extraPath, NameInitial: "M"})
			if err != nil {
				t.Fatal(err)
			}
			beforeConfig := c.config.Snapshot()
			plan := startRepairPreview(t, c, lib.ID, lib.Path, target)
			if plan.BlockerCount == 0 {
				t.Fatal("incomplete source explanation accepted")
			}
			details := previewDetails(t, c, plan.ID, "kind=blocker")
			if len(details.Items) == 0 || details.Items[0].Reason != "outside_source_root" {
				t.Fatalf("specific blocker: %+v", details)
			}
			rec := httptest.NewRecorder()
			c.commitRelocationPlan(rec, requestWithRouteParam(http.MethodPost, "/api/relocation-plans/1/commit", []byte(`{}`), "planId", strconv.FormatInt(plan.ID, 10)))
			if rec.Code != http.StatusConflict {
				t.Fatalf("blocked confirmation: %d %s", rec.Code, rec.Body.String())
			}
			gotLibrary, _ := store.GetLibrary(context.Background(), lib.ID)
			gotSeries, _ := store.GetSeries(context.Background(), series.ID)
			gotBook, _ := store.GetBook(context.Background(), book.ID)
			gotExtra, _ := store.GetSeries(context.Background(), extra.ID)
			if gotLibrary.Path != target || gotSeries.Path != series.Path || gotBook.Path != book.Path || gotExtra.Path != extraPath {
				t.Fatal("blocked repair partially updated records")
			}
			before, _ := json.Marshal(beforeConfig)
			after, _ := json.Marshal(c.config.Snapshot())
			if !bytes.Equal(before, after) {
				t.Fatal("blocked repair changed configuration")
			}
		})
	}
}

func startRepairPreview(t *testing.T, c *Controller, libraryID int64, source, target string) RelocationPlan {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"source_root": source, "target_root": target})
	rec := httptest.NewRecorder()
	c.createRelocationPlan(rec, requestWithRouteParam(http.MethodPost, "/api/libraries/1/relocation-plans", body, "libraryId", strconv.FormatInt(libraryID, 10)))
	var plan RelocationPlan
	if err := json.Unmarshal(rec.Body.Bytes(), &plan); err != nil || rec.Code != http.StatusAccepted {
		t.Fatalf("repair preview: %d %s %v", rec.Code, rec.Body.String(), err)
	}
	return awaitRelocationPlan(t, c, plan.ID)
}
