package database

import "context"

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
