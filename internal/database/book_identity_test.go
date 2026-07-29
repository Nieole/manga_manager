// 业务说明：本文件守卫「KOReader 身份索引回填不会顶起 books.updated_at」。
//
// file_hash / quick_hash / path_fingerprint* 是 KOReader 匹配用的内部索引，不构成
// 「这本书变了」。此前 UpdateBookIdentity 无条件 `updated_at = CURRENT_TIMESTAMP`，
// 于是一次「重建 KOReader 索引」会把整库的 books.updated_at 推到同一秒：
//   - 三条健康报告查询的 `ORDER BY b.updated_at DESC` 第一排序键全部相等，退化成按 id；
//   - 前端封面 URL 上的 ?v=updated_at 整库失效，等于让所有人重下一遍全部封面。

package database

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// seedIdentityTestBook 造一本书，并把 updated_at 压到一个远古值当哨兵。
func seedIdentityTestBook(t *testing.T, store Store, sentinel time.Time) Book {
	t.Helper()
	ctx := context.Background()

	lib, err := store.CreateLibrary(ctx, CreateLibraryParams{
		Name:         "L",
		Path:         t.TempDir(),
		ScanMode:     "manual",
		ScanInterval: 0,
		ScanFormats:  "cbz",
	})
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}
	series, err := store.CreateSeries(ctx, CreateSeriesParams{
		LibraryID:   lib.ID,
		Name:        "S",
		Path:        filepath.Join(lib.Path, "S"),
		NameInitial: SeriesInitial("", "S"),
	})
	if err != nil {
		t.Fatalf("CreateSeries: %v", err)
	}
	book, err := store.UpsertBookByPath(ctx, UpsertBookByPathParams{
		SeriesID:       series.ID,
		LibraryID:      lib.ID,
		Name:           "Volume01.cbz",
		Path:           filepath.Join(series.Path, "Volume01.cbz"),
		Size:           16,
		FileModifiedAt: time.Now(),
		PageCount:      10,
	})
	if err != nil {
		t.Fatalf("UpsertBookByPath: %v", err)
	}

	sqlStore, ok := store.(*SqlStore)
	if !ok {
		t.Fatalf("需要 *SqlStore 才能压 updated_at，得到 %T", store)
	}
	if _, err := sqlStore.DB().ExecContext(ctx,
		`UPDATE books SET updated_at = ? WHERE id = ?`, sentinel, book.ID); err != nil {
		t.Fatalf("压 updated_at: %v", err)
	}
	return book
}

func TestUpdateBookIdentityDoesNotBumpUpdatedAt(t *testing.T) {
	store := newStoreForTest(t)
	ctx := context.Background()
	sentinel := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

	t.Run("回填空列不刷新 updated_at", func(t *testing.T) {
		book := seedIdentityTestBook(t, store, sentinel)
		if err := store.UpdateBookIdentity(ctx, UpdateBookIdentityParams{
			ID:                   book.ID,
			FileHash:             "deadbeef",
			PathFingerprint:      "fp",
			PathFingerprintNoExt: "fpne",
		}); err != nil {
			t.Fatalf("UpdateBookIdentity: %v", err)
		}
		got, err := store.GetBook(ctx, book.ID)
		if err != nil {
			t.Fatalf("GetBook: %v", err)
		}
		if got.FileHash.String != "deadbeef" {
			t.Fatalf("身份列没写进去：file_hash=%q —— 守卫写反会让回填跑完但一本都没更新",
				got.FileHash.String)
		}
		if !got.UpdatedAt.Before(time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)) {
			t.Errorf("updated_at 被刷成 %v（哨兵 %v）—— 一次重建索引会把整库推到同一秒，"+
				"健康报告排序退化成按 id，前端封面 URL 也整库失效",
				got.UpdatedAt, sentinel)
		}
	})

	t.Run("覆盖已有非空列同样不刷新", func(t *testing.T) {
		// 这一支守的是 `COALESCE(col,'') <> ?` 那半边守卫——扫描器每次调用的正是这个形态。
		book := seedIdentityTestBook(t, store, sentinel)
		if err := store.UpdateBookIdentity(ctx, UpdateBookIdentityParams{
			ID: book.ID, FileHash: "deadbeef",
		}); err != nil {
			t.Fatalf("首次回填: %v", err)
		}
		if err := store.UpdateBookIdentity(ctx, UpdateBookIdentityParams{
			ID: book.ID, FileHash: "cafebabe",
		}); err != nil {
			t.Fatalf("二次回填: %v", err)
		}
		got, err := store.GetBook(ctx, book.ID)
		if err != nil {
			t.Fatalf("GetBook: %v", err)
		}
		if got.FileHash.String != "cafebabe" {
			t.Fatalf("非空列没被更新：file_hash=%q —— 守卫写反了，回填会静默失效",
				got.FileHash.String)
		}
		if !got.UpdatedAt.Before(time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)) {
			t.Errorf("updated_at 被刷成 %v（哨兵 %v）", got.UpdatedAt, sentinel)
		}
	})

	t.Run("全空参数是彻底的空操作", func(t *testing.T) {
		book := seedIdentityTestBook(t, store, sentinel)
		if err := store.UpdateBookIdentity(ctx, UpdateBookIdentityParams{ID: book.ID}); err != nil {
			t.Fatalf("UpdateBookIdentity: %v", err)
		}
		got, err := store.GetBook(ctx, book.ID)
		if err != nil {
			t.Fatalf("GetBook: %v", err)
		}
		if !got.UpdatedAt.Before(time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)) {
			t.Errorf("空参数调用也刷新了 updated_at（%v）—— 每次扫描都会给全库无谓写放大",
				got.UpdatedAt)
		}
	})
}
