package api

import (
	"manga-manager/internal/bookcontent"
	"manga-manager/internal/database"
)

type bookPageSource = bookcontent.Source

func bookPageSourceFromBook(book database.Book) bookPageSource {
	return bookcontent.Source{ID: book.ID, LibraryID: book.LibraryID, Path: book.Path, FileModifiedAt: book.FileModifiedAt, Size: book.Size, PageCount: book.PageCount}
}

func (c *Controller) purgeReadingPathCaches() { c.content.PurgeSources() }
