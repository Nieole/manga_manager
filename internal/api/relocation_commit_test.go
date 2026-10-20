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

	"manga-manager/internal/parser"
)

func TestRelocationCommitPreservesIdentityAndReadsNewLocation(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, series, book := seedBookFixture(t, store, root, "旧盘", "漫画 空格", "第一卷.cbz", 1)
	seedBookArchivePages(t, c, book, png1x1)
	read := func() {
		t.Helper()
		rec := httptest.NewRecorder()
		c.servePageImage(rec, requestWithRouteParams(http.MethodGet, "/api/books/page/1/1", nil, map[string]string{"bookId": strconv.FormatInt(book.ID, 10), "pageNumber": "1"}))
		if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), png1x1) {
			t.Fatalf("read same book: %d %s", rec.Code, rec.Body.String())
		}
	}
	read()
	parser.EvictArchiveFromPool(book.Path)
	target := filepath.Join(root, "目标盘")
	if err := os.Rename(lib.Path, target); err != nil {
		t.Fatal(err)
	}
	plan := startRelocationPreview(t, c, lib.ID, target)
	if plan.BlockerCount != 0 || plan.AnomalyCount != 0 {
		t.Fatalf("preview: %+v", plan)
	}
	rec := httptest.NewRecorder()
	c.commitRelocationPlan(rec, requestWithRouteParam(http.MethodPost, "/api/relocation-plans/1/commit", []byte(`{}`), "planId", strconv.FormatInt(plan.ID, 10)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("commit: %d %s", rec.Code, rec.Body.String())
	}
	var result RelocationOperation
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(10 * time.Second)
	for result.Status != "completed" {
		select {
		case <-deadline:
			t.Fatalf("operation did not finish: %+v", result)
		default:
		}
		rec = httptest.NewRecorder()
		c.getRelocationOperation(rec, requestWithRouteParam(http.MethodGet, "/api/relocation-operations/1", nil, "operationId", strconv.FormatInt(result.ID, 10)))
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("get operation: %d %s %v", rec.Code, rec.Body.String(), err)
		}
		time.Sleep(time.Millisecond)
	}
	gotLib, _ := store.GetLibrary(context.Background(), lib.ID)
	gotSeries, _ := store.GetSeries(context.Background(), series.ID)
	gotBook, _ := store.GetBook(context.Background(), book.ID)
	if gotLib.Path != target || gotSeries.Path != filepath.Join(target, "漫画 空格") || gotBook.Path != filepath.Join(target, "漫画 空格", "第一卷.cbz") {
		t.Fatalf("paths: %s %s %s", gotLib.Path, gotSeries.Path, gotBook.Path)
	}
	if gotBook.Title != book.Title || gotSeries.Name != series.Name || result.PlanID != plan.ID || result.RunID == 0 {
		t.Fatalf("preserved identity/result: %+v %+v", gotBook, result)
	}
	read()
}
