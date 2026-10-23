package api

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strconv"
	"testing"

	"manga-manager/internal/metadata"
	"manga-manager/internal/proposal"
)

func TestSeriesMetadataSourcesComposeWithoutChangingIdentity(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, series, book := seedBookFixture(t, store, root, "Sources", "Original", "01.cbz", 0)
	ctx := t.Context()
	for _, seed := range []struct {
		query string
		args  []any
	}{
		{`UPDATE books SET cover_path='chosen-cover', cover_locked=1, last_read_page=7 WHERE id=?`, []any{book.ID}},
		{`INSERT INTO users(id,username) VALUES(77,'reader')`, nil},
		{`INSERT INTO user_book_progress(user_id,book_id,last_read_page) VALUES(77,?,7)`, []any{book.ID}},
		{`INSERT INTO series_custom_fields VALUES(?,'edition','kept')`, []any{series.ID}},
		{`INSERT INTO series_relations(source_series_id,target_series_id) VALUES(?,?)`, []any{series.ID, series.ID}},
	} {
		if _, err := store.DB().ExecContext(ctx, seed.query, seed.args...); err != nil {
			t.Fatal(err)
		}
	}
	assertContent := func(title, summary, status string, tags, authors []string) {
		t.Helper()
		saved, err := store.GetSeries(ctx, series.ID)
		if err != nil {
			t.Fatal(err)
		}
		gotTags, err := store.GetTagsForSeries(ctx, series.ID)
		if err != nil {
			t.Fatal(err)
		}
		gotAuthors, err := store.GetAuthorsForSeries(ctx, series.ID)
		if err != nil {
			t.Fatal(err)
		}
		var names, authorNames []string
		for _, tag := range gotTags {
			names = append(names, tag.Name)
		}
		for _, author := range gotAuthors {
			authorNames = append(authorNames, author.Name)
		}
		sort.Strings(names)
		sort.Strings(authorNames)
		if saved.Title.String != title || !reflect.DeepEqual(names, tags) || !reflect.DeepEqual(authorNames, authors) {
			t.Fatalf("content: title=%q tags=%v authors=%v", saved.Title.String, names, authorNames)
		}
		if saved.Summary.String != summary || saved.Status.String != status || saved.Publisher.Valid || saved.Language.Valid || saved.Rating.Valid || saved.LockedFields.String != "" {
			t.Fatalf("scalar compatibility: %+v", saved)
		}
		if (title == "Manual" && saved.NameInitial != "M") || (title == "Proposal" && saved.NameInitial != "P") {
			t.Fatalf("title initial: %q", saved.NameInitial)
		}
		if saved.ID != series.ID || saved.LibraryID != lib.ID || saved.Path != series.Path || saved.Name != series.Name {
			t.Fatalf("series identity changed: %+v", saved)
		}
	}
	version := seriesMetadataVersionOf(t, c, series.ID)
	assertChanged := func() {
		t.Helper()
		next := seriesMetadataVersionOf(t, c, series.ID)
		if next == version {
			t.Fatal("source changed content without invalidating edit fingerprint")
		}
		if code, _ := putSeriesInfo(t, c, series.ID, `{"title":"stale","expected_version":"`+version+`"}`); code != http.StatusConflict {
			t.Fatalf("stale save: %d", code)
		}
		version = next
	}
	if code, _ := putSeriesInfo(t, c, series.ID, `{"title":"Manual","summary":"Manual summary","tags":["Manual","Shared"],"authors":[{"name":"Manual Author","role":"story"}],"links":[],"locked_fields":""}`); code != 200 {
		t.Fatalf("manual: %d", code)
	}
	assertContent("Manual", "Manual summary", "", []string{"Manual", "Shared"}, []string{"Manual Author"})
	assertChanged()
	rec := httptest.NewRecorder()
	c.bulkEditSeries(rec, httptest.NewRequest(http.MethodPost, "/api/series/bulk-edit", bytes.NewBufferString(fmt.Sprintf(`{"series_ids":[%d],"add_tags":["Bulk"],"remove_tags":["Shared"],"status":"completed"}`, series.ID))))
	if rec.Code != 200 {
		t.Fatalf("bulk: %d %s", rec.Code, rec.Body.String())
	}
	assertContent("Manual", "Manual summary", "completed", []string{"Bulk", "Manual"}, []string{"Manual Author"})
	assertChanged()
	if err := writeTestCBZ(book.Path, map[string][]byte{"ComicInfo.xml": []byte(`<ComicInfo><Summary>Archive summary</Summary><Genre>Scan</Genre><Writer>Scan Author</Writer></ComicInfo>`)}); err != nil {
		t.Fatal(err)
	}
	if err := c.scanner.ScanLibrary(ctx, lib.ID, lib.Path, true, nil); err != nil {
		t.Fatal(err)
	}
	assertContent("Manual", "Archive summary", "", []string{"Bulk", "Manual", "Scan"}, []string{"Manual Author", "Scan Author"})
	assertChanged()
	before, err := store.GetSeriesMetadataProvenance(ctx, series.ID)
	if err != nil || len(before) != 0 {
		t.Fatalf("non-proposal sources created provenance: %+v %v", before, err)
	}
	current, err := store.GetSeries(ctx, series.ID)
	if err != nil {
		t.Fatal(err)
	}
	queued, err := c.proposals.Queue(ctx, current, &metadata.SeriesMetadata{Title: "Proposal", Summary: "Proposed summary", Tags: []string{"Proposal"}, Authors: []metadata.SeriesAuthor{{Name: "Proposal Author", Role: "story"}}, SourceID: 7, SourceURL: "https://bgm.tv/subject/7", Confidence: 0.8}, "bangumi", "Original", proposal.QueueOptions{})
	if err != nil || queued.Status != proposal.QueueQueued {
		t.Fatalf("queue: %+v %v", queued, err)
	}
	result, err := c.proposals.Apply(ctx, queued.Proposal.ID, proposal.ApplyModeAll)
	if err != nil || result.Status != proposal.ApplyApplied {
		t.Fatalf("apply: %+v %v", result, err)
	}
	assertContent("Proposal", "Proposed summary", "", []string{"Proposal"}, []string{"Proposal Author"})
	assertChanged()
	provenance, err := store.GetSeriesMetadataProvenance(ctx, series.ID)
	if err != nil || len(provenance) == 0 {
		t.Fatalf("missing provenance: %v", err)
	}
	for _, row := range provenance {
		if row.Source != "bangumi" || row.SourceUrl != "https://bgm.tv/subject/7" || row.ReviewID.Int64 != queued.Proposal.ID || row.Confidence != 0.8 {
			t.Fatalf("provenance: %+v", row)
		}
	}
	savedBook, err := store.GetBook(ctx, book.ID)
	if err != nil {
		t.Fatal(err)
	}
	if savedBook.ID != book.ID || savedBook.SeriesID != series.ID || savedBook.LibraryID != lib.ID || savedBook.Path != book.Path || savedBook.CoverPath.String != "chosen-cover" || savedBook.LastReadPage.Int64 != 7 {
		t.Fatalf("book identity/reading/cover changed: %+v", savedBook)
	}
	var progress, relations int
	var custom string
	if err := store.DB().QueryRow(`SELECT last_read_page FROM user_book_progress WHERE user_id=77 AND book_id=?`, book.ID).Scan(&progress); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRow(`SELECT field_value FROM series_custom_fields WHERE series_id=? AND field_key='edition'`, series.ID).Scan(&custom); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRow(`SELECT count(*) FROM series_relations WHERE source_series_id=? AND target_series_id=?`, series.ID, series.ID).Scan(&relations); err != nil {
		t.Fatal(err)
	}
	if progress != 7 || custom != "kept" || relations != 1 {
		t.Fatalf("unrelated data: progress=%d custom=%q relations=%d", progress, custom, relations)
	}
	var cached string
	var matches, tagCount int
	if err := store.DB().QueryRow(`SELECT tag_names_cache FROM series_stats WHERE series_id=?`, series.ID).Scan(&cached); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRow(`SELECT count(*) FROM series_search_fts WHERE series_search_fts MATCH 'Proposal'`).Scan(&matches); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRow(`SELECT series_count FROM tags WHERE name='Proposal'`).Scan(&tagCount); err != nil {
		t.Fatal(err)
	}
	if cached != "Proposal" || matches != 1 || tagCount != 1 {
		t.Fatalf("derived: cache=%q search=%d tag count=%d", cached, matches, tagCount)
	}
}

