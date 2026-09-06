// 运行那一半：一次执行一行的读写，以及把两条部分唯一索引的违例翻成领域的准入哨兵。

package taskstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	sqlite "modernc.org/sqlite"

	"manga-manager/internal/task"
)

// sqliteConstraintUnique 是 SQLITE_CONSTRAINT_UNIQUE 的扩展结果码。
//
// 判据取错误码而不是错误串：驱动给出的文案只点到 runs.task_id，两条部分唯一索引在里面长得一模一样。
const sqliteConstraintUnique = 2067

// runWriteColumns 是运行行上除主键外的全部列，取数、插入与回写共用这一份。
//
// 三条路各抄一遍列名的话，漏一列不会有编译错误，后果是那个字段静默丢在写入或读回的路上。
// runValues 与 scanRun 必须按同样的次序排列，两者与本表一起改。
var runWriteColumns = []string{
	"task_id", "task_key", "scope_name", "trigger", "nth_run", "status", "phase", "current_item", "current", "total",
	"paused_at", "pause_reason", "control_paused_ms", "coalesced_count", "message_code", "message_params",
	"error", "started_at", "updated_at", "finished_at", "sequence",
}

var (
	runSelectColumns = "id, " + strings.Join(runWriteColumns, ", ")
	runInsertColumns = strings.Join(runWriteColumns, ", ")
	runPlaceholders  = strings.TrimSuffix(strings.Repeat("?, ", len(runWriteColumns)), ", ")
	runAssignments   = strings.Join(runWriteColumns, " = ?, ") + " = ?"
)

// runValues 把一条运行摊成与 runWriteColumns 同序的实参。
func runValues(run task.Run, messageParams string) []any {
	return []any{
		run.TaskID, run.Key, run.ScopeName, string(run.Trigger), run.NthRun, string(run.Status), run.Phase, run.CurrentItem,
		run.Current, run.Total, millisFromTimePtr(run.PausedAt), string(run.PauseReason), run.ControlPausedMillis,
		run.CoalescedCount, run.MessageCode, messageParams, run.Error, millisFromTime(run.StartedAt),
		millisFromTime(run.UpdatedAt), millisFromTimePtr(run.FinishedAt), run.Sequence,
	}
}

// CreateRun 落一条新运行并回填 id。
//
// **准入由数据库保证**：写活动态时撞上 idx_runs_one_active_per_task 返回 task.ErrRunAlreadyActive，
// 写**排队中**时撞上 idx_runs_one_queued_per_task 返回 task.ErrRunAlreadyQueued。
func (s *Store) CreateRun(ctx context.Context, run task.Run) (task.Run, error) {
	params, err := encodeMessageParams(run.MessageParams)
	if err != nil {
		return task.Run{}, err
	}
	result, err := s.db.ExecContext(ctx,
		`INSERT INTO `+tableRuns+` (`+runInsertColumns+`) VALUES (`+runPlaceholders+`)`,
		runValues(run, params)...)
	if err != nil {
		return task.Run{}, admissionError(err, run.Status)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return task.Run{}, err
	}
	run.ID = id
	return run, nil
}

// SaveRun 覆盖写一条已存在的运行；运行不存在返回 task.ErrRunNotFound。
//
// 状态跃迁同样过那两条索引：把一条**排队中**的运行改成运行中，而这个任务已经有一条活动运行时，
// 拿到的是 task.ErrRunAlreadyActive——队列放行因此不必自己再判一次。
func (s *Store) SaveRun(ctx context.Context, run task.Run) error {
	params, err := encodeMessageParams(run.MessageParams)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx,
		`UPDATE `+tableRuns+` SET `+runAssignments+` WHERE id = ?`,
		append(runValues(run, params), run.ID)...)
	if err != nil {
		return admissionError(err, run.Status)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return task.ErrRunNotFound
	}
	return nil
}

// LoadRun 按 id 取一条运行；不存在返回 task.ErrRunNotFound。
func (s *Store) LoadRun(ctx context.Context, runID int64) (task.Run, error) {
	run, err := scanRun(s.db.QueryRowContext(ctx, `SELECT `+runSelectColumns+` FROM `+tableRuns+` WHERE id = ?`, runID))
	if errors.Is(err, sql.ErrNoRows) {
		return task.Run{}, task.ErrRunNotFound
	}
	if err != nil {
		return task.Run{}, err
	}
	return run, nil
}

