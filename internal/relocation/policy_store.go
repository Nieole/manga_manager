package relocation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// preparePolicy atomically journals the owned directory-policy intent and prepared phase.
func (store sqliteOperations) preparePolicy(ctx context.Context, op *RelocationOperation, j policyJournal) error {
	data, err := json.Marshal(j)
	if err != nil {
		return err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO relocation_policy_journal(operation_id,patch) VALUES(?,?)`, op.ID, data); err != nil {
		return err
	}
	prepared := *op
	prepared.Status = "prepared"
	data, err = json.Marshal(prepared)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE relocation_operations SET status='prepared',result=? WHERE id=? AND status='pending'`, data, op.ID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	op.Status = "prepared"

	return nil
}
func (store sqliteOperations) policy(ctx context.Context, id int64) (*policyJournal, error) {
	var data string
	err := store.db.QueryRowContext(ctx, `SELECT patch FROM relocation_policy_journal WHERE operation_id=?`, id).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var j policyJournal
	if err = json.Unmarshal([]byte(data), &j); err != nil {
		return nil, err
	}
	if j.Version != 1 {
		return nil, errPolicyRecoveryConflict
	}
	return &j, nil
}
