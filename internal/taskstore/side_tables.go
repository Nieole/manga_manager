// 挂在运行上的六样侧数据：上限、重启入参、展示标签、累计指标、**运行事件**与**采样**。
// 它们各有一张表，因此加一个上限字段是改 schema，而不是往一个键值堆里塞键。

package taskstore

import (
	"context"
	"database/sql"

	"manga-manager/internal/task"
)

// SaveRunLimits 记下这次运行实际生效的并发上限。整份覆盖：上限是一次快照，不按键合并。
func (s *Store) SaveRunLimits(ctx context.Context, runID int64, limits task.Limits) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO `+tableRunLimits+` (run_id, scan_profile, scanner_workers_configured,
			scanner_workers_effective, storage_profile, volume_key, scan_concurrency,
			archive_open_concurrency, cover_concurrency, hash_concurrency,
			pause_background_when_reading, idle_only_heavy_tasks, disable_same_disk_page_cache)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, runID, limits.ScanProfile, limits.ScannerWorkersConfigured, limits.ScannerWorkersEffective,
		limits.StorageProfile, limits.VolumeKey, limits.ScanConcurrency, limits.ArchiveOpenConcurrency,
		limits.CoverConcurrency, limits.HashConcurrency, boolToInt(limits.PauseBackgroundWhenReading),
		boolToInt(limits.IdleOnlyHeavyTasks), boolToInt(limits.DisableSameDiskPageCache))
	return err
}

// MergeRunArgs 按键合并重启入参：**重启函数**读回原始入参读的就是它。
func (s *Store) MergeRunArgs(ctx context.Context, runID int64, args map[string]string) error {
	return s.mergeKeyValues(ctx, tableRunArgs, runID, args)
}

// MergeRunLabels 按键合并展示标签。标签自成一张表，不与入参同居一个命名空间。
func (s *Store) MergeRunLabels(ctx context.Context, runID int64, labels map[string]string) error {
	return s.mergeKeyValues(ctx, tableRunLabels, runID, labels)
}

// SetRunMetrics 按键**设**指标：上报方握着全量当前值时走这一路。
func (s *Store) SetRunMetrics(ctx context.Context, runID int64, values map[string]int64) error {
	return s.writeMetrics(ctx, runID, values, `value = excluded.value`)
}

// AddRunMetrics 按键**累加**指标增量：跨资料库的运行收到的每份报文只覆盖其中一个库，
// 全局总量只能加出来。累加落在 SQL 里而不是先读后写，读改写会在两条并发报文之间丢掉一份增量。
func (s *Store) AddRunMetrics(ctx context.Context, runID int64, increments map[string]int64) error {
	return s.writeMetrics(ctx, runID, increments, `value = `+tableRunMetrics+`.value + excluded.value`)
}

// AppendRunEvents 追加**运行事件**。
func (s *Store) AppendRunEvents(ctx context.Context, runID int64, events []task.Event) error {
	rows := make([][]any, 0, len(events))
	for _, event := range events {
		rows = append(rows, []any{runID, millisFromTime(event.At), string(event.Kind), event.Payload})
	}
	return s.execRows(ctx,
		`INSERT INTO `+tableRunEvents+` (run_id, at, kind, payload) VALUES (?, ?, ?, ?)`, rows)
}

// AppendRunSamples 追加**采样**点。
func (s *Store) AppendRunSamples(ctx context.Context, runID int64, samples []task.Sample) error {
	rows := make([][]any, 0, len(samples))
	for _, sample := range samples {
		rows = append(rows, []any{runID, millisFromTime(sample.At), sample.Current, sample.RatePerMinute})
	}
	return s.execRows(ctx,
		`INSERT INTO `+tableRunSamples+` (run_id, at, current, rate_per_minute) VALUES (?, ?, ?, ?)`, rows)
}

// mergeKeyValues 按键写进一张 (run_id, key, value) 侧表，已有的键改值、没有的键新增。
func (s *Store) mergeKeyValues(ctx context.Context, table string, runID int64, values map[string]string) error {
	rows := make([][]any, 0, len(values))
	for key, value := range values {
		rows = append(rows, []any{runID, key, value})
	}
	return s.execRows(ctx, `INSERT INTO `+table+` (run_id, key, value) VALUES (?, ?, ?)
		ON CONFLICT(run_id, key) DO UPDATE SET value = excluded.value`, rows)
}

// writeMetrics 按键写指标，冲突时的处置由调用方给出——设与加的差别只在那一句上。
func (s *Store) writeMetrics(ctx context.Context, runID int64, values map[string]int64, onConflict string) error {
	rows := make([][]any, 0, len(values))
	for key, value := range values {
		rows = append(rows, []any{runID, key, value})
	}
	return s.execRows(ctx, `INSERT INTO `+tableRunMetrics+` (run_id, key, value) VALUES (?, ?, ?)
		ON CONFLICT(run_id, key) DO UPDATE SET `+onConflict, rows)
}

// execRows 在一个事务里预编译一条语句并逐行执行，空批是无操作。
//
// 整批同进同退：半批落地会让重启入参缺几个键，而**重启函数**读到的是一份残缺声明。
func (s *Store) execRows(ctx context.Context, query string, rows [][]any) error {
	if len(rows) == 0 {
		return nil
	}
	return s.execTx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx, query)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, row := range rows {
			if _, err := stmt.ExecContext(ctx, row...); err != nil {
				return err
			}
		}
		return nil
	})
}
