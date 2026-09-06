// 适配器本体与身份那一半，外加时间列的编解码。运行那一半在 runs.go，侧表在 side_tables.go。

package taskstore

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"manga-manager/internal/task"
)

// Store 是 task.Store 的 SQLite 实现。它不持有任何领域状态：准入、序号与「第几次」的判据
// 全在库里，因此多个实例指向同一个库也不会各自算出一套答案。
type Store struct {
	db *sql.DB
}

// 编译期钉住：端口的形状由领域声明，本包只负责实现它。
var _ task.Store = (*Store)(nil)

// New 用一个已打开的库句柄构造适配器。表由 Migrate 建起，本函数不建表。
func New(db *sql.DB) *Store {
	return &Store{db: db}
}

// EnsureTask 按身份四要素取回任务，不存在就建一条。
//
// 建与取合成一条路径而不是「先查再插」：两个发起方同时首发同一个身份时，先查再插会双双查空、
// 双双插入，而唯一约束只会让其中一个活下来——那一个的调用方拿到的是一个错误而不是任务。
func (s *Store) EnsureTask(ctx context.Context, id task.Identity) (task.Task, error) {
	if err := id.Validate(); err != nil {
		return task.Task{}, err
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO `+tableTasks+` (type, scope, scope_id, variant)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(type, scope, scope_id, variant) DO NOTHING
	`, string(id.Type), string(id.Scope), id.ScopeID, string(id.Variant)); err != nil {
		return task.Task{}, err
	}

	var (
		owner         task.Task
		disabled      int
		lastSuccessAt sql.NullInt64
		backoffUntil  sql.NullInt64
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT id, disabled, last_success_at, fail_streak, backoff_until
		FROM `+tableTasks+`
		WHERE type = ? AND scope = ? AND scope_id = ? AND variant = ?
	`, string(id.Type), string(id.Scope), id.ScopeID, string(id.Variant)).
		Scan(&owner.ID, &disabled, &lastSuccessAt, &owner.FailStreak, &backoffUntil)
	if err != nil {
		return task.Task{}, err
	}
	owner.Identity = id
	owner.Disabled = disabled != 0
	owner.LastSuccessAt = timePtrFromMillis(lastSuccessAt)
	owner.BackoffUntil = timePtrFromMillis(backoffUntil)
	return owner, nil
}

// LoadTasks 按 id 批量取回任务，查不到的 id 不出现在结果里。
//
// 一句 IN 而不是逐条查：任务中心一页有几十条运行，而它们绝大多数指向同一批任务。
func (s *Store) LoadTasks(ctx context.Context, taskIDs []int64) (map[int64]task.Task, error) {
	owners := make(map[int64]task.Task, len(taskIDs))
	if len(taskIDs) == 0 {
		return owners, nil
	}
	placeholders, args := int64Placeholders(taskIDs)

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, type, scope, scope_id, variant, disabled, last_success_at, fail_streak, backoff_until
		FROM `+tableTasks+` WHERE id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			owner         task.Task
			taskType      string
			scope         string
			variant       string
			disabled      int
			lastSuccessAt sql.NullInt64
			backoffUntil  sql.NullInt64
		)
		if err := rows.Scan(&owner.ID, &taskType, &scope, &owner.ScopeID, &variant, &disabled,
			&lastSuccessAt, &owner.FailStreak, &backoffUntil); err != nil {
			return nil, err
		}
		owner.Type = task.Type(taskType)
		owner.Scope = task.Scope(scope)
		owner.Variant = task.Variant(variant)
		owner.Disabled = disabled != 0
		owner.LastSuccessAt = timePtrFromMillis(lastSuccessAt)
		owner.BackoffUntil = timePtrFromMillis(backoffUntil)
		owners[owner.ID] = owner
	}
	return owners, rows.Err()
}

// int64Placeholders 把一批 id 摊成 `?, ?, …` 与同序的实参。
func int64Placeholders(ids []int64) (string, []any) {
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	return strings.TrimSuffix(strings.Repeat("?, ", len(ids)), ", "), args
}

// ErrTaskNotFound 是身份行不存在时的哨兵错误。
var ErrTaskNotFound = errors.New("task not found")

// SaveTaskAttributes 写回身份的长期属性；身份不存在返回 ErrTaskNotFound。
func (s *Store) SaveTaskAttributes(ctx context.Context, taskID int64, attrs task.TaskAttributes) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE `+tableTasks+`
		SET disabled = ?, last_success_at = ?, fail_streak = ?, backoff_until = ?
		WHERE id = ?
	`, boolToInt(attrs.Disabled), millisFromTimePtr(attrs.LastSuccessAt), attrs.FailStreak,
		millisFromTimePtr(attrs.BackoffUntil), taskID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrTaskNotFound
	}
	return nil
}

// execTx 在一个事务里跑一批写入。侧表的按键合并要么整批落地要么一条不落，
// 半批落地会让重启入参缺几个键，而**重启函数**读到的是一份残缺声明。
func (s *Store) execTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

// 时间列存 epoch 毫秒：零值时间与 nil 指针一律落成 NULL，读回时还原成零值与 nil。
// 读回的时刻一律是 UTC——时区不落盘，比较与聚合因此只看那个整数。

func millisFromTime(at time.Time) sql.NullInt64 {
	if at.IsZero() {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: at.UnixMilli(), Valid: true}
}

func millisFromTimePtr(at *time.Time) sql.NullInt64 {
	if at == nil {
		return sql.NullInt64{}
	}
	return millisFromTime(*at)
}

func timeFromMillis(value sql.NullInt64) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return time.UnixMilli(value.Int64).UTC()
}

func timePtrFromMillis(value sql.NullInt64) *time.Time {
	if !value.Valid {
		return nil
	}
	at := time.UnixMilli(value.Int64).UTC()
	return &at
}
