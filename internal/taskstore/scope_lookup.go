package taskstore

import (
	"context"
	"strings"

	"manga-manager/internal/task"
)

// ScopeRef 指向一个作用域对象，是 LastRunKeysForScopes 的查询键与结果键。
//
// 与 task.Identity 的区别在于它只有作用域这一半：调用方问的是「这个资料库上最近跑过什么」，
// 不关心那是扫描还是刮削，也不关心**变体**。
type ScopeRef struct {
	Scope   task.Scope
	ScopeID int64
}

// LastRunKeysForScopes 批量取每个作用域对象上最近一次运行的**任务键**，没跑过的不出现在结果里。
//
// 一条 SQL 里用窗口函数分组取最新，而不是每个作用域发一次单行查询：调用方一次可以问上千个
// 作用域（健康报告就是），逐个单查光是往返开销就让那个请求变成秒级。占位符按批切分，
// 免得撞上 SQLite 的变量数上限（32766）。重复的 scopes 无害，只是多拼一段等价的 OR 条件。
//
// 定序取 sequence 而不是时间列：序号由落盘侧单调发放，同一毫秒内的两条运行也分得出先后。
// 任务键为空的运行被跳过——那一列是**过渡期**列，取回一个空串只会让界面上多一个点不动的按钮。
func (s *Store) LastRunKeysForScopes(ctx context.Context, scopes []ScopeRef) (map[ScopeRef]string, error) {
	latest := make(map[ScopeRef]string, len(scopes))
	if len(scopes) == 0 {
		return latest, nil
	}

	// 每个作用域占 2 个占位符（scope + scope_id），留足余量按 400 个一批。
	const batchSize = 400
	for start := 0; start < len(scopes); start += batchSize {
		end := min(start+batchSize, len(scopes))
		batch := scopes[start:end]

		conditions := make([]string, 0, len(batch))
		args := make([]any, 0, len(batch)*2)
		for _, scope := range batch {
			conditions = append(conditions, "(t.scope = ? AND t.scope_id = ?)")
			args = append(args, string(scope.Scope), scope.ScopeID)
		}

		rows, err := s.db.QueryContext(ctx, `
			SELECT scope, scope_id, task_key FROM (
				SELECT t.scope AS scope, t.scope_id AS scope_id, r.task_key AS task_key,
					ROW_NUMBER() OVER (PARTITION BY t.scope, t.scope_id ORDER BY r.sequence DESC, r.id DESC) AS rn
				FROM `+tableRuns+` r
				JOIN `+tableTasks+` t ON t.id = r.task_id
				WHERE r.task_key != '' AND (`+strings.Join(conditions, " OR ")+`)
			) WHERE rn = 1`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var (
				scope   string
				scopeID int64
				taskKey string
			)
			if err := rows.Scan(&scope, &scopeID, &taskKey); err != nil {
				_ = rows.Close()
				return nil, err
			}
			latest[ScopeRef{Scope: task.Scope(scope), ScopeID: scopeID}] = taskKey
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return latest, nil
}
