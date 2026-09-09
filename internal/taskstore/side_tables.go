// 挂在运行上的六样侧数据：上限、重启入参、展示标签、累计指标、**运行事件**与**采样**。
// 它们各有一张表，因此加一个上限字段是改 schema，而不是往一个键值堆里塞键。

package taskstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strconv"

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

// eventPayload 是**运行事件**那几格有值字段在 payload 那一列里的编码。
//
// 编码归适配器，不归领域：领域那边是一组具名字段（哪个文件、什么原因、哪个动作），
// 表这边只有一列 TEXT。字段名一律短且稳定——它写进的是用户库里的历史行，改名等于把
// 已经落盘的那些事件读成空白。
type eventPayload struct {
	Phase  string `json:"phase,omitempty"`
	Item   string `json:"item,omitempty"`
	Reason string `json:"reason,omitempty"`
	Action string `json:"action,omitempty"`
	Code   string `json:"code,omitempty"`
	Detail string `json:"detail,omitempty"`
	Count  int64  `json:"count,omitempty"`
}

// AppendRunEvents 追加**运行事件**。
func (s *Store) AppendRunEvents(ctx context.Context, runID int64, events []task.Event) error {
	rows := make([][]any, 0, len(events))
	for _, event := range events {
		payload, err := json.Marshal(eventPayload{
			Phase:  event.Phase,
			Item:   event.Item,
			Reason: event.Reason,
			Action: string(event.Action),
			Code:   event.Code,
			Detail: event.Detail,
			Count:  event.Count,
		})
		if err != nil {
			return fmt.Errorf("taskstore: 编码运行事件失败: %w", err)
		}
		rows = append(rows, []any{runID, millisFromTime(event.At), string(event.Kind), string(payload)})
	}
	return s.execRows(ctx,
		`INSERT INTO `+tableRunEvents+` (run_id, at, kind, payload) VALUES (?, ?, ?, ?)`, rows)
}

// ListRunEvents 按**发生顺序**取一条运行的运行事件；limit 小于等于 0 表示不限条数。
//
// 定序取自增主键而不是 at：同一毫秒里落下的两条事件在时间列上分不出先后，而阶段时间线正是
// 靠相邻两条相减得出的——乱序读回会算出一段负的耗时。自增主键与插入顺序恒一致。
//
// 读不回的 payload 不中断整趟：那一条降级成只剩种类与时刻，别的照常交出去。
// 整份报错的话，一行写坏的历史就能让详情页从此打不开。
func (s *Store) ListRunEvents(ctx context.Context, runID int64, limit int) ([]task.Event, error) {
	query := `SELECT at, kind, payload FROM ` + tableRunEvents + ` WHERE run_id = ? ORDER BY id ASC`
	args := []any{runID}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := make([]task.Event, 0, 16)
	for rows.Next() {
		var (
			at      sql.NullInt64
			kind    string
			payload string
		)
		if err := rows.Scan(&at, &kind, &payload); err != nil {
			return nil, err
		}
		event := task.Event{At: timeFromMillis(at), Kind: task.EventKind(kind)}
		var decoded eventPayload
		if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
			slog.WarnContext(ctx, "Failed to decode a run event payload", "run_id", runID, "kind", kind, "error", err)
			events = append(events, event)
			continue
		}
		event.Phase = decoded.Phase
		event.Item = decoded.Item
		event.Reason = decoded.Reason
		event.Action = task.ControlAction(decoded.Action)
		event.Code = decoded.Code
		event.Detail = decoded.Detail
		event.Count = decoded.Count
		events = append(events, event)
	}
	return events, rows.Err()
}

// AppendRunSamples 追加**采样**点。
func (s *Store) AppendRunSamples(ctx context.Context, runID int64, samples []task.Sample) error {
	rows := make([][]any, 0, len(samples))
	for _, sample := range samples {
		rows = append(rows, []any{runID, millisFromTime(sample.At), sample.Current, sample.ThroughputPerMinute})
	}
	return s.execRows(ctx,
		`INSERT INTO `+tableRunSamples+` (run_id, at, current, throughput_per_minute) VALUES (?, ?, ?, ?)`, rows)
}

