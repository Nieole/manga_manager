package reading

import (
	"database/sql"

	"manga-manager/internal/database"
)

// BookState separates an explicit completion mark from a proven absolute page position.
type BookState struct {
	Completed bool
	Started   bool
	ReadPages int64
}

// InspectBook interprets projected reader state without turning a completion mark into page evidence.
func InspectBook(book database.Book) BookState {
	return inspectState(book.PageCount, book.LastReadPage, book.IsRead)
}

func inspectState(pageCount int64, lastReadPage sql.NullInt64, isRead bool) BookState {
	page := lastReadPage.Int64
	if !lastReadPage.Valid || page < 0 {
		page = 0
	}
	if pageCount > 0 && page > pageCount {
		page = pageCount
	}
	completed := isRead || (pageCount > 0 && page >= pageCount)
	return BookState{Completed: completed, Started: isRead || page > 0, ReadPages: page}
}
