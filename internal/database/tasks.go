// 本文件由 store.go 拆分而来，属于 SQLite 数据访问层的「后台任务持久化」子域。
// 它负责任务记录的写入、按筛选条件列出与清理，是任务面板与重试链路的数据来源。

package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

type TaskRecord struct {
	Key        string            `json:"key"`
	Type       string            `json:"type"`
	Scope      string            `json:"scope"`
	ScopeID    *int64            `json:"scope_id,omitempty"`
	ScopeName  string            `json:"scope_name,omitempty"`
	Status     string            `json:"status"`
	Message    string            `json:"message"`
	Error      string            `json:"error,omitempty"`
	Current    int               `json:"current"`
	Total      int               `json:"total"`
	CanCancel  bool              `json:"can_cancel"`
	Retryable  bool              `json:"retryable"`
	Params     map[string]string `json:"params,omitempty"`
	StartedAt  time.Time         `json:"started_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
	FinishedAt *time.Time        `json:"finished_at,omitempty"`
	Sequence   int64             `json:"-"`
}

type TaskFilters struct {
	Status  string
	Scope   string
	Type    string
	ScopeID *int64
	Query   string
	Limit   int
}

func (s *SqlStore) UpsertTask(ctx context.Context, task TaskRecord) error {
	paramsJSON := ""
	if len(task.Params) > 0 {
		data, err := json.Marshal(task.Params)
		if err != nil {
			return err
		}
		paramsJSON = string(data)
	}

	var scopeID sql.NullInt64
	if task.ScopeID != nil {
		scopeID = sql.NullInt64{Int64: *task.ScopeID, Valid: true}
	}
	var finishedAt sql.NullTime
	if task.FinishedAt != nil {
		finishedAt = sql.NullTime{Time: *task.FinishedAt, Valid: true}
	}

	return s.Queries.UpsertTaskRecord(ctx, UpsertTaskRecordParams{
		Key:        task.Key,
		Type:       task.Type,
		Scope:      task.Scope,
		ScopeID:    scopeID,
		ScopeName:  task.ScopeName,
		Status:     task.Status,
		Message:    task.Message,
		Error:      task.Error,
		Current:    int64(task.Current),
		Total:      int64(task.Total),
		CanCancel:  task.CanCancel,
		Retryable:  task.Retryable,
		Params:     paramsJSON,
		StartedAt:  task.StartedAt,
		UpdatedAt:  task.UpdatedAt,
		FinishedAt: finishedAt,
		Sequence:   task.Sequence,
	})
}

func (s *SqlStore) ListTasks(ctx context.Context, filters TaskFilters) ([]TaskRecord, error) {
	query := `
		SELECT key, type, scope, scope_id, scope_name, status, message, error,
		       current, total, can_cancel, retryable, params,
		       started_at, updated_at, finished_at, sequence
		FROM tasks
		WHERE 1 = 1
	`
	args := make([]any, 0)
	if filters.Status != "" {
		query += ` AND status = ?`
		args = append(args, filters.Status)
	}
	if filters.Scope != "" {
		query += ` AND scope = ?`
		args = append(args, filters.Scope)
	}
	if filters.Type != "" {
		query += ` AND type = ?`
		args = append(args, filters.Type)
	}
	if filters.ScopeID != nil {
		query += ` AND scope_id = ?`
		args = append(args, *filters.ScopeID)
	}
	if filters.Query != "" {
		query += ` AND lower(key || ' ' || message || ' ' || error) LIKE ?`
		args = append(args, "%"+strings.ToLower(filters.Query)+"%")
	}
	// 这条排序只决定「带 Limit 时取哪一批历史」，任务中心最终呈现的顺序由 api 侧
	// sortTaskStatusesByActivity 合并内存快照后重排——活动任务在内存里恒有一份，不靠这一页捞回来。
	//
	// 主排序键取 sequence 而不是 updated_at：后者由 UpsertTaskRecord（time.Time 参数，存成
	// "2026-05-01 12:00:00 +0000 UTC"）与 MarkInterruptedTasks（CURRENT_TIMESTAMP，存成
	// "2026-08-30 19:01:14"）两个写入方以两种文本格式写入，而 SQLite 比的是文本：同一秒的两种写法
	// 既比不出相等、短的还总是排在前面。sequence 由引擎单调发放（跨重启从 MaxTaskSequence 接着发），
	// 没有这个问题；它作为兜底键留在这里，只在序号相同时才被问到。
	query += ` ORDER BY sequence DESC, updated_at DESC, started_at DESC, key DESC`
	if filters.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, filters.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tasks := make([]TaskRecord, 0)
	for rows.Next() {
		task, err := scanTaskRecord(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return tasks, nil
}

// MaxTaskSequence 返回任务表里已用掉的最大序号，表为空时返回 0。
//
// 序号是任务中心的主排序键，而它由内存里的引擎发放。引擎在装配期从这里接上，序号才跨重启单调——
// 从 0 重来的话，重启后新起的任务序号远小于重启前的历史，会一律排在它们之后。
func (s *SqlStore) MaxTaskSequence(ctx context.Context) (int64, error) {
	var maxSequence int64
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence), 0) FROM tasks`).Scan(&maxSequence); err != nil {
		return 0, err
	}
	return maxSequence, nil
}

func (s *SqlStore) DeleteTasks(ctx context.Context, filters TaskFilters) (int64, error) {
	query := `DELETE FROM tasks WHERE status NOT IN ('running', 'paused', 'cancelling')`
	args := make([]any, 0)
	if filters.Status != "" {
		query += ` AND status = ?`
		args = append(args, filters.Status)
	}
	if filters.Scope != "" {
		query += ` AND scope = ?`
		args = append(args, filters.Scope)
	}
	if filters.Type != "" {
		query += ` AND type = ?`
		args = append(args, filters.Type)
	}
	if filters.ScopeID != nil {
		query += ` AND scope_id = ?`
		args = append(args, *filters.ScopeID)
	}
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

type taskScanner interface {
	Scan(dest ...any) error
}

func scanTaskRecord(row taskScanner) (TaskRecord, error) {
	var (
		task       TaskRecord
		scopeID    sql.NullInt64
		finishedAt sql.NullTime
		paramsJSON string
	)
	err := row.Scan(
		&task.Key,
		&task.Type,
		&task.Scope,
		&scopeID,
		&task.ScopeName,
		&task.Status,
		&task.Message,
		&task.Error,
		&task.Current,
		&task.Total,
		&task.CanCancel,
		&task.Retryable,
		&paramsJSON,
		&task.StartedAt,
		&task.UpdatedAt,
		&finishedAt,
		&task.Sequence,
	)
	if err != nil {
		return TaskRecord{}, err
	}
	if scopeID.Valid {
		task.ScopeID = &scopeID.Int64
	}
	if finishedAt.Valid {
		task.FinishedAt = &finishedAt.Time
	}
	if strings.TrimSpace(paramsJSON) != "" {
		var params map[string]string
		if err := json.Unmarshal([]byte(paramsJSON), &params); err != nil {
			return TaskRecord{}, err
		}
		task.Params = params
	}
	return task, nil
}
