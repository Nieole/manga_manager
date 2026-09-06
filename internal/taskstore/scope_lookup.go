// 按作用域对象反查运行：健康报告要在每条问题旁边挂一个「去看这个库/系列最近那次任务的日志」。

package taskstore

import (
	"context"
	"strings"
)

// ScopeRef 指向一个作用域对象，是 LastRunKeysForScopes 的查询键与结果键。
//
// 与 task.Identity 的区别在于它只有作用域这一半：健康报告问的是「这个资料库上最近跑过什么」，
// 不关心那是扫描还是刮削，也不关心**变体**。
type ScopeRef struct {
	Scope   string
	ScopeID int64
}

// LastRunKeysForScopes 批量取每个作用域对象上最近一次运行的**任务键**，没跑过的不出现在结果里。
//
// 一条 SQL 里用窗口函数分组取最新，而不是每个作用域发一次单行查询：健康报告一次最多带回上千条
// 问题，逐条单查光是往返开销就让整个报告变成秒级请求。占位符按批切分，免得撞上 SQLite 的
// 变量数上限（32766）。
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
			args = append(args, scope.Scope, scope.ScopeID)
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
				ref     ScopeRef
				taskKey string
			)
			if err := rows.Scan(&ref.Scope, &ref.ScopeID, &taskKey); err != nil {
				_ = rows.Close()
				return nil, err
			}
			latest[ref] = taskKey
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
