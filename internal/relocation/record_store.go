package relocation

import (
	"context"
	"fmt"
	"manga-manager/internal/database"
)

// sqliteRecords supplies current identity facts without exposing SQL to mapping decisions.
type sqliteRecords struct{ db database.DBTX }

func (s *Service) records() sqliteRecords { return sqliteRecords{s.dbtx()} }

type libraryRecordFacts struct {
	root, scanMode, formats string
	interval, protection    int64
	series, books           int
}

func (q sqliteRecords) library(ctx context.Context, id int64) (libraryRecordFacts, error) {
	var f libraryRecordFacts
	err := q.db.QueryRowContext(ctx, `SELECT path,scan_mode,scan_interval,scan_formats,(SELECT COUNT(*) FROM series WHERE library_id=?),(SELECT COUNT(*) FROM books WHERE library_id=?),COALESCE((SELECT operation_id FROM library_relocation_protection WHERE library_id=?),0) FROM libraries WHERE id=?`, id, id, id, id).Scan(&f.root, &f.scanMode, &f.interval, &f.formats, &f.series, &f.books, &f.protection)
	return f, err
}
func (q sqliteRecords) saveDigest(ctx context.Context, id int64, digest string) error {
	_, err := q.db.ExecContext(ctx, `INSERT INTO relocation_record_facts(plan_id,digest) VALUES(?,?) ON CONFLICT(plan_id) DO UPDATE SET digest=excluded.digest`, id, digest)
	return err
}
func (q sqliteRecords) walk(ctx context.Context, libraryID int64, kind string, visit func(storedRecord) error) error {
	table, size := "series", "0"
	if kind == "book" {
		table, size = "books", "size"
	}
	var last int64
	for {
		query := fmt.Sprintf("SELECT id,path,%s,CAST(updated_at AS TEXT) FROM %s WHERE library_id=? AND id>? ORDER BY id LIMIT 100", size, table)
		rows, err := q.db.QueryContext(ctx, query, libraryID, last)
		if err != nil {
			return err
		}
		batch := make([]storedRecord, 0, 100)
		for rows.Next() {
			var item storedRecord
			if err = rows.Scan(&item.ID, &item.Path, &item.Size, &item.ChangedAt); err != nil {
				rows.Close()
				return err
			}
			batch = append(batch, item)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(batch) == 0 {
			return nil
		}
		for _, item := range batch {
			if err = visit(item); err != nil {
				return err
			}
			last = item.ID
		}
	}
}

func (q sqliteRecords) occupancy(ctx context.Context) (map[string][]int64, error) {
	rows, err := q.db.QueryContext(ctx, `SELECT 'series',id,path FROM series UNION ALL SELECT 'book',id,path FROM books UNION ALL SELECT 'library',id,path FROM libraries`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string][]int64{}
	for rows.Next() {
		var kind, path string
		var id int64
		if err = rows.Scan(&kind, &id, &path); err != nil {
			return nil, err
		}
		key := kind + ":" + pathKey(path)
		result[key] = append(result[key], id)
	}
	return result, rows.Err()
}
