package reading

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"time"

	"manga-manager/internal/booksort"
	"manga-manager/internal/database"
)

var ErrInvalidSubject = errors.New("invalid reading subject")

// Subject identifies either a site user or the explicit anonymous compatibility path.
// Its zero value is invalid.
type Subject struct {
	userID    int64
	anonymous bool
}

func AnonymousSubject() Subject { return Subject{anonymous: true} }

func UserSubject(userID int64) (Subject, error) {
	if userID <= 0 {
		return Subject{}, ErrInvalidSubject
	}
	return Subject{userID: userID}, nil
}

func (s Subject) validate() error {
	if s.userID > 0 && !s.anonymous || s.userID == 0 && s.anonymous {
		return nil
	}
	return ErrInvalidSubject
}

// BookFacts describes catalogue identity and content, independently of any reader.
type BookFacts struct {
	ID                   int64
	SeriesID             int64
	LibraryID            int64
	Name                 string
	Path                 string
	Size                 int64
	FileModifiedAt       time.Time
	Volume               string
	Title                sql.NullString
	Summary              sql.NullString
	Number               sql.NullString
	SortNumber           sql.NullFloat64
	PageCount            int64
	CoverPath            sql.NullString
	CoverLocked          bool
	FileHash             sql.NullString
	QuickHash            sql.NullString
	PathFingerprint      sql.NullString
	PathFingerprintNoExt sql.NullString
	FilenameFingerprint  sql.NullString
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// ReaderState contains only the selected subject's reading facts.
type ReaderState struct {
	LastReadPage sql.NullInt64
	LastReadAt   sql.NullTime
	IsRead       bool
}

// BookView binds catalogue facts and reading state to one explicit subject.
type BookView struct {
	Subject Subject
	Facts   BookFacts
	State   ReaderState
}

// BookView returns a complete detail view without requiring caller-side projection.
func (q *Queries) BookView(ctx context.Context, subject Subject, bid int64) (BookView, error) {
	if err := subject.validate(); err != nil {
		return BookView{}, err
	}
	book, err := q.store.GetBook(ctx, bid)
	if err != nil {
		return BookView{}, err
	}
	views, err := q.bindBooks(ctx, subject, []database.Book{book})
	if err != nil {
		return BookView{}, err
	}
	return views[0], nil
}

func bookView(subject Subject, book database.Book, state ReaderState) BookView {
	return BookView{
		Subject: subject,
		Facts: BookFacts{
			ID: book.ID, SeriesID: book.SeriesID, LibraryID: book.LibraryID,
			Name: book.Name, Path: book.Path, Size: book.Size, FileModifiedAt: book.FileModifiedAt,
			Volume: book.Volume, Title: book.Title, Summary: book.Summary, Number: book.Number,
			SortNumber: book.SortNumber, PageCount: book.PageCount, CoverPath: book.CoverPath,
			CoverLocked: book.CoverLocked, FileHash: book.FileHash, QuickHash: book.QuickHash,
			PathFingerprint: book.PathFingerprint, PathFingerprintNoExt: book.PathFingerprintNoExt,
			FilenameFingerprint: book.FilenameFingerprint, CreatedAt: book.CreatedAt, UpdatedAt: book.UpdatedAt,
		},
		State: state,
	}
}

// BookSeriesView contains ordered reader-bound books and their continuation summary.
type BookSeriesView struct {
	Books    []BookView
	Continue Continue
}

func (q *Queries) SeriesBooks(ctx context.Context, subject Subject, sid int64) (BookSeriesView, error) {
	if err := subject.validate(); err != nil {
		return BookSeriesView{}, err
	}
	books, err := q.store.ListBooksBySeries(ctx, sid)
	if err != nil {
		return BookSeriesView{}, err
	}
	sort.SliceStable(books, func(i, j int) bool { return booksort.CompareBooks(books[i], books[j]) < 0 })
	views, err := q.bindBooks(ctx, subject, books)
	if err != nil {
		return BookSeriesView{}, err
	}
	return BookSeriesView{Books: views, Continue: summarize(views)}, nil
}

func (q *Queries) AdjacentView(ctx context.Context, subject Subject, bid int64, next bool) (BookView, error) {
	if err := subject.validate(); err != nil {
		return BookView{}, err
	}
	book, err := q.store.GetBook(ctx, bid)
	if err != nil {
		return BookView{}, err
	}
	series, err := q.SeriesBooks(ctx, subject, book.SeriesID)
	if err != nil {
		return BookView{}, err
	}
	for i, item := range series.Books {
		if item.Facts.ID != bid {
			continue
		}
		target := i - 1
		if next {
			target = i + 1
		}
		if target >= 0 && target < len(series.Books) {
			return series.Books[target], nil
		}
		break
	}
	return BookView{}, sql.ErrNoRows
}

// bindBooks separates reader state without mutating catalogue rows. The lookup remains batched.
func (q *Queries) bindBooks(ctx context.Context, subject Subject, books []database.Book) ([]BookView, error) {
	var positions map[int64]database.UserBookProgress
	if subject.userID > 0 && len(books) > 0 {
		ids := make([]int64, len(books))
		for i, book := range books {
			ids[i] = book.ID
		}
		var err error
		positions, err = q.store.GetUserBookProgressMap(ctx, subject.userID, ids)
		if err != nil {
			return nil, err
		}
	}
	views := make([]BookView, len(books))
	for i, book := range books {
		state := ReaderState{LastReadPage: book.LastReadPage, LastReadAt: book.LastReadAt, IsRead: book.IsRead}
		if subject.userID > 0 {
			p := positions[book.ID]
			state = ReaderState{LastReadPage: p.LastReadPage, LastReadAt: p.LastReadAt, IsRead: p.IsRead}
		}
		views[i] = bookView(subject, book, state)
	}
	return views, nil
}

// UserID returns the SQL binding for this subject. Zero is valid only for explicit anonymous compatibility.
func (s Subject) UserID() (int64, error) {
	if err := s.validate(); err != nil {
		return 0, err
	}
	return s.userID, nil
}

// Inspect interprets this reader's progress without treating completion as page evidence.
func (b BookView) Inspect() BookState {
	return inspectState(b.Facts.PageCount, b.State.LastReadPage, b.State.IsRead)
}
