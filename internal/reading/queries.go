package reading

import (
	"context"
	"database/sql"
	"sort"
	"time"

	"manga-manager/internal/booksort"
	"manga-manager/internal/database"
)

// ProjectionStore supplies catalogue facts and subject-owned reading positions.
type ProjectionStore interface {
	GetBook(context.Context, int64) (database.Book, error)
	ListBooksBySeries(context.Context, int64) ([]database.Book, error)
	GetUserBookProgressMap(context.Context, int64, []int64) (map[int64]database.UserBookProgress, error)
}

// Queries provides read-only reading views without recording activity or receipt time.
type Queries struct{ store ProjectionStore }

func NewQueries(store ProjectionStore) *Queries { return &Queries{store: store} }

// ProjectBooks applies a subject's state to catalogue results, preserving their order.
// User zero retains the catalogue's legacy global progress. Other subjects never inherit it,
// including when loading their state fails.
func (q *Queries) ProjectBooks(ctx context.Context, uid int64, books []database.Book) error {
	if uid == 0 || len(books) == 0 {
		return nil
	}
	ids := make([]int64, len(books))
	for i := range books {
		ids[i] = books[i].ID
		books[i].LastReadPage = sql.NullInt64{}
		books[i].LastReadAt = sql.NullTime{}
		books[i].IsRead = false
	}
	positions, err := q.store.GetUserBookProgressMap(ctx, uid, ids)
	if err != nil {
		return err
	}
	for i := range books {
		if p, ok := positions[books[i].ID]; ok {
			books[i].LastReadPage = p.LastReadPage
			books[i].LastReadAt = p.LastReadAt
			books[i].IsRead = p.IsRead
		}
	}
	return nil
}

func (q *Queries) Book(ctx context.Context, uid, bid int64) (database.Book, error) {
	book, err := q.store.GetBook(ctx, bid)
	if err != nil {
		return database.Book{}, err
	}
	books := []database.Book{book}
	err = q.ProjectBooks(ctx, uid, books)
	return books[0], err
}

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

type SeriesView struct {
	Books    []database.Book
	Continue Continue
}

func (q *Queries) Series(ctx context.Context, uid, sid int64) (SeriesView, error) {
	books, err := q.store.ListBooksBySeries(ctx, sid)
	if err != nil {
		return SeriesView{}, err
	}
	if err = q.ProjectBooks(ctx, uid, books); err != nil {
		return SeriesView{}, err
	}
	SortBooks(books)
	return SeriesView{Books: books, Continue: Summarize(books)}, nil
}

// SortBooks preserves the catalogue's natural reading order for navigation and continuation.
func SortBooks(books []database.Book) {
	sort.SliceStable(books, func(i, j int) bool { return booksort.CompareBooks(books[i], books[j]) < 0 })
}

// Summarize uses already projected books in reading order. All completed books leave
// NextUnreadBookID zero; ties in reading time retain the first book in that order.
func Summarize(books []database.Book) Continue {
	out := Continue{TotalBooks: len(books)}
	for _, book := range books {
		out.TotalPages += book.PageCount
		state := InspectBook(book)
		out.ReadPages += state.ReadPages
		if state.Completed {
			out.ReadBooks++
		} else if out.NextUnreadBookID == 0 {
			out.NextUnreadBookID = book.ID
		}
		if book.LastReadAt.Valid && (out.LastReadAt == nil || book.LastReadAt.Time.After(*out.LastReadAt)) {
			at := book.LastReadAt.Time
			out.LastReadAt = &at
			out.LastReadBookID = book.ID
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

// RecentReads preserves catalogue ordering and limits while selecting the reader explicitly.
func RecentReads(ctx context.Context, store RecentStore, uid, limit int64) ([]database.GetRecentReadAllRow, error) {
	if uid > 0 {
		return store.GetUserRecentReadAll(ctx, uid, limit)
	}
	return store.GetRecentReadAll(ctx, limit)
}

// Adjacent returns the previous or next book with the same subject projection as details.
func (q *Queries) Adjacent(ctx context.Context, uid, bid int64, next bool) (database.Book, error) {
	book, err := q.store.GetBook(ctx, bid)
	if err != nil {
		return database.Book{}, err
	}
	view, err := q.Series(ctx, uid, book.SeriesID)
	if err != nil {
		return database.Book{}, err
	}
	for i, b := range view.Books {
		if b.ID == bid {
			target := i - 1
			if next {
				target = i + 1
			}
			if target >= 0 && target < len(view.Books) {
				return view.Books[target], nil
			}
			break
		}
	}
	return database.Book{}, sql.ErrNoRows
}
