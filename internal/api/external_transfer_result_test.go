package api

import (
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

func TestExternalTransferHTTPAcceptedRunPreservesPartialCommitAndCoverage(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, series, missing := seedBookFixture(t, store, root, "Transfer", "Series", "first.cbz", 1)
	source := filepath.Join(series.Path, "second.cbz")
	if err := os.WriteFile(source, []byte("second book"), 0644); err != nil {
		t.Fatal(err)
	}
	book, err := store.CreateBook(context.Background(), database.CreateBookParams{LibraryID: lib.ID, SeriesID: series.ID, Name: "second.cbz", Path: source, Size: 11, PageCount: 1, FileModifiedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(missing.Path); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	c.taskEngine.runBackground = func(fn func()) { fn() }
	session, err := c.external.CreateSession(context.Background(), lib.ID, t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.external.ScanSession(context.Background(), session.SessionID, discardScanHandle{}); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{"series_ids": []int64{series.ID}})
	if err != nil {
		t.Fatal(err)
	}
	params := map[string]string{"libraryId": strconv.FormatInt(lib.ID, 10), "sessionId": session.SessionID}
	rec := httptest.NewRecorder()
	c.transferToExternalLibrary(rec, requestWithRouteParams(http.MethodPost, "/transfer", body, params))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("launch=%d %s", rec.Code, rec.Body.String())
	}
	run := currentTask(t, c.taskEngine, externalTransferKey(lib.ID, session.SessionID))
	if run.Status != "failed" || run.MessageParams["success"] != "1" || run.MessageParams["failed"] != "1" || run.MessageParams["committed_book_ids"] != strconv.FormatInt(book.ID, 10) || run.Current != 1 {
		t.Fatalf("actual run=%+v", run)
	}
	rec = httptest.NewRecorder()
	c.getExternalLibrarySession(rec, requestWithRouteParams(http.MethodGet, "/session", nil, params))
	var coverage struct {
		MatchedBooks int `json:"matched_books"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &coverage); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || coverage.MatchedBooks != 1 {
		t.Fatalf("projection=%d %s", rec.Code, rec.Body.String())
	}
}
