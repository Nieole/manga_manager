package relocation

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// sqliteOperations persists operation history independently of prunable run history.
type sqliteOperations struct{ db *sql.DB }

func (s *Service) operations() sqliteOperations { return sqliteOperations{s.persistence.db} }
func (j sqliteOperations) load(ctx context.Context, id int64) (RelocationOperation, error) {
	var op RelocationOperation
	var data string
	var runID int64
	var status string
	err := j.db.QueryRowContext(ctx, `SELECT run_id,status,result FROM relocation_operations WHERE id=?`, id).Scan(&runID, &status, &data)
	if err != nil {
		return op, err
	}
	if err = json.Unmarshal([]byte(data), &op); err != nil {
		return op, err
	}
	op.ID, op.RunID, op.Status = id, runID, status
	return op, err
}

func (j sqliteOperations) save(ctx context.Context, op RelocationOperation) error {
	op.UpdatedAt = time.Now().UTC()
	data, err := json.Marshal(op)
	if err != nil {
		return err
	}
	_, err = j.db.ExecContext(ctx, `UPDATE relocation_operations SET status=?,result=? WHERE id=?`, op.Status, data, op.ID)
	return err
}

func (j sqliteOperations) unfinished(ctx context.Context) ([]int64, error) {
	rows, err := j.db.QueryContext(ctx, `SELECT id FROM relocation_operations WHERE status IN ('pending','prepared','paths_committed') ORDER BY id`)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// sqlitePersistence is private: domain callers cannot recover DB/Queries through RecordFacts.
type sqlitePersistence struct{ db *sql.DB }

func (j sqliteOperations) create(ctx context.Context, op RelocationOperation) (RelocationOperation, bool, error) {
	var existing int64
	data, err := json.Marshal(op)
	if err != nil {
		return op, false, err
	}
	result, err := j.db.ExecContext(ctx, `INSERT INTO relocation_operations(plan_id,library_id,status,result) VALUES(?,?,?,?) ON CONFLICT(plan_id) DO NOTHING`, op.PlanID, op.LibraryID, op.Status, data)
	if err != nil {
		return op, false, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return op, false, err
	}
	if err = j.db.QueryRowContext(ctx, `SELECT id FROM relocation_operations WHERE plan_id=?`, op.PlanID).Scan(&existing); err != nil {
		return op, false, err
	}
	op.ID = existing
	if n == 0 {
		existingOp, e := j.load(ctx, existing)
		return existingOp, false, e
	}
	if err = j.save(ctx, op); err != nil {
		return op, false, err
	}
	return op, n == 1, nil
}

func (j sqliteOperations) find(ctx context.Context, planID int64, id *int64) error {
	return j.db.QueryRowContext(ctx, `SELECT id FROM relocation_operations WHERE plan_id=?`, planID).Scan(id)
}
func (j sqliteOperations) bind(ctx context.Context, id, runID int64) error {
	_, err := j.db.ExecContext(ctx, `UPDATE relocation_operations SET run_id=? WHERE id=? AND run_id=0`, runID, id)
	return err
}
