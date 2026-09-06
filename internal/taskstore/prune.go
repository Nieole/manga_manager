// 分层保留的裁剪。**活动态与排队中的运行永不被选中**，这一条写死在每条谓词里，不经策略。

package taskstore

import (
	"context"
	"database/sql"
	"time"

	"manga-manager/internal/task"
)

// PruneRuns 按分层保留裁剪历史，返回各层清掉的行数。
//
// 三层各跑一遍且先后有序：先按条数留下每个任务最近的那几条**终态**运行，再按时长清掉更老的，
// 最后单独清**采样**——采样比运行留得短，因此运行还在、曲线没了。前两层删的是运行行本身，
// 事件与采样随外键级联而去，因此调用方的连接必须开着 foreign_keys（Migrate 已拦过一道）。
//
// 阈值取零或负数表示这一层不裁剪：负数照面值算会让谓词选中全部终态运行（留 -1 条、
// 截止时刻落在未来），而阈值一路来自手写的配置文件。策略里没有「要不要保护活动运行」这一项：
// 那不是策略而是前提。
func (s *Store) PruneRuns(ctx context.Context, policy task.RetentionPolicy) (task.PruneResult, error) {
	notLive := `status NOT IN (` + quotedStatuses(task.LiveStatuses()) + `)`
	// 排名按序号倒序：序号是唯一的排序主键，时间列不是。
	beyondQuota := `SELECT id FROM (
		SELECT id, ROW_NUMBER() OVER (PARTITION BY task_id ORDER BY sequence DESC, id DESC) AS rn
		FROM ` + tableRuns + ` WHERE ` + notLive + `
	) WHERE rn > ?`
	// 终态运行的年龄取收尾时刻，没有收尾时刻的退回最后一次心跳。两者都没有的算无穷老：
	// 一条既没收尾时刻也没心跳的终态运行没有任何可留的信息，而 NULL 比不出大小，
	// 不给它兜底就等于让它永远躺在库里，连清理运行都报不出它。
	tooOld := `SELECT id FROM ` + tableRuns + ` WHERE ` + notLive + `
		AND COALESCE(finished_at, updated_at, 0) < ?`

	now := time.Now()
	var result task.PruneResult
	err := s.execTx(ctx, func(tx *sql.Tx) error {
		if policy.RunsPerTask > 0 {
			if err := deleteRuns(ctx, tx, beyondQuota, &result, policy.RunsPerTask); err != nil {
				return err
			}
		}
		if policy.TerminalAge > 0 {
			cutoff := now.Add(-policy.TerminalAge).UnixMilli()
			if err := deleteRuns(ctx, tx, tooOld, &result, cutoff); err != nil {
				return err
			}
		}
		if policy.SampleAge > 0 {
			cutoff := now.Add(-policy.SampleAge).UnixMilli()
			outcome, err := tx.ExecContext(ctx, `DELETE FROM `+tableRunSamples+` WHERE at < ?`, cutoff)
			if err != nil {
				return err
			}
			removed, err := outcome.RowsAffected()
			if err != nil {
				return err
			}
			result.Samples += removed
		}
		return nil
	})
	if err != nil {
		return task.PruneResult{}, err
	}
	return result, nil
}

// deleteRuns 删掉 doomedQuery 这句子查询选中的运行，并把随之级联而去的事件与采样计进结果。
//
// 子行先数后删：级联删除不报行数，删完再数只会数到 0，而清理运行要报出「清了多少」。
func deleteRuns(ctx context.Context, tx *sql.Tx, doomedQuery string, result *task.PruneResult, args ...any) error {
	for _, child := range []struct {
		table string
		count *int64
	}{
		{table: tableRunEvents, count: &result.Events},
		{table: tableRunSamples, count: &result.Samples},
	} {
		var doomedChildren int64
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM `+child.table+` WHERE run_id IN (`+doomedQuery+`)`, args...,
		).Scan(&doomedChildren); err != nil {
			return err
		}
		*child.count += doomedChildren
	}

	outcome, err := tx.ExecContext(ctx, `DELETE FROM `+tableRuns+` WHERE id IN (`+doomedQuery+`)`, args...)
	if err != nil {
		return err
	}
	removed, err := outcome.RowsAffected()
	if err != nil {
		return err
	}
	result.Runs += removed
	return nil
}
