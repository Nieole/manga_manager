package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"manga-manager/internal/logger"
	"manga-manager/internal/task"
)

var errLibraryUnavailable = task.ErrLibraryUnavailable

// acquireLibraryExclusive reserves a library after every existing accessor exits.
// The caller must defer release, including on cancellation and failed commits.
func (c *Controller) acquireLibraryExclusive(ctx context.Context, libraryID int64) (func(), error) {
	return c.taskEngine.engine.AcquireLibraryExclusiveForRun(ctx, libraryID, logger.RunIDFrom(ctx))
}

func (c *Controller) taskLibraries(ctx context.Context, identity TaskIdentity) ([]int64, error) {
	switch identity.scope {
	case taskScopeLibrary:
		return []int64{*identity.scopeID}, nil
	case taskScopeSeries:
		series, err := c.store.GetSeries(ctx, *identity.scopeID)
		if errors.Is(err, sql.ErrNoRows) {
			return []int64{task.AllLibraries}, nil
		}
		if err != nil {
			return nil, err
		}
		return []int64{series.LibraryID}, nil
	case taskScopeSystem:
		switch identity.taskType {
		case cleanupRunHistoryTaskKey, "rebuild_index", "cleanup_thumbnails", "reconcile_koreader_progress", "refresh_koreader_matching":
			return nil, nil
		}
		return []int64{task.AllLibraries}, nil
	}
	return nil, nil
}

func (c *Controller) guardLibraryRead(w http.ResponseWriter, libraryID int64) (func(), bool) {
	release, err := c.taskEngine.engine.AcquireLibraryRead(libraryID)
	if err != nil {
		writeLibraryUnavailable(w)
		return nil, false
	}
	return release, true
}

func (c *Controller) guardBookRead(w http.ResponseWriter, r *http.Request, bookID int64) (func(), bool) {
	version := c.taskEngine.engine.LibraryAccessVersion()
	source, cached := c.pageArchive.lookupSource(bookID, time.Now())
	if !cached {
		book, err := c.store.GetBook(r.Context(), bookID)
		if err != nil {
			jsonError(w, http.StatusNotFound, "Book entity not found")
			return nil, false
		}
		source = bookPageSourceFromBook(book)
	}
	release, ok := c.guardLibraryRead(w, source.LibraryID)
	if !ok {
		return nil, false
	}
	if !cached && version == c.taskEngine.engine.LibraryAccessVersion() {
		c.pageArchive.storeSource(bookID, source, time.Now())
	}
	return release, true
}

func writeLibraryUnavailable(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "1")
	jsonResponse(w, http.StatusServiceUnavailable, map[string]string{"error": "Library is temporarily unavailable", "code": "library_unavailable"})
}

func (c *Controller) libraryAccessConflicts(ctx context.Context, libraryID int64) ([]task.LibraryConflict, error) {
	return c.taskEngine.engine.LibraryConflicts(ctx, libraryID, logger.RunIDFrom(ctx))
}
