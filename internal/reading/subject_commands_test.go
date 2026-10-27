package reading_test

import (
	"context"
	"errors"
	"testing"

	"manga-manager/internal/reading"
)

func TestReaderCommandsRejectMissingSubjectWithoutWriting(t *testing.T) {
	store, uid, bid := positionFixture(t)
	ctx := context.Background()
	bookmarks := reading.NewBookmarks(store)
	if _, err := bookmarks.Save(ctx, reading.BookmarkInput{BookID: bid, Page: 4}); !errors.Is(err, reading.ErrInvalidSubject) {
		t.Fatalf("save missing subject: %v", err)
	}
	if _, err := bookmarks.List(ctx, reading.Subject{}, bid); !errors.Is(err, reading.ErrInvalidSubject) {
		t.Fatalf("list missing subject: %v", err)
	}
	if err := bookmarks.Delete(ctx, reading.Subject{}, bid, 1); !errors.Is(err, reading.ErrInvalidSubject) {
		t.Fatalf("delete missing subject: %v", err)
	}
	if err := reading.New(store, nil).RecordTime(ctx, reading.TimeReport{BookID: bid, Seconds: 30}); !errors.Is(err, reading.ErrInvalidSubject) {
		t.Fatalf("time missing subject: %v", err)
	}
	subject, err := reading.UserSubject(uid)
	if err != nil {
		t.Fatal(err)
	}
	items, err := bookmarks.List(ctx, subject, bid)
	if err != nil || len(items) != 0 {
		t.Fatalf("bookmarks=%+v err=%v", items, err)
	}
	total, err := store.GetUserTotalReadingTime(ctx, uid)
	if err != nil || total != 0 {
		t.Fatalf("seconds=%d err=%v", total, err)
	}
}

func commandSubject(uid int64) reading.Subject {
	if uid == 0 {
		return reading.AnonymousSubject()
	}
	subject, err := reading.UserSubject(uid)
	if err != nil {
		panic(err)
	}
	return subject
}
