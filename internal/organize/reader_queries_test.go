package organize_test

import (
	"context"
	"errors"
	"manga-manager/internal/database"
	"manga-manager/internal/organize"
	"manga-manager/internal/reading"
	"testing"
)

func TestOrganizedReaderQueriesBindSubjectsAndKeepListFallbackSeparate(t *testing.T) {
	store, manual := newCollections(t)
	ctx := context.Background()
	if _, err := store.(*database.SqlStore).DB().Exec(`
 INSERT INTO libraries(id,name,path) VALUES(1,'Library','lib');
 INSERT INTO users(id,username,password_hash,role) VALUES(1,'alice','hash','admin'),(2,'bob','hash','regular');
 INSERT INTO series(id,library_id,name,path) VALUES(1,1,'Series','series'),(2,1,'Empty','empty');
 INSERT INTO books(id,library_id,series_id,name,path,page_count,size,file_modified_at) VALUES
 (1,1,1,'01','one',10,1,CURRENT_TIMESTAMP),(2,1,1,'02','two',0,1,CURRENT_TIMESTAMP);`); err != nil {
		t.Fatal(err)
	}
	owner, _ := reading.UserSubject(1)
	other, _ := reading.UserSubject(2)
	writes := reading.New(store, nil)
	if _, err := writes.MarkSeries(ctx, reading.MarkSeriesInput{UserID: 1, SeriesIDs: []int64{1}, IsRead: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := writes.MarkBooks(ctx, reading.MarkBooksInput{UserID: 2, BookIDs: []int64{1}, IsRead: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.RefreshSeriesDerivedData(ctx, 1); err != nil {
		t.Fatal(err)
	}
	shelves := organize.NewSmartCollections(store)
	filter, err := shelves.Save(ctx, 1, organize.SaveSmartFilter{Name: "Complete", ReadState: ptr("completed")})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		subject reading.Subject
		want    int
	}{{owner, 1}, {other, 0}, {reading.AnonymousSubject(), 0}} {
		page, err := shelves.Members(ctx, filter.ID, test.subject, 1, 0)
		if err != nil || page.Total != test.want || len(page.Items) != test.want || page.Subject != test.subject {
			t.Fatalf("members=%+v err=%v", page, err)
		}
		views, err := shelves.Views(ctx, test.subject)
		if err != nil || len(views) != 1 || views[0].SeriesCount != test.want {
			t.Fatalf("views=%+v err=%v", views, err)
		}
	}
	id, err := manual.Create(ctx, "Manual", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manual.AddMembers(ctx, id, []int64{1, 2}); err != nil {
		t.Fatal(err)
	}
	listService := organize.NewReadingLists(store)
	list, err := listService.Create(ctx, "Order", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, sid := range []int64{1, 2} {
		if _, err := listService.Add(ctx, list.ID, sid, ""); err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct {
		subject   reading.Subject
		target    int64
		completed int64
	}{{owner, 1, 2}, {other, 2, 1}, {reading.AnonymousSubject(), 1, 0}} {
		items, err := listService.Items(ctx, test.subject, list.ID)
		if err != nil || len(items) != 2 || items[0].NextBookID != test.target || items[0].CompletedBooks != test.completed || items[1].NextBookID != 0 || items[0].Subject != test.subject {
			t.Fatalf("items=%+v err=%v", items, err)
		}
		page, err := listService.Page(ctx, test.subject, list.ID, 1, 0)
		if err != nil || page.Subject != test.subject || page.Total != 2 || len(page.Rows) != 1 || page.Rows[0].NextBookID != test.target {
			t.Fatalf("page=%+v err=%v", page, err)
		}
	}
	normal, err := reading.NewQueries(store).SeriesBooks(ctx, owner, 1)
	if err != nil || normal.Continue.NextUnreadBookID != 0 || normal.Continue.ReadPages != 10 {
		t.Fatalf("normal=%+v err=%v", normal, err)
	}
	if _, err := shelves.Members(ctx, filter.ID, reading.Subject{}, 1, 0); !errors.Is(err, reading.ErrInvalidSubject) {
		t.Fatalf("invalid shelf subject=%v", err)
	}
	if _, err := listService.Items(ctx, reading.Subject{}, list.ID); !errors.Is(err, reading.ErrInvalidSubject) {
		t.Fatalf("invalid list subject=%v", err)
	}
}

func readerSubject(t *testing.T, uid int64) reading.Subject {
	t.Helper()
	if uid == 0 {
		return reading.AnonymousSubject()
	}
	subject, err := reading.UserSubject(uid)
	if err != nil {
		t.Fatal(err)
	}
	return subject
}