func TestGlobalTagMaintenanceInvalidatesMetadataFingerprint(t *testing.T) {
	c, store, _, root := newTestController(t)
	_, series, _ := seedBookFixture(t, store, root, "Tags", "Series", "01.cbz", 1)
	if code, _ := putSeriesInfo(t, c, series.ID, `{"title":"Series","tags":["Source","Target"]}`); code != 200 {
		t.Fatal(code)
	}
	tags, err := store.GetTagsForSeries(t.Context(), series.ID)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]int64{}
	for _, tag := range tags {
		ids[tag.Name] = tag.ID
	}
	for _, step := range []struct {
		handler http.HandlerFunc
		id      int64
		body    string
	}{
		{c.renameTag, ids["Source"], `{"name":"Renamed"}`},
		{c.mergeTag, ids["Source"], fmt.Sprintf(`{"target_id":%d}`, ids["Target"])},
		{c.deleteTag, ids["Target"], ""},
	} {
		before := seriesMetadataVersionOf(t, c, series.ID)
		rec := httptest.NewRecorder()
		step.handler(rec, requestWithRouteParam(http.MethodPost, "/tags", []byte(step.body), "tagId", strconv.FormatInt(step.id, 10)))
		if rec.Code != 200 {
			t.Fatalf("tag maintenance: %d %s", rec.Code, rec.Body.String())
		}
		if seriesMetadataVersionOf(t, c, series.ID) == before {
			t.Fatal("tag maintenance did not invalidate fingerprint")
		}
	}
	tags, err = store.GetTagsForSeries(t.Context(), series.ID)
	if err != nil || len(tags) != 0 {
		t.Fatalf("tags after deletion: %+v %v", tags, err)
	}
}

