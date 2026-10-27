package api

import (
	"net/http"

	"manga-manager/internal/database"
	"manga-manager/internal/reading"
)

func (c *Controller) currentReadingSubject(r *http.Request) (reading.Subject, error) {
	if user, ok := userFromContext(r.Context()); ok {
		return reading.UserSubject(user.ID)
	}
	return reading.AnonymousSubject(), nil
}

// readerBookResponse preserves the public book shape and its host-path redaction policy.
func readerBookResponse(view reading.BookView) database.Book {
	facts, state := view.Facts, view.State
	return database.Book{
		ID: facts.ID, SeriesID: facts.SeriesID, LibraryID: facts.LibraryID,
		Name: facts.Name, Path: facts.Path, Size: facts.Size, FileModifiedAt: facts.FileModifiedAt,
		Volume: facts.Volume, Title: facts.Title, Summary: facts.Summary, Number: facts.Number,
		SortNumber: facts.SortNumber, PageCount: facts.PageCount, CoverPath: facts.CoverPath,
		CoverLocked: facts.CoverLocked, FileHash: facts.FileHash, QuickHash: facts.QuickHash,
		PathFingerprint: facts.PathFingerprint, PathFingerprintNoExt: facts.PathFingerprintNoExt,
		FilenameFingerprint: facts.FilenameFingerprint, CreatedAt: facts.CreatedAt, UpdatedAt: facts.UpdatedAt,
		LastReadPage: state.LastReadPage, LastReadAt: state.LastReadAt, IsRead: state.IsRead,
	}
}

func readerBookResponses(views []reading.BookView) []database.Book {
	books := make([]database.Book, len(views))
	for i, view := range views {
		books[i] = readerBookResponse(view)
	}
	return books
}
