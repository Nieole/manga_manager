package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"manga-manager/internal/database"
	"manga-manager/internal/parser"
)

func TestComicInfoCommittedFileSurvivesFactStoreFailure(t *testing.T) {
	c, store, _, root := newTestController(t)
	_, series, book := seedBookFixture(t, store, root, "Publication", "Series", "book.cbz", 1)
	if err := writeTestCBZ(book.Path, map[string][]byte{"001.png": png1x1}); err != nil {
		t.Fatal(err)
	}
	db := store.(*database.SqlStore).DB()
	if _, err := db.Exec(`CREATE TRIGGER fail_content_facts BEFORE UPDATE OF size ON books BEGIN SELECT RAISE(ABORT,'fact store failure'); END`); err != nil {
		t.Fatal(err)
	}
	c.taskEngine.runBackground = func(fn func()) { fn() }
	launched, err := c.diskJobs.WriteSeriesComicInfo(context.Background(), series.ID)
	if err != nil {
		t.Fatal(err)
	}
	run, err := c.runtime.Tasks.LoadRun(context.Background(), launched.Run.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.MessageParams["written"] != "1" || run.MessageParams["failed"] != "0" || run.MessageParams["warnings"] != "1" {
		t.Fatalf("committed result: %+v", run)
	}
	if run.MessageParams["committed_book_ids"] != strconv.FormatInt(book.ID, 10) {
		t.Fatalf("lost committed scope: %+v", run.MessageParams)
	}
	events, err := c.runtime.Tasks.ListRunEvents(context.Background(), run.ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range events {
		if event.Code == "comicinfo_facts_after_commit" && strings.Contains(event.Detail, "file committed; refresh facts before retrying") {
			found = true
		}
	}
	if !found {
		t.Fatalf("committed warning missing from actual run events: %+v", events)
	}
	arc, err := parser.OpenArchive(book.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer arc.Close()
	if _, err := arc.ReadMetadataFile("ComicInfo.xml"); err != nil {
		t.Fatal(err)
	}
}

func TestPublicationWindowRejectsNewHTTPDownloads(t *testing.T) {
	c, store, _, root := newTestController(t)
	_, _, book := seedBookFixture(t, store, root, "Publication", "Series", "book.cbz", 1)
	if err := writeTestCBZ(book.Path, map[string][]byte{"001.png": png1x1}); err != nil {
		t.Fatal(err)
	}
	release, err := parser.AcquireArchivePublication(context.Background(), book.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	id := strconv.FormatInt(book.ID, 10)
	rec := httptest.NewRecorder()
	c.media.serveBookFile(rec, requestWithRouteParam(http.MethodGet, "/file", nil, "bookId", id))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("publication download: %d %s", rec.Code, rec.Body.String())
	}
}
