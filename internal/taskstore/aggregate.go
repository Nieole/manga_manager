// 聚合查询：指标进了自己的表、耗时是运行行上的真列，因此「这个任务最近十次跑成什么样」
// 是一句 SQL，而不是一段把行读回内存再循环的 Go。

package taskstore

import (
	"context"
	"time"
)

// AverageRunDuration 返回这个任务**最近 lastN 次运行**里已收尾那几条的平均耗时，以及参与平均的条数。
//
// 窗口先取后筛，不是筛完再取：**排队中**与还在跑的那几条没有收尾时刻，算不出耗时，
// 但它们确实占掉窗口里的名额——先筛的话「最近十次」会悄悄够到第十一条更老的运行，
// 而调用方看到的是一个它以为只覆盖近十次的平均值。条数为 0 时耗时为 0，
// 调用方据此分辨「跑得快」与「没跑完过」。
func (s *Store) AverageRunDuration(ctx context.Context, taskID int64, lastN int) (time.Duration, int, error) {
	if lastN <= 0 {
		return 0, 0, nil
	}
	var (
		counted int
		average float64
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(AVG(finished_at - started_at), 0)
		FROM (
			SELECT started_at, finished_at FROM `+tableRuns+`
			WHERE task_id = ?
			ORDER BY sequence DESC, id DESC
			LIMIT ?
		)
		WHERE started_at IS NOT NULL AND finished_at IS NOT NULL
	`, taskID, lastN).Scan(&counted, &average)
	if err != nil {
		return 0, 0, err
	}
	return time.Duration(average) * time.Millisecond, counted, nil
}

// SumRunMetrics 把这个任务最近 lastN 次运行的指标按键加总，没有指标时返回空表。
func (s *Store) SumRunMetrics(ctx context.Context, taskID int64, lastN int) (map[string]int64, error) {
	totals := make(map[string]int64)
	if lastN <= 0 {
		return totals, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT key, SUM(value) FROM `+tableRunMetrics+`
		WHERE run_id IN (
			SELECT id FROM `+tableRuns+` WHERE task_id = ? ORDER BY sequence DESC, id DESC LIMIT ?
		)
		GROUP BY key
	`, taskID, lastN)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			key   string
			total int64
		)
		if err := rows.Scan(&key, &total); err != nil {
			return nil, err
		}
		totals[key] = total
	}
	return totals, rows.Err()
}
