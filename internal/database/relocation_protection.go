package database

import (
	"context"
	"database/sql"
	"errors"
)

var ErrLibraryProtected = errors.New("relocation_protected")

// LibraryRelocationProtection retains the accepted facts and original settings
// independently of run retention. Only explicit restoration may remove it.
type LibraryRelocationProtection struct {
	LibraryID    int64  `json:"library_id"`
	OperationID  int64  `json:"operation_id"`
	PlanID       int64  `json:"plan_id"`
	FactsDigest  string `json:"facts_digest"`
	AnomalyCount int    `json:"anomaly_count"`
	ScanMode     string `json:"scan_mode"`
	ScanInterval int64  `json:"scan_interval"`
	ScanFormats  string `json:"scan_formats"`
}

func (s *SqlStore) LibraryRelocationProtection(ctx context.Context, id int64) (*LibraryRelocationProtection, error) {
	p := &LibraryRelocationProtection{}
	err := s.db.QueryRowContext(ctx, `SELECT library_id,operation_id,plan_id,facts_digest,anomaly_count,scan_mode,scan_interval,scan_formats FROM library_relocation_protection WHERE library_id=?`, id).Scan(&p.LibraryID, &p.OperationID, &p.PlanID, &p.FactsDigest, &p.AnomalyCount, &p.ScanMode, &p.ScanInterval, &p.ScanFormats)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

// DeleteMissingSeries and DeleteMissingBook make protection and deletion one
// SQLite write, including callers outside Controller admission.
func (s *SqlStore) DeleteMissingSeries(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM series WHERE id=? AND NOT EXISTS (SELECT 1 FROM library_relocation_protection p WHERE p.library_id=series.library_id)`, id)
	return err
}

func (s *SqlStore) DeleteMissingBook(ctx context.Context, id int64) error {
	var seriesID int64
	err := s.db.QueryRowContext(ctx, `DELETE FROM books WHERE id=? AND NOT EXISTS (SELECT 1 FROM library_relocation_protection p WHERE p.library_id=books.library_id) RETURNING series_id`, id).Scan(&seriesID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.RefreshSeriesStats(ctx, seriesID)
}
