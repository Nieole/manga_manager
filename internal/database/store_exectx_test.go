// 业务说明：本文件守卫「批量写事务在任何失败路径上一定被终结」。
//
// DSN 里的 _txlock=immediate 让 BeginTx 当场握住 SQLite 写锁。事务若既不提交也不回滚地悬着，
// 连接不还池、写锁不释放，此后整个进程的写入都要等满 busy_timeout 才报 database is locked，
// 只有重启能救。fn panic 是最容易造成这种悬挂的路径，而它在 HTTP 上还会被 Recoverer 吃掉，
// 表现为「服务还活着，但所有写入都失败了」。

package database

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func newExecTxTestStore(t *testing.T) *SqlStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "exectx.db")
	if err := Migrate(dbPath); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	sqlStore, ok := store.(*SqlStore)
	if !ok {
		t.Fatalf("NewStore 返回了 %T，本用例需要 *SqlStore 才能读连接池统计", store)
	}
	return sqlStore
}

// waitForIdleConns 等待连接池里没有在用连接。事务被正确终结后连接才会还池，
// 因此这是「事务是否悬挂」的可观测代理指标。
func waitForIdleConns(t *testing.T, store *SqlStore, timeout time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		inUse := store.db.Stats().InUse
		if inUse == 0 || time.Now().After(deadline) {
			return inUse
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestExecTxAlwaysFinishesTransaction(t *testing.T) {
	t.Run("fn panic 时事务必须被回滚且连接还池", func(t *testing.T) {
		store := newExecTxTestStore(t)

		func() {
			// panic 必须穿透 ExecTx：把它转成 error 会让程序 bug 伪装成一次普通的事务失败。
			defer func() {
				if rec := recover(); rec == nil {
					t.Error("ExecTx 吞掉了 fn 的 panic —— 它应当穿透，回滚交给 defer")
				}
			}()
			_ = store.ExecTx(context.Background(), func(q *Queries) error {
				panic("boom")
			})
		}()

		if inUse := waitForIdleConns(t, store, 2*time.Second); inUse != 0 {
			t.Fatalf("panic 后仍有 %d 条连接在用 —— 事务悬挂，SQLite 写锁不会释放", inUse)
		}

		// 悬挂的写事务会让后续写入卡到 busy_timeout；能正常写入才说明锁确实放了。
		if _, err := store.CreateLibrary(context.Background(), CreateLibraryParams{
			Name: "after-panic", Path: t.TempDir(), ScanMode: "none", ScanInterval: 60,
		}); err != nil {
			t.Fatalf("panic 之后写入失败，写锁未释放: %v", err)
		}
	})

	t.Run("fn 返回错误时错误链完整", func(t *testing.T) {
		store := newExecTxTestStore(t)
		sentinel := errors.New("business rule violated")

		err := store.ExecTx(context.Background(), func(q *Queries) error { return sentinel })
		if !errors.Is(err, sentinel) {
			t.Fatalf("errors.Is 认不出原始错误: %v —— 上层据此分流的判定会全部失效", err)
		}
		if inUse := waitForIdleConns(t, store, 2*time.Second); inUse != 0 {
			t.Fatalf("错误返回后仍有 %d 条连接在用", inUse)
		}
	})

	t.Run("ctx 取消不被误报成回滚失败", func(t *testing.T) {
		store := newExecTxTestStore(t)
		ctx, cancel := context.WithCancel(context.Background())

		err := store.ExecTx(ctx, func(q *Queries) error {
			cancel()
			// database/sql 的 awaitDone 会自行回滚，随后 ExecTx 的显式 Rollback 拿到 ErrTxDone。
			// 那是正常收尾，不该被拼进错误信息。
			time.Sleep(50 * time.Millisecond)
			return ctx.Err()
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("取消场景下 errors.Is(err, context.Canceled) 应成立，实际: %v", err)
		}
		if errors.Is(err, sql.ErrTxDone) {
			t.Fatalf("ErrTxDone 不该出现在返回的错误链里: %v", err)
		}
		if inUse := waitForIdleConns(t, store, 2*time.Second); inUse != 0 {
			t.Fatalf("取消后仍有 %d 条连接在用", inUse)
		}
	})

	t.Run("成功提交后 defer 里的 Rollback 无害", func(t *testing.T) {
		store := newExecTxTestStore(t)
		ctx := context.Background()

		if err := store.ExecTx(ctx, func(q *Queries) error {
			_, err := q.CreateLibrary(ctx, CreateLibraryParams{
				Name: "committed", Path: t.TempDir(), ScanMode: "none", ScanInterval: 60,
			})
			return err
		}); err != nil {
			t.Fatalf("ExecTx: %v", err)
		}

		libs, err := store.ListLibraries(ctx)
		if err != nil {
			t.Fatalf("ListLibraries: %v", err)
		}
		if len(libs) != 1 {
			t.Fatalf("提交的数据不见了：期望 1 个资料库，得到 %d 个（defer Rollback 不该撤销已提交的事务）", len(libs))
		}
	})
}
