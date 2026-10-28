package reading

import (
	"context"
	"time"

	"manga-manager/internal/database"
)

// BookQueryStore supplies catalogue rows and one batched reader-position lookup.
type BookQueryStore interface {
	GetBook(context.Context, int64) (database.Book, error)
	ListBooksBySeries(context.Context, int64) ([]database.Book, error)
	GetUserBookProgressMap(context.Context, int64, []int64) (map[int64]database.UserBookProgress, error)
}

// Queries provides complete reader-bound queries without recording activity or receipt time.
type Queries struct{ store BookQueryStore }

func NewQueries(store BookQueryStore) *Queries { return &Queries{store: store} }

// Continue describes a series' progress and first unfinished book in reading order.
type Continue struct {
	NextUnreadBookID int64      `json:"next_unread_book_id,omitempty"`
	LastReadBookID   int64      `json:"last_read_book_id,omitempty"`
	LastReadPage     int64      `json:"last_read_page,omitempty"`
	LastReadAt       *time.Time `json:"last_read_at,omitempty"`
	TotalBooks       int        `json:"total_books"`
	ReadBooks        int        `json:"read_books"`
	TotalPages       int64      `json:"total_pages"`
	ReadPages        int64      `json:"read_pages"`
}

// summarize uses bound books in reading order. All-complete series have no next unread book.
func summarize(books []BookView) Continue {
	out := Continue{TotalBooks: len(books)}
	for _, book := range books {
		out.TotalPages += book.Facts.PageCount
		state := book.Inspect()
		out.ReadPages += state.ReadPages
		if state.Completed {
			out.ReadBooks++
		} else if out.NextUnreadBookID == 0 {
			out.NextUnreadBookID = book.Facts.ID
		}
		at := book.State.LastReadAt
		if at.Valid && (out.LastReadAt == nil || at.Time.After(*out.LastReadAt)) {
			lastReadAt := at.Time
			out.LastReadAt = &lastReadAt
			out.LastReadBookID = book.Facts.ID
			out.LastReadPage = state.ReadPages
		}
	}
	return out
}

// RecentStore provides the subject-specific catalogue query and its legacy equivalent.
type RecentStore interface {
	GetUserRecentReadAll(context.Context, int64, int64) ([]database.GetRecentReadAllRow, error)
	GetRecentReadAll(context.Context, int64) ([]database.GetRecentReadAllRow, error)
}
