package organize

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"

	"manga-manager/internal/database"
	"manga-manager/internal/reading"
)

// MaxReadingListReorderItems bounds a single reorder gesture.
const MaxReadingListReorderItems = 1000

var (
	ErrReadingListMissing       = errors.New("Reading list not found")
	ErrReadingListSeriesMissing = errors.New("Series not found")
	ErrReadingListItemMissing   = errors.New("Reading list item not found")
	ErrReadingListNameRequired  = errors.New("Name is required")
	ErrReadingListItemsRequired = errors.New("item_ids is required")
	ErrReadingListTooManyItems  = errors.New("Too many items in one request")
)

// ReadingListStore supplies ordered catalogue facts and transaction-bound writes.
type ReadingListStore interface {
	reading.BookQueryStore
	WithReadingListWrite(context.Context, func(database.ReadingListWrite) error) error
	ListReadingLists(context.Context) ([]database.ListReadingListsRow, error)
	GetReadingList(context.Context, int64) (database.ReadingList, error)
	ListReadingListItems(context.Context, int64) ([]database.ListReadingListItemsRow, error)
	CountReadingListSeries(context.Context, int64) (int64, error)
	ListReadingListSeriesPage(context.Context, database.ListReadingListSeriesPageParams) ([]database.ListReadingListSeriesPageRow, error)
}

// ReadingLists owns ordered membership and reader-specific continuation independently of collections.
type ReadingLists struct {
	store   ReadingListStore
	reading *reading.Queries
}

func NewReadingLists(store ReadingListStore) *ReadingLists {
	return &ReadingLists{store: store, reading: reading.NewQueries(store)}
}

func (s *ReadingLists) List(ctx context.Context) ([]database.ListReadingListsRow, error) {
	return s.store.ListReadingLists(ctx)
}
func (s *ReadingLists) Get(ctx context.Context, id int64) (database.ReadingList, error) {
	row, err := s.store.GetReadingList(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrReadingListMissing
	}
	return row, err
}
func (s *ReadingLists) Create(ctx context.Context, name, description string) (database.ReadingList, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return database.ReadingList{}, ErrReadingListNameRequired
	}
	var row database.ReadingList
	err := s.store.WithReadingListWrite(ctx, func(q database.ReadingListWrite) error {
		var err error
		row, err = q.CreateReadingList(ctx, database.CreateReadingListParams{Name: name, Description: strings.TrimSpace(description)})
		return err
	})
	return row, err
}
func (s *ReadingLists) Update(ctx context.Context, id int64, name, description string) (database.ReadingList, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return database.ReadingList{}, ErrReadingListNameRequired
	}
	var row database.ReadingList
	err := s.store.WithReadingListWrite(ctx, func(q database.ReadingListWrite) error {
		var err error
		row, err = q.UpdateReadingList(ctx, database.UpdateReadingListParams{ID: id, Name: name, Description: strings.TrimSpace(description)})
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrReadingListMissing
	}
	return row, err
}
func (s *ReadingLists) Delete(ctx context.Context, id int64) error {
	return s.store.WithReadingListWrite(ctx, func(q database.ReadingListWrite) error {
		n, err := q.DeleteReadingList(ctx, id)
		if err == nil && n == 0 {
			return ErrReadingListMissing
		}
		return err
	})
}
func (s *ReadingLists) Add(ctx context.Context, id, seriesID int64, note string) (database.ReadingListItem, error) {
	var item database.ReadingListItem
	err := s.store.WithReadingListWrite(ctx, func(q database.ReadingListWrite) error {
		if _, err := q.GetReadingList(ctx, id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrReadingListMissing
			}
			return err
		}
		if _, err := q.GetSeries(ctx, seriesID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrReadingListSeriesMissing
			}
			return err
		}
		var err error
		item, err = q.AddReadingListItem(ctx, database.AddReadingListItemParams{ReadingListID: id, SeriesID: seriesID, Note: strings.TrimSpace(note)})
		return err
	})
	return item, err
}
func (s *ReadingLists) Remove(ctx context.Context, id, itemID int64) error {
	return s.store.WithReadingListWrite(ctx, func(q database.ReadingListWrite) error {
		n, err := q.RemoveReadingListItem(ctx, database.RemoveReadingListItemParams{ReadingListID: id, ID: itemID})
		if err == nil && n == 0 {
			return ErrReadingListItemMissing
		}
		return err
	})
}