func TestMetadataFingerprintIgnoresCollectionStorageOrder(t *testing.T) {
	c, _, _, root := newTestController(t)
	_, series, _ := seedBookFixture(t, c.store, root, "Order", "Series", "01.cbz", 1)
	for i, payload := range []string{
		`{"title":"Series","locked_fields":"title,tags","tags":["Alpha","Beta"],"authors":[{"name":"A","role":"story"},{"name":"B","role":"art"}],"links":[{"name":"A","url":"https://a.example"},{"name":"B","url":"https://b.example"}]}`,
		`{"title":"Series","locked_fields":"tags,title","tags":["Beta","Alpha"],"authors":[{"name":"B","role":"art"},{"name":"A","role":"story"}],"links":[{"name":"B","url":"https://b.example"},{"name":"A","url":"https://a.example"}]}`,
	} {
		before := seriesMetadataVersionOf(t, c, series.ID)
		if code, _ := putSeriesInfo(t, c, series.ID, payload); code != http.StatusOK {
			t.Fatalf("save: %d", code)
		}
		if i == 1 && seriesMetadataVersionOf(t, c, series.ID) != before {
			t.Fatal("reordering stored collections changed the content fingerprint")
		}
	}
}

func TestManualCompetingSavesCommitExactlyOneCompleteEdit(t *testing.T) {
	c, store, _, root := newTestController(t)
	_, series, _ := seedBookFixture(t, store, root, "Competing", "Series", "01.cbz", 1)
	version := seriesMetadataVersionOf(t, c, series.ID)
	start := make(chan struct{})
	type outcome struct {
		name string
		code int
	}
	results := make(chan outcome, 2)
	for _, name := range []string{"Alice", "Bob"} {
		go func() {
			<-start
			payload := fmt.Sprintf(`{"title":%q,"summary":%q,"tags":[%q],"authors":[{"name":%q,"role":"story"}],"links":[{"name":%q,"url":"https://example.com"}],"expected_version":%q}`, name, name, name, name, name, version)
			code, _ := putSeriesInfo(t, c, series.ID, payload)
			results <- outcome{name, code}
		}()
	}
	close(start)
	first, second := <-results, <-results
	if first.code == http.StatusConflict {
		first, second = second, first
	}
	if first.code != http.StatusOK || second.code != http.StatusConflict {
		t.Fatalf("competing saves: %+v %+v", first, second)
	}
	saved, err := store.GetSeries(t.Context(), series.ID)
	if err != nil {
		t.Fatal(err)
	}
	tags, err := store.GetTagsForSeries(t.Context(), series.ID)
	if err != nil {
		t.Fatal(err)
	}
	authors, err := store.GetAuthorsForSeries(t.Context(), series.ID)
	if err != nil {
		t.Fatal(err)
	}
	links, err := store.GetLinksForSeries(t.Context(), series.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Title.String != first.name || saved.Summary.String != first.name || len(tags) != 1 || tags[0].Name != first.name || len(authors) != 1 || authors[0].Name != first.name || len(links) != 1 || links[0].Name != first.name {
		t.Fatalf("winner edit was mixed: series=%+v tags=%+v authors=%+v links=%+v", saved, tags, authors, links)
	}
}

func TestManualStatsRefreshFailureRollsBackContentAndAssociations(t *testing.T) {
	c, store, _, root := newTestController(t)
	_, series, _ := seedBookFixture(t, store, root, "Stats", "Series", "01.cbz", 1)
	if code, _ := putSeriesInfo(t, c, series.ID, `{"title":"Original","tags":["Original"],"authors":[{"name":"Original","role":"story"}],"links":[{"name":"Original","url":"https://original.example"}]}`); code != 200 {
		t.Fatal(code)
	}
	version := seriesMetadataVersionOf(t, c, series.ID)
	if _, err := store.DB().Exec(`CREATE TRIGGER reject_manual_stats BEFORE INSERT ON series_stats BEGIN SELECT RAISE(ABORT,'stats rejected'); END`); err != nil {
		t.Fatal(err)
	}
	if code, _ := putSeriesInfo(t, c, series.ID, `{"title":"Changed","tags":["Changed"],"authors":[],"links":[],"expected_version":"`+version+`"}`); code != http.StatusInternalServerError {
		t.Fatalf("stats failure: %d", code)
	}
	if after := seriesMetadataVersionOf(t, c, series.ID); after != version {
		t.Fatal("statistics failure committed metadata changes")
	}
	var cached string
	if err := store.DB().QueryRow(`SELECT tag_names_cache FROM series_stats WHERE series_id=?`, series.ID).Scan(&cached); err != nil {
		t.Fatal(err)
	}
	if cached != "Original" {
		t.Fatalf("statistics cache changed: %q", cached)
	}
}
