package reading

import "manga-manager/internal/database"

// BookState separates an explicit completion mark from a proven absolute page position.
type BookState struct {
	Completed bool
	Started   bool
	ReadPages int64
}

// InspectBook interprets projected reader state without turning a completion mark into page evidence.
func InspectBook(book database.Book) BookState {
	page := book.LastReadPage.Int64
	if !book.LastReadPage.Valid || page < 0 {
		page = 0
	}
	if book.PageCount > 0 && page > book.PageCount {
		page = book.PageCount
	}
	completed := book.IsRead || (book.PageCount > 0 && page >= book.PageCount)
	return BookState{Completed: completed, Started: book.IsRead || page > 0, ReadPages: page}
}
