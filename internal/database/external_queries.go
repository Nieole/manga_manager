package database

import "context"

func (s *SqlStore) ListExternalLibraryBooksByLibrary(ctx context.Context, libraryID int64) ([]ExternalLibraryBookRow, error) {
	rows, err := s.Queries.ListExternalLibraryBooks(ctx, libraryID)
	if err != nil {
		return nil, err
	}
	items := make([]ExternalLibraryBookRow, 0, len(rows))
	for _, r := range rows {
		items = append(items, ExternalLibraryBookRow{
			BookID:     r.BookID,
			SeriesID:   r.SeriesID,
			SeriesName: r.SeriesName,
			Path:       r.Path,
		})
	}
	return items, nil
}
