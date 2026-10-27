package database

import "context"

// ExistingSeriesIDs returns each selected existing ID once, using batched SQL.
func (q *Queries) ExistingSeriesIDs(ctx context.Context, ids []int64) ([]int64, error) {
	result := make([]int64, 0, len(ids))
	seen := make(map[int64]bool, len(ids))
	err := sqlInBatches(ids, func(placeholders string, args []interface{}) error {
		rows, err := q.db.QueryContext(ctx, `SELECT id FROM series WHERE id IN (`+placeholders+`) ORDER BY id`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return err
			}
			if !seen[id] {
				result = append(result, id)
				seen[id] = true
			}
		}
		return rows.Err()
	})
	return result, err
}

// SetSeriesStatusBatch updates only status for existing IDs, preserving the supplied value.
func (q *Queries) SetSeriesStatusBatch(ctx context.Context, ids []int64, status string) error {
	return sqlInBatches(ids, func(placeholders string, args []interface{}) error {
		_, err := q.db.ExecContext(ctx, `UPDATE series SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id IN (`+placeholders+`)`, append([]interface{}{status}, args...)...)
		return err
	})
}

// SetSeriesPublisherBatch updates only publisher for existing IDs.
func (q *Queries) SetSeriesPublisherBatch(ctx context.Context, ids []int64, publisher string) error {
	return sqlInBatches(ids, func(placeholders string, args []interface{}) error {
		_, err := q.db.ExecContext(ctx, `UPDATE series SET publisher = ?, updated_at = CURRENT_TIMESTAMP WHERE id IN (`+placeholders+`)`, append([]interface{}{publisher}, args...)...)
		return err
	})
}

// LinkSeriesTagBatch links a tag to existing selected series, ignoring duplicate links.
func (q *Queries) LinkSeriesTagBatch(ctx context.Context, ids []int64, tagID int64) error {
	return sqlInBatches(ids, func(placeholders string, args []interface{}) error {
		_, err := q.db.ExecContext(ctx, `INSERT OR IGNORE INTO series_tags (series_id, tag_id) SELECT id, ? FROM series WHERE id IN (`+placeholders+`)`, append([]interface{}{tagID}, args...)...)
		return err
	})
}

// UnlinkSeriesTagByNameBatch removes a named tag from selected series.
func (q *Queries) UnlinkSeriesTagByNameBatch(ctx context.Context, ids []int64, name string) error {
	return sqlInBatches(ids, func(placeholders string, args []interface{}) error {
		_, err := q.db.ExecContext(ctx, `DELETE FROM series_tags WHERE series_id IN (`+placeholders+`) AND tag_id IN (SELECT id FROM tags WHERE name = ?)`, append(args, name)...)
		return err
	})
}