// ListRuns 按谓词取运行，定序由 filter.Order 指定。
//
// 序号相等时按 id 兜底：序号由引擎在临界区内发放，跨重启接续，但库里可以躺着一批序号为零的
// 历史行，没有兜底键时它们在两次查询之间的相对次序是 SQLite 说了算。
func (s *Store) ListRuns(ctx context.Context, filter task.RunFilter) ([]task.Run, error) {
	where, args := runFilterClause(filter)
	query := `SELECT ` + runSelectColumns + ` FROM ` + tableRuns + where + orderClause(filter.Order)
	if filter.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, filter.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	runs := make([]task.Run, 0)
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

// CountRuns 数满足谓词的运行条数。条数不受 filter.Limit 约束：槽位占用问的是「一共有几条」。
func (s *Store) CountRuns(ctx context.Context, filter task.RunFilter) (int, error) {
	where, args := runFilterClause(filter)
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+tableRuns+where, args...).Scan(&count)
	return count, err
}

// MaxRunSequence 返回已用掉的最大序号，一条运行都没有时返回 0。
func (s *Store) MaxRunSequence(ctx context.Context) (int64, error) {
	var highest int64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence), 0) FROM `+tableRuns).Scan(&highest)
	return highest, err
}

// MaxNthRun 返回这个任务用掉的最大「第几次」，没有任何运行时返回 0。
//
// 取最大值而不是数行数：保留裁剪会删掉早先那几条**终态**运行，按行数算的下一次运行会与历史行撞号。
func (s *Store) MaxNthRun(ctx context.Context, taskID int64) (int, error) {
	var highest int
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(nth_run), 0) FROM `+tableRuns+` WHERE task_id = ?`, taskID).Scan(&highest)
	return highest, err
}

// rowScanner 抹平 QueryRow 与 Rows 的差别，让行的解码只写一份。
type rowScanner interface {
	Scan(dest ...any) error
}

func scanRun(row rowScanner) (task.Run, error) {
	var (
		run         task.Run
		trigger     string
		status      string
		pauseReason string
		params      string
		pausedAt    sql.NullInt64
		startedAt   sql.NullInt64
		updatedAt   sql.NullInt64
		finishedAt  sql.NullInt64
	)
	err := row.Scan(&run.ID, &run.TaskID, &run.Key, &run.ScopeName, &trigger, &run.NthRun, &status, &run.Phase, &run.CurrentItem,
		&run.Current, &run.Total, &pausedAt, &pauseReason, &run.ControlPausedMillis, &run.CoalescedCount,
		&run.MessageCode, &params, &run.Error, &startedAt, &updatedAt, &finishedAt, &run.Sequence)
	if err != nil {
		return task.Run{}, err
	}
	run.Trigger = task.Trigger(trigger)
	run.Status = task.RunStatus(status)
	run.PauseReason = task.PauseReason(pauseReason)
	run.MessageParams, err = decodeMessageParams(params)
	if err != nil {
		return task.Run{}, err
	}
	run.PausedAt = timePtrFromMillis(pausedAt)
	run.StartedAt = timeFromMillis(startedAt)
	run.UpdatedAt = timeFromMillis(updatedAt)
	run.FinishedAt = timePtrFromMillis(finishedAt)
	return run, nil
}

// runFilterClause 把谓词拼成 WHERE 子句。零值谓词不筛，因此返回空串。
//
// 身份那三项判在一句 `task_id IN (SELECT …)` 里而不是连表：ListRuns、CountRuns 与 DeleteRuns
// 共用这一份谓词，其中 DELETE 在 SQLite 里根本连不了表。子查询命中的是身份表那条四列唯一索引。
func runFilterClause(filter task.RunFilter) (string, []any) {
	clauses := make([]string, 0, 5)
	args := make([]any, 0, len(filter.Statuses)+len(filter.Types)+4)
	if filter.TaskID != 0 {
		clauses = append(clauses, `task_id = ?`)
		args = append(args, filter.TaskID)
	}
	if len(filter.Statuses) > 0 {
		placeholders, statusArgs := stringPlaceholders(filter.Statuses)
		clauses = append(clauses, `status IN (`+placeholders+`)`)
		args = append(args, statusArgs...)
	}
	if filter.Key != "" {
		clauses = append(clauses, `task_key = ?`)
		args = append(args, filter.Key)
	}
	if filter.Query != "" {
		// 与旧引擎同口径：键、文案码与错误串接起来做大小写无关的子串匹配。
		// LOWER 只作用于 ASCII，而这三样都是本仓自己生成的标识串，不含大小写敏感的非 ASCII。
		clauses = append(clauses, `LOWER(task_key || ' ' || message_code || ' ' || error) LIKE ?`)
		args = append(args, "%"+strings.ToLower(filter.Query)+"%")
	}
	if identity, identityArgs := identityClause(filter); identity != "" {
		clauses = append(clauses, identity)
		args = append(args, identityArgs...)
	}
	if len(clauses) == 0 {
		return "", args
	}
	return ` WHERE ` + strings.Join(clauses, " AND "), args
}

