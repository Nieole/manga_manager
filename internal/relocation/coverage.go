package relocation

import (
	"context"
	"encoding/json"
)

// VisitRecordPaths streams the current path coverage needed by a preview.
func (s *Service) VisitRecordPaths(ctx context.Context, libraryID int64, visit func(string) error) error {
	rows, err := s.dbtx().QueryContext(ctx, `SELECT path FROM series WHERE library_id=? UNION ALL SELECT path FROM books WHERE library_id=?`, libraryID, libraryID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var path string
		if err = rows.Scan(&path); err != nil {
			return err
		}
		if err = visit(path); err != nil {
			return err
		}
	}
	return rows.Err()
}

// VisitMappingPaths streams accepted coverage without exposing durable table layout.
func (s *Service) VisitMappingPaths(ctx context.Context, planID int64, visit func(string, string) error) error {
	rows, err := s.dbtx().QueryContext(ctx, `SELECT detail FROM relocation_details WHERE plan_id=? AND kind='mapping'`, planID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var body string
		var detail RelocationDetail
		if err = rows.Scan(&body); err != nil {
			return err
		}
		if err = json.Unmarshal([]byte(body), &detail); err != nil {
			return err
		}
		if err = visit(detail.SourcePath, detail.TargetPath); err != nil {
			return err
		}
	}
	return rows.Err()
}
