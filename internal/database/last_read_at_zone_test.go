// 守 last_read_at 的存量归一：Migrate 要把历史上以别的时区落库的行改回本地墙钟，
// 且不碰真正跑在 UTC 的服务器那些本就正确的行。

package database

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// pinLocalZone 把进程时区钉在给定偏移上，用例结束还原。
// 用 time.FixedZone 而非 TZ / time.LoadLocation：后者要系统 tzdata，Windows 上未必有。
func pinLocalZone(t *testing.T, name string, offsetSeconds int) {
	t.Helper()
	original := time.Local
	t.Cleanup(func() { time.Local = original })
	time.Local = time.FixedZone(name, offsetSeconds)
}

// seedZoneFixture 建一个库 + 一个系列 + 两本书，返回 (系列 id, 书 id 列表)。
func seedZoneFixture(t *testing.T, store *SqlStore) (int64, []int64) {
	t.Helper()
	ctx := context.Background()
	lib, err := store.CreateLibrary(ctx, CreateLibraryParams{
		Name: "lib", Path: t.TempDir(), ScanMode: "none", ScanInterval: 60,
	})
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}
	series, err := store.UpsertSeriesByPath(ctx, UpsertSeriesByPathParams{
		LibraryID: lib.ID, Name: "S", Path: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("UpsertSeriesByPath: %v", err)
	}
	var bookIDs []int64
	for _, name := range []string{"03.cbz", "05.cbz"} {
		res, err := store.DB().ExecContext(ctx,
			`INSERT INTO books (series_id, library_id, name, path, size, file_modified_at, page_count)
			 VALUES (?, ?, ?, ?, 0, CURRENT_TIMESTAMP, 10)`,
			series.ID, lib.ID, name, filepath.Join(t.TempDir(), name))
		if err != nil {
			t.Fatalf("insert book: %v", err)
		}
		id, _ := res.LastInsertId()
		bookIDs = append(bookIDs, id)
	}
	return series.ID, bookIDs
}

func readText(t *testing.T, store *SqlStore, query string, args ...interface{}) string {
	t.Helper()
	var raw string
	if err := store.DB().QueryRowContext(context.Background(), query, args...).Scan(&raw); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	return raw
}

// TestMigrateRewritesForeignZoneLastReadAt：存量里那条 UTC 文本要被改成同一瞬间的本地墙钟，
// 在线写的那条要一字不动，派生的「继续阅读」要跟着重算到真正最近读的那本。
func TestMigrateRewritesForeignZoneLastReadAt(t *testing.T) {
	pinLocalZone(t, "CST", 8*3600)
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	if err := Migrate(dbPath); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	opened, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	store := opened.(*SqlStore)
	seriesID, bookIDs := seedZoneFixture(t, store)
	onlineBook, offlineBook := bookIDs[0], bookIDs[1]

	user, err := store.CreateUser(ctx, CreateUserParams{
		Username: "reader", PasswordHash: "x", Role: "regular", DisplayName: "R",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// 在线那条：本地 2030-10-01 06:00，走正常写入路径。
	if err := store.SetUserBookProgress(ctx, user.ID, onlineBook, 10, time.Date(2030, 10, 1, 6, 0, 0, 0, time.Local)); err != nil {
		t.Fatalf("SetUserBookProgress: %v", err)
	}
	onlineRaw := readText(t, store,
		`SELECT CAST(last_read_at AS TEXT) FROM user_book_progress WHERE user_id = ? AND book_id = ?`, user.ID, onlineBook)

	// 离线补传那条：UTC 墙钟文本，实为本地 2030-10-01 07:00，比在线那条晚一小时。
	const brokenUTC = "2030-09-30 23:00:00 +0000 UTC"
	if _, err := store.DB().ExecContext(ctx,
		`INSERT INTO user_book_progress (user_id, book_id, last_read_page, last_read_at, updated_at)
		 VALUES (?, ?, 10, ?, CURRENT_TIMESTAMP)`, user.ID, offlineBook, brokenUTC); err != nil {
		t.Fatalf("insert broken user progress: %v", err)
	}
	// 单用户 / KOReader 未关联账户时同样的文本会落在 books 上。
	if _, err := store.DB().ExecContext(ctx,
		`UPDATE books SET last_read_page = 10, last_read_at = ? WHERE id = ?`, brokenUTC, offlineBook); err != nil {
		t.Fatalf("update broken book progress: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if err := Migrate(dbPath); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	reopened, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	store = reopened.(*SqlStore)
	t.Cleanup(func() { _ = store.Close() })

	want := time.Date(2030, 10, 1, 7, 0, 0, 0, time.Local).String()
	got := readText(t, store,
		`SELECT CAST(last_read_at AS TEXT) FROM user_book_progress WHERE user_id = ? AND book_id = ?`, user.ID, offlineBook)
	if got != want {
		t.Fatalf("user_book_progress.last_read_at = %q, want %q", got, want)
	}
	if got := readText(t, store, `SELECT CAST(last_read_at AS TEXT) FROM books WHERE id = ?`, offlineBook); got != want {
		t.Fatalf("books.last_read_at = %q, want %q", got, want)
	}
	if got := readText(t, store,
		`SELECT CAST(last_read_at AS TEXT) FROM user_book_progress WHERE user_id = ? AND book_id = ?`, user.ID, onlineBook); got != onlineRaw {
		t.Fatalf("在线写入那条被改动了：%q，原为 %q", got, onlineRaw)
	}

	// 派生聚合必须跟着重算，否则「继续阅读」仍停在更早的那一卷。
	var lastReadBookID int64
	if err := store.DB().QueryRowContext(ctx,
		`SELECT last_read_book_id FROM user_series_progress WHERE user_id = ? AND series_id = ?`,
		user.ID, seriesID).Scan(&lastReadBookID); err != nil {
		t.Fatalf("read user_series_progress: %v", err)
	}
	if lastReadBookID != offlineBook {
		t.Fatalf("user_series_progress.last_read_book_id = %d，应为 %d", lastReadBookID, offlineBook)
	}
	if got := readText(t, store, `SELECT CAST(last_read_at AS TEXT) FROM series_stats WHERE series_id = ?`, seriesID); got != want {
		t.Fatalf("series_stats.last_read_at = %q, want %q", got, want)
	}
}

// TestMigrateLeavesUTCServerRowsUntouched：真正跑在 UTC 的服务器，它的行本来就是对的，
// 归一不得把它们改坏——判据认下的行改写后与原文逐字相同。
func TestMigrateLeavesUTCServerRowsUntouched(t *testing.T) {
	pinLocalZone(t, "UTC", 0)
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "utc.db")
	if err := Migrate(dbPath); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	opened, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	store := opened.(*SqlStore)
	_, bookIDs := seedZoneFixture(t, store)

	const utcRaw = "2030-09-30 23:00:00 +0000 UTC"
	if _, err := store.DB().ExecContext(ctx,
		`UPDATE books SET last_read_page = 10, last_read_at = ? WHERE id = ?`, utcRaw, bookIDs[0]); err != nil {
		t.Fatalf("seed utc row: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if err := Migrate(dbPath); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	reopened, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	store = reopened.(*SqlStore)
	t.Cleanup(func() { _ = store.Close() })

	if got := readText(t, store, `SELECT CAST(last_read_at AS TEXT) FROM books WHERE id = ?`, bookIDs[0]); got != utcRaw {
		t.Fatalf("UTC 服务器上的行被改动了：%q，原为 %q", got, utcRaw)
	}
}
