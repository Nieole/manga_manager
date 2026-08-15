// 本文件守卫存量库升级时 gram 索引的一次性回填。
//
// 新库靠触发器实时维护，但升级前就有数据的库需要补一次。回填挂在 user_version 门控上，
// 而版本号是在回填**成功之后**才写的——中途被杀不会留下「已完成」的假象。

package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestGramIndexBackfillOnUpgrade(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "upgrade.db")
	if err := Migrate(dbPath); err != nil {
		t.Fatalf("首次 Migrate: %v", err)
	}
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	sqlStore := store.(*SqlStore)
	ctx := context.Background()

	lib, err := store.CreateLibrary(ctx, CreateLibraryParams{
		Name: "old", Path: t.TempDir(), ScanMode: "none", ScanInterval: 60,
	})
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}
	if _, err := store.UpsertSeriesByPath(ctx, UpsertSeriesByPathParams{
		LibraryID: lib.ID, Name: "海贼王", Path: "/lib/op",
	}); err != nil {
		t.Fatalf("UpsertSeriesByPath: %v", err)
	}

	// 模拟「升级前的库」：清空 gram 索引并把 user_version 退回旧版本。
	if _, err := sqlStore.DB().ExecContext(ctx, `DELETE FROM series_gram_fts`); err != nil {
		t.Fatalf("clear gram: %v", err)
	}
	if _, err := sqlStore.DB().ExecContext(ctx, `PRAGMA user_version = 2`); err != nil {
		t.Fatalf("reset version: %v", err)
	}
	_ = store.Close()

	expr, _ := gramMatchExpr("海贼")
	// 回填前应当搜不到。
	db, err := sql.Open("sqlite", sqliteDSN(dbPath))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	var before int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM series_gram_fts WHERE series_gram_fts MATCH ?`, expr).Scan(&before); err != nil {
		t.Fatalf("count before: %v", err)
	}
	_ = db.Close()
	if before != 0 {
		t.Fatalf("用例前提不成立：清空后仍有 %d 条", before)
	}

	// 再次 Migrate：user_version < currentSchemaVersion，应触发全量回填。
	if err := Migrate(dbPath); err != nil {
		t.Fatalf("升级 Migrate: %v", err)
	}

	store2, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store2.Close()
	got := queryIDs(t, store2.(*SqlStore),
		`SELECT s.id FROM series s WHERE `+seriesGramFilter, expr)
	if len(got) != 1 {
		t.Fatalf("升级后搜「海贼」命中 %d 条, want 1 —— 存量库的 gram 索引没有被回填", len(got))
	}
}
