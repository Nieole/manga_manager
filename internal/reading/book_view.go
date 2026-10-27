package reading

import (
	"context"
	"database/sql"
	"errors"
	"time"

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
	books := []database.Book{book}
	if err := q.ProjectBooks(ctx, subject.userID, books); err != nil {
		return BookView{}, err
	}
	return bookView(subject, books[0]), nil
}

func bookView(subject Subject, book database.Book) BookView {
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
		State: ReaderState{LastReadPage: book.LastReadPage, LastReadAt: book.LastReadAt, IsRead: book.IsRead},
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
	series, err := q.Series(ctx, subject.userID, sid)
	if err != nil {
		return BookSeriesView{}, err
	}
	views := make([]BookView, len(series.Books))
	for i, book := range series.Books {
		views[i] = bookView(subject, book)
	}
	return BookSeriesView{Books: views, Continue: series.Continue}, nil
}

func (q *Queries) AdjacentView(ctx context.Context, subject Subject, bid int64, next bool) (BookView, error) {
	if err := subject.validate(); err != nil {
		return BookView{}, err
	}
	book, err := q.Adjacent(ctx, subject.userID, bid, next)
	if err != nil {
		return BookView{}, err
	}
	return bookView(subject, book), nil
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