// Reorder commits a submitted ordering atomically; omitted concurrent members retain their position.
func (s *ReadingLists) Reorder(ctx context.Context, id int64, itemIDs []int64) error {
	if len(itemIDs) == 0 {
		return ErrReadingListItemsRequired
	}
	if len(itemIDs) > MaxReadingListReorderItems {
		return ErrReadingListTooManyItems
	}
	return s.store.WithReadingListWrite(ctx, func(q database.ReadingListWrite) error {
		if _, err := q.GetReadingList(ctx, id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrReadingListMissing
			}
			return err
		}
		for i, itemID := range itemIDs {
			n, err := q.UpdateReadingListItemSortOrder(ctx, database.UpdateReadingListItemSortOrderParams{ReadingListID: id, ID: itemID, SortOrder: int64((i + 1) * 10)})
			if err != nil {
				return err
			}
			if n == 0 {
				return ErrReadingListItemMissing
			}
		}
		return nil
	})
}

// ReadingListItem preserves member identity and adds current-reader progress.
type ReadingListItem struct {
	Subject reading.Subject `json:"-"`
	database.ListReadingListItemsRow
	ReadBooks      int64 `json:"read_books"`
	CompletedBooks int64 `json:"completed_books"`
	TotalBooks     int64 `json:"total_books"`
}

func (s *ReadingLists) Items(ctx context.Context, subject reading.Subject, id int64) ([]ReadingListItem, error) {
	if _, err := subject.UserID(); err != nil {
		return nil, err
	}
	if _, err := s.Get(ctx, id); err != nil {
		return nil, err
	}
	rows, err := s.store.ListReadingListItems(ctx, id)
	if err != nil {
		return nil, err
	}
	out := make([]ReadingListItem, 0, len(rows))
	for _, row := range rows {
		view, err := s.reading.SeriesBooks(ctx, subject, row.SeriesID)
		if err != nil {
			return nil, err
		}
		row.NextBookID = readingListTarget(view)
		item := ReadingListItem{Subject: subject, ListReadingListItemsRow: row, TotalBooks: int64(view.Continue.TotalBooks), CompletedBooks: int64(view.Continue.ReadBooks)}
		for _, book := range view.Books {
			if book.Inspect().Started {
				item.ReadBooks++
			}
		}
		out = append(out, item)
	}
	return out, nil
}

// ReadingListPage keeps protocol list identity, member order and total together.
type ReadingListPage struct {
	Subject reading.Subject
	List    database.ReadingList
	Rows    []database.ListReadingListSeriesPageRow
	Total   int64
}

// Page preserves protocol identities and pagination while applying the same subject continuation as the web list.
func (s *ReadingLists) Page(ctx context.Context, subject reading.Subject, id, limit, offset int64) (ReadingListPage, error) {
	out := ReadingListPage{Subject: subject}
	if _, err := subject.UserID(); err != nil {
		return out, err
	}
	var err error
	out.List, err = s.Get(ctx, id)
	if err != nil {
		return out, err
	}
	out.Total, err = s.store.CountReadingListSeries(ctx, id)
	if err != nil {
		return out, err
	}
	out.Rows, err = s.store.ListReadingListSeriesPage(ctx, database.ListReadingListSeriesPageParams{ReadingListID: id, Limit: limit, Offset: offset})
	if err != nil {
		return out, err
	}
	for i := range out.Rows {
		view, err := s.reading.SeriesBooks(ctx, subject, out.Rows[i].SeriesID)
		if err != nil {
			return out, err
		}
		out.Rows[i].NextBookID = readingListTarget(view)
	}
	return out, nil
}

func readingListTarget(view reading.BookSeriesView) int64 {
	if len(view.Books) == 0 {
		return 0
	}
	// Lists prefer unfinished books, then retain catalogue sort_number/name order,
	// including the first-book fallback when every book is complete.
	book := slices.MinFunc(view.Books, func(a, b reading.BookView) int {
		aDone, bDone := a.Inspect().Completed, b.Inspect().Completed
		if aDone != bDone {
			if aDone {
				return 1
			}
			return -1
		}
		if a.Facts.SortNumber.Valid != b.Facts.SortNumber.Valid {
			if a.Facts.SortNumber.Valid {
				return 1
			}
			return -1
		}
		if a.Facts.SortNumber.Valid {
			if order := cmp.Compare(a.Facts.SortNumber.Float64, b.Facts.SortNumber.Float64); order != 0 {
				return order
			}
		}
		return strings.Compare(a.Facts.Name, b.Facts.Name)
	})
	return book.Facts.ID
}
