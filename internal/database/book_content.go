package database

import (
	"context"
	"database/sql"
	"time"
)

// BookContentUpdate contains facts of an already committed archive replacement.
type BookContentUpdate struct {
	ID             int64
	Path           string
	Size           int64
	FileModifiedAt time.Time
}

// UpdateBookContent refreshes byte-dependent facts without changing book identity,
// reading state or path fingerprints. A changed path must be reconciled explicitly.
func (q *Queries) UpdateBookContent(ctx context.Context, arg BookContentUpdate) error {
	result, err := q.db.ExecContext(ctx, `UPDATE books SET size=?, file_modified_at=?, file_hash=NULL, quick_hash=NULL WHERE id=? AND path=?`, arg.Size, arg.FileModifiedAt, arg.ID, arg.Path)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return sql.ErrNoRows
	}
	return nil
}
