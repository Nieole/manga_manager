package reading_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/reading"
)

func TestBookmarksCRUDPreservesIdentityAndOrdersPages(t *testing.T) {
	store, uid, bid := positionFixture(t)
	bookmarks := reading.NewBookmarks(store)
	ctx := context.Background()
	items, err := bookmarks.List(ctx, uid, bid)
	if err != nil || items == nil || len(items) != 0 {
		t.Fatalf("empty list=%+v err=%v", items, err)
	}
	created, err := bookmarks.Save(ctx, reading.BookmarkInput{UserID: uid, BookID: bid, Page: 12, Note: "note"})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := bookmarks.Save(ctx, reading.BookmarkInput{UserID: uid, BookID: bid, Page: 12, Note: "edited"})
	if err != nil || updated.ID != created.ID || updated.Note != "edited" || !updated.CreatedAt.Equal(created.CreatedAt) {
		t.Fatalf("updated=%+v created=%+v err=%v", updated, created, err)
	}
	first, err := bookmarks.Save(ctx, reading.BookmarkInput{UserID: uid, BookID: bid, Page: 3})
	if err != nil {
		t.Fatal(err)
	}
	items, err = bookmarks.List(ctx, uid, bid)
	if err != nil || len(items) != 2 || items[0].ID != first.ID || items[1].ID != created.ID {
		t.Fatalf("list=%+v err=%v", items, err)
	}
	if err := bookmarks.Delete(ctx, uid, bid, created.ID); err != nil {
		t.Fatal(err)
	}
	items, err = bookmarks.List(ctx, uid, bid)
	if err != nil || len(items) != 1 || items[0].ID != first.ID {
		t.Fatalf("remaining=%+v err=%v", items, err)
	}
}

func TestBookmarksScopeEveryOperationToReaderAndBookWithoutReadingEffects(t *testing.T) {
	store, uid, bid := positionFixture(t)
	ctx := context.Background()
	otherUser, err := store.CreateUser(ctx, database.CreateUserParams{Username: "other", PasswordHash: "x", Role: "user"})
	if err != nil {
		t.Fatal(err)
	}
	book, err := store.GetBook(ctx, bid)
	if err != nil {
		t.Fatal(err)
	}
	otherBook, err := store.CreateBook(ctx, database.CreateBookParams{Name: "other", Path: "/library/series/other", LibraryID: book.LibraryID, SeriesID: book.SeriesID, PageCount: 20, FileModifiedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.Local)
	service := reading.New(store, nil, reading.WithClock(func() time.Time { return at }))
	if _, err := service.RecordPosition(ctx, reading.Position{UserID: uid, BookID: bid, Page: 7}); err != nil {
		t.Fatal(err)
	}
	if err := service.RecordTime(ctx, reading.TimeReport{UserID: uid, BookID: bid, Seconds: 30}); err != nil {
		t.Fatal(err)
	}
	queries := reading.NewQueries(store)
	before, err := queries.Book(ctx, uid, bid)
	if err != nil {
		t.Fatal(err)
	}
	statsBefore, err := store.GetUserPeriodStats(ctx, uid, 2026, 10)
	if err != nil {
		t.Fatal(err)
	}
	bookmarks := reading.NewBookmarks(store)
	owner, err := bookmarks.Save(ctx, reading.BookmarkInput{UserID: uid, BookID: bid, Page: 8, Note: "private"})
	if err != nil {
		t.Fatal(err)
	}
	for _, subject := range []struct{ uid, bid int64 }{{otherUser.ID, bid}, {uid, otherBook.ID}, {0, bid}} {
		items, err := bookmarks.List(ctx, subject.uid, subject.bid)
		if err != nil || len(items) != 0 {
			t.Fatalf("subject=%+v leaked=%+v err=%v", subject, items, err)
		}
		if err := bookmarks.Delete(ctx, subject.uid, subject.bid, owner.ID); !errors.Is(err, reading.ErrBookmarkNotFound) {
			t.Fatalf("subject=%+v delete error=%v", subject, err)
		}
		other, err := bookmarks.Save(ctx, reading.BookmarkInput{UserID: subject.uid, BookID: subject.bid, Page: 8, Note: "other"})
		if err != nil || other.ID == owner.ID {
			t.Fatalf("subject=%+v bookmark=%+v err=%v", subject, other, err)
		}
		if err := bookmarks.Delete(ctx, subject.uid, subject.bid, other.ID); err != nil {
			t.Fatal(err)
		}
	}
	items, err := bookmarks.List(ctx, uid, bid)
	if err != nil || len(items) != 1 || items[0].Note != "private" {
		t.Fatalf("owner list=%+v err=%v", items, err)
	}
	if err := bookmarks.Delete(ctx, uid, bid, owner.ID); err != nil {
		t.Fatal(err)
	}
	after, err := queries.Book(ctx, uid, bid)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("book changed before=%+v after=%+v err=%v", before, after, err)
	}
	statsAfter, err := store.GetUserPeriodStats(ctx, uid, 2026, 10)
	if err != nil || !reflect.DeepEqual(statsBefore, statsAfter) {
		t.Fatalf("activity changed before=%+v after=%+v err=%v", statsBefore, statsAfter, err)
	}
	total, err := store.GetUserTotalReadingTime(ctx, uid)
	if err != nil || total != 30 {
		t.Fatalf("reading seconds=%d err=%v", total, err)
	}
}

