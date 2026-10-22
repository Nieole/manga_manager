package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/parser"
)

func TestRelocationCommitPreservesSeededMetadataAndUserAssociations(t *testing.T) {
	assertRelocationPreservesAssociations(t, false)
}

func TestRelocationRepairPreservesSeededMetadataAndUserAssociations(t *testing.T) {
	assertRelocationPreservesAssociations(t, true)
}

func assertRelocationPreservesAssociations(t *testing.T, repair bool) {
	t.Helper()
	c, store, _, root := newTestController(t)
	ctx := context.Background()
	lib, series, book := seedBookFixture(t, store, root, "原盘", "嵌套/系列", "第一卷.cbz", 10)
	pages := make([][]byte, 10)
	for i := range pages {
		pages[i] = png1x1
	}
	seedBookArchivePages(t, c, book, pages...)
	coverPath := filepath.Join(root, "custom-cover.png")
	if err := os.WriteFile(coverPath, png1x1, 0o644); err != nil {
		t.Fatal(err)
	}
	execSeed := func(query string, args ...any) int64 {
		t.Helper()
		result, err := store.DB().ExecContext(ctx, query, args...)
		if err != nil {
			t.Fatalf("seed association: %s: %v", query, err)
		}
		id, err := result.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	execSeed(`UPDATE libraries SET scan_interval=17,scan_formats='cbz',koreader_sync_enabled=1 WHERE id=?`, lib.ID)
	execSeed(`UPDATE series SET title='Preserved title',summary='Curated summary',rating=4.25,locked_fields='title,summary',is_favorite=1,language='zh' WHERE id=?`, series.ID)
	execSeed(`UPDATE books SET title='Locked book title',summary='Book summary',volume='4',number='7',sort_number=7,
 cover_path=?,cover_locked=1,file_hash='content-hash-sentinel',quick_hash='quick-hash-sentinel',
 path_fingerprint='relative-path-sentinel',path_fingerprint_no_ext='relative-noext-sentinel',filename_fingerprint='filename-sentinel' WHERE id=?`, coverPath, book.ID)
	other, err := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib.ID, Name: "续篇", Path: filepath.Join(lib.Path, "虚拟系列"), NameInitial: "X"})
	if err != nil {
		t.Fatal(err)
	}
	tag := execSeed(`INSERT INTO tags(name) VALUES ('迁移保留标签')`)
	execSeed(`INSERT INTO series_tags(series_id,tag_id) VALUES (?,?)`, series.ID, tag)
	author := execSeed(`INSERT INTO authors(name,role) VALUES ('保留作者','writer')`)
	execSeed(`INSERT INTO series_authors(series_id,author_id) VALUES (?,?)`, series.ID, author)
	execSeed(`INSERT INTO series_links(series_id,name,url) VALUES (?,'source','https://example.test/series')`, series.ID)
	execSeed(`INSERT INTO series_custom_fields(series_id,field_key,field_value) VALUES (?,'translator','译者甲')`, series.ID)
	pendingReview := execSeed(`INSERT INTO metadata_reviews(series_id,provider,status,summary,raw_payload) VALUES (?,'manual','pending','Pending proposal','{"title":"Proposed"}')`, series.ID)
	execSeed(`INSERT INTO metadata_review_fields(review_id,field_name,current_value,proposed_value,confidence,source,source_url) VALUES (?,'title','Preserved title','Proposed',0.75,'manual','https://example.test/proposal')`, pendingReview)
	appliedReview := execSeed(`INSERT INTO metadata_reviews(series_id,provider,status,summary) VALUES (?,'manual','applied','Applied source')`, series.ID)
	execSeed(`INSERT INTO series_metadata_provenance(series_id,field_name,value,source,source_url,confidence,review_id) VALUES (?,'summary','Curated summary','manual','https://example.test/applied',0.95,?)`, series.ID, appliedReview)
	groupReview := execSeed(`INSERT INTO ai_grouping_reviews(library_id,provider,status,summary,candidate_count,collection_count) VALUES (?,'manual','applied','Retained grouping',2,1)`, lib.ID)
	collection := execSeed(`INSERT INTO collections(name,description,cover_url,source_type,source_review_id,source_key) VALUES ('稳定合集','Collection description','https://example.test/cover','ai',?,'stable-group')`, groupReview)
	execSeed(`INSERT INTO collection_series(collection_id,series_id,sort_order) VALUES (?,?,3)`, collection, series.ID)
	execSeed(`INSERT INTO ai_grouping_review_collections(review_id,name,series_ids,series_count,status,created_collection_id) VALUES (?,'稳定合集',?,1,'applied',?)`, groupReview, "["+strconv.FormatInt(series.ID, 10)+"]", collection)
	readingList := execSeed(`INSERT INTO reading_lists(name,description) VALUES ('保留阅读列表','Reading list description')`)
	execSeed(`INSERT INTO reading_list_items(reading_list_id,series_id,sort_order,note) VALUES (?,?,2,'Read next')`, readingList, series.ID)
	execSeed(`INSERT INTO series_relations(source_series_id,target_series_id,relation_type) VALUES (?,?,'sequel')`, series.ID, other.ID)
	execSeed(`INSERT INTO reading_activity(book_id,date,pages_read) VALUES (?,'2026-01-02',5)`, book.ID)
	for i, userID := range []int64{101, 202} {
		execSeed(`INSERT INTO users(id,username,role,display_name) VALUES (?,?,'regular',?)`, userID, "reader-"+strconv.Itoa(i), "读者"+strconv.Itoa(i))
		if err = store.SetUserBookProgress(ctx, userID, book.ID, int64(3+i*4), time.Date(2026, 1, 2+i, 3, 4, 5, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		if err = store.AddUserBookReadingTime(ctx, userID, book.ID, int64(41+i*42)); err != nil {
			t.Fatal(err)
		}
		execSeed(`INSERT INTO user_reading_activity(user_id,book_id,date,pages_read,read_seconds) VALUES (?,?,'2026-01-02',?,?)`, userID, book.ID, 3+i*4, 11+i*12)
		execSeed(`INSERT INTO reading_bookmarks(user_id,book_id,page,note) VALUES (?,?,2,?)`, userID, book.ID, "独立书签"+strconv.Itoa(i))
		execSeed(`INSERT INTO user_series_review(user_id,series_id,rating,review) VALUES (?,?,?,?)`, userID, series.ID, 3+i, "独立短评"+strconv.Itoa(i))
	}
	readPage := func() {
		t.Helper()
		rec := httptest.NewRecorder()
		c.servePageImage(rec, requestWithRouteParams(http.MethodGet, "/api/books/page/1/1", nil, map[string]string{"bookId": strconv.FormatInt(book.ID, 10), "pageNumber": "1"}))
		if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), png1x1) {
			t.Fatalf("same book page: %d %s", rec.Code, rec.Body.String())
		}
	}
	readPage()
	beforeLib, err := store.GetLibrary(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	beforeSeries, err := store.GetSeries(ctx, series.ID)
	if err != nil {
		t.Fatal(err)
	}
	beforeBook, err := store.GetBook(ctx, book.ID)
	if err != nil {
		t.Fatal(err)
	}
	archiveBytes, err := os.ReadFile(book.Path)
	if err != nil {
		t.Fatal(err)
	}
	tables := map[string]int{
		"users": 2, "user_book_progress": 2, "user_series_progress": 2,
		"reading_bookmarks": 2, "reading_activity": 1, "user_reading_activity": 4,
		"user_book_reading_time": 2, "user_series_review": 2, "series_stats": 1,
		"tags": 1, "series_tags": 1, "authors": 1, "series_authors": 1,
		"series_links": 1, "series_custom_fields": 1, "metadata_reviews": 2,
		"metadata_review_fields": 1, "series_metadata_provenance": 1,
		"ai_grouping_reviews": 1, "ai_grouping_review_collections": 1,
		"collections": 1, "collection_series": 1, "reading_lists": 1,
		"reading_list_items": 1, "series_relations": 1,
	}
	snapshot := func(table string, minimum int) string {
		t.Helper()
		// Tables are the fixed fixture set above, never request-supplied identifiers.
		rows, queryErr := store.DB().QueryContext(ctx, "SELECT * FROM "+table+" ORDER BY 1,2")
		if queryErr != nil {
			t.Fatal(queryErr)
		}
		defer rows.Close()
		columns, queryErr := rows.Columns()
		if queryErr != nil {
			t.Fatal(queryErr)
		}
		var records [][]any
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for i := range values {
				pointers[i] = &values[i]
			}
			if queryErr = rows.Scan(pointers...); queryErr != nil {
				t.Fatal(queryErr)
			}
			records = append(records, values)
		}
		if queryErr = rows.Err(); queryErr != nil {
			t.Fatal(queryErr)
		}
		if len(records) < minimum {
			t.Fatalf("association fixture %s has %d rows, need at least %d", table, len(records), minimum)
		}
		encoded, queryErr := json.Marshal(records)
		if queryErr != nil {
			t.Fatal(queryErr)
		}
		return string(encoded)
	}
	before := make(map[string]string, len(tables))
	for table, minimum := range tables {
		before[table] = snapshot(table, minimum)
	}
	parser.EvictArchiveFromPool(book.Path)
	target := filepath.Join(root, "目标盘")
	if err = os.Rename(lib.Path, target); err != nil {
		t.Fatal(err)
	}
	var plan RelocationPlan
	if repair {
		if _, err = store.DB().Exec(`UPDATE libraries SET path=? WHERE id=?`, target, lib.ID); err != nil {
			t.Fatal(err)
		}
		plan = startRepairPreview(t, c, lib.ID, lib.Path, target)
	} else {
		plan = startRelocationPreview(t, c, lib.ID, target)
	}
	if plan.Status != "checked" || plan.BlockerCount != 0 || plan.AnomalyCount != 0 {
		t.Fatalf("preview: %+v", plan)
	}
	rec := httptest.NewRecorder()
	c.commitRelocationPlan(rec, requestWithRouteParam(http.MethodPost, "/api/relocation-plans/1/commit", []byte(`{}`), "planId", strconv.FormatInt(plan.ID, 10)))
	var op RelocationOperation
	if err = json.Unmarshal(rec.Body.Bytes(), &op); err != nil || rec.Code != http.StatusAccepted {
		t.Fatalf("confirm: %d %s (%v)", rec.Code, rec.Body.String(), err)
	}
	deadline := time.After(10 * time.Second)
	for op.Status != "completed" {
		if op.Status == "failed" {
			t.Fatalf("migration failed: %+v", op)
		}
		select {
		case <-deadline:
			t.Fatalf("migration did not complete: %+v", op)
		default:
		}
		rec = httptest.NewRecorder()
		c.getRelocationOperation(rec, requestWithRouteParam(http.MethodGet, "/api/relocation-operations/1", nil, "operationId", strconv.FormatInt(op.ID, 10)))
		if err = json.Unmarshal(rec.Body.Bytes(), &op); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("result: %d %s (%v)", rec.Code, rec.Body.String(), err)
		}
		time.Sleep(time.Millisecond)
	}
	afterLib, err := store.GetLibrary(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	afterSeries, err := store.GetSeries(ctx, series.ID)
	if err != nil {
		t.Fatal(err)
	}
	afterBook, err := store.GetBook(ctx, book.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterLib.Path != target || afterSeries.Path != filepath.Join(target, "嵌套", "系列") || afterBook.Path != filepath.Join(target, "嵌套", "系列", "第一卷.cbz") {
		t.Fatalf("relative paths changed: %s %s %s", afterLib.Path, afterSeries.Path, afterBook.Path)
	}
	afterLib.Path, afterSeries.Path, afterBook.Path = beforeLib.Path, beforeSeries.Path, beforeBook.Path
	if !reflect.DeepEqual(beforeLib, afterLib) || !reflect.DeepEqual(beforeSeries, afterSeries) || !reflect.DeepEqual(beforeBook, afterBook) {
		t.Fatalf("migration modified unrelated library, series or book fields\nbefore: %+v %+v %+v\nafter: %+v %+v %+v", beforeLib, beforeSeries, beforeBook, afterLib, afterSeries, afterBook)
	}
	for table, minimum := range tables {
		if got := snapshot(table, minimum); got != before[table] {
			t.Errorf("association data changed in %s\nbefore: %s\nafter: %s", table, before[table], got)
		}
	}
	for i, userID := range []int64{101, 202} {
		progress, found, progressErr := store.GetUserBookProgress(ctx, userID, book.ID)
		if progressErr != nil || !found || !progress.LastReadPage.Valid || progress.LastReadPage.Int64 != int64(3+i*4) {
			t.Errorf("user %d lost independent progress: %+v %v", userID, progress, progressErr)
		}
		seconds, timeErr := store.GetUserTotalReadingTime(ctx, userID)
		if timeErr != nil || seconds != int64(41+i*42) {
			t.Errorf("user %d lost reading time: %d %v", userID, seconds, timeErr)
		}
	}
	newArchive, err := os.ReadFile(filepath.Join(target, "嵌套", "系列", "第一卷.cbz"))
	if err != nil || !bytes.Equal(archiveBytes, newArchive) {
		t.Fatalf("archive bytes changed: %v", err)
	}
	newCover, err := os.ReadFile(coverPath)
	if err != nil || !bytes.Equal(newCover, png1x1) {
		t.Fatalf("custom cover bytes changed: %v", err)
	}
	readPage()
}