// ListRunSamples 按时刻升序取一条运行的**采样**点；limit 大于 0 时只取**最近的**那几个。
//
// 取最近的一段是先用 `ORDER BY id DESC LIMIT ?` 选出来、再倒回时间顺序：曲线要按时刻画，
// 而截断该留的是最新的那一段——掐掉最近的一段等于把「它此刻是不是卡住了」这个问题掐掉。
//
// 定序按自增主键而不是 at：同一毫秒里落下的两个点用 at 分不出先后，而 at 落盘就是毫秒。
func (s *Store) ListRunSamples(ctx context.Context, runID int64, limit int) ([]task.Sample, error) {
	const columns = `SELECT at, current, throughput_per_minute FROM ` + tableRunSamples + ` WHERE run_id = ? `
	query, args := columns+`ORDER BY id ASC`, []any{runID}
	if limit > 0 {
		query, args = columns+`ORDER BY id DESC LIMIT ?`, []any{runID, limit}
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var samples []task.Sample
	for rows.Next() {
		var (
			at     sql.NullInt64
			sample task.Sample
		)
		if err := rows.Scan(&at, &sample.Current, &sample.ThroughputPerMinute); err != nil {
			return nil, err
		}
		sample.At = timeFromMillis(at)
		samples = append(samples, sample)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if limit > 0 {
		slices.Reverse(samples)
	}
	return samples, nil
}

// LoadRunSideData 批量读回这批运行的侧数据：三张键值表各一句查询，上限那张再一句。
//
// 四句而不是一句连表：侧表与运行是一对多，连成一句会把行数乘起来，读回时还要自己去重。
// 一页运行走四句是常数次查询，逐条运行取四样才是 N+1。
func (s *Store) LoadRunSideData(ctx context.Context, runIDs []int64) (map[int64]task.SideData, error) {
	side := make(map[int64]task.SideData, len(runIDs))
	if len(runIDs) == 0 {
		return side, nil
	}
	placeholders, args := int64Placeholders(runIDs)

	// 三张键值表的取数形状完全相同，只差「这一格放进侧数据的哪个字段」。
	for _, source := range []struct {
		table  string
		assign func(data *task.SideData, key, value string) error
	}{
		{tableRunArgs, func(data *task.SideData, key, value string) error {
			if data.Args == nil {
				data.Args = map[string]string{}
			}
			data.Args[key] = value
			return nil
		}},
		{tableRunLabels, func(data *task.SideData, key, value string) error {
			if data.Labels == nil {
				data.Labels = map[string]string{}
			}
			data.Labels[key] = value
			return nil
		}},
		{tableRunMetrics, func(data *task.SideData, key, value string) error {
			parsed, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return fmt.Errorf("taskstore: 指标 %q 不是整数: %w", key, err)
			}
			if data.Metrics == nil {
				data.Metrics = map[string]int64{}
			}
			data.Metrics[key] = parsed
			return nil
		}},
	} {
		if err := s.scanKeyValues(ctx, source.table, placeholders, args, side, source.assign); err != nil {
			return nil, err
		}
	}
	if err := s.scanLimits(ctx, placeholders, args, side); err != nil {
		return nil, err
	}
	return side, nil
}

// scanKeyValues 把一张 (run_id, key, value) 侧表读进这批运行的侧数据，值怎么落由调用方给出。
//
// 值一律扫成字符串、由 assign 决定怎么解释：三张表在 SQL 那一侧长得一模一样，差别只在指标那张
// 要把它解析成整数。分开写三份的话，加一张侧表就是再抄一遍同样的循环。
func (s *Store) scanKeyValues(ctx context.Context, table, placeholders string, args []any,
	side map[int64]task.SideData, assign func(*task.SideData, string, string) error) error {
	rows, err := s.db.QueryContext(ctx,
		`SELECT run_id, key, value FROM `+table+` WHERE run_id IN (`+placeholders+`)`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			runID int64
			key   string
			value string
		)
		if err := rows.Scan(&runID, &key, &value); err != nil {
			return err
		}
		data := side[runID]
		if err := assign(&data, key, value); err != nil {
			return err
		}
		side[runID] = data
	}
	return rows.Err()
}

func (s *Store) scanLimits(ctx context.Context, placeholders string, args []any, side map[int64]task.SideData) error {
	rows, err := s.db.QueryContext(ctx, `
		SELECT run_id, scan_profile, scanner_workers_configured, scanner_workers_effective,
			storage_profile, volume_key, scan_concurrency, archive_open_concurrency,
			cover_concurrency, hash_concurrency, pause_background_when_reading,
			idle_only_heavy_tasks, disable_same_disk_page_cache
		FROM `+tableRunLimits+` WHERE run_id IN (`+placeholders+`)`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			runID          int64
			limits         task.Limits
			pauseOnReading int
			idleOnly       int
			disableCache   int
		)
		if err := rows.Scan(&runID, &limits.ScanProfile, &limits.ScannerWorkersConfigured,
			&limits.ScannerWorkersEffective, &limits.StorageProfile, &limits.VolumeKey,
			&limits.ScanConcurrency, &limits.ArchiveOpenConcurrency, &limits.CoverConcurrency,
			&limits.HashConcurrency, &pauseOnReading, &idleOnly, &disableCache); err != nil {
			return err
		}
		limits.PauseBackgroundWhenReading = pauseOnReading != 0
		limits.IdleOnlyHeavyTasks = idleOnly != 0
		limits.DisableSameDiskPageCache = disableCache != 0
		data := side[runID]
		// 取本轮这一份的地址：直接 &limits 在循环变量共享的写法下会让每一格都指向最后一行。
		stored := limits
		data.Limits = &stored
		side[runID] = data
	}
	return rows.Err()
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
