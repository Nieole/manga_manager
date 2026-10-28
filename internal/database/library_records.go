package database

import "context"

// LibraryHasRecords includes detached book rows as well as series identities.
func (s *SqlStore) LibraryHasRecords(ctx context.Context, id int64) (bool, error) {
	var populated bool
	err := s.DB().QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM series WHERE library_id=?) OR EXISTS(SELECT 1 FROM books WHERE library_id=?)`, id, id).Scan(&populated)
	return populated, err
}