// identityClause 把身份那三项谓词拼成一句对身份表的子查询；一项都没给时返回空串。
func identityClause(filter task.RunFilter) (string, []any) {
	clauses := make([]string, 0, 3)
	args := make([]any, 0, len(filter.Types)+2)
	if len(filter.Types) > 0 {
		placeholders, typeArgs := stringPlaceholders(filter.Types)
		clauses = append(clauses, `type IN (`+placeholders+`)`)
		args = append(args, typeArgs...)
	}
	if filter.Scope != "" {
		clauses = append(clauses, `scope = ?`)
		args = append(args, string(filter.Scope))
	}
	if filter.ScopeID != nil {
		clauses = append(clauses, `scope_id = ?`)
		args = append(args, *filter.ScopeID)
	}
	if len(clauses) == 0 {
		return "", nil
	}
	return `task_id IN (SELECT id FROM ` + tableTasks + ` WHERE ` + strings.Join(clauses, " AND ") + `)`, args
}

// orderClause 给出定序。序号相等时按 id 兜底，理由见 ListRuns。
func orderClause(order task.RunOrder) string {
	switch order {
	case task.OrderLiveFirst:
		// 仍会变化的排在最前，其后与 OrderSequenceDesc 一致。CASE 而不是 `status IN (…) DESC`：
		// 后者在 SQLite 里也成立，但把「1 在前」这件事写明白，读的人不必去想布尔怎么排。
		return ` ORDER BY CASE WHEN status IN (` + quotedStatuses(task.LiveStatuses()) +
			`) THEN 0 ELSE 1 END, sequence DESC, id DESC`
	case task.OrderSequenceDesc:
		return ` ORDER BY sequence DESC, id DESC`
	default:
		return ` ORDER BY sequence ASC, id ASC`
	}
}

// DeleteRuns 按谓词删除运行；**仍会变化的运行永不被删**，这一条写死在谓词里，不经调用方。
func (s *Store) DeleteRuns(ctx context.Context, filter task.RunFilter) (int64, error) {
	where, args := runFilterClause(filter)
	if where == "" {
		where = ` WHERE `
	} else {
		where += ` AND `
	}
	where += `status NOT IN (` + quotedStatuses(task.LiveStatuses()) + `)`

	outcome, err := s.db.ExecContext(ctx, `DELETE FROM `+tableRuns+where, args...)
	if err != nil {
		return 0, err
	}
	return outcome.RowsAffected()
}

// admissionError 把唯一约束的违例翻成领域的准入哨兵。
//
// 认的是**正在写入的状态**而不是索引名：驱动的错误串只报到列，两条索引在里面无从分辨。
// runs 上只有这两条部分唯一索引，而它们的谓词互斥——写活动态只可能撞前一条，写排队中只可能撞后一条。
func admissionError(err error, status task.RunStatus) error {
	var sqliteErr *sqlite.Error
	if !errors.As(err, &sqliteErr) || sqliteErr.Code() != sqliteConstraintUnique {
		return err
	}
	switch {
	case status.IsActive():
		return task.ErrRunAlreadyActive
	case status == task.StatusQueued:
		return task.ErrRunAlreadyQueued
	default:
		return err
	}
}

// encodeMessageParams 把 i18n 占位参数编成随 message_code 同行落地的一列。
//
// 这一列不是被 ADR 0004 否掉的那个 JSON 堆：那个堆把五类语义挤进一个命名空间，而这里只装一类，
// 随它的 message_code 一起整份改写，也从不参与查询与聚合——指标、上限、入参、标签各有自己的表。
func encodeMessageParams(params map[string]string) (string, error) {
	if len(params) == 0 {
		return "", nil
	}
	encoded, err := json.Marshal(params)
	if err != nil {
		return "", fmt.Errorf("taskstore: 编码文案参数失败: %w", err)
	}
	return string(encoded), nil
}

func decodeMessageParams(encoded string) (map[string]string, error) {
	if encoded == "" {
		return nil, nil
	}
	params := make(map[string]string)
	if err := json.Unmarshal([]byte(encoded), &params); err != nil {
		return nil, fmt.Errorf("taskstore: 解码文案参数失败: %w", err)
	}
	return params, nil
}