func TestBookmarksExposeStorageFailuresAndKeepSavedNotes(t *testing.T) {
	for _, op := range []string{"insert", "update", "delete", "list", "book lookup"} {
		t.Run(op, func(t *testing.T) {
			store, uid, bid := positionFixture(t)
			ctx := context.Background()
			bookmarks := reading.NewBookmarks(store)
			saved, err := bookmarks.Save(ctx, reading.BookmarkInput{UserID: uid, BookID: bid, Page: 5, Note: "retained"})
			if err != nil {
				t.Fatal(err)
			}
			switch op {
			case "list":
				if _, err := store.DB().Exec(`ALTER TABLE reading_bookmarks RENAME TO unavailable_bookmarks`); err != nil {
					t.Fatal(err)
				}
				_, err = bookmarks.List(ctx, uid, bid)
			case "book lookup":
				if err := store.Close(); err != nil {
					t.Fatal(err)
				}
				_, err = bookmarks.List(ctx, uid, bid)
				if !errors.Is(err, reading.ErrBookmarkBookUnavailable) {
					t.Fatalf("list book failure=%v", err)
				}
				_, err = bookmarks.Save(ctx, reading.BookmarkInput{UserID: uid, BookID: bid, Page: 5})
				if !errors.Is(err, reading.ErrBookmarkBookUnavailable) {
					t.Fatalf("save book failure=%v", err)
				}
			default:
				if _, err := store.DB().Exec(`CREATE TRIGGER fail_bookmarks BEFORE ` + op + ` ON reading_bookmarks BEGIN SELECT RAISE(ABORT,'bookmark storage unavailable'); END`); err != nil {
					t.Fatal(err)
				}
				if op == "delete" {
					err = bookmarks.Delete(ctx, uid, bid, saved.ID)
				} else {
					page := int64(5)
					if op == "insert" {
						page = 6
					}
					_, err = bookmarks.Save(ctx, reading.BookmarkInput{UserID: uid, BookID: bid, Page: page, Note: "lost"})
				}
			}
			if err == nil || errors.Is(err, reading.ErrBookmarkNotFound) || errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("storage failure=%v", err)
			}
			if op == "insert" || op == "update" || op == "delete" {
				items, err := bookmarks.List(ctx, uid, bid)
				if err != nil || len(items) != 1 || items[0].ID != saved.ID || items[0].Note != "retained" {
					t.Fatalf("after failure=%+v err=%v", items, err)
				}
			}
		})
	}
}

func TestBookmarksMissingBookAndBookmarkRemainObservable(t *testing.T) {
	store, uid, _ := positionFixture(t)
	bookmarks := reading.NewBookmarks(store)
	ctx := context.Background()
	if _, err := bookmarks.List(ctx, uid, 999999); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("list missing book error=%v", err)
	}
	if _, err := bookmarks.Save(ctx, reading.BookmarkInput{UserID: uid, BookID: 999999, Page: 1}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("save missing book error=%v", err)
	}
	if err := bookmarks.Delete(ctx, uid, 999999, 999999); !errors.Is(err, reading.ErrBookmarkNotFound) {
		t.Fatalf("delete missing bookmark error=%v", err)
	}
}

func TestBookmarksClampPagesTrimNotesAndKeepExistingIdentity(t *testing.T) {
	store, uid, bid := positionFixture(t)
	ctx := context.Background()
	bookmarks := reading.NewBookmarks(store)
	existing, err := store.UpsertReadingBookmark(ctx, database.UpsertReadingBookmarkParams{UserID: uid, BookID: bid, Page: 20, Note: "legacy note"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		page, want     int64
		note, wantNote string
	}{{-1, 1, "\t 笔记 \n", "笔记"}, {0, 1, " \n ", ""}, {1, 1, "first", "first"}, {20, 20, "last", "last"}, {500, 20, "\u3000末页\u3000", "末页"}} {
		t.Run(tc.note, func(t *testing.T) {
			got, err := bookmarks.Save(ctx, reading.BookmarkInput{UserID: uid, BookID: bid, Page: tc.page, Note: tc.note})
			if err != nil || got.Page != tc.want || got.Note != tc.wantNote {
				t.Fatalf("bookmark=%+v err=%v want page=%d note=%q", got, err, tc.want, tc.wantNote)
			}
			if tc.want == 20 && (got.ID != existing.ID || !got.CreatedAt.Equal(existing.CreatedAt)) {
				t.Fatalf("existing identity changed: %+v -> %+v", existing, got)
			}
		})
	}
	if _, err := store.DB().Exec(`UPDATE books SET page_count=0 WHERE id=?`, bid); err != nil {
		t.Fatal(err)
	}
	got, err := bookmarks.Save(ctx, reading.BookmarkInput{UserID: uid, BookID: bid, Page: 500})
	if err != nil || got.Page != 500 {
		t.Fatalf("unknown page count bookmark=%+v err=%v", got, err)
	}
}
