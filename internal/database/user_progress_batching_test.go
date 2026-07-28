// 业务说明：本文件守卫批量读状态写入的分批。
//
// DSN 里的 _txlock=immediate 让 BeginTx 当场握住 SQLite 写锁，而这条路径的书数由用户选区决定
// （批量标记「整个系列已读」会把系列展开成全部书）。整批装进一个事务意味着写锁被连续持有到最后一本，
// 期间扫描器落库与所有阅读进度保存一起排队。

package database

import (
	"context"
	"testing"
	"time"
)

// TestSetUserBooksReadStateBatching 用超过一批的书数跑真实读写，
// 断言结果正确且不再逐书查 series_id。
func TestSetUserBooksReadStateBatching(t *testing.T) {
	store := newExecTxTestStore(t)
	ctx := context.Background()

	lib, err := store.CreateLibrary(ctx, CreateLibraryParams{
		Name: "batch", Path: t.TempDir(), ScanMode: "none", ScanInterval: 60,
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
	user, err := store.CreateUser(ctx, CreateUserParams{
		Username: "reader", PasswordHash: "x", Role: "regular", DisplayName: "R",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// 跨过一批（500）才能真正走到分批路径。
	const bookCount = setUserBooksReadStateBatch + 137
	bookIDs := make([]int64, 0, bookCount)
	for i := 0; i < bookCount; i++ {
		name := "vol" + itoa(i)
		res, err := store.DB().ExecContext(ctx,
			`INSERT INTO books (series_id, library_id, name, path, size, file_modified_at, page_count)
			 VALUES (?, ?, ?, ?, 0, CURRENT_TIMESTAMP, 10)`,
			series.ID, lib.ID, name, "/lib/"+name+".cbz")
		if err != nil {
			t.Fatalf("insert book: %v", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			t.Fatalf("LastInsertId: %v", err)
		}
		bookIDs = append(bookIDs, id)
	}
	// 混入一个不存在的 id：旧实现靠逐条 ErrNoRows 跳过，新实现靠 IN 查询天然过滤。
	bookIDs = append(bookIDs, bookIDs[len(bookIDs)-1]+100000)

	now := time.Now()
	if err := store.SetUserBooksReadState(ctx, user.ID, bookIDs, true, now); err != nil {
		t.Fatalf("SetUserBooksReadState(read): %v", err)
	}

	progress, err := store.GetUserBookProgressMap(ctx, user.ID, bookIDs)
	if err != nil {
		t.Fatalf("GetUserBookProgressMap: %v", err)
	}
	if len(progress) != bookCount {
		t.Fatalf("已读记录 %d 条, want %d —— 分批后有书被漏掉", len(progress), bookCount)
	}
	for _, id := range bookIDs[:bookCount] {
		p, ok := progress[id]
		if !ok {
			t.Fatalf("book %d 没有进度记录", id)
		}
		if !p.LastReadPage.Valid || p.LastReadPage.Int64 != 10 {
			t.Fatalf("book %d 的 last_read_page = %+v, want 10（页数）", id, p.LastReadPage)
		}
	}

	// 反向：批量清除。
	if err := store.SetUserBooksReadState(ctx, user.ID, bookIDs, false, now); err != nil {
		t.Fatalf("SetUserBooksReadState(unread): %v", err)
	}
	progress, err = store.GetUserBookProgressMap(ctx, user.ID, bookIDs)
	if err != nil {
		t.Fatalf("GetUserBookProgressMap: %v", err)
	}
	if len(progress) != 0 {
		t.Fatalf("清除后仍有 %d 条进度记录", len(progress))
	}
}
