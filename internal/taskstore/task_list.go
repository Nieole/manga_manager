// 任务清单那一半的取数：一个任务一行，外加「这个任务最近一次运行」这句相关子查询。
// 一次执行一行的那一半在 runs.go，身份的读写在 store.go。

package taskstore

import (
	"context"
	"database/sql"
	"strings"

	"manga-manager/internal/task"
)

// taskReadColumns 是身份行上除主键外要读回的列，LoadTasks 与 ListTasks 共用这一份。
// 两条路各抄一遍的话，漏一列不会有编译错误，后果是那个属性在其中一条路上静默变成零值。
// scanTask 必须按同样的次序排列，两者与本表一起改。
var taskReadColumns = []string{"type", "scope", "scope_id", "variant", "disabled", "last_success_at", "fail_streak", "backoff_until"}

// taskSelectColumns 拼出带表别名的取数列。别名为空串时不加前缀。
func taskSelectColumns(alias string) string {
	if alias != "" {
		alias += "."
	}
	columns := make([]string, 0, len(taskReadColumns)+1)
	columns = append(columns, alias+"id")
	for _, column := range taskReadColumns {
		columns = append(columns, alias+column)
	}
	return strings.Join(columns, ", ")
}

// latestRunIDClause 是「这个任务最近一次运行的 id」，`taskID` 给出外层那一句里任务 id 的表达式
// （身份表那边是 `t.id`，运行表自连那边是 `r.task_id`）。
//
// 判序号而不是时间列：序号由引擎在临界区里单调发放，每一次会被用户看见的变化都取一个，
// 因此同一个任务上活着的那条恒排在它自己的历史之前。序号相等时按 id 兜底，理由见 ListRuns。
func latestRunIDClause(taskID string) string {
	return `(SELECT id FROM ` + tableRuns + ` WHERE task_id = ` + taskID + ` ORDER BY sequence DESC, id DESC LIMIT 1)`
}

// ListTasks 按谓词取任务清单。
//
// 末次运行经 LEFT JOIN 接上：它既是定序的依据，也是「上次结果」那两条谓词判的对象。
// 左连而不是内连——一次运行都没有的任务仍在清单上，只是排在最后、且不满足任何一条末次谓词。
func (s *Store) ListTasks(ctx context.Context, filter task.TaskFilter) ([]task.Task, error) {
	where, args := taskFilterClause(filter)
	query := `SELECT ` + taskSelectColumns("t") + `
		FROM ` + tableTasks + ` t
		LEFT JOIN ` + tableRuns + ` r ON r.id = ` + latestRunIDClause("t.id") + where + `
		ORDER BY r.sequence DESC, r.id DESC, t.id DESC`
	if filter.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, filter.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	owners := make([]task.Task, 0)
	for rows.Next() {
		owner, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		owners = append(owners, owner)
	}
	return owners, rows.Err()
}

// LatestRuns 批量取这批任务各自最近一次运行；一次运行都没有的任务不出现在结果里。
func (s *Store) LatestRuns(ctx context.Context, taskIDs []int64) (map[int64]task.Run, error) {
	latest := make(map[int64]task.Run, len(taskIDs))
	if len(taskIDs) == 0 {
		return latest, nil
	}
	placeholders, args := int64Placeholders(taskIDs)

	rows, err := s.db.QueryContext(ctx, `
		SELECT `+runSelectColumns+` FROM `+tableRuns+` r
		WHERE r.task_id IN (`+placeholders+`) AND r.id = `+latestRunIDClause("r.task_id"), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		latest[run.TaskID] = run
	}
	return latest, rows.Err()
}

// taskFilterClause 把任务清单的谓词拼成 WHERE 子句。零值谓词不筛，因此返回空串。
//
// 身份三项判在 t 上、末次两项判在 r 上：左连之下 r 的列对没有运行的任务是 NULL，
// 而 NULL 不满足 IN 与 LIKE——「没有上次」因此天然被状态与关键词筛掉，不必另写一句。
func taskFilterClause(filter task.TaskFilter) (string, []any) {
	clauses := make([]string, 0, 5)
	args := make([]any, 0, len(filter.Types)+len(filter.LastRunStatuses)+3)
	if len(filter.Types) > 0 {
		placeholders := make([]string, 0, len(filter.Types))
		for _, taskType := range filter.Types {
			placeholders = append(placeholders, "?")
			args = append(args, string(taskType))
		}
		clauses = append(clauses, `t.type IN (`+strings.Join(placeholders, ", ")+`)`)
	}
	if filter.Scope != "" {
		clauses = append(clauses, `t.scope = ?`)
		args = append(args, string(filter.Scope))
	}
	if filter.ScopeID != nil {
		clauses = append(clauses, `t.scope_id = ?`)
		args = append(args, *filter.ScopeID)
	}
	if len(filter.LastRunStatuses) > 0 {
		placeholders := make([]string, 0, len(filter.LastRunStatuses))
		for _, status := range filter.LastRunStatuses {
			placeholders = append(placeholders, "?")
			args = append(args, string(status))
		}
		clauses = append(clauses, `r.status IN (`+strings.Join(placeholders, ", ")+`)`)
	}
	if filter.LastRunQuery != "" {
		// 与运行列表同口径：键、文案码与错误串接起来做大小写无关的子串匹配。
		clauses = append(clauses, `LOWER(r.task_key || ' ' || r.message_code || ' ' || r.error) LIKE ?`)
		args = append(args, "%"+strings.ToLower(filter.LastRunQuery)+"%")
	}
	if len(clauses) == 0 {
		return "", args
	}
	return ` WHERE ` + strings.Join(clauses, " AND "), args
}

// scanTask 解一行身份，列序与 taskSelectColumns 一致。
func scanTask(row rowScanner) (task.Task, error) {
	var (
		owner         task.Task
		taskType      string
		scope         string
		variant       string
		disabled      int
		lastSuccessAt sql.NullInt64
		backoffUntil  sql.NullInt64
	)
	if err := row.Scan(&owner.ID, &taskType, &scope, &owner.ScopeID, &variant, &disabled,
		&lastSuccessAt, &owner.FailStreak, &backoffUntil); err != nil {
		return task.Task{}, err
	}
	owner.Type = task.Type(taskType)
	owner.Scope = task.Scope(scope)
	owner.Variant = task.Variant(variant)
	owner.Disabled = disabled != 0
	owner.LastSuccessAt = timePtrFromMillis(lastSuccessAt)
	owner.BackoffUntil = timePtrFromMillis(backoffUntil)
	return owner, nil
}
