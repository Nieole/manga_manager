package database

import "context"

func (s *SqlStore) ListExternalLibraryBooksByLibrary(ctx context.Context, libraryID int64) ([]ExternalLibraryBookRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT b.id, b.series_id, s.name, b.path
		FROM books b
		JOIN series s ON s.id = b.series_id
		WHERE b.library_id = ?
		ORDER BY s.name, b.path
	`, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]ExternalLibraryBookRow, 0)
	for rows.Next() {
		var item ExternalLibraryBookRow
		if err := rows.Scan(&item.BookID, &item.SeriesID, &item.SeriesName, &item.Path); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}
