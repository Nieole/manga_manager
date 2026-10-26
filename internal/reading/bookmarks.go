package reading

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"manga-manager/internal/database"
)

// BookmarkStore supplies only bookmark persistence and the book's page bounds.
type BookmarkStore interface {
	GetBook(context.Context, int64) (database.Book, error)
	ListReadingBookmarks(context.Context, database.ListReadingBookmarksParams) ([]database.ReadingBookmark, error)
	UpsertReadingBookmark(context.Context, database.UpsertReadingBookmarkParams) (database.ReadingBookmark, error)
	DeleteReadingBookmark(context.Context, database.DeleteReadingBookmarkParams) (int64, error)
}

// Bookmarks owns subject-scoped bookmarks without recording reading activity.
type Bookmarks struct{ store BookmarkStore }

func NewBookmarks(store BookmarkStore) *Bookmarks { return &Bookmarks{store: store} }

// BookmarkInput saves the note at a reader's page; saving again retains its identity.
type BookmarkInput struct {
	UserID int64
	BookID int64
	Page   int64
	Note   string
}

// ErrBookmarkNotFound includes a bookmark belonging to a different reader or book.
var ErrBookmarkNotFound = errors.New("bookmark not found")

// ErrBookmarkBookUnavailable identifies failure to load the book before listing or
// saving a bookmark. The underlying storage error remains available to callers.
var ErrBookmarkBookUnavailable = errors.New("bookmark book unavailable")

func (b *Bookmarks) List(ctx context.Context, uid, bid int64) ([]database.ReadingBookmark, error) {
	if _, err := b.store.GetBook(ctx, bid); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBookmarkBookUnavailable, err)
	}
	items, err := b.store.ListReadingBookmarks(ctx, database.ListReadingBookmarksParams{UserID: uid, BookID: bid})
	if err == nil && items == nil {
		items = []database.ReadingBookmark{}
	}
	return items, err
}

func (b *Bookmarks) Save(ctx context.Context, input BookmarkInput) (database.ReadingBookmark, error) {
	book, err := b.store.GetBook(ctx, input.BookID)
	if err != nil {
		return database.ReadingBookmark{}, fmt.Errorf("%w: %w", ErrBookmarkBookUnavailable, err)
	}
	if input.Page < 1 {
		input.Page = 1
	}
	if book.PageCount > 0 && input.Page > book.PageCount {
		input.Page = book.PageCount
	}
	return b.store.UpsertReadingBookmark(ctx, database.UpsertReadingBookmarkParams{
		UserID: input.UserID, BookID: input.BookID, Page: input.Page, Note: strings.TrimSpace(input.Note),
	})
}

func (b *Bookmarks) Delete(ctx context.Context, uid, bid, id int64) error {
	affected, err := b.store.DeleteReadingBookmark(ctx, database.DeleteReadingBookmarkParams{ID: id, UserID: uid, BookID: bid})
	if err == nil && affected == 0 {
		return ErrBookmarkNotFound
	}
	return err
}
