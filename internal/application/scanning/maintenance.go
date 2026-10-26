package scanning

import (
	"context"
	"log/slog"
	"manga-manager/internal/database"
	"manga-manager/internal/runhandle"
	"manga-manager/internal/scanner"
	"sync"
)

// ReindexLibraries waits for all forced scans inside an already admitted maintenance run.
// It does not acquire a second run slot; cover batches still use their own queued runs.
func (c *Service) ReindexLibraries(ctx context.Context, handle *runhandle.Handle, libs []database.Library) error {
	var wg sync.WaitGroup
	defer wg.Wait()
	for _, lib := range libs {
		if err := handle.Checkpoint(ctx); err != nil {
			return err
		}
		wg.Go(func() {
			defer c.effects.PurgeReadingPaths()
			if err := c.scanner.ScanLibrary(ctx, lib.ID, lib.Path, true, nil); err != nil {
				slog.ErrorContext(ctx, "Global scan of library failed", "library_id", lib.ID, "path", lib.Path, "error", err)
			}
		})
	}
	wg.Wait()
	return ctx.Err()
}

// RebuildThumbnails scans every known format in an existing maintenance run.
func (c *Service) RebuildThumbnails(ctx context.Context, h *runhandle.Handle, libs []database.Library, observer func(database.Library, int) scanner.ScanObserver) error {
	scan := c.scanner
	for _, lib := range libs {
		if err := h.Checkpoint(ctx); err != nil {
			return err
		}
		if err := scan.ScanLibraryWithOptions(ctx, lib.ID, lib.Path, scanner.LibraryScanOptions{Force: true, IgnoreFormatFilter: true}, observer(lib, len(libs))); err != nil {
			return err
		}
		c.effects.PurgeReadingPaths()
	}
	return nil
}

// CleanupThumbnails removes only unreferenced generated covers.
func (c *Service) CleanupThumbnails(ctx context.Context, progress func(int, int)) error {
	return c.scanner.CleanupThumbnails(ctx, progress)
}
